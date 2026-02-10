package beat

import (
	"runtime"
	"strconv"
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

// BenchmarkImplSyncMemoryEfficiency sync内存效率测试
func BenchmarkImplSyncMemoryEfficiency(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	for i := 0; i < 10; i++ {
		bus.On("bench", func(e *Event) error { return nil })
	}

	evt := &Event{Type: "bench", Data: make([]byte, 64)}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
}

// BenchmarkImplComparison 实现对比测试（不同事件大小）
func BenchmarkImplComparison(b *testing.B) {
	sizes := []int{16, 256, 1024, 4096}

	for _, size := range sizes {
		b.Run("Sync_"+strconv.Itoa(size)+"B", func(b *testing.B) {
			bus := implsync.NewBus()
			defer bus.Close()

			bus.On("bench", func(e *Event) error { return nil })
			evt := &Event{Type: "bench", Data: make([]byte, size)}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = bus.Emit(evt)
			}
		})
	}
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
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		evt := p.Acquire()
		evt.Type = "bench"
		evt.Data = []byte("data")
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

// BenchmarkEventPool_vs_NewEvent 对比: Pool vs 直接new
func BenchmarkEventPool_vs_NewEvent(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		p := pool.New()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			evt := p.Acquire()
			p.Release(evt)
		}
	})
	b.Run("NewDirect", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			evt := new(Event)
			evt.Type = "bench"
			runtime.KeepAlive(evt)
		}
	})
}

// BenchmarkEventPool_Arena 批量Arena分配Data（自动Arena管理）
func BenchmarkEventPool_Arena(b *testing.B) {
	p := pool.New()
	p.EnableArena = true
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
// 实现横向对比 — 所有实现同一条件 (sync / async / flow)
// ═══════════════════════════════════════════════════════════════════

// BenchmarkAllImpls_SingleProducer 单生产者全对比
func BenchmarkAllImpls_SingleProducer(b *testing.B) {
	b.Run("Sync", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()
		bus.On("bench", func(e *Event) error { return nil })
		evt := &Event{Type: "bench", Data: []byte("x")}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = bus.Emit(evt)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "M/s")
	})
	b.Run("Async", func(b *testing.B) {
		bus := implasync.New(implasync.DefaultConfig())
		defer bus.Close()
		bus.On("bench", func(e *Event) error { return nil })
		evt := &Event{Type: "bench", Data: []byte("x")}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = bus.Emit(evt)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "M/s")
	})
	b.Run("Flow", func(b *testing.B) {
		bus := flow.New([]flow.Stage{func(e []*Event) error { return nil }}, 100, 100)
		defer bus.Close()
		bus.On("bench", func(e *Event) error { return nil })
		evt := &Event{Type: "bench", Data: []byte("x")}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = bus.Emit(evt)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "M/s")
	})
}

// BenchmarkAllImpls_HighConcurrency 高并发全对比
func BenchmarkAllImpls_HighConcurrency(b *testing.B) {
	b.Run("Sync", func(b *testing.B) {
		bus := implsync.NewBus()
		defer bus.Close()
		for i := 0; i < 100; i++ {
			bus.On("bench", func(e *Event) error { return nil })
		}
		evt := &Event{Type: "bench", Data: []byte("x")}
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = bus.Emit(evt)
			}
		})
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "M/s")
	})
	b.Run("Async", func(b *testing.B) {
		bus := implasync.New(&implasync.Config{
			Workers:  runtime.NumCPU() / 2,
			RingSize: 1 << 13,
		})
		defer bus.Close()
		for i := 0; i < 100; i++ {
			bus.On("bench", func(e *Event) error { return nil })
		}
		evt := &Event{Type: "bench", Data: []byte("x")}
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = bus.Emit(evt)
			}
		})
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "M/s")
	})
}

// ═══════════════════════════════════════════════════════════════════
// Arena 跨实现应用演示
// ═══════════════════════════════════════════════════════════════════

// BenchmarkWithArena_SyncNoArena 基线：不用Arena
func BenchmarkWithArena_SyncNoArena(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	bus.On("bench", func(e *Event) error { return nil })

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		evt := &Event{Type: "bench", Data: make([]byte, 256)}
		_ = bus.Emit(evt)
	}
}

// BenchmarkWithArena_SyncWithArena Sync使用Arena
func BenchmarkWithArena_SyncWithArena(b *testing.B) {
	bus := implsync.NewBus()
	defer bus.Close()

	p := pool.Global()
	p.EnableArena = true
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
}

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
		p.EnableArena = true
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
