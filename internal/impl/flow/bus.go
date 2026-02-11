// Package flow 提供基于 Pipeline 的高性能批处理流处理器
package flow

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/util"
)

// Stage 处理阶段定义，支持批量处理和错误返回
type Stage func([]*core.Event) error

// subscription 订阅信息（支持CoW模式）
type subscription struct {
	id      uint64
	pattern string
	handler core.Handler
}

// flowSnapshot CoW 快照 — 预构建 handler map，消除消费者侧双循环
type flowSnapshot struct {
	subs        []*subscription
	handlers    map[string][]core.Handler // key=pattern, 扁平化 handler
	hasWildcard bool                      // 是否包含通配符模式
}

// slot Disruptor风格槽位（与queue包一致的设计）
type flowSlot struct {
	seq  atomic.Int64
	data unsafe.Pointer // *core.Event
}

// RB MPSC无锁环形缓冲区（Disruptor风格序列号屏障）
type RB struct {
	// 生产者
	tail atomic.Uint64
	_    [56]byte // 缓存行填充

	// 消费者
	head atomic.Uint64
	_2   [56]byte // 缓存行填充

	buf  []flowSlot
	cap  uint64
	mask uint64
}

// newRB 创建环形缓冲区
func newRB(capacity int) *RB {
	sz := 1
	for sz < capacity {
		sz *= 2
	}
	rb := &RB{
		buf:  make([]flowSlot, sz),
		cap:  uint64(sz),
		mask: uint64(sz - 1),
	}
	for i := 0; i < sz; i++ {
		rb.buf[i].seq.Store(int64(i))
	}
	return rb
}

// push MPSC安全推入（Disruptor风格）
func (r *RB) push(evt *core.Event) bool {
	for {
		tail := r.tail.Load()
		s := &r.buf[tail&r.mask]
		seq := s.seq.Load()
		diff := seq - int64(tail)
		if diff == 0 {
			if r.tail.CompareAndSwap(tail, tail+1) {
				atomic.StorePointer(&s.data, unsafe.Pointer(evt))
				s.seq.Store(int64(tail + 1))
				return true
			}
		} else if diff < 0 {
			return false // 满
		}
	}
}

// popBatch 批量弹出（单消费者，Disruptor风格序列号验证）
func (r *RB) popBatch(batch []*core.Event) int {
	head := r.head.Load()
	maxCount := len(batch)
	count := 0

	for i := 0; i < maxCount; i++ {
		s := &r.buf[(head+uint64(i))&r.mask]
		seq := s.seq.Load()
		if seq != int64(head+uint64(i)+1) {
			break // 槽位未提交
		}
		batch[count] = (*core.Event)(atomic.LoadPointer(&s.data))
		atomic.StorePointer(&s.data, nil)
		s.seq.Store(int64(head+uint64(i)) + int64(r.cap))
		count++
	}

	if count > 0 {
		r.head.Store(head + uint64(count))
	}
	return count
}

// Bus 高性能批处理事件总线（字段按访问频率排列）
type Bus struct {
	// === Reader 热路径 ===
	matcher   *core.TrieMatcher // 具体类型，消除接口分发
	buffers   []*RB
	shardMask uint64
	numShards int

	// === Reader 快照 ===
	subsPtr atomic.Pointer[flowSnapshot]
	closed  atomic.Bool

	// Pipeline阶段
	stages []Stage

	// 批处理配置（只读）
	batchSz      int
	batchTimeout time.Duration

	// ID生成
	nextID atomic.Uint64

	// 批次缓冲池
	batchPool sync.Pool

	// 生命周期
	wg   sync.WaitGroup
	done chan struct{}

	// 统计信息
	emitted   atomic.Uint64 // 直接 atomic，消除 PerCPU 间接
	processed atomic.Uint64
	batches   atomic.Uint64
	panics    *util.PerCPUCounter
}

// New 创建批处理处理器
func New(stages []Stage, batchSz int, timeout time.Duration) *Bus {
	if batchSz <= 0 {
		batchSz = 100
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}

	// 使用分片减少竞争
	numShards := runtime.NumCPU()
	if numShards < 4 {
		numShards = 4
	}
	// 向上取2的幂
	shards := 1
	for shards < numShards {
		shards *= 2
	}

	p := &Bus{
		stages:       stages,
		numShards:    shards,
		shardMask:    uint64(shards - 1),
		buffers:      make([]*RB, shards),
		batchSz:      batchSz,
		batchTimeout: timeout,
		done:         make(chan struct{}),
		panics:       util.NewPerCPUCounter(),
	}

	// 初始化RingBuffer（每个分片独立）
	bufferSize := batchSz * 4 // 足够的缓冲
	for i := 0; i < shards; i++ {
		p.buffers[i] = newRB(bufferSize)
	}

	// 初始化批次池
	p.batchPool = sync.Pool{
		New: func() interface{} {
			return make([]*core.Event, 0, batchSz)
		},
	}

	// 初始化空订阅快照和匹配器
	p.subsPtr.Store(&flowSnapshot{
		subs:     make([]*subscription, 0),
		handlers: make(map[string][]core.Handler),
	})
	p.matcher = core.NewTrieMatcher()

	// 为每个分片启动消费者
	for i := 0; i < shards; i++ {
		p.wg.Add(1)
		go p.consumer(i)
	}

	return p
}

// getShard 获取分片索引（字节级哈希，避免rune解码开销）
func (p *Bus) getShard(eventType string) uint64 {
	h := uint64(0)
	for i := 0; i < len(eventType); i++ {
		h = h*31 + uint64(eventType[i])
	}
	return h & p.shardMask
}

// consumer 消费者协程（LockOSThread + 批量处理）
// LockOSThread: 绑定OS线程，保持L1/L2 cache热度
func (p *Bus) consumer(shardIdx int) {
	defer p.wg.Done()

	// 绑定OS线程 — 消除Go调度器在M间迁移G的开销
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	rb := p.buffers[shardIdx]
	batch := make([]*core.Event, p.batchSz)
	ticker := time.NewTicker(p.batchTimeout)
	defer ticker.Stop()

	lastProcessed := time.Now()
	idle := 0

	for {
		select {
		case <-p.done:
			// 关闭时处理剩余事件
			for {
				count := rb.popBatch(batch)
				if count > 0 {
					p.safeProcessBatch(batch[:count])
				} else {
					break
				}
			}
			return

		case <-ticker.C:
			// 超时触发：处理已有事件
			count := rb.popBatch(batch)
			if count > 0 {
				p.safeProcessBatch(batch[:count])
				lastProcessed = time.Now()
				idle = 0
			}

		default:
			// 批量弹出事件
			count := rb.popBatch(batch)
			if count > 0 {
				p.safeProcessBatch(batch[:count])
				lastProcessed = time.Now()
				idle = 0
			} else {
				idle++
				// 空闲时检查超时
				if time.Since(lastProcessed) > p.batchTimeout {
					ticker.Reset(p.batchTimeout)
				}
				// 三级背压: spin → Gosched → Sleep
				if idle <= 32 {
					runtime.Gosched()
				} else if idle <= 256 {
					time.Sleep(time.Microsecond)
				} else {
					time.Sleep(10 * time.Microsecond)
				}
			}
		}
	}
}

// safeProcessBatch 安全处理批次：捕获 panic，防止 consumer 崩溃
func (p *Bus) safeProcessBatch(events []*core.Event) {
	defer func() {
		if r := recover(); r != nil {
			p.panics.Add(1)
		}
	}()
	p.processBatch(events)
}

// processBatch 处理一个批次的事件
// 精确匹配: 直接索引 handlers[evt.Type]，零分配
// 通配符: fallback 到 TrieMatcher.Match，仅在有通配符订阅时触发
func (p *Bus) processBatch(events []*core.Event) {
	if len(events) == 0 {
		return
	}

	// 执行Pipeline阶段
	current := events
	for _, stage := range p.stages {
		if len(current) == 0 {
			break
		}
		if err := stage(current); err != nil {
			break
		}
	}

	// 调用订阅handler
	snap := p.subsPtr.Load()
	if len(snap.handlers) > 0 && len(current) > 0 {
		if !snap.hasWildcard {
			// 快速路径: 仅精确匹配，直接 map 索引，零分配
			for _, evt := range current {
				for _, h := range snap.handlers[evt.Type] {
					h(evt)
				}
			}
		} else {
			// 通配符路径: 需要 TrieMatcher 解析
			for _, evt := range current {
				patterns := p.matcher.Match(evt.Type)
				for _, pat := range *patterns {
					for _, h := range snap.handlers[pat] {
						h(evt)
					}
				}
				p.matcher.Put(patterns)
			}
		}
	}

	// 更新统计
	p.processed.Add(uint64(len(events)))
	p.batches.Add(1)
}

// buildFlowSnapshot 从订阅列表构建快照（On/Off 时调用，非热路径）
func buildFlowSnapshot(subs []*subscription) *flowSnapshot {
	handlers := make(map[string][]core.Handler)
	hasWild := false
	for _, s := range subs {
		handlers[s.pattern] = append(handlers[s.pattern], s.handler)
		if !hasWild && containsWildcard(s.pattern) {
			hasWild = true
		}
	}
	return &flowSnapshot{subs: subs, handlers: handlers, hasWildcard: hasWild}
}

// containsWildcard 检查 pattern 是否包含通配符
func containsWildcard(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' {
			return true
		}
	}
	return false
}

// On 订阅事件
func (p *Bus) On(pattern string, handler core.Handler) uint64 {
	if handler == nil {
		return 0
	}

	id := p.nextID.Add(1)
	sub := &subscription{
		id:      id,
		pattern: pattern,
		handler: handler,
	}

	p.matcher.Add(pattern)

	for {
		old := p.subsPtr.Load()
		newSubs := make([]*subscription, len(old.subs)+1)
		copy(newSubs, old.subs)
		newSubs[len(old.subs)] = sub

		if p.subsPtr.CompareAndSwap(old, buildFlowSnapshot(newSubs)) {
			break
		}
	}

	return id
}

// Off 取消订阅
func (p *Bus) Off(id uint64) {
	if id == 0 {
		return
	}

	for {
		old := p.subsPtr.Load()
		found := false
		for i, sub := range old.subs {
			if sub.id == id {
				newSubs := make([]*subscription, 0, len(old.subs)-1)
				newSubs = append(newSubs, old.subs[:i]...)
				newSubs = append(newSubs, old.subs[i+1:]...)

				p.matcher.Remove(sub.pattern)

				if p.subsPtr.CompareAndSwap(old, buildFlowSnapshot(newSubs)) {
					return
				}
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
}

// Emit 发射单个事件（无锁，直接Push到RingBuffer）
func (p *Bus) Emit(evt *core.Event) error {
	if evt == nil || p.closed.Load() {
		return nil
	}

	p.emitted.Add(1)

	shard := p.getShard(evt.Type)
	if !p.buffers[shard].push(evt) {
		// Buffer满时，尝试其他分片（负载均衡）
		for i := 0; i < p.numShards; i++ {
			if p.buffers[i].push(evt) {
				return nil
			}
		}
		// 所有buffer都满，同步降级处理避免丢数据
		p.processBatch([]*core.Event{evt})
	}
	return nil
}

// EmitMatch 发射单个事件（匹配模式）
func (p *Bus) EmitMatch(evt *core.Event) error {
	return p.Emit(evt)
}

// EmitBatch 批量发射事件
func (p *Bus) EmitBatch(events []*core.Event) error {
	if len(events) == 0 || p.closed.Load() {
		return nil
	}

	for _, evt := range events {
		if evt == nil {
			continue
		}
		p.emitted.Add(1)
		shard := p.getShard(evt.Type)
		p.buffers[shard].push(evt)
	}
	return nil
}

// EmitMatchBatch 批量发射匹配事件
func (p *Bus) EmitMatchBatch(events []*core.Event) error {
	return p.EmitBatch(events)
}

// Flush 立即刷新当前批次（触发消费者处理）
func (p *Bus) Flush() error {
	if p.closed.Load() {
		return nil
	}
	// 等待一小段时间让消费者处理
	time.Sleep(p.batchTimeout)
	return nil
}

// Close 关闭处理器
func (p *Bus) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}

	close(p.done)
	p.wg.Wait()
}

// Drain 优雅关闭（等待队列排空或超时）
func (p *Bus) Drain(timeout time.Duration) error {
	if timeout <= 0 {
		p.Close()
		return nil
	}
	done := make(chan struct{})
	go func() {
		p.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("flow: graceful close timed out after %v", timeout)
	}
}

// Stats 获取运行时统计（实现 core.Bus 接口）
func (p *Bus) Stats() core.Stats {
	var depth int64
	for _, rb := range p.buffers {
		d := rb.tail.Load() - rb.head.Load()
		depth += int64(d)
	}
	return core.Stats{
		Emitted:   int64(p.emitted.Load()),
		Processed: int64(p.processed.Load()),
		Panics:    p.panics.Read(),
		Depth:     depth,
	}
}

// BatchStats 获取批处理统计（processed, batches）
func (p *Bus) BatchStats() (processed, batches uint64) {
	return p.processed.Load(), p.batches.Load()
}
