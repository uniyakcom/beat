// Package async 提供基于 Per-P SPSC Ring 的高性能事件总线
//
// 设计特点:
//   - 零 CAS 热路径: SPSC ring 仅用 atomic Load/Store（x86 = 普通 MOV）
//   - Per-P 分发: procPin 保证同一 P 上 goroutine 串行化 → 真 SPSC 单写者
//   - Worker 亲和性: worker[i] 静态拥有 rings {i, i+w, ...}，无 work-stealing
//   - RCU 订阅管理: atomic.Pointer 读无锁，写时 CoW
//   - 扁平化 handler: dispatch 直接遍历 []core.Handler（消除 *sub 间接访问）
//   - 单类型快速路径: 仅 1 种事件类型时跳过 map lookup（节省 ~16ns）
//   - 零分配: Emit 路径无堆分配
//
// 性能路径拆分:
//
//	Producer: Emit() → procPin → SPSC Enqueue (~2-3ns) → procUnpin → wake
//	Consumer: SPSC Dequeue → dispatchDirect → []Handler 迭代 → processed++
package async

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/util"
)

// sub 内部订阅条目
type sub struct {
	id      uint64
	pattern string
	handler core.Handler
}

// subsSnapshot RCU 快照 — 双层结构
//   - byID: On/Off 管理路径（含 sub.ID 用于删除）
//   - handlers: dispatch 热路径（预扁平化 []core.Handler，消除 *sub 间接访问）
//   - singleKey/singleHandlers: 单事件类型快速路径（跳过 map hash+lookup ≈ 16ns）
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
	// 单事件类型快速路径：跳过 map lookup
	if len(byID) == 1 {
		for k, hs := range snap.handlers {
			snap.singleKey = k
			snap.singleHandlers = hs
		}
	}
	return snap
}

var globalSubID atomic.Uint64

// ─── Bus ─────────────────────────────────────────────────────────────

// Bus Per-P SPSC 事件总线 — 适配 core.Bus 接口
type Bus struct {
	// SPSC 分片调度器
	sched *ShardedScheduler[*core.Event]

	// 通配符匹配器
	matcher *core.TrieMatcher

	// RCU 订阅快照（读无锁，写时 CoW + buildSnapshot）
	subs atomic.Pointer[subsSnapshot]
	mu   sync.Mutex

	// 生命周期
	closed atomic.Bool

	// 运行时统计（emitted 已移除：消除 consumer 热路径 7ns 开销）
	processed *util.PerCPUCounter
	panics    *util.PerCPUCounter
}

// Config SPSC 配置（简化：不再需要 NodeCount/NodeSize）
type Config struct {
	Workers  int    // worker 数量（0=NumCPU/2 = 物理核数）
	RingSize uint64 // 每个 SPSC ring 大小（0=8192，必须 2 的幂）
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	w := runtime.NumCPU() / 2
	if w < 1 {
		w = 1
	}
	return &Config{
		Workers:  w,
		RingSize: 1 << 13, // 8192
	}
}

// New 创建 Per-P SPSC Bus
func New(cfg *Config) *Bus {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.Workers <= 0 {
		w := runtime.NumCPU() / 2
		if w < 1 {
			w = 1
		}
		cfg.Workers = w
	}
	if cfg.RingSize == 0 {
		cfg.RingSize = 1 << 13
	}

	e := &Bus{
		sched:     NewShardedScheduler[*core.Event](cfg.RingSize, cfg.Workers),
		matcher:   core.NewTrieMatcher(),
		processed: util.NewPerCPUCounter(),
		panics:    util.NewPerCPUCounter(),
	}

	e.subs.Store(buildSnapshot(make(map[string][]*sub)))

	e.sched.OnPanic = func(r any) {
		e.panics.Add(1)
	}

	e.sched.Start(func(evt *core.Event) {
		e.dispatchDirect(evt)
		e.processed.Add(1)
	})

	return e
}

// ─── core.Bus 接口实现 ────────────────────────────────────────────

// On 订阅事件
func (e *Bus) On(pattern string, handler core.Handler) uint64 {
	id := globalSubID.Add(1)
	s := &sub{id: id, pattern: pattern, handler: handler}

	e.mu.Lock()
	old := e.subs.Load()
	newByID := make(map[string][]*sub, len(old.byID)+1)
	for k, v := range old.byID {
		newByID[k] = v
	}
	newByID[pattern] = append(newByID[pattern], s)
	e.subs.Store(buildSnapshot(newByID))
	e.matcher.Add(pattern)
	e.mu.Unlock()

	return id
}

// Off 取消订阅
func (e *Bus) Off(id uint64) {
	e.mu.Lock()
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
	e.mu.Unlock()
}

// Emit 发布事件 — 零分配入队
// 优化: emitted 计数移到 consumer 侧（与 processed 合并），消除 producer 热路径 7 ns 开销
func (e *Bus) Emit(evt *core.Event) error {
	if evt == nil || e.closed.Load() {
		return nil
	}
	e.sched.Submit(evt)
	return nil
}

// EmitMatch 发布事件（支持通配符匹配 — 同步处理）
func (e *Bus) EmitMatch(evt *core.Event) error {
	if evt == nil || e.closed.Load() {
		return nil
	}

	snap := e.subs.Load()
	patterns := e.matcher.Match(evt.Type)
	defer e.matcher.Put(patterns)

	for _, pattern := range *patterns {
		for _, h := range snap.handlers[pattern] {
			if err := h(evt); err != nil {
				return err
			}
		}
	}
	e.processed.Add(1)
	return nil
}

// EmitBatch 批量发布
func (e *Bus) EmitBatch(events []*core.Event) error {
	if len(events) == 0 || e.closed.Load() {
		return nil
	}
	for _, evt := range events {
		if evt == nil {
			continue
		}
		if err := e.Emit(evt); err != nil {
			return err
		}
	}
	return nil
}

// EmitMatchBatch 批量发布（带匹配）
func (e *Bus) EmitMatchBatch(events []*core.Event) error {
	for _, evt := range events {
		if err := e.EmitMatch(evt); err != nil {
			return err
		}
	}
	return nil
}

// Stats 返回运行时统计
// emitted 近似等于 processed（差值 = ring 中未消费事件数）
func (e *Bus) Stats() core.BusStats {
	processed := e.processed.Read()
	return core.BusStats{
		EventsEmitted:   processed,
		EventsProcessed: processed,
		Panics:          e.panics.Read(),
	}
}

// Close 关闭
func (e *Bus) Close() {
	if !e.closed.CompareAndSwap(false, true) {
		return
	}
	e.sched.Stop()
}

// GracefulClose 优雅关闭
func (e *Bus) GracefulClose(timeout time.Duration) error {
	if timeout <= 0 {
		e.Close()
		return nil
	}
	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("async: graceful close timed out after %v", timeout)
	}
}

// ─── 内部方法 ─────────────────────────────────────────────────────

// dispatchDirect 精确匹配分发（消费者热路径）
// 优化: RCU 快照 + 预扁平化 handler + 单类型快速路径
func (e *Bus) dispatchDirect(evt *core.Event) {
	snap := e.subs.Load()
	// 快速路径: 仅 1 种事件类型时跳过 map hash+lookup（≈16ns）
	if snap.singleKey == evt.Type {
		for _, h := range snap.singleHandlers {
			_ = h(evt)
		}
		return
	}
	// 通用路径: map lookup（多事件类型）
	for _, h := range snap.handlers[evt.Type] {
		_ = h(evt)
	}
}
