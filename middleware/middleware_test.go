package middleware_test

import (
	"errors"
	"testing"
	"time"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/middleware/correlation"
	"github.com/uniyakcom/beat/middleware/recoverer"
	"github.com/uniyakcom/beat/middleware/retry"
	"github.com/uniyakcom/beat/middleware/timeout"
	"github.com/uniyakcom/beat/router"
)

func TestRetryMiddleware(t *testing.T) {
	var attempts int

	mw := retry.New(retry.Config{
		MaxRetries:      2,
		InitialInterval: 10 * time.Millisecond,
	})

	handler := mw(func(msg *message.Message) ([]*message.Message, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("transient error")
		}
		return nil, nil
	})

	msg := message.NewMessage("", []byte("test"))
	_, err := handler(msg)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryExhausted(t *testing.T) {
	mw := retry.New(retry.Config{
		MaxRetries:      2,
		InitialInterval: 10 * time.Millisecond,
	})

	var attempts int
	handler := mw(func(msg *message.Message) ([]*message.Message, error) {
		attempts++
		return nil, errors.New("persistent error")
	})

	msg := message.NewMessage("", []byte("test"))
	_, err := handler(msg)

	if err == nil {
		t.Error("expected error after retry exhaustion")
	}
	if attempts != 3 { // 1 initial + 2 retries
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryShouldRetry(t *testing.T) {
	var errNoRetry = errors.New("no retry")

	mw := retry.New(retry.Config{
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, errNoRetry)
		},
	})

	var attempts int
	handler := mw(func(msg *message.Message) ([]*message.Message, error) {
		attempts++
		return nil, errNoRetry
	})

	msg := message.NewMessage("", []byte("test"))
	_, err := handler(msg)

	if !errors.Is(err, errNoRetry) {
		t.Errorf("expected errNoRetry, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (should not retry)", attempts)
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	mw := timeout.New(50 * time.Millisecond)

	handler := mw(func(msg *message.Message) ([]*message.Message, error) {
		select {
		case <-msg.Context().Done():
			return nil, msg.Context().Err()
		case <-time.After(200 * time.Millisecond):
			return nil, nil
		}
	})

	msg := message.NewMessage("", []byte("test"))
	_, err := handler(msg)

	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestRecovererMiddleware(t *testing.T) {
	mw := recoverer.New()

	handler := mw(func(msg *message.Message) ([]*message.Message, error) {
		panic("test panic")
	})

	msg := message.NewMessage("", []byte("test"))
	_, err := handler(msg)

	if err == nil {
		t.Fatal("expected error from panic recovery")
	}

	var pe *recoverer.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PanicError, got %T", err)
	}
	if pe.Value != "test panic" {
		t.Errorf("panic value = %v, want 'test panic'", pe.Value)
	}
}

func TestCorrelationMiddleware(t *testing.T) {
	mw := correlation.New()

	t.Run("generates ID if missing", func(t *testing.T) {
		handler := mw(func(msg *message.Message) ([]*message.Message, error) {
			return []*message.Message{message.NewMessage("", nil)}, nil
		})

		msg := message.NewMessage("", nil)
		produced, err := handler(msg)
		if err != nil {
			t.Fatal(err)
		}

		id := msg.Metadata.Get(correlation.HeaderCorrelationID)
		if id == "" {
			t.Error("correlation_id should be generated")
		}

		if len(produced) != 1 {
			t.Fatalf("produced %d messages, want 1", len(produced))
		}
		if produced[0].Metadata.Get(correlation.HeaderCorrelationID) != id {
			t.Error("correlation_id should propagate to produced messages")
		}
	})

	t.Run("preserves existing ID", func(t *testing.T) {
		handler := mw(func(msg *message.Message) ([]*message.Message, error) {
			return nil, nil
		})

		msg := message.NewMessage("", nil)
		msg.Metadata.Set(correlation.HeaderCorrelationID, "existing-id")
		_, _ = handler(msg)

		if msg.Metadata.Get(correlation.HeaderCorrelationID) != "existing-id" {
			t.Error("existing correlation_id should be preserved")
		}
	})
}

func TestMiddlewareChaining(t *testing.T) {
	var order []string

	mw1 := func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			order = append(order, "mw1-before")
			result, err := h(msg)
			order = append(order, "mw1-after")
			return result, err
		}
	}

	mw2 := func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			order = append(order, "mw2-before")
			result, err := h(msg)
			order = append(order, "mw2-after")
			return result, err
		}
	}

	handler := router.HandlerFunc(func(msg *message.Message) ([]*message.Message, error) {
		order = append(order, "handler")
		return nil, nil
	})

	// 洋葱模型: mw1 wraps mw2 wraps handler
	wrapped := mw1(mw2(handler))

	msg := message.NewMessage("", nil)
	_, _ = wrapped(msg)

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(expected), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}
