package beat

import (
	"testing"
)

// BenchmarkPanicRecoveryOverhead 测量异步 Panic 恢复的性能开销
// 对比: 正常处理 vs 带 panic 的异步处理
// 注: 同步路径不再恢复 panic，需在应用层面处理
func BenchmarkPanicRecoveryOverhead(b *testing.B) {
	// 使用异步模式以获得 panic 恢复功能
	bus, _ := ForAsync()
	defer bus.Close()

	// 注册会 panic 的 handler
	bus.On("panic.event", func(e *Event) error {
		panic("intentional panic for benchmark")
	})

	evt := &Event{Type: "panic.event", Data: []byte("test")}

	// 执行异步 emit（异步模式会恢复 panic）
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	b.StopTimer()
}

// BenchmarkNormalHandlerVsPanicHandler 对比：正常处理 vs panic 处理（异步）
func BenchmarkNormalHandlerVsPanicHandler(b *testing.B) {
	b.Run("NormalHandler", func(b *testing.B) {
		bus, _ := ForAsync()
		defer bus.Close()

		bus.On("normal", func(e *Event) error {
			return nil
		})

		evt := &Event{Type: "normal", Data: []byte("test")}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.Emit(evt)
		}
	})

	b.Run("PanicHandlerAsync", func(b *testing.B) {
		bus, _ := ForAsync()
		defer bus.Close()

		bus.On("panic_handler", func(e *Event) error {
			panic("test panic")
		})

		evt := &Event{Type: "panic_handler", Data: []byte("test")}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.Emit(evt)
		}
	})
}

// BenchmarkPanicHandlerCount 测量异步 panic 处理器的计数准确性
func BenchmarkPanicHandlerCount(b *testing.B) {
	bus, _ := ForAsync()
	defer bus.Close()

	// 多个handler，每第3个 panic
	for i := 0; i < 10; i++ {
		if i%3 == 0 {
			bus.On("count.panic", func(e *Event) error {
				panic("panic in handler")
			})
		} else {
			bus.On("count.panic", func(e *Event) error {
				return nil
			})
		}
	}

	evt := &Event{Type: "count.panic", Data: []byte("test")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
}

// BenchmarkStatsCollectionOverhead 测量Stats收集的开销
func BenchmarkStatsCollectionOverhead(b *testing.B) {
	bus, _ := ForSync()
	defer bus.Close()

	bus.On("stats.test", func(e *Event) error {
		return nil
	})

	evt := &Event{Type: "stats.test", Data: []byte("test")}

	// 预热
	for i := 0; i < 1000; i++ {
		bus.Emit(evt)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
		// 在每个emit后获取stats（测量开销）
		_ = bus.Stats()
	}
}
