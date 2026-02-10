package beat_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniyakcom/beat"
)

// 说明：压力测试需要较长运行时间，使用 go test -v ./test/ 单独运行
// 使用 -short 标志可跳过这些测试

// TestStressHighConcurrency 高并发压力测试
// 1000个goroutines并发发布，每个100个事件，100个handlers
func TestStressHighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForSync()
	defer bus.Close()

	var processed int64
	var wg sync.WaitGroup

	// 注册100个handlers
	for i := 0; i < 100; i++ {
		bus.On("stress.concurrent", func(e *beat.Event) error {
			atomic.AddInt64(&processed, 1)
			return nil
		})
	}

	goroutineCount := 1000
	eventsPerGoroutine := 100
	start := time.Now()

	// 1000个goroutine并发发射
	for g := 0; g < goroutineCount; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				evt := &beat.Event{Type: "stress.concurrent", Data: []byte("test")}
				bus.Emit(evt)
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	expected := int64(goroutineCount * eventsPerGoroutine * 100)
	actual := atomic.LoadInt64(&processed)

	t.Logf("High concurrency: %d events in %v", actual, duration)
	t.Logf("Throughput: %.0f events/sec", float64(actual)/duration.Seconds())

	// 允许2%误差
	minExpected := expected * 98 / 100
	if actual < minExpected {
		t.Errorf("expected at least %d events, got %d", minExpected, actual)
	}
}

// TestStressMemoryUsage 内存使用压力测试
func TestStressMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	bus, _ := beat.ForSync()
	defer bus.Close()

	// 注册10000个handlers
	for i := 0; i < 10000; i++ {
		bus.On("stress.memory", func(e *beat.Event) error {
			return nil
		})
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// 发射10000个事件
	for i := 0; i < 10000; i++ {
		evt := &beat.Event{Type: "stress.memory", Data: []byte("memory test")}
		bus.Emit(evt)
	}

	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)

	t.Logf("Memory: Before=%d KB, After registration=%d KB, After events=%d KB",
		m1.Alloc/1024, m2.Alloc/1024, m3.Alloc/1024)

	// 内存不应超过10倍
	if m3.Alloc > m1.Alloc*10 {
		t.Errorf("memory too high: %d > %d", m3.Alloc, m1.Alloc*10)
	}
}

// TestStressLongRunning 长时间运行压力测试
// 持续10秒，不断注册/取消/发布事件
func TestStressLongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForAsync() // 使用Async场景
	defer bus.Close()

	var totalProcessed int64
	stop := make(chan struct{})

	// 持续注册和移除
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				id := bus.On("stress.continuous", func(e *beat.Event) error {
					atomic.AddInt64(&totalProcessed, 1)
					return nil
				})
				time.Sleep(50 * time.Millisecond)
				bus.Off(id)
			}
		}
	}()

	// 持续发射事件
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				evt := &beat.Event{Type: "stress.continuous", Data: []byte("data")}
				bus.Emit(evt)
				time.Sleep(25 * time.Millisecond)
			}
		}
	}()

	// 运行10秒
	time.Sleep(10 * time.Second)
	close(stop)

	processed := atomic.LoadInt64(&totalProcessed)
	t.Logf("Long running: %d events in 10 seconds", processed)

	if processed == 0 {
		t.Error("no events processed")
	}
}

// TestStressBursty 突发流量压力测试
// 模拟10次突发，每次1000个事件，50个handlers
func TestStressBursty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForAsync() // 使用Async场景（适合高吸吐）
	defer bus.Close()

	var processed int64

	// 注册50个handlers
	for i := 0; i < 50; i++ {
		bus.On("stress.burst", func(e *beat.Event) error {
			atomic.AddInt64(&processed, 1)
			return nil
		})
	}

	// 10次突发
	for burst := 0; burst < 10; burst++ {
		// 突发阶段：快速发射1000个事件
		for i := 0; i < 1000; i++ {
			evt := &beat.Event{Type: "stress.burst", Data: []byte("burst")}
			bus.Emit(evt)
		}

		// 空闲阶段
		time.Sleep(100 * time.Millisecond)
	}

	// 等待处理完成
	time.Sleep(500 * time.Millisecond)

	expected := int64(10 * 1000 * 50)
	actual := atomic.LoadInt64(&processed)

	// 允许5%误差
	minExpected := expected * 95 / 100
	if actual < minExpected {
		t.Errorf("expected at least %d events, got %d", minExpected, actual)
	}
	t.Logf("Bursty: %d events (%.1f%% success)", actual, float64(actual)/float64(expected)*100)
}

// TestStressResourceLimits 资源限制压力测试
// 测试大量handlers注册时的内存和性能
func TestStressResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForSync()
	defer bus.Close()

	maxHandlers := 50000
	ids := make([]uint64, 0, maxHandlers)

	// 注册大量handlers
	for i := 0; i < maxHandlers; i++ {
		id := bus.On("stress.resource", func(e *beat.Event) error {
			return nil
		})
		ids = append(ids, id)

		// 定期检查内存
		if i%10000 == 0 && i > 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			t.Logf("Registered %d handlers, memory: %d MB", i, m.Alloc/1024/1024)

			// 内存超过500MB则停止
			if m.Alloc > 500*1024*1024 {
				t.Logf("Stopping at %d handlers (memory limit)", i)
				break
			}
		}
	}

	// 清理
	for _, id := range ids {
		bus.Off(id)
	}

	t.Logf("Resource test: %d handlers registered", len(ids))
}

// TestStressMixedOperations 混合操作压力测试
// 同时进行订阅、取消、发布、模式匹配等操作
func TestStressMixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForAsync()
	defer bus.Close()

	var processed int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Goroutine 1: 持续订阅和取消
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				id := bus.On("mixed.test", func(e *beat.Event) error {
					atomic.AddInt64(&processed, 1)
					return nil
				})
				time.Sleep(10 * time.Millisecond)
				bus.Off(id)
			}
		}
	}()

	// Goroutine 2: 持续发布精确匹配事件
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				evt := &beat.Event{Type: "mixed.test", Data: []byte("exact")}
				bus.Emit(evt)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: 持续发布模式匹配事件
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.On("mixed.*", func(e *beat.Event) error {
			atomic.AddInt64(&processed, 1)
			return nil
		})
		for {
			select {
			case <-stop:
				return
			default:
				evt := &beat.Event{Type: "mixed.pattern", Data: []byte("pattern")}
				bus.EmitMatch(evt)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// 运行5秒
	time.Sleep(5 * time.Second)
	close(stop)
	wg.Wait()

	t.Logf("Mixed operations: %d events processed", atomic.LoadInt64(&processed))
}

// TestStressSlowHandlers 慢handler压力测试
func TestStressSlowHandlers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForSync()
	defer bus.Close()

	var processed int64

	// 注册慢handlers
	for i := 0; i < 10; i++ {
		bus.On("slow.test", func(e *beat.Event) error {
			atomic.AddInt64(&processed, 1)
			time.Sleep(50 * time.Millisecond) // 模拟慢处理
			return nil
		})
	}

	start := time.Now()

	// 发射10个事件
	for i := 0; i < 10; i++ {
		evt := &beat.Event{Type: "slow.test", Data: []byte("slow")}
		bus.Emit(evt)
	}

	duration := time.Since(start)

	expected := int64(10 * 10) // 10 events * 10 handlers
	actual := atomic.LoadInt64(&processed)

	if actual != expected {
		t.Errorf("expected %d handlers called, got %d", expected, actual)
	}

	t.Logf("Slow handlers: %d calls in %v", actual, duration)

	// 因为是同步，应该至少花费 10 events * 50ms = 500ms
	if duration < 500*time.Millisecond {
		t.Errorf("expected at least 500ms, got %v", duration)
	}
}

// TestStressBatchEmit 批量发布压力测试
func TestStressBatchEmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	bus, _ := beat.ForFlow()

	var processed int64
	bus.On("batch.stress", func(e *beat.Event) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})

	// 准备100个批次，每批1000个事件
	batchSize := 1000
	batchCount := 100

	start := time.Now()

	for b := 0; b < batchCount; b++ {
		events := make([]*beat.Event, batchSize)
		for i := range events {
			events[i] = &beat.Event{Type: "batch.stress", Data: []byte("data")}
		}
		bus.EmitBatch(events)
	}

	// Flow bus 是异步的，EmitBatch 只是推入环形缓冲区
	// 需要 GracefulClose 等待所有事件消费完毕后再断言
	bus.GracefulClose(10 * time.Second)

	duration := time.Since(start)

	expected := int64(batchCount * batchSize)
	actual := atomic.LoadInt64(&processed)

	t.Logf("Batch emit: %d events in %v", actual, duration)
	t.Logf("Throughput: %.0f events/sec", float64(actual)/duration.Seconds())

	if actual != expected {
		t.Errorf("expected %d events, got %d", expected, actual)
	}
}
