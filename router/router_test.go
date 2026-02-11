package router_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniyakcom/beat"
	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/pubsub/local"
	"github.com/uniyakcom/beat/router"
)

func TestRouterOn(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	sub := local.NewSubscriber(bus)
	pub := local.NewPublisher(bus)

	r := router.NewRouter()

	var received atomic.Int64

	r.On("test.handler", "order.created", sub, func(msg *message.Message) error {
		received.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = r.Run(ctx)
	}()

	// 等待 router 启动
	<-r.Running()

	// 发布消息
	for i := 0; i < 10; i++ {
		_ = pub.Publish(context.Background(), "order.created", message.New("", []byte("order-data")))
	}

	// 等待处理
	time.Sleep(200 * time.Millisecond)

	if got := received.Load(); got != 10 {
		t.Errorf("received %d messages, want 10", got)
	}

	cancel()
	<-r.Closed()
}

func TestRouterAddHandler(t *testing.T) {
	inBus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer inBus.Close()
	outBus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer outBus.Close()

	inSub := local.NewSubscriber(inBus)
	inPub := local.NewPublisher(inBus)
	outPub := local.NewPublisher(outBus)
	outSub := local.NewSubscriber(outBus)

	r := router.NewRouter()

	// 接收 → 转换 → 发布
	r.Handle(
		"transform",
		"input.topic", inSub,
		"output.topic", outPub,
		func(msg *message.Message) ([]*message.Message, error) {
			out := message.New("", append([]byte("processed:"), msg.Payload...))
			return []*message.Message{out}, nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())

	// 在 output bus 上订阅
	outCh, _ := outSub.Subscribe(ctx, "output.topic")

	go func() {
		_ = r.Run(ctx)
	}()
	<-r.Running()

	// 发布到 input
	_ = inPub.Publish(context.Background(), "input.topic", message.New("", []byte("hello")))

	select {
	case msg := <-outCh:
		got := string(msg.Payload)
		want := "processed:hello"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for output")
	}

	cancel()
	<-r.Closed()
}

func TestRouterMiddleware(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	sub := local.NewSubscriber(bus)
	pub := local.NewPublisher(bus)

	r := router.NewRouter()

	// 追踪中间件执行顺序（用 mutex 保护，避免竞态）
	var mu sync.Mutex
	var order []string

	r.Use(func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			mu.Lock()
			order = append(order, "global-before")
			mu.Unlock()
			result, err := h(msg)
			mu.Lock()
			order = append(order, "global-after")
			mu.Unlock()
			return result, err
		}
	})

	handler := r.On("mw.test", "mw.topic", sub, func(msg *message.Message) error {
		mu.Lock()
		order = append(order, "handler")
		mu.Unlock()
		return nil
	})

	handler.AddMiddleware(func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			mu.Lock()
			order = append(order, "handler-before")
			mu.Unlock()
			result, err := h(msg)
			mu.Lock()
			order = append(order, "handler-after")
			mu.Unlock()
			return result, err
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = r.Run(ctx)
	}()
	<-r.Running()

	_ = pub.Publish(context.Background(), "mw.topic", message.New("", []byte("test")))
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-r.Closed()

	// 洋葱模型：global-before → handler-before → handler → handler-after → global-after
	mu.Lock()
	snapshot := make([]string, len(order))
	copy(snapshot, order)
	mu.Unlock()

	expected := []string{"global-before", "handler-before", "handler", "handler-after", "global-after"}
	if len(snapshot) != len(expected) {
		t.Fatalf("order length %d, want %d: %v", len(snapshot), len(expected), snapshot)
	}
	for i, v := range expected {
		if snapshot[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, snapshot[i], v)
		}
	}
}

func TestRouterHandlerPanicRecovery(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	sub := local.NewSubscriber(bus)
	pub := local.NewPublisher(bus)

	r := router.NewRouter()

	var afterPanic atomic.Int64

	r.On("panic.handler", "panic.topic", sub, func(msg *message.Message) error {
		if string(msg.Payload) == "panic" {
			panic("test panic")
		}
		afterPanic.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = r.Run(ctx)
	}()
	<-r.Running()

	// 触发 panic
	_ = pub.Publish(context.Background(), "panic.topic", message.New("", []byte("panic")))
	time.Sleep(100 * time.Millisecond)

	// panic 后 handler 应继续运行
	_ = pub.Publish(context.Background(), "panic.topic", message.New("", []byte("normal")))
	time.Sleep(100 * time.Millisecond)

	if got := afterPanic.Load(); got != 1 {
		t.Errorf("afterPanic = %d, want 1", got)
	}

	cancel()
	<-r.Closed()
}

func TestRouterIsRunning(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	sub := local.NewSubscriber(bus)

	r := router.NewRouter()
	r.On("state.test", "state.topic", sub, func(msg *message.Message) error { return nil })

	if r.IsRunning() {
		t.Error("should not be running before Run()")
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = r.Run(ctx)
	}()
	<-r.Running()

	if !r.IsRunning() {
		t.Error("should be running after Run()")
	}

	cancel()
	<-r.Closed()

	// 给关闭一点时间完成
	time.Sleep(50 * time.Millisecond)

	if r.IsRunning() {
		t.Error("should not be running after close")
	}
}

func TestRouterHandlers(t *testing.T) {
	bus, err := beat.ForSync()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	sub := local.NewSubscriber(bus)

	r := router.NewRouter()
	r.On("h1", "t1", sub, func(msg *message.Message) error { return nil })
	r.On("h2", "t2", sub, func(msg *message.Message) error { return nil })

	if got := len(r.Handlers()); got != 2 {
		t.Errorf("handlers count = %d, want 2", got)
	}
}
