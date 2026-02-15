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

	// === sync 热路径独立缓存行 ===
	// syncCnt 已移除 — Emit 热路径不再有计数开销
	// Stats() 通过 emitted/processed PerCPU 计数器延迟读取
	_pad1 [64]byte // 独立 cache line，避免与 subs 的 false sharing

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

	// === 异步任务池 ===
	taskPool stdsync.Pool

	// === 运行时统计（per-CPU 无竞争计数） ===
	emitted   *util.PerCPUCounter
	processed *util.PerCPUCounter
	panics    *util.PerCPUCounter
}

// asyncTask 异步任务结构体（复用池化，消除闭包分配）
// 实现 wpool.Task 接口，通过 interface 分发避免 method value 分配
type asyncTask struct {
	bus     *Bus
	handler core.Handler
	evt     *core.Event
}

// Run 执行异步任务（实现 wpool.Task 接口）
func (t *asyncTask) Run() {
	if t.bus.closed.Load() {
		t.bus.taskPool.Put(t)
		return
	}
	defer func() {
		if r := recover(); r != nil {
			t.bus.panics.Add(1)
		}
		// 清理引用防止 GC 保留
		t.handler = nil
		t.evt = nil
		t.bus.taskPool.Put(t)
	}()
	if err := t.handler(t.evt); err != nil {
		select {
		case t.bus.errChan <- err:
		case <-t.bus.errDone:
			return
		default:
			t.bus.errMu.Lock()
			t.bus.lastErr = err
			t.bus.errMu.Unlock()
		}
	}
	t.bus.processed.Add(1)
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
	emitter.taskPool = stdsync.Pool{
		New: func() interface{} { return &asyncTask{} },
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
			// 安全: Remove 次数等于该 pattern 的 Add 次数（refCount 匹配）
			for range subs {
				e.matcher.Remove(k)
			}
		}
	}
	e.subs.Store(buildSnapshot(newByID))
}

// UnsafeEmit 发布事件 — 零保护极致性能路径
// 不捕获 handler panic，panic 直接传播到调用方。
// 不更新 Stats().Emitted 计数 — 追求最低开销。
// 适用于 handler 已知不会 panic 的高性能场景。
//
//go:nosplit
func (e *Bus) UnsafeEmit(evt *core.Event) error {
	snap := e.subs.Load()
	// 快速路径: 单事件类型跳过 map hash+lookup
	if snap.singleKey == evt.Type {
		for _, h := range snap.singleHandlers {
			if err := h(evt); err != nil {
				return err
			}
		}
		return nil
	}
	for _, h := range snap.handlers[evt.Type] {
		if err := h(evt); err != nil {
			return err
		}
	}
	return nil
}

// UnsafeEmitMatch 通配符匹配发布 — 零保护极致性能路径
//
//go:nosplit
func (e *Bus) UnsafeEmitMatch(evt *core.Event) error {
	snap := e.subs.Load()
	patterns := e.matcher.Match(evt.Type)
	for _, pattern := range *patterns {
		for _, h := range snap.handlers[pattern] {
			if err := h(evt); err != nil {
				e.matcher.Put(patterns)
				return err
			}
		}
	}
	e.matcher.Put(patterns)
	return nil
}

// Emit 发布事件 — 带 panic 保护的安全路径
// 同步: defer recover 捕获 handler panic
// 异步: 闭包/defer 隔离在 emitAsync 中
func (e *Bus) Emit(evt *core.Event) error {
	if evt == nil {
		return nil
	}
	if e.async {
		return e.emitAsync(evt)
	}
	return e.emitSyncSafe(evt)
}

// emitSyncSafe 同步安全路径 — defer recover 隔离在独立函数中
// 将 defer 限制在最小作用域，减少非 panic 路径的固定开销
//
//go:noinline
func (e *Bus) emitSyncSafe(evt *core.Event) (retErr error) {
	e.emitted.Add(1)
	defer func() {
		if r := recover(); r != nil {
			e.panics.Add(1)
			retErr = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return e.UnsafeEmit(evt)
}

// emitAsync 异步 Emit — 池化任务结构体，消除闭包分配
func (e *Bus) emitAsync(evt *core.Event) error {
	e.emitted.Add(1)
	snap := e.subs.Load()
	for _, h := range snap.handlers[evt.Type] {
		t := e.taskPool.Get().(*asyncTask)
		t.bus = e
		t.handler = h
		t.evt = evt
		e.gPool.SubmitTask(t)
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
	return e.emitMatchSyncSafe(evt)
}

// emitMatchSyncSafe 同步通配符安全路径
//
//go:noinline
func (e *Bus) emitMatchSyncSafe(evt *core.Event) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			e.panics.Add(1)
			retErr = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return e.UnsafeEmitMatch(evt)
}

// emitMatchAsync 异步通配符匹配 — 池化任务结构体，消除闭包分配
func (e *Bus) emitMatchAsync(evt *core.Event) error {
	snap := e.subs.Load()
	patterns := e.matcher.Match(evt.Type)
	defer e.matcher.Put(patterns)

	for _, pattern := range *patterns {
		for _, h := range snap.handlers[pattern] {
			t := e.taskPool.Get().(*asyncTask)
			t.bus = e
			t.handler = h
			t.evt = evt
			e.gPool.SubmitTask(t)
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
func (e *Bus) Stats() core.Stats {
	return core.Stats{
		Emitted:   e.emitted.Read(),
		Processed: e.processed.Read(),
		Panics:    e.panics.Read(),
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

// Drain 优雅关闭（等待异步任务完成或超时）
func (e *Bus) Drain(timeout time.Duration) error {
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
