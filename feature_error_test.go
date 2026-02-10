package beat

import (
	"errors"
	"testing"
)

// TestErrorHandling 测试handler返回错误
func TestErrorHandling(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	expectedErr := errors.New("handler error")

	bus.On("error.test", func(e *Event) error {
		return expectedErr
	})

	evt := &Event{Type: "error.test", Data: []byte("data")}
	err := bus.Emit(evt)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

// TestMultipleHandlerErrors 测试多个handler返回错误
func TestMultipleHandlerErrors(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	err1 := errors.New("handler 1 error")
	err2 := errors.New("handler 2 error")

	bus.On("multi.error", func(e *Event) error {
		return err1
	})
	bus.On("multi.error", func(e *Event) error {
		return err2
	})

	evt := &Event{Type: "multi.error", Data: []byte("data")}
	err := bus.Emit(evt)

	if err == nil {
		t.Fatal("expected error from multiple handlers, got nil")
	}

	// 验证至少包含一个错误
	if !errors.Is(err, err1) && !errors.Is(err, err2) {
		t.Errorf("expected error containing err1 or err2, got %v", err)
	}
}

// TestOffInvalidID 测试取消无效的订阅ID
func TestOffInvalidID(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	// 取消不存在的ID（不应该panic）
	bus.Off(99999)

	// 重复取消
	id := bus.On("test", func(e *Event) error { return nil })
	bus.Off(id)
	bus.Off(id) // 第二次取消
}

// TestEmitBatchWithErrors 测试批量发布时的错误处理
func TestEmitBatchWithErrors(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	expectedErr := errors.New("batch error")

	bus.On("batch.error", func(e *Event) error {
		return expectedErr
	})

	events := []*Event{
		{Type: "batch.error", Data: []byte("1")},
		{Type: "batch.error", Data: []byte("2")},
		{Type: "batch.error", Data: []byte("3")},
	}

	err := bus.EmitBatch(events)
	if err == nil {
		t.Error("expected error from batch emit, got nil")
	}
}

// TestEmitMatchNoMatch 测试模式匹配无匹配项
func TestEmitMatchNoMatch(t *testing.T) {
	bus, _ := ForSync()
	defer bus.Close()

	bus.On("user.*", func(e *Event) error {
		t.Error("should not be called - pattern doesn't match")
		return nil
	})

	evt := &Event{Type: "order.created", Data: []byte("data")}
	err := bus.EmitMatch(evt)

	if err != nil {
		t.Errorf("EmitMatch with no matches should not error, got %v", err)
	}
}
