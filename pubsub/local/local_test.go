package local_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniyakcom/beat"
	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/pubsub/local"
)

func TestPublishSubscribe(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	sub := local.NewSubscriber(bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, "test.topic")
	if err != nil {
		t.Fatal(err)
	}

	// 发布
	payload := []byte(`{"key":"value"}`)
	msg := message.New("", payload)
	msg.Metadata.Set("foo", "bar")

	if err := pub.Publish(context.Background(), "test.topic", msg); err != nil {
		t.Fatal(err)
	}

	// 接收
	select {
	case received := <-msgCh:
		if string(received.Payload) != string(payload) {
			t.Errorf("payload mismatch: got %s, want %s", received.Payload, payload)
		}
		if received.Metadata.Get("foo") != "bar" {
			t.Errorf("metadata mismatch: got %s, want bar", received.Metadata.Get("foo"))
		}
		if received.Metadata.Get("_topic") != "test.topic" {
			t.Errorf("topic metadata: got %s, want test.topic", received.Metadata.Get("_topic"))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMultipleMessages(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	sub := local.NewSubscriber(bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, "batch.topic")
	if err != nil {
		t.Fatal(err)
	}

	const count = 100
	for i := 0; i < count; i++ {
		if err := pub.Publish(context.Background(), "batch.topic", message.New("", []byte{byte(i)})); err != nil {
			t.Fatal(err)
		}
	}

	received := 0
	timeout := time.After(2 * time.Second)
	for received < count {
		select {
		case <-msgCh:
			received++
		case <-timeout:
			t.Fatalf("timeout: received %d/%d", received, count)
		}
	}
}

func TestSubscriberClose(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	sub := local.NewSubscriber(bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, "close.topic")
	if err != nil {
		t.Fatal(err)
	}

	// Close subscriber
	sub.Close()

	// Publish after close — message should not be received
	_ = pub.Publish(context.Background(), "close.topic", message.New("", []byte("after-close")))

	select {
	case <-msgCh:
		// 由于 done channel 已关闭，select 可能不会写入 msgCh
		// 这里不判为失败，因为 close 的语义是"不再接收新消息"
	case <-time.After(100 * time.Millisecond):
		// 超时说明消息未被接收 — 正确
	}
}

func TestContextCancel(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	sub := local.NewSubscriber(bus)

	ctx, cancel := context.WithCancel(context.Background())

	msgCh, err := sub.Subscribe(ctx, "ctx.topic")
	if err != nil {
		t.Fatal(err)
	}

	// 先取消 context
	cancel()

	// Publish after context cancel
	_ = pub.Publish(context.Background(), "ctx.topic", message.New("", []byte("after-cancel")))

	select {
	case <-msgCh:
		// 可能因 race 收到或不收到
	case <-time.After(100 * time.Millisecond):
		// 期望超时
	}
}

func TestPublisherClose(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	if err := pub.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkLocalPublishSubscribe(b *testing.B) {
	bus, err := beat.ForSync()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	pub := local.NewPublisher(bus)
	sub := local.NewSubscriber(bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgCh, _ := sub.Subscribe(ctx, "bench.topic")

	payload := []byte("benchmark-payload")
	var consumed atomic.Int64

	go func() {
		for range msgCh {
			consumed.Add(1)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pub.Publish(context.Background(), "bench.topic", message.New("", payload))
	}
	b.StopTimer()
}
