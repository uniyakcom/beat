// Package compare 竞品基准对比测试
//
// 测试场景说明：
//   - Emit_1Handler:        单 handler 精确匹配发布（核心热路径）
//   - Emit_10Handlers:      10 个 handler 精确匹配发布（多 handler 扇出）
//   - Parallel_Emit:        高并发 RunParallel 发布（并发吞吐）
//   - Async_Emit_1Handler:  异步发布单 handler（异步热路径）
//   - Async_Parallel_Emit:  异步高并发 RunParallel 发布（异步并发吞吐）
//
// 被测库：
//   - beat (Sync/Emit)          — 本项目同步安全路径
//   - beat (Sync/UnsafeEmit)    — 本项目同步零保护路径（最优参数）
//   - beat (Async)              — 本项目 Per-P SPSC 异步
//   - asaskevich/EventBus       — 2k⭐ 经典事件总线（reflect 分发）
//   - gookit/event              — 565⭐ 事件管理器（接口分发 + 优先级）
//
// 运行方式：
//
//	cd _benchmarks
//	go test -bench=. -benchmem -benchtime=3s -count=3 -run=^$ | tee results.txt
package compare

import (
	"testing"

	EventBus "github.com/asaskevich/EventBus"
	"github.com/gookit/event"
	"github.com/uniyakcom/beat"
)

// ═══════════════════════════════════════════════════════════════════
// beat Sync
// ═══════════════════════════════════════════════════════════════════

func BenchmarkBeatSync_Emit_1Handler(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })
	evt := &beat.Event{Type: "bench.event", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Emit(evt)
	}
}

func BenchmarkBeatSync_Emit_10Handlers(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	for i := 0; i < 10; i++ {
		bus.On("bench.event", func(e *beat.Event) error { return nil })
	}
	evt := &beat.Event{Type: "bench.event", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Emit(evt)
	}
}

func BenchmarkBeatSync_Parallel_Emit(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		evt := &beat.Event{Type: "bench.event", Data: []byte("data")}
		for pb.Next() {
			bus.Emit(evt)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════
// beat UnsafeEmit (零保护极致性能)
// ═══════════════════════════════════════════════════════════════════

func BenchmarkBeatUnsafe_Emit_1Handler(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })
	evt := &beat.Event{Type: "bench.event", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.UnsafeEmit(evt)
	}
}

func BenchmarkBeatUnsafe_Emit_10Handlers(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	for i := 0; i < 10; i++ {
		bus.On("bench.event", func(e *beat.Event) error { return nil })
	}
	evt := &beat.Event{Type: "bench.event", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.UnsafeEmit(evt)
	}
}

func BenchmarkBeatUnsafe_Parallel_Emit(b *testing.B) {
	bus, _ := beat.ForSync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		evt := &beat.Event{Type: "bench.event", Data: []byte("data")}
		for pb.Next() {
			bus.UnsafeEmit(evt)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════
// asaskevich/EventBus
// ═══════════════════════════════════════════════════════════════════

func BenchmarkEventBus_Emit_1Handler(b *testing.B) {
	bus := EventBus.New()
	bus.Subscribe("bench.event", func() {})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Publish("bench.event")
	}
}

func BenchmarkEventBus_Emit_10Handlers(b *testing.B) {
	bus := EventBus.New()
	for i := 0; i < 10; i++ {
		bus.Subscribe("bench.event", func() {})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Publish("bench.event")
	}
}

func BenchmarkEventBus_Parallel_Emit(b *testing.B) {
	bus := EventBus.New()
	bus.Subscribe("bench.event", func() {})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Publish("bench.event")
		}
	})
}

// ═══════════════════════════════════════════════════════════════════
// gookit/event
// ═══════════════════════════════════════════════════════════════════

func BenchmarkGookitEvent_Emit_1Handler(b *testing.B) {
	em := event.NewManager("bench")
	em.On("bench.event", event.ListenerFunc(func(e event.Event) error {
		return nil
	}), event.Normal)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		em.MustFire("bench.event", nil)
	}
}

func BenchmarkGookitEvent_Emit_10Handlers(b *testing.B) {
	em := event.NewManager("bench")
	for i := 0; i < 10; i++ {
		em.On("bench.event", event.ListenerFunc(func(e event.Event) error {
			return nil
		}), event.Normal)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		em.MustFire("bench.event", nil)
	}
}

func BenchmarkGookitEvent_Parallel_Emit(b *testing.B) {
	em := event.NewManager("bench")
	em.On("bench.event", event.ListenerFunc(func(e event.Event) error {
		return nil
	}), event.Normal)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			em.MustFire("bench.event", nil)
		}
	})
}

// ═════════════════════════════════════════════════════════════════
// Async 异步对比 — 各库公开异步 API
// ═════════════════════════════════════════════════════════════════

// --- beat Async (Per-P SPSC ring) ---

func BenchmarkBeatAsync_Emit_1Handler(b *testing.B) {
	bus, _ := beat.ForAsync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })
	evt := &beat.Event{Type: "bench.event", Data: []byte("data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Emit(evt)
	}
}

func BenchmarkBeatAsync_Parallel_Emit(b *testing.B) {
	bus, _ := beat.ForAsync()
	defer bus.Close()
	bus.On("bench.event", func(e *beat.Event) error { return nil })

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		evt := &beat.Event{Type: "bench.event", Data: []byte("data")}
		for pb.Next() {
			bus.Emit(evt)
		}
	})
}

// --- EventBus Async (SubscribeAsync + goroutine-per-call) ---

func BenchmarkEventBus_Async_Emit_1Handler(b *testing.B) {
	bus := EventBus.New()
	bus.SubscribeAsync("bench.event", func() {}, false)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bus.Publish("bench.event")
	}
}

func BenchmarkEventBus_Async_Parallel_Emit(b *testing.B) {
	bus := EventBus.New()
	bus.SubscribeAsync("bench.event", func() {}, false)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Publish("bench.event")
		}
	})
}

// --- gookit/event Async (AsyncFire / go keyword) ---

func BenchmarkGookitEvent_Async_Emit_1Handler(b *testing.B) {
	em := event.NewManager("bench")
	em.On("bench.event", event.ListenerFunc(func(e event.Event) error {
		return nil
	}), event.Normal)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		em.AsyncFire(event.NewBasic("bench.event", nil))
	}
}

func BenchmarkGookitEvent_Async_Parallel_Emit(b *testing.B) {
	em := event.NewManager("bench")
	em.On("bench.event", event.ListenerFunc(func(e event.Event) error {
		return nil
	}), event.Normal)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			em.AsyncFire(event.NewBasic("bench.event", nil))
		}
	})
}
