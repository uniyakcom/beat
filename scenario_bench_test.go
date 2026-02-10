package beat

import (
	"sync/atomic"
	"testing"
)

// BenchmarkScenarioSync 基准测试同步场景
// 目标性能: ~35ns/op, 27+ M/s
func BenchmarkScenarioSync(b *testing.B) {
	bus, err := ForSync()
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

// BenchmarkScenarioPubSub 基准测试发布订阅场景
// 目标性能: ~7.5ns/op, 100+ M/s
func BenchmarkScenarioPubSub(b *testing.B) {
	bus, err := ForAsync()
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

// BenchmarkScenarioLogging 基准测试日志采集场景
// 目标性能: >100 M/s
func BenchmarkScenarioLogging(b *testing.B) {
	bus, err := ForAsync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("log entry")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioStream 基准测试流式处理场景
// 目标性能: 10-100 M/s
func BenchmarkScenarioStream(b *testing.B) {
	bus, err := ForFlow()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("stream data")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioBatch 基准测试批量处理场景
// 目标性能: ~50 M/s (批处理聚合)
func BenchmarkScenarioBatch(b *testing.B) {
	bus, err := ForFlow()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("batch record")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioUltra 基准测试超低延迟场景
// 目标性能: ~7.5ns/op, 100+ M/s
func BenchmarkScenarioUltra(b *testing.B) {
	bus, err := ForAsync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	id := bus.On("bench", func(e *Event) error { return nil })
	defer bus.Off(id)

	evt := &Event{Type: "bench", Data: []byte("tick")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioSyncConcurrent 并发基准测试同步场景
func BenchmarkScenarioSyncConcurrent(b *testing.B) {
	bus, err := ForSync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	// 注册100个handler模拟复杂场景
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

// BenchmarkScenarioPubSubConcurrent 并发基准测试发布订阅场景
func BenchmarkScenarioPubSubConcurrent(b *testing.B) {
	bus, err := ForAsync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	// 注册100个订阅者
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

// BenchmarkScenarioLoggingHighThroughput 高吞吐日志场景
func BenchmarkScenarioLoggingHighThroughput(b *testing.B) {
	bus, err := ForAsync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	var counter int64
	bus.On("log.*", func(e *Event) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	evt := &Event{Type: "log.info", Data: []byte("log message")}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bus.EmitMatch(evt)
		}
	})
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioStreamPipeline 流式处理pipeline场景
func BenchmarkScenarioStreamPipeline(b *testing.B) {
	bus, err := ForFlow()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	// 模拟多阶段处理
	var processed int64
	bus.On("stream.data", func(e *Event) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})

	evt := &Event{Type: "stream.data", Data: make([]byte, 256)}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}
	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioBatchBulkInsert 批量插入场景
func BenchmarkScenarioBatchBulkInsert(b *testing.B) {
	bus, err := ForFlow()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	var inserted int64
	bus.On("batch.insert", func(e *Event) error {
		atomic.AddInt64(&inserted, 1)
		return nil
	})

	// 批量事件
	batchSize := 1000
	events := make([]*Event, batchSize)
	for i := 0; i < batchSize; i++ {
		events[i] = &Event{
			Type: "batch.insert",
			Data: []byte("record"),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.EmitBatch(events)
	}

	totalEvents := int64(b.N * batchSize)
	throughput := float64(totalEvents) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}

// BenchmarkScenarioUltraLowLatency 超低延迟场景（微秒级）
func BenchmarkScenarioUltraLowLatency(b *testing.B) {
	bus, err := ForAsync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	// 最小化handler开销
	bus.On("tick", func(e *Event) error { return nil })

	evt := &Event{Type: "tick", Data: []byte("t")}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bus.Emit(evt)
	}

	avgLatency := b.Elapsed().Nanoseconds() / int64(b.N)
	b.ReportMetric(float64(avgLatency), "ns/op")

	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput/1e6, "M/s")
}
