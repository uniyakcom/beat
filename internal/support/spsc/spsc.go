// SPSC 无等待环形缓冲区 — 单生产者/单消费者
//
// 热路径零 CAS：仅使用 atomic Load/Store
// x86-64 上 Load/Store 编译为普通 MOV（TSO 保证）
//
// SPSC 安全条件（由 ShardedScheduler 架构保证）：
//   - 生产者: runtime.procPin 保证同一 P 上 goroutine 串行化 → 单写者
//   - 消费者: worker 亲和性保证每个 ring 仅一个 worker → 单读者
//   - procPin 必须覆盖 Enqueue 调用全程（pin → write → unpin）
//
// 性能预估:
//   - Enqueue: ~2-3 ns (1 Load + 1 Store + 1 buf write)
//   - Dequeue: ~2-3 ns (1 Load + 1 Store + 1 buf read)
//   - vs LCRQ CAS: ~10-15 ns under contention
package spsc

import (
	"sync/atomic"
	"unsafe"
)

const _spscCacheLine = 64

// SPSCRing 单生产者单消费者无等待环形缓冲区
//
// 缓存行布局优化:
//   - Consumer 侧: head + cachedTail（consumer 独占，无 false sharing）
//   - Producer 侧: tail + cachedHead（producer 独占，无 false sharing）
//   - cachedHead/cachedTail 消除常态跨核读: Enqueue 不再每次读 head，Dequeue 不再每次读 tail
type SPSCRing[T any] struct {
	// Consumer 侧: head (consumer 写) + cachedTail (consumer 本地缓存 tail)
	head       atomic.Uint64
	cachedTail uint64
	_          [_spscCacheLine - unsafe.Sizeof(atomic.Uint64{}) - 8]byte

	// Producer 侧: tail (producer 写) + cachedHead (producer 本地缓存 head)
	tail       atomic.Uint64
	cachedHead uint64
	_          [_spscCacheLine - unsafe.Sizeof(atomic.Uint64{}) - 8]byte

	// 只读（初始化后不变）
	buf  []T
	mask uint64
}

// NewSPSCRing 创建 SPSC ring。size 必须为 2 的幂
func NewSPSCRing[T any](size uint64) *SPSCRing[T] {
	if size == 0 {
		size = 8192
	}
	if size&(size-1) != 0 {
		panic("SPSCRing: size must be power of 2")
	}
	return &SPSCRing[T]{
		buf:  make([]T, size),
		mask: size - 1,
	}
}

// Enqueue 生产者写入 — 单写者，零 CAS
// cachedHead 避免每次跨核读 head: 仅在 ring 看似满时才重新加载
//
//go:nosplit
func (r *SPSCRing[T]) Enqueue(v T) bool {
	tail := r.tail.Load()
	if tail-r.cachedHead > r.mask {
		// 本地缓存显示可能满 → 重新读取真实 head
		r.cachedHead = r.head.Load()
		if tail-r.cachedHead > r.mask {
			return false // 真的满了
		}
	}
	r.buf[tail&r.mask] = v
	r.tail.Store(tail + 1) // 发布给消费者
	return true
}

// Dequeue 消费者读取 — 单读者，零 CAS
// cachedTail 避免每次跨核读 tail: 仅在 ring 看似空时才重新加载
//
//go:nosplit
func (r *SPSCRing[T]) Dequeue() (T, bool) {
	var zero T
	head := r.head.Load()
	if head == r.cachedTail {
		// 本地缓存显示可能空 → 重新读取真实 tail
		r.cachedTail = r.tail.Load()
		if head == r.cachedTail {
			return zero, false // 真的空了
		}
	}
	v := r.buf[head&r.mask]
	r.buf[head&r.mask] = zero // help GC
	r.head.Store(head + 1)    // 释放槽位
	return v, true
}
