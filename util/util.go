// Package util 提供事件总线通用的工具函数
package util

import (
	"sync/atomic"
	"unsafe"
)

// PerCPUCounter per-CPU 无竞争计数器（避免 atomic 竞争）
// 使用 256 个 slots，每个 slot 独占 cache line，消除 false sharing
type PerCPUCounter struct {
	counters [256]counterSlot
}

type counterSlot struct {
	count atomic.Int64
	_     [56]byte // cache line padding (64 - 8 bytes for Int64)
}

// NewPerCPUCounter 创建新的 per-CPU 计数器
func NewPerCPUCounter() *PerCPUCounter {
	return &PerCPUCounter{}
}

// Add 原子加法（per-CPU 无竞争）
func (c *PerCPUCounter) Add(delta int64) {
	id := getSlotIndex()
	c.counters[id].count.Add(delta)
}

// Read 读取所有 CPU 的累计值
func (c *PerCPUCounter) Read() int64 {
	var sum int64
	for i := 0; i < 256; i++ {
		sum += c.counters[i].count.Load()
	}
	return sum
}

// getSlotIndex 获取当前 goroutine 应使用的 slot 索引
// 使用栈地址作为哈希源，在多核系统上自然分散竞争
func getSlotIndex() int {
	var dummy int
	addr := uintptr(unsafe.Pointer(&dummy))
	return int(addr % 256)
}
