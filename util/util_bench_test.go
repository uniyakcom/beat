package util

import (
	"testing"
)

// BenchmarkPerCPUCounterAdd 测量 PerCPUCounter.Add 的性能
func BenchmarkPerCPUCounterAdd(b *testing.B) {
	c := NewPerCPUCounter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Add(1)
	}
}

// BenchmarkPerCPUCounterRead 测量 PerCPUCounter.Read 的性能
func BenchmarkPerCPUCounterRead(b *testing.B) {
	c := NewPerCPUCounter()
	// 预热
	for i := 0; i < 1000; i++ {
		c.Add(1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Read()
	}
}

// BenchmarkPerCPUCounterParallel 并发测试 Add
func BenchmarkPerCPUCounterParallel(b *testing.B) {
	c := NewPerCPUCounter()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Add(1)
		}
	})
}
