// Package util 提供事件总线通用的工具函数
package util

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// maxSlots 最大 slot 数量（覆盖常见 GOMAXPROCS）
const maxSlots = 256

// PerCPUCounter per-CPU 无竞争计数器（避免 atomic 竞争）
// 使用 goroutine 栈地址哈希分散写入到不同 cache line
type PerCPUCounter struct {
	counters [maxSlots]counterSlot
	mask     int
}

type counterSlot struct {
	count atomic.Int64
	_     [56]byte // cache line padding (64 - 8 bytes for Int64)
}

// NewPerCPUCounter 创建新的 per-CPU 计数器
// 自适应: 最小 8 slot，避免低核环境（2-4 vCPU）goroutine 栈地址哈希冲突率过高。
// 8 slot 在 2 vCPU 上将冲突率从 ~100%（2 slot）降至 ~25%。
func NewPerCPUCounter() *PerCPUCounter {
	// 向上取 2 的幂，用于 bitmask
	n := runtime.GOMAXPROCS(0)
	sz := 1
	for sz < n {
		sz *= 2
	}
	// 低核环境保底: 最少 8 slot 分散哈希冲突
	if sz < 8 {
		sz = 8
	}
	if sz > maxSlots {
		sz = maxSlots
	}
	return &PerCPUCounter{mask: sz - 1}
}

// Add 原子加法（per-goroutine 栈地址分散）
// 利用不同 goroutine 栈地址天然分散在不同内存页的特性，
// 通过栈变量地址右移 + bitmask 映射到不同 slot，减少跨核 cache 争用。
// 保证 x 不会被分配到堆上。
//
//go:nosplit
func (c *PerCPUCounter) Add(delta int64) {
	var x uintptr
	// 右移 13 位: goroutine 最小栈 8KB = 2^13，确保不同 goroutine 哈希到不同 slot
	id := int(uintptr(unsafe.Pointer(&x)) >> 13)
	c.counters[id&c.mask].count.Add(delta)
}

// Read 读取所有 slot 的累计值
func (c *PerCPUCounter) Read() int64 {
	var sum int64
	n := c.mask + 1
	for i := 0; i < n; i++ {
		sum += c.counters[i].count.Load()
	}
	return sum
}
