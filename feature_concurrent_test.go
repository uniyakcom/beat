package beat

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentOnOff 测试并发订阅和取消订阅
func TestConcurrentOnOff(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var wg sync.WaitGroup
	const goroutines = 100

	// 并发注册和取消
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			// 注册
			id := bus.On("concurrent.test", func(e *Event) error {
				return nil
			})

			// 短暂等待
			time.Sleep(time.Millisecond)

			// 取消
			bus.Off(id)
		}(i)
	}

	wg.Wait()
	// 测试通过表示未发生死锁或panic
}

// TestConcurrentEmit 测试并发发布事件
func TestConcurrentEmit(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var counter int64
	bus.On("concurrent.emit", func(e *Event) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	var wg sync.WaitGroup
	const goroutines = 100
	const eventsPerGoroutine = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				evt := &Event{
					Type: "concurrent.emit",
					Data: []byte("test"),
				}
				bus.Emit(evt)
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * eventsPerGoroutine)
	actual := atomic.LoadInt64(&counter)

	if actual != expected {
		t.Errorf("expected %d events, got %d", expected, actual)
	}
}

// TestConcurrentOnOffEmit 测试混合并发场景
func TestConcurrentOnOffEmit(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var counter int64
	var wg sync.WaitGroup

	// 持续订阅/取消的goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				id := bus.On("mixed.test", func(e *Event) error {
					atomic.AddInt64(&counter, 1)
					return nil
				})
				time.Sleep(time.Millisecond)
				bus.Off(id)
			}
		}()
	}

	// 持续发布事件的goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				evt := &Event{
					Type: "mixed.test",
					Data: []byte("data"),
				}
				bus.Emit(evt)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	// 测试通过表示未发生竞态或panic
	t.Logf("processed %d events in mixed concurrent scenario", counter)
}

// TestRaceConditions 竞态条件检测（需要-race运行）
func TestRaceConditions(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var wg sync.WaitGroup

	// 场景1：读写竞争
	var ids []uint64
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := bus.On("race.test", func(e *Event) error {
				return nil
			})
			mu.Lock()
			ids = append(ids, id)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// 场景2：发布竞争
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				evt := &Event{Type: "race.test", Data: []byte("test")}
				bus.Emit(evt)
			}
		}()
	}

	wg.Wait()

	// 场景3：取消订阅竞争
	for _, id := range ids {
		wg.Add(1)
		go func(handlerID uint64) {
			defer wg.Done()
			bus.Off(handlerID)
		}(id)
	}

	wg.Wait()
}

// TestConcurrentPatternMatching 并发模式匹配
func TestConcurrentPatternMatching(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var counter int64

	// 注册多个模式
	patterns := []string{"user.*", "order.*", "system.*", "log.*"}
	for _, pattern := range patterns {
		bus.On(pattern, func(e *Event) error {
			atomic.AddInt64(&counter, 1)
			return nil
		})
	}

	var wg sync.WaitGroup
	eventTypes := []string{
		"user.login", "user.logout",
		"order.created", "order.paid",
		"system.startup", "system.shutdown",
		"log.info", "log.error",
	}

	// 并发发射不同匹配的事件
	for _, eventType := range eventTypes {
		wg.Add(1)
		go func(et string) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				evt := &Event{Type: et, Data: []byte("data")}
				bus.EmitMatch(evt)
			}
		}(eventType)
	}

	wg.Wait()

	// 每个事件类型匹配1个pattern，共8种类型 × 50次 = 400
	expected := int64(len(eventTypes) * 50)
	actual := atomic.LoadInt64(&counter)

	if actual != expected {
		t.Errorf("expected %d pattern matches, got %d", expected, actual)
	}
}

// TestConcurrentBatchEmit 并发批量发布
func TestConcurrentBatchEmit(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var counter int64
	bus.On("batch.test", func(e *Event) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})

	var wg sync.WaitGroup
	const goroutines = 10
	const batchSize = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			events := make([]*Event, batchSize)
			for j := 0; j < batchSize; j++ {
				events[j] = &Event{
					Type: "batch.test",
					Data: []byte("data"),
				}
			}

			bus.EmitBatch(events)
		}()
	}

	wg.Wait()

	expected := int64(goroutines * batchSize)
	actual := atomic.LoadInt64(&counter)

	if actual != expected {
		t.Errorf("expected %d events in batch, got %d", expected, actual)
	}
}

// TestNestedHandlerOn 测试嵌套订阅（在handler中订阅）
func TestNestedHandlerOn(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var innerCalled bool

	bus.On("outer", func(e *Event) error {
		// 在handler中注册新订阅
		bus.On("inner", func(e *Event) error {
			innerCalled = true
			return nil
		})
		return nil
	})

	// 触发外层
	evt1 := &Event{Type: "outer", Data: []byte("data")}
	if err := bus.Emit(evt1); err != nil {
		t.Fatalf("outer emit failed: %v", err)
	}

	// 触发内层
	evt2 := &Event{Type: "inner", Data: []byte("data")}
	if err := bus.Emit(evt2); err != nil {
		t.Fatalf("inner emit failed: %v", err)
	}

	if !innerCalled {
		t.Error("nested handler not called")
	}
}

// TestNestedHandlerOff 测试嵌套取消订阅（在handler中取消）
func TestNestedHandlerOff(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var callCount int
	var id uint64

	id = bus.On("self-cancel", func(e *Event) error {
		callCount++
		if callCount == 1 {
			// 第一次调用时取消自己
			bus.Off(id)
		}
		return nil
	})

	// 第一次触发
	evt := &Event{Type: "self-cancel", Data: []byte("data")}
	bus.Emit(evt)

	// 第二次触发（应该不会调用）
	bus.Emit(evt)

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// TestConcurrentClose 测试并发关闭
func TestConcurrentClose(t *testing.T) {
	bus, _ := ForSync()

	bus.On("test", func(e *Event) error {
		return nil
	})

	var wg sync.WaitGroup

	// 并发发射事件
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				evt := &Event{Type: "test", Data: []byte("data")}
				bus.Emit(evt)
			}
		}()
	}

	// 等待一段时间后关闭
	time.Sleep(10 * time.Millisecond)
	bus.Close()

	wg.Wait()
	// 测试通过表示Close()不会panic且能优雅处理并发
}
