package beat

import (
	"sync/atomic"
	"testing"
)

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
