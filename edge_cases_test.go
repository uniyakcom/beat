package beat

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestEdgeCaseZeroHandlers 测试零handler场景
func TestEdgeCaseZeroHandlers(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// 没有注册任何handler，直接发射
	evt := &Event{Type: "no.handlers", Data: []byte("data")}
	err := bus.Emit(evt)

	if err != nil {
		t.Errorf("emit with no handlers should not error, got %v", err)
	}
}

// TestEdgeCaseEmptyData 测试空数据
func TestEdgeCaseEmptyData(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var called bool
	bus.On("empty.data", func(e *Event) error {
		called = true
		if len(e.Data) != 0 {
			t.Error("expected empty data")
		}
		return nil
	})

	// nil data
	evt1 := &Event{Type: "empty.data", Data: nil}
	bus.Emit(evt1)

	// empty slice
	evt2 := &Event{Type: "empty.data", Data: []byte{}}
	bus.Emit(evt2)

	if !called {
		t.Error("handler not called for empty data events")
	}
}

// TestEdgeCaseLargeEventData 测试大数据事件
func TestEdgeCaseLargeEventData(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var received int
	bus.On("large.data", func(e *Event) error {
		received = len(e.Data)
		return nil
	})

	// 1MB数据
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	evt := &Event{Type: "large.data", Data: largeData}
	err := bus.Emit(evt)

	if err != nil {
		t.Errorf("large event failed: %v", err)
	}
	if received != len(largeData) {
		t.Errorf("expected %d bytes, got %d", len(largeData), received)
	}
}

// TestEdgeCaseDuplicateOff 测试重复取消订阅
func TestEdgeCaseDuplicateOff(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	id := bus.On("dup.off", func(e *Event) error { return nil })

	// 第一次取消
	bus.Off(id)

	// 重复取消（不应panic）
	bus.Off(id)
	bus.Off(id)
	bus.Off(id)
}

// TestEdgeCaseOffZeroID 测试取消ID为0的订阅
func TestEdgeCaseOffZeroID(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// Off(0)不应panic
	bus.Off(0)
}

// TestEdgeCaseManyHandlers 测试大量handler
func TestEdgeCaseManyHandlers(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	const handlerCount = 1000
	var callCount int

	// 注册1000个handler
	for i := 0; i < handlerCount; i++ {
		bus.On("many.handlers", func(e *Event) error {
			callCount++
			return nil
		})
	}

	evt := &Event{Type: "many.handlers", Data: []byte("data")}
	err := bus.Emit(evt)

	if err != nil {
		t.Errorf("emit with many handlers failed: %v", err)
	}
	if callCount != handlerCount {
		t.Errorf("expected %d calls, got %d", handlerCount, callCount)
	}
}

// TestEdgeCaseVeryLongEventType 测试超长事件类型
func TestEdgeCaseVeryLongEventType(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// 1KB的事件类型名
	longType := string(make([]byte, 1024))

	var called bool
	bus.On(longType, func(e *Event) error {
		called = true
		return nil
	})

	evt := &Event{Type: longType, Data: []byte("data")}
	err := bus.Emit(evt)

	if err != nil {
		t.Errorf("very long event type failed: %v", err)
	}
	if !called {
		t.Error("handler not called for long event type")
	}
}

// TestEdgeCaseSpecialCharsEventType 测试特殊字符事件类型
func TestEdgeCaseSpecialCharsEventType(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	specialTypes := []string{
		"user:login",
		"order/created",
		"system@startup",
		"log#error",
		"event-name",
		"event_name",
	}

	for _, eventType := range specialTypes {
		t.Run(eventType, func(t *testing.T) {
			var called bool
			bus.On(eventType, func(e *Event) error {
				called = true
				return nil
			})

			evt := &Event{Type: eventType, Data: []byte("data")}
			err := bus.Emit(evt)

			if err != nil {
				t.Errorf("special char event type %q failed: %v", eventType, err)
			}
			if !called {
				t.Errorf("handler not called for %q", eventType)
			}
		})
	}
}

// TestEdgeCaseNestedWildcards 测试嵌套通配符
func TestEdgeCaseNestedWildcards(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var callCount int32
	bus.On("user.*.action.*", func(e *Event) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// 应该匹配
	evt1 := &Event{Type: "user.123.action.login", Data: []byte("data")}
	bus.EmitMatch(evt1)

	// 不应该匹配
	evt2 := &Event{Type: "user.123.login", Data: []byte("data")}
	bus.EmitMatch(evt2)

	if actual := atomic.LoadInt32(&callCount); actual != 1 {
		t.Errorf("nested wildcard should match exactly 1, got %d", actual)
	}
}

// TestEdgeCaseBatchEmpty 测试空批量
func TestEdgeCaseBatchEmpty(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// 空批量
	err := bus.EmitBatch(nil)
	if err != nil {
		t.Errorf("empty batch should not error, got %v", err)
	}

	// 空slice
	err = bus.EmitBatch([]*Event{})
	if err != nil {
		t.Errorf("empty slice batch should not error, got %v", err)
	}
}

// TestEdgeCaseBatchSingle 测试单事件批量
func TestEdgeCaseBatchSingle(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var called bool
	bus.On("batch.single", func(e *Event) error {
		called = true
		return nil
	})

	events := []*Event{{Type: "batch.single", Data: []byte("data")}}
	err := bus.EmitBatch(events)

	if err != nil {
		t.Errorf("single event batch failed: %v", err)
	}
	if !called {
		t.Error("handler not called for single event batch")
	}
}

// TestEdgeCaseRapidOnOff 测试快速注册/取消
func TestEdgeCaseRapidOnOff(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// 快速注册和取消1000次
	for i := 0; i < 1000; i++ {
		id := bus.On("rapid.test", func(e *Event) error { return nil })
		bus.Off(id)
	}
}

// TestEdgeCaseSlowHandler 测试慢handler
func TestEdgeCaseSlowHandler(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var called bool
	bus.On("slow.handler", func(e *Event) error {
		time.Sleep(100 * time.Millisecond) // 模拟慢处理
		called = true
		return nil
	})

	start := time.Now()
	evt := &Event{Type: "slow.handler", Data: []byte("data")}
	err := bus.Emit(evt)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("slow handler failed: %v", err)
	}
	if !called {
		t.Error("slow handler not called")
	}
	if duration < 100*time.Millisecond {
		t.Error("slow handler completed too quickly")
	}
}

// TestEdgeCaseEventReuse 测试事件对象重用
func TestEdgeCaseEventReuse(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	var callCount int
	bus.On("reuse.test", func(e *Event) error {
		callCount++
		return nil
	})

	// 重用同一事件对象
	evt := &Event{Type: "reuse.test", Data: []byte("data")}
	for i := 0; i < 10; i++ {
		bus.Emit(evt)
	}

	if callCount != 10 {
		t.Errorf("expected 10 calls with reused event, got %d", callCount)
	}
}
