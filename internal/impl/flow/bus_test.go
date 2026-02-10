package flow

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniyakcom/beat/core"
)

// TestFlowBasic 测试基本功能
func TestFlowBasic(t *testing.T) {
	// 创建简单的stage
	var stageProcessed atomic.Int32
	stage := func(events []*core.Event) error {
		stageProcessed.Add(int32(len(events)))
		return nil
	}

	proc := New([]Stage{stage}, 10, 50*time.Millisecond)
	defer proc.Close()

	// 发送事件
	for i := 0; i < 25; i++ {
		evt := &core.Event{Type: "test.topic", Data: []byte{byte(i)}}
		if err := proc.Emit(evt); err != nil {
			t.Fatalf("Emit failed: %v", err)
		}
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)

	// 验证统计
	processed, batches := proc.BatchStats()
	if processed != 25 {
		t.Errorf("Expected 25 processed events, got %d", processed)
	}
	if batches < 2 {
		t.Errorf("Expected at least 2 batches (size 10), got %d", batches)
	}
	if stageProcessed.Load() != 25 {
		t.Errorf("Expected stage to process 25 events, got %d", stageProcessed.Load())
	}
}

// TestFlowSubscription 测试订阅机制
func TestFlowSubscription(t *testing.T) {
	proc := New([]Stage{}, 5, 50*time.Millisecond)
	defer proc.Close()

	var received atomic.Int32
	var mu sync.Mutex
	var topics []string

	// 订阅
	id := proc.On("test.*", func(evt *core.Event) error {
		received.Add(1)
		mu.Lock()
		topics = append(topics, evt.Type)
		mu.Unlock()
		return nil
	})

	if id == 0 {
		t.Fatal("Expected non-zero subscription ID")
	}

	// 发送匹配事件
	proc.Emit(&core.Event{Type: "test.foo", Data: []byte{1}})
	proc.Emit(&core.Event{Type: "test.bar", Data: []byte{2}})
	proc.Emit(&core.Event{Type: "other.baz", Data: []byte{3}}) // 不匹配

	// 等待处理
	time.Sleep(150 * time.Millisecond)

	if received.Load() != 2 {
		t.Errorf("Expected 2 received events, got %d", received.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(topics) != 2 {
		t.Errorf("Expected 2 topics, got %d: %v", len(topics), topics)
	}
}

// TestFlowUnsubscribe 测试取消订阅
func TestFlowUnsubscribe(t *testing.T) {
	proc := New([]Stage{}, 5, 50*time.Millisecond)
	defer proc.Close()

	var count atomic.Int32
	id := proc.On("test.*", func(evt *core.Event) error {
		count.Add(1)
		return nil
	})

	// 发送第一批
	proc.Emit(&core.Event{Type: "test.1", Data: []byte{1}})
	time.Sleep(100 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("Expected 1 event before unsubscribe, got %d", count.Load())
	}

	// 取消订阅
	proc.Off(id)

	// 发送第二批
	proc.Emit(&core.Event{Type: "test.2", Data: []byte{2}})
	time.Sleep(100 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("Expected 1 event after unsubscribe, got %d", count.Load())
	}
}

// TestFlowBatchSize 测试批量大小触发
func TestFlowBatchSize(t *testing.T) {
	var batchSizes []int
	var mu sync.Mutex

	stage := func(events []*core.Event) error {
		mu.Lock()
		batchSizes = append(batchSizes, len(events))
		mu.Unlock()
		return nil
	}

	proc := New([]Stage{stage}, 10, 1*time.Second) // 长超时，确保按大小触发
	defer proc.Close()

	// 发送恰好10个事件
	for i := 0; i < 10; i++ {
		proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
	}

	// 等待批次处理
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// 分片架构：事件可能分布到多个分片，因此可能有多个批次
	if len(batchSizes) == 0 {
		t.Error("Expected at least 1 batch")
	}
	// 验证总事件数
	totalEvents := 0
	for _, size := range batchSizes {
		totalEvents += size
	}
	if totalEvents != 10 {
		t.Errorf("Expected total 10 events, got %d", totalEvents)
	}
}

// TestFlowTimeout 测试超时触发
func TestFlowTimeout(t *testing.T) {
	var batchSizes []int
	var mu sync.Mutex

	stage := func(events []*core.Event) error {
		mu.Lock()
		batchSizes = append(batchSizes, len(events))
		mu.Unlock()
		return nil
	}

	proc := New([]Stage{stage}, 100, 50*time.Millisecond) // 小超时
	defer proc.Close()

	// 发送少量事件（不足批量大小）
	for i := 0; i < 5; i++ {
		proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
	}

	// 等待超时触发
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(batchSizes) == 0 {
		t.Error("Expected at least 1 batch from timeout")
	}
	// 分片架构：验证总事件数而不是单批次大小
	totalEvents := 0
	for _, size := range batchSizes {
		totalEvents += size
	}
	if totalEvents != 5 {
		t.Errorf("Expected total 5 events, got %d", totalEvents)
	}
}

// TestFlowMultipleStages 测试多阶段Pipeline
func TestFlowMultipleStages(t *testing.T) {
	var stage1Count, stage2Count, stage3Count atomic.Int32

	stage1 := func(events []*core.Event) error {
		stage1Count.Add(int32(len(events)))
		return nil
	}

	stage2 := func(events []*core.Event) error {
		stage2Count.Add(int32(len(events)))
		return nil
	}

	stage3 := func(events []*core.Event) error {
		stage3Count.Add(int32(len(events)))
		return nil
	}

	proc := New([]Stage{stage1, stage2, stage3}, 5, 50*time.Millisecond)
	defer proc.Close()

	// 发送事件
	for i := 0; i < 15; i++ {
		proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
	}

	time.Sleep(150 * time.Millisecond)

	// 所有stage都应该处理相同数量的事件
	if stage1Count.Load() != 15 {
		t.Errorf("Stage1: expected 15, got %d", stage1Count.Load())
	}
	if stage2Count.Load() != 15 {
		t.Errorf("Stage2: expected 15, got %d", stage2Count.Load())
	}
	if stage3Count.Load() != 15 {
		t.Errorf("Stage3: expected 15, got %d", stage3Count.Load())
	}
}

// TestFlowEmitBatch 测试批量发送
func TestFlowEmitBatch(t *testing.T) {
	var count atomic.Int32
	stage := func(events []*core.Event) error {
		count.Add(int32(len(events)))
		return nil
	}

	proc := New([]Stage{stage}, 20, 50*time.Millisecond)
	defer proc.Close()

	// 批量发送
	events := make([]*core.Event, 30)
	for i := 0; i < 30; i++ {
		events[i] = &core.Event{Type: "test", Data: []byte{byte(i)}}
	}

	if err := proc.EmitBatch(events); err != nil {
		t.Fatalf("EmitBatch failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	if count.Load() != 30 {
		t.Errorf("Expected 30 processed events, got %d", count.Load())
	}
}

// TestFlowFlush 测试手动刷新
func TestFlowFlush(t *testing.T) {
	var count atomic.Int32
	stage := func(events []*core.Event) error {
		count.Add(int32(len(events)))
		return nil
	}

	proc := New([]Stage{stage}, 100, 10*time.Second) // 长超时，需手动刷新
	defer proc.Close()

	// 发送少量事件
	for i := 0; i < 5; i++ {
		proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
	}

	// 等待事件进入批次
	time.Sleep(10 * time.Millisecond)

	// 立即刷新
	if err := proc.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// 等待处理完成（因为handler是异步的）
	time.Sleep(50 * time.Millisecond)

	if count.Load() != 5 {
		t.Errorf("Expected 5 processed events after flush, got %d", count.Load())
	}
}

// TestFlowClose 测试关闭
func TestFlowClose(t *testing.T) {
	proc := New([]Stage{}, 10, 50*time.Millisecond)

	// 发送一些事件
	for i := 0; i < 5; i++ {
		proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
	}

	// 关闭
	proc.Close()

	// 关闭后发送应该不panic
	err := proc.Emit(&core.Event{Type: "test", Data: []byte{255}})
	if err != nil {
		t.Errorf("Emit after close should not error, got %v", err)
	}

	// 二次关闭应该安全
	proc.Close()
}

// TestFlowConcurrent 测试并发安全
func TestFlowConcurrent(t *testing.T) {
	var count atomic.Int32
	stage := func(events []*core.Event) error {
		count.Add(int32(len(events)))
		return nil
	}

	proc := New([]Stage{stage}, 20, 50*time.Millisecond)
	defer proc.Close()

	// 并发发送
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				proc.Emit(&core.Event{Type: "test", Data: []byte{byte(i)}})
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	if count.Load() != 1000 {
		t.Errorf("Expected 1000 processed events, got %d", count.Load())
	}
}

// BenchmarkFlowEmit 基准测试
func BenchmarkFlowEmit(b *testing.B) {
	stage := func(events []*core.Event) error {
		return nil
	}

	proc := New([]Stage{stage}, 100, 10*time.Millisecond)
	defer proc.Close()

	evt := &core.Event{Type: "bench.test", Data: []byte{123}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			proc.Emit(evt)
		}
	})
}

// BenchmarkFlowBatchThroughput 批处理吞吐量基准测试
func BenchmarkFlowBatchThroughput(b *testing.B) {
	var processed atomic.Int64
	stage := func(events []*core.Event) error {
		processed.Add(int64(len(events)))
		return nil
	}

	proc := New([]Stage{stage}, 100, 1*time.Millisecond)
	defer proc.Close()

	evt := &core.Event{Type: "bench.test", Data: []byte{123}}

	b.ResetTimer()
	start := time.Now()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			proc.Emit(evt)
		}
	})

	// 等待批处理完成
	time.Sleep(10 * time.Millisecond)
	proc.Flush()
	time.Sleep(10 * time.Millisecond)

	elapsed := time.Since(start)
	total := processed.Load()
	b.ReportMetric(float64(total)/elapsed.Seconds(), "events/sec")
}

// BenchmarkFlowEmitBatch 批量发送基准测试
func BenchmarkFlowEmitBatch(b *testing.B) {
	stage := func(events []*core.Event) error {
		return nil
	}

	proc := New([]Stage{stage}, 100, 10*time.Millisecond)
	defer proc.Close()

	events := make([]*core.Event, 100)
	for i := 0; i < 100; i++ {
		events[i] = &core.Event{Type: "bench.test", Data: []byte{byte(i)}}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc.EmitBatch(events)
	}
}
