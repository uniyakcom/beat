package message_test

import (
	"context"
	"testing"

	"github.com/uniyakcom/beat/message"
)

func TestNew(t *testing.T) {
	msg := message.New("", []byte("hello"))

	if msg.UUID == "" {
		t.Error("UUID should be auto-generated")
	}
	if len(msg.UUID) != 36 {
		t.Errorf("UUID length = %d, want 36", len(msg.UUID))
	}
	if string(msg.Payload) != "hello" {
		t.Errorf("Payload = %q, want %q", msg.Payload, "hello")
	}
}

func TestNewMessageWithUUID(t *testing.T) {
	msg := message.New("custom-uuid", []byte("data"))
	if msg.UUID != "custom-uuid" {
		t.Errorf("UUID = %q, want %q", msg.UUID, "custom-uuid")
	}
}

func TestAckNack(t *testing.T) {
	t.Run("Ack", func(t *testing.T) {
		msg := message.New("", nil)
		msg.Ack()
		select {
		case <-msg.Acked():
			// OK
		default:
			t.Error("Acked() channel should be closed after Ack()")
		}
	})

	t.Run("Nack", func(t *testing.T) {
		msg := message.New("", nil)
		msg.Nack()
		select {
		case <-msg.Nacked():
			// OK
		default:
			t.Error("Nacked() channel should be closed after Nack()")
		}
	})

	t.Run("DoubleAck", func(t *testing.T) {
		msg := message.New("", nil)
		msg.Ack()
		msg.Ack() // should not panic
	})

	t.Run("DoubleNack", func(t *testing.T) {
		msg := message.New("", nil)
		msg.Nack()
		msg.Nack() // should not panic
	})
}

func TestMessageContext(t *testing.T) {
	msg := message.New("", nil)

	if msg.Context() == nil {
		t.Error("default context should not be nil")
	}

	ctx := context.WithValue(context.Background(), "key", "value")
	msg.SetContext(ctx)

	if msg.Context().Value("key") != "value" {
		t.Error("context value not preserved")
	}
}

func TestMessageCopy(t *testing.T) {
	orig := message.New("orig-uuid", []byte("payload"))
	orig.Metadata.Set("key", "value")

	cp := orig.Copy()

	// 新 UUID
	if cp.UUID == orig.UUID {
		t.Error("copy should have new UUID")
	}

	// 独立 payload
	if string(cp.Payload) != "payload" {
		t.Errorf("copy payload = %q, want %q", cp.Payload, "payload")
	}
	cp.Payload[0] = 'X'
	if orig.Payload[0] == 'X' {
		t.Error("copy payload should be independent")
	}

	// 独立 metadata
	if cp.Metadata.Get("key") != "value" {
		t.Error("copy metadata should contain original values")
	}
	cp.Metadata.Set("key", "changed")
	if orig.Metadata.Get("key") == "changed" {
		t.Error("copy metadata should be independent")
	}
}

func TestMetadata(t *testing.T) {
	m := make(message.Metadata)
	m.Set("a", "1")

	if m.Get("a") != "1" {
		t.Error("Get should return set value")
	}
	if !m.Has("a") {
		t.Error("Has should return true for existing key")
	}
	if m.Has("b") {
		t.Error("Has should return false for missing key")
	}
	if m.Get("missing") != "" {
		t.Error("Get missing key should return empty string")
	}

	cp := m.Copy()
	cp.Set("a", "2")
	if m.Get("a") != "1" {
		t.Error("copy should be independent")
	}
}

func TestMetadataNil(t *testing.T) {
	var m message.Metadata
	if m.Get("key") != "" {
		t.Error("nil metadata Get should return empty string")
	}
	if m.Has("key") {
		t.Error("nil metadata Has should return false")
	}
}

func TestNewUUID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		uuid := message.NewUUID()
		if len(uuid) != 36 {
			t.Fatalf("UUID length = %d, want 36: %s", len(uuid), uuid)
		}
		// version 4
		if uuid[14] != '4' {
			t.Fatalf("UUID version byte = %c, want 4", uuid[14])
		}
		// variant 10xx
		variant := uuid[19]
		if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
			t.Fatalf("UUID variant byte = %c, want 8/9/a/b", variant)
		}
		if seen[uuid] {
			t.Fatalf("duplicate UUID: %s", uuid)
		}
		seen[uuid] = true
	}
}

func BenchmarkNew(b *testing.B) {
	payload := []byte("benchmark-payload")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = message.New("", payload)
	}
}

func BenchmarkNewUUID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = message.NewUUID()
	}
}
