// Package sched 提供基于 Per-P SPSC Ring 的分片调度器。
//
// 架构核心：
//   - rings 数量 = GOMAXPROCS（每个 P 一个独立 SPSC ring）
//   - procPin 保证同一 P 上 goroutine 串行化 → 单写者（SPSC producer）
//   - worker[i] 固定消费分配给它的 rings → 单读者（SPSC consumer）
//   - 零 CAS 热路径：Enqueue/Dequeue 仅用 atomic Load/Store
//
// Worker 分配策略：
//   - worker[i] 拥有 rings {i, i+workers, i+2*workers, ...}
//   - workers = NumCPU/2（物理核数），rings = GOMAXPROCS（逻辑核数）
//   - 例: 6C/12T → 12 rings, 6 workers, 每 worker 2 rings
package sched

import (
	"runtime"
	"sync"
	"sync/atomic"
	_ "unsafe"

	sl "github.com/uniyakcom/beat/internal/support/spsc"
)

//go:linkname runtime_procPin runtime.procPin
func runtime_procPin() int

//go:linkname runtime_procUnpin runtime.procUnpin
func runtime_procUnpin()

//go:linkname runtime_procyield runtime.procyield
func runtime_procyield(cycles uint32)

// ShardedScheduler SPSC 分片调度器
// 每个 P 有独立的 SPSC ring（零 CAS），worker 按静态亲和性消费
type ShardedScheduler[T any] struct {
	rings    []*sl.SPSCRing[T]
	numRings int
	ringMask int // numRings-1（2 的幂），用于 pid&mask 替代 pid%numRings
	workers  int
	wg       sync.WaitGroup
	stop     atomic.Bool
	done     chan struct{}
	OnPanic  func(any)
	parked   atomic.Int32
	sem      chan struct{}
}

// NewShardedScheduler 创建 SPSC 分片调度器
// ringSize: 每个 ring 的容量（2 的幂，0=8192）
// workers: 消费者数量（0=NumCPU/2=物理核数）
func NewShardedScheduler[T any](ringSize uint64, workers int) *ShardedScheduler[T] {
	numRings := runtime.GOMAXPROCS(0)
	if workers <= 0 {
		workers = runtime.NumCPU() / 2
		if workers < 1 {
			workers = 1
		}
	}
	if ringSize == 0 {
		ringSize = 8192
	}

	// round numRings up to next power of 2 for bitmask
	n := numRings
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	numRings = n

	ss := &ShardedScheduler[T]{
		rings:    make([]*sl.SPSCRing[T], numRings),
		numRings: numRings,
		ringMask: numRings - 1,
		workers:  workers,
		done:     make(chan struct{}),
		sem:      make(chan struct{}, workers),
	}
	for i := 0; i < numRings; i++ {
		ss.rings[i] = sl.NewSPSCRing[T](ringSize)
	}
	return ss
}

// Submit producer 入队 — procPin 保证 SPSC 单写者
// 快速路径: procPin → SPSC Enqueue (零 CAS) → procUnpin
func (ss *ShardedScheduler[T]) Submit(v T) {
	// 快速路径：pin 住当前 P，选择对应 ring，写入
	// procPin 必须覆盖 Enqueue 全程以保证 SPSC 单写者
	pid := runtime_procPin()
	ring := ss.rings[pid&ss.ringMask]
	ok := ring.Enqueue(v)
	runtime_procUnpin()

	if !ok {
		// 慢路径：ring 满，背压重试（极少触发）
		ss.submitSlow(v)
	}

	// 唤醒泊车 worker（仅在有 worker 泊车时）
	if ss.parked.Load() > 0 {
		select {
		case ss.sem <- struct{}{}:
		default:
		}
	}
}

// submitSlow ring 满时的背压重试
func (ss *ShardedScheduler[T]) submitSlow(v T) {
	for {
		runtime.Gosched() // 让出 CPU 让 consumer 消费
		pid := runtime_procPin()
		ring := ss.rings[pid&ss.ringMask]
		ok := ring.Enqueue(v)
		runtime_procUnpin()
		if ok {
			return
		}
	}
}

// Start 启动 workers
func (ss *ShardedScheduler[T]) Start(loop func(T)) {
	for i := 0; i < ss.workers; i++ {
		ss.wg.Add(1)
		go ss.worker(i, loop)
	}
}

func (ss *ShardedScheduler[T]) worker(id int, loop func(T)) {
	defer ss.wg.Done()

	// 计算该 worker 拥有的 rings（静态分配，不可偷取）
	owned := make([]int, 0, (ss.numRings+ss.workers-1)/ss.workers)
	for r := id; r < ss.numRings; r += ss.workers {
		owned = append(owned, r)
	}

	for !ss.stop.Load() {
		ss.workerLoop(owned, loop)
	}
}

func (ss *ShardedScheduler[T]) workerLoop(owned []int, loop func(T)) {
	defer func() {
		if r := recover(); r != nil && ss.OnPanic != nil {
			ss.OnPanic(r)
		}
	}()

	idle := 0
	for !ss.stop.Load() {
		consumed := false

		// 轮询拥有的 rings（SPSC Dequeue = 零 CAS）
		for _, ringIdx := range owned {
			ring := ss.rings[ringIdx]
			// 每个 ring 批量消费最多 32 个事件
			for i := 0; i < 32; i++ {
				t, ok := ring.Dequeue()
				if !ok {
					break
				}
				loop(t)
				consumed = true
			}
		}

		if consumed {
			idle = 0
			continue
		}

		// 三级自适应空转策略:
		// Level 0: CPU spin（PAUSE 指令，~3ns/iter，不进入 Go 调度器）
		// Level 1: 协作让出（~5ns/iter，释放 P 但涉及调度器锁）
		// Level 2: 真正 park（仅长时间无事件时挂起）
		idle++
		if idle <= 4096 {
			// Level 0: PAUSE 指令自旋，完全规避 Go 调度器开销
			// 4096 × ~3ns ≈ 12μs 窗口，覆盖单线程 submit 间隔（~65ns）
			runtime_procyield(10)
			continue
		}
		if idle <= 4096+256 {
			// Level 1: 协作让出，为低优先级场景提供公平性
			runtime.Gosched()
			continue
		}

		// Level 2: 泊车等待唤醒
		ss.parked.Add(1)
		select {
		case <-ss.sem:
			ss.parked.Add(-1)
			idle = 0
		case <-ss.done:
			ss.parked.Add(-1)
			return
		}
	}
}

// Stop 停止所有 workers
func (ss *ShardedScheduler[T]) Stop() {
	ss.stop.Store(true)
	close(ss.done) // 通知所有 parked workers 退出
	ss.wg.Wait()
}
