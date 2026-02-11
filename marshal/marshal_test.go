package marshal_test

import (
	"testing"

	"github.com/uniyakcom/beat/marshal"
	"github.com/uniyakcom/beat/message"
)

func TestJSONMarshalRoundTrip(t *testing.T) {
	m := marshal.JSON{}

	orig := message.New("test-uuid-123", []byte(`{"order_id":42}`))
	orig.Metadata.Set("source", "test")
	orig.Metadata.Set("priority", "high")

	data, err := m.Marshal("test.topic", orig)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := m.Unmarshal("test.topic", data)
	if err != nil {
		t.Fatal(err)
	}

	if restored.UUID != orig.UUID {
		t.Errorf("UUID = %q, want %q", restored.UUID, orig.UUID)
	}
	if string(restored.Payload) != string(orig.Payload) {
		t.Errorf("Payload = %q, want %q", restored.Payload, orig.Payload)
	}
	if restored.Metadata.Get("source") != "test" {
		t.Errorf("metadata source = %q, want %q", restored.Metadata.Get("source"), "test")
	}
	if restored.Metadata.Get("priority") != "high" {
		t.Errorf("metadata priority = %q, want %q", restored.Metadata.Get("priority"), "high")
	}
}

func TestJSONMarshalEmptyPayload(t *testing.T) {
	m := marshal.JSON{}

	orig := message.New("", nil)
	data, err := m.Marshal("topic", orig)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := m.Unmarshal("topic", data)
	if err != nil {
		t.Fatal(err)
	}

	if restored.UUID == "" {
		t.Error("UUID should not be empty")
	}
}

func TestJSONUnmarshalInvalid(t *testing.T) {
	m := marshal.JSON{}
	_, err := m.Unmarshal("topic", []byte("not-json"))
	if err == nil {
		t.Error("should return error for invalid JSON")
	}
}

func BenchmarkJSONMarshal(b *testing.B) {
	m := marshal.JSON{}
	msg := message.New("bench-uuid", []byte(`{"data":"benchmark"}`))
	msg.Metadata.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Marshal("topic", msg)
	}
}

func BenchmarkJSONUnmarshal(b *testing.B) {
	m := marshal.JSON{}
	msg := message.New("bench-uuid", []byte(`{"data":"benchmark"}`))
	msg.Metadata.Set("key", "value")
	data, _ := m.Marshal("topic", msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Unmarshal("topic", data)
	}
}
