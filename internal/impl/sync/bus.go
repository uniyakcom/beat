// Package sync 提供同步/异步事件总线实现
package sync

import (
	"fmt"
	"runtime"
	stdsync "sync"
	"sync/atomic"
	"time"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/util"

	"github.com/uniyakcom/beat/internal/support/wpool"
)

// optPoolSz 根据OS和架构获取最优池大小
func optPoolSz() int {
	base := runtime.NumCPU()
	switch runtime.GOOS {
	case "linux":
		return base * 15
	case "darwin":
		return base * 12
	case "windows":
		return base * 10
	default:
		return base * 5
	}
}

// subsSnapshot CoW 快照 — 双层结构
//   - byID: On/Off 管理路径（含 sub.ID 用于删除）
//   - handlers: Emit 热路径（预扁平化 []core.Handler，消除 *sub 间接访问）
//   - singleKey/singleHandlers: 单事件类型快速路径（跳过 map hash+lookup）
type subsSnapshot struct {
	byID           map[string][]*sub
	handlers       map[string][]core.Handler
	singleKey      string
	singleHandlers []core.Handler
}

// buildSnapshot 从 byID 构建完整快照（On/Off 时调用，非热路径）
func buildSnapshot(byID map[string][]*sub) *subsSnapshot {
	snap := &subsSnapshot{
		byID:     byID,
		handlers: make(map[string][]core.Handler, len(byID)),
	}
	for k, subs := range byID {
		hs := make([]core.Handler, len(subs))
		for i, s := range subs {
			hs[i] = s.handler
		}
		snap.handlers[k] = hs
	}
	if len(byID) == 1 {
		for k, hs := range snap.handlers {
			snap.singleKey = k
			snap.singleHandlers = hs
		}
	}
	return snap
}

// Bus 同步事件总线（字段按访问频率+大小对齐排列）
// Reader 热路径字段在前（Emit读取），Writer 冷路径字段在后（On/Off写入）
type Bus struct {
	// === Reader 热路径（Emit/EmitMatch 每次调用都读） ===
	matcher *core.TrieMatcher            // 8B — 具体类型，消除接口分发
	subs    atomic.Pointer[subsSnapshot] // 8B — CoW快照（含扁平化 handlers + singleKey）
	closed  atomic.Bool                  // 1B
	async   bool                         // 1B
	_       [6]byte                      // padding到cache line

	// === sync 热路径独立缓存行 — processed 计数器 ===
	syncCnt atomic.Int64 // 同步模式 processed 计数（直接 atomic，无 PerCPU 间接）
	_pad1   [56]byte     // 独立 cache line，避免与 subs 的 false sharing

	// === Reader 异步路径 ===
	gPool *wpool.Pool // 8B

	// === Writer 冷路径（On/Off） ===
	mu stdsync.Mutex // 8B

	// === 对象池 ===
	pool stdsync.Pool

	// === 错误处理（仅异步模式使用） ===
	errChan chan error
	errDone chan struct{}
	errMu   stdsync.Mutex
	lastErr error

	// === 运行时统计（per-CPU 无竞争计数） ===
	emitted   *util.PerCPUCounter
	processed *util.PerCPUCounter
	panics    *util.PerCPUCounter
}

// sub 订阅者
type sub struct {
	pattern string
	handler core.Handler
	id      uint64
}

var subID atomic.Uint64

// NewBus 创建同步事件总线（同步模式，不分配异步资源）
func NewBus() *Bus {
	emitter := &Bus{
		matcher:   core.NewTrieMatcher(),
		async:     false,
		emitted:   util.NewPerCPUCounter(),
		processed: util.NewPerCPUCounter(),
		panics:    util.NewPerCPUCounter(),
	}
	emitter.subs.Store(buildSnapshot(make(map[string][]*sub)))
	emitter.pool = stdsync.Pool{
		New: func() interface{} {
			return &core.Event{}
		},
	}
	return emitter
}

// Prewarm 预热
func (e *Bus) Prewarm(eventTypes []string) {
	const cnt = 1024
	events := make([]*core.Event, cnt)
	for i := 0; i < cnt; i++ {
		events[i] = e.pool.Get().(*core.Event)
	}
	for _, evt := range events {
		e.pool.Put(evt)
	}

	for _, et := range eventTypes {
		e.matcher.HasMatch(et)
	}
}

// NewAsync 创建异步模式事件总线
func NewAsync(poolSize int) (*Bus, error) {
	if poolSize <= 0 {
		poolSize = optPoolSz()
	}
	emitter := &Bus{
		matcher:   core.NewTrieMatcher(),
		async:     true,
		gPool:     wpool.New(poolSize, 0),
		errChan:   make(chan error, 1024),
		errDone:   make(chan struct{}),
		emitted:   util.NewPerCPUCounter(),
		processed: util.NewPerCPUCounter(),
		panics:    util.NewPerCPUCounter(),
	}
	emitter.subs.Store(buildSnapshot(make(map[string][]*sub)))
	emitter.pool = stdsync.Pool{
		New: func() interface{} { return &core.Event{} },
	}

	// 启动错误处理goroutine
	go emitter.errorHandler()

	return emitter, nil
}

// On 订阅事件 - 使用CoW（Copy-on-Write）机制
func (e *Bus) On(pattern string, handler core.Handler) uint64 {
	id := subID.Add(1)
	s := &sub{
		id:      id,
		pattern: pattern,
		handler: handler,
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	old := e.subs.Load()
	newByID := make(map[string][]*sub, len(old.byID)+1)
	for k, v := range old.byID {
		newByID[k] = v
	}
	newByID[pattern] = append(newByID[pattern], s)
	e.subs.Store(buildSnapshot(newByID))
	e.matcher.Add(pattern)

	return id
}

// Off 取消订阅 - 使用CoW（Copy-on-Write）机制
func (e *Bus) Off(id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	old := e.subs.Load()
	newByID := make(map[string][]*sub, len(old.byID))
	for k, subs := range old.byID {
		filtered := make([]*sub, 0, len(subs))
		for _, s := range subs {
			if s.id != id {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) > 0 {
			newByID[k] = filtered
		} else {
			e.matcher.Remove(k)
		}
	}
	e.subs.Store(buildSnapshot(newByID))
}

// Emit 发布事件 — 同步路径直接内嵌，异步路径分离
// 同步:  无闭包/defer → 更小栈帧 + syncCnt 直接 atomic（消除 PerCPU 间接）
// 异步:  闭包/defer 隔离在 emitAsync 中
func (e *Bus) Emit(evt *core.Event) error {
	if evt == nil {
		return nil
	}
	if e.async {
		return e.emitAsync(evt)
	}

	// === 同步处理 — 极致轻量热路径 ===
	snap := e.subs.Load()

	// 快速路径: 单事件类型跳过 map hash+lookup
	if snap.singleKey == evt.Type {
		for _, h := range snap.singleHandlers {
			if err := h(evt); err != nil {
				e.syncCnt.Add(1)
				return err
			}
		}
		e.syncCnt.Add(1)
		return nil
	}
	// 通用路径: map lookup（多事件类型）
	for _, h := range snap.handlers[evt.Type] {
		if err := h(evt); err != nil {
			e.syncCnt.Add(1)
			return err
		}
	}
	e.syncCnt.Add(1)
	return nil
}

// emitAsync 异步 Emit — 闭包 + defer 隔离在此
func (e *Bus) emitAsync(evt *core.Event) error {
	e.emitted.Add(1)
	snap := e.subs.Load()
	for _, h := range snap.handlers[evt.Type] {
		handler := h
		e.gPool.Submit(func() {
			if e.closed.Load() {
				return
			}
			defer func() {
				if r := recover(); r != nil {
					e.panics.Add(1)
				}
			}()
			if err := handler(evt); err != nil {
				select {
				case e.errChan <- err:
				case <-e.errDone:
					return
				default:
					e.errMu.Lock()
					e.lastErr = err
					e.errMu.Unlock()
				}
			}
			e.processed.Add(1)
		})
	}
	return nil
}

// EmitMatch 支持通配符匹配的发布 — 同步内嵌，异步分离
func (e *Bus) EmitMatch(evt *core.Event) error {
	if evt == nil {
		return nil
	}
	if e.async {
		return e.emitMatchAsync(evt)
	}

	// === 同步通配符匹配 ===
	snap := e.subs.Load()
	patterns := e.matcher.Match(evt.Type)
	defer e.matcher.Put(patterns)

	for _, pattern := range *patterns {
		for _, h := range snap.handlers[pattern] {
			if err := h(evt); err != nil {
				e.syncCnt.Add(1)
				return err
			}
		}
	}
	e.syncCnt.Add(1)
	return nil
}

// emitMatchAsync 异步通配符匹配 — 闭包隔离
func (e *Bus) emitMatchAsync(evt *core.Event) error {
	snap := e.subs.Load()
	patterns := e.matcher.Match(evt.Type)
	defer e.matcher.Put(patterns)

	for _, pattern := range *patterns {
		for _, h := range snap.handlers[pattern] {
			handler := h
			e.gPool.Submit(func() {
				if e.closed.Load() {
					return
				}
				if err := handler(evt); err != nil {
					select {
					case e.errChan <- err:
					case <-e.errDone:
						return
					default:
						e.errMu.Lock()
						e.lastErr = err
						e.errMu.Unlock()
					}
				}
				e.processed.Add(1)
			})
		}
	}
	return nil
}

// EmitBatch 批量发布事件
func (e *Bus) EmitBatch(events []*core.Event) error {
	for _, evt := range events {
		if err := e.Emit(evt); err != nil {
			return err
		}
	}
	return nil
}

// EmitMatchBatch 批量发布支持通配符匹配的事件
func (e *Bus) EmitMatchBatch(events []*core.Event) error {
	for _, evt := range events {
		if err := e.EmitMatch(evt); err != nil {
			return err
		}
	}
	return nil
}

// Preload 预加载event types（兼容API）
func (e *Bus) Preload(eventTypes []string) {
	// 为来来扩展
}

// Stats 返回运行时统计
// 同步模式: syncCnt 即 emitted ≡ processed
// 异步模式: emitted 独立计数，processed 通过 PerCPU 计数
func (e *Bus) Stats() core.BusStats {
	if e.async {
		return core.BusStats{
			EventsEmitted:   e.emitted.Read(),
			EventsProcessed: e.processed.Read(),
			Panics:          e.panics.Read(),
		}
	}
	cnt := e.syncCnt.Load()
	return core.BusStats{
		EventsEmitted:   cnt,
		EventsProcessed: cnt,
		Panics:          e.panics.Read(),
	}
}

// Close 关闭发射器
func (e *Bus) Close() {
	if !e.closed.CompareAndSwap(false, true) {
		return // 已关闭
	}

	// 先释放goroutine池（等待所有任务完成）
	if e.gPool != nil {
		e.gPool.Release()
	}

	// 关闭错误处理goroutine
	if e.errDone != nil {
		close(e.errDone)
	}

	// 不关闭errChan，避免与仍在执行的goroutine产生竞态
	// channel会GC自动回收
}

// GracefulClose 优雅关闭（等待异步任务完成或超时）
func (e *Bus) GracefulClose(timeout time.Duration) error {
	if timeout <= 0 {
		e.Close()
		return nil
	}
	// ants.Pool.Release() 本身会等待所有任务完成
	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("sync: graceful close timed out after %v", timeout)
	}
}

// LastError 获取最后一个异步错误
func (e *Bus) LastError() error {
	e.errMu.Lock()
	defer e.errMu.Unlock()
	return e.lastErr
}

// ClearError 清除错误状态
func (e *Bus) ClearError() {
	e.errMu.Lock()
	defer e.errMu.Unlock()
	e.lastErr = nil
}

// errorHandler 异步错误处理器
func (e *Bus) errorHandler() {
	for {
		select {
		case err, ok := <-e.errChan:
			if !ok {
				return // 通道已关闭
			}
			e.errMu.Lock()
			e.lastErr = err
			e.errMu.Unlock()
		case <-e.errDone:
			// 清空剩余错误
			for len(e.errChan) > 0 {
				if err, ok := <-e.errChan; ok {
					e.errMu.Lock()
					e.lastErr = err
					e.errMu.Unlock()
				}
			}
			return
		}
	}
}
