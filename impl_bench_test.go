package beat

import (
	"runtime"
	"testing"

	implasync "github.com/uniyakcom/beat/internal/impl/async"
	"github.com/uniyakcom/beat/internal/impl/flow"
	implsync "github.com/uniyakcom/beat/internal/impl/sync"
	"github.com/uniyakcom/beat/internal/support/pool"
)

// ═══════════════════════════════════════════════════════════════════
// Sync 基准
// ═══════════════════════════════════════════════════════════════════

// BenchmarkImplSync 基准测试sync实现（同步模式）
func BenchmarkImplSync(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplSyncUnsafe 基准测试sync实现（UnsafeEmit 零保护路径）
func BenchmarkImplSyncUnsafe(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.UnsafeEmit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplSyncAsync 基准测试sync实现（异步模式）
func BenchmarkImplSyncAsync(b *testing.B) {
	bus, err := implsync.NewAsync(runtime.NumCPU() * 10)
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplFlow 基准测试flow实现
func BenchmarkImplFlow(b *testing.B) {
	stages := []flow.Stage{
		func(events []*Event) error {
			return nil
		},
	}
	bus := flow.New(stages, 100, 100)
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplSyncHighConcurrency sync实现高并发场景
func BenchmarkImplSyncHighConcurrency(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	for i := 0; i < 100; i++ {
		bus.On("bench", func(e *Event) error { return nil })
	}

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.Emit(evt)
		}
	})
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplFlowHighConcurrency flow实现高并发场景
func BenchmarkImplFlowHighConcurrency(b *testing.B) {
	stages := []flow.Stage{
		func(events []*Event) error {
			return nil
		},
	}
	bus := flow.New(stages, 1000, 100)
	defer bus.Close()

	for i := 0; i < 100; i++ {
		bus.On("bench", func(e *Event) error { return nil })
	}

	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.Emit(evt)
		}
	})
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplPatternMatching 模式匹配性能对比
func BenchmarkImplPatternMatching(b *testing.B) {
	b.Run("Sync", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()

		bus.On("user.*", func(e *Event) error { return nil })
		evt := &Event{Type: "user.login", Data: []byte("data")}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.EmitMatch(evt)
		}
	})

	b.Run("Flow", func(b *testing.B) {
		stages := []flow.Stage{
			func(events []*Event) error {
				return nil
			},
		}
		bus := flow.New(stages, 100, 100)
		defer bus.Close()

		bus.On("user.*", func(e *Event) error { return nil })
		evt := &Event{Type: "user.login", Data: []byte("data")}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.EmitMatch(evt)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════
// Event Pool 专项基准
// ═══════════════════════════════════════════════════════════════════

// BenchmarkEventPool_AcquireRelease 对象池获取/归还基线
func BenchmarkEventPool_AcquireRelease(b *testing.B) {
	p := pool.New()
	data := []byte("data")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		evt := p.Acquire()
		evt.Type = "bench"
		evt.Data = data
		p.Release(evt)
	}
}

// BenchmarkEventPool_Parallel 并行对象池
func BenchmarkEventPool_Parallel(b *testing.B) {
	p := pool.New()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			evt := p.Acquire()
			evt.Type = "bench"
			p.Release(evt)
		}
	})
}

// BenchmarkEventPool_Arena 批量Arena分配Data（自动Arena管理）
func BenchmarkEventPool_Arena(b *testing.B) {
	p := pool.New()
	pool.SetEnableArena(true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 64; j++ {
			buf := p.AllocData(128)
			_ = buf
		}
	}
	b.ReportMetric(float64(b.N)*64, "allocs_saved")
}

// ═══════════════════════════════════════════════════════════════════
// Arena ± 对比
// ═══════════════════════════════════════════════════════════════════

// BenchmarkArena_ComparisonAllImpls 所有实现 ± Arena 对比
func BenchmarkArena_ComparisonAllImpls(b *testing.B) {
	b.Run("Sync/NoArena", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()
		bus.On("bench", func(e *Event) error { return nil })
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			evt := &Event{Type: "bench", Data: make([]byte, 256)}
			_ = bus.Emit(evt)
		}
	})

	b.Run("Sync/WithArena", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()
		p := pool.Global()
		pool.SetEnableArena(true)
		bus.On("bench", func(e *Event) error { return nil })
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			evt := p.Acquire()
			evt.Type = "bench"
			evt.Data = p.AllocData(256)
			_ = bus.Emit(evt)
			p.Release(evt)
		}
	})

	b.Run("Sync/UnsafeEmit+Arena", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()
		p := pool.Global()
		pool.SetEnableArena(true)
		bus.On("bench", func(e *Event) error { return nil })
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			evt := p.Acquire()
			evt.Type = "bench"
			evt.Data = p.AllocData(256)
			_ = bus.UnsafeEmit(evt)
			p.Release(evt)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════
// Async (Per-P SPSC) 专项基准
// ═══════════════════════════════════════════════════════════════════

// BenchmarkImplAsync 单线程基线
func BenchmarkImplAsync(b *testing.B) {
	bus := implasync.New(implasync.DefaultConfig())
	defer bus.Close()

	bus.On("bench", func(e *Event) error { return nil })
	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkImplAsyncHighConcurrency 高并发：GOMAXPROCS goroutine 并行发布
func BenchmarkImplAsyncHighConcurrency(b *testing.B) {
	bus := implasync.New(&implasync.Config{
		Workers:  runtime.NumCPU() / 2,
		RingSize: 1 << 13,
	})
	defer bus.Close()

	bus.On("bench", func(e *Event) error { return nil })
	evt := &Event{Type: "bench", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.Emit(evt)
		}
	})
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}
