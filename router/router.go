// Package router 提供消息路由器，连接 Subscriber 和 Publisher 形成处理管道。
//
// Router 是 beat 消息框架的调度中心：
//  1. 从 Subscriber 拉取消息
//  2. 通过中间件链处理
//  3. 将产出消息发布到 Publisher
//
// 提供两套 API：
//   - AddHandler：行业标准風格（subscribe → process → publish）
//   - On / Emit：beat 风格便捷 API
package router

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uniyakcom/beat/message"
)

// Router 消息路由器
type Router struct {
	handlers    []*Handler
	middlewares []Middleware
	plugins     []Plugin
	logger      *slog.Logger

	running     chan struct{}
	runningOnce sync.Once
	closed      chan struct{}
	closedOnce  sync.Once
	mu          sync.Mutex
	isRunning   bool
}

// Config 路由器配置
type Config struct {
	// Logger 自定义日志。为 nil 时使用 slog.Default()。
	Logger *slog.Logger
}

// NewRouter 创建路由器。
func NewRouter(cfg ...Config) *Router {
	var logger *slog.Logger
	if len(cfg) > 0 && cfg[0].Logger != nil {
		logger = cfg[0].Logger
	} else {
		logger = slog.Default()
	}
	return &Router{
		logger:  logger,
		running: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

// Use 添加全局中间件（对所有 Handler 生效）。
func (r *Router) Use(m ...Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

// Plugin 添加路由器插件（生命周期钩子）。
func (r *Router) Plugin(p ...Plugin) {
	r.plugins = append(r.plugins, p...)
}

// Handle 添加完整处理管道：subscribeTopic → handler → publishTopic。
//
//	r.Handle(
//	    "order.process",
//	    "order.created", sub,
//	    "order.processed", pub,
//	    func(msg *message.Message) ([]*message.Message, error) {
//	        // 处理订单
//	        return []*message.Message{result}, nil
//	    },
//	)
func (r *Router) Handle(
	name string,
	subscribeTopic string,
	subscriber message.Subscriber,
	publishTopic string,
	publisher message.Publisher,
	handlerFunc HandlerFunc,
) *Handler {
	h := &Handler{
		name:           name,
		subscribeTopic: subscribeTopic,
		subscriber:     subscriber,
		publishTopic:   publishTopic,
		publisher:      publisher,
		handlerFunc:    handlerFunc,
		logger:         r.logger.With("handler", name),
	}
	r.handlers = append(r.handlers, h)
	return h
}

// On 订阅主题并处理（不发布产出），beat 风格便捷 API。
//
//	r.On("user.signup", sub, func(msg *message.Message) error {
//	    // 发送欢迎邮件
//	    return nil
//	})
func (r *Router) On(
	name string,
	topic string,
	subscriber message.Subscriber,
	handlerFunc ConsumerFunc,
) *Handler {
	return r.Handle(
		name, topic, subscriber, "", nil,
		func(msg *message.Message) ([]*message.Message, error) {
			return nil, handlerFunc(msg)
		},
	)
}

// HandleBatch 添加批量处理管道：subscribeTopic → batchHandler → publishTopic。
//
// batchSize 条消息或 batchTimeout 超时后触发一次批量处理。
// 中间件对批量处理器不生效，批量函数本身即为处理单元。
func (r *Router) HandleBatch(
	name string,
	subscribeTopic string,
	subscriber message.Subscriber,
	publishTopic string,
	publisher message.Publisher,
	handlerFunc BatchFunc,
	batchSize int,
	batchTimeout time.Duration,
) *Handler {
	h := &Handler{
		name:           name,
		subscribeTopic: subscribeTopic,
		subscriber:     subscriber,
		publishTopic:   publishTopic,
		publisher:      publisher,
		batchFunc:      handlerFunc,
		batchSize:      batchSize,
		batchTimeout:   batchTimeout,
		logger:         r.logger.With("handler", name),
	}
	r.handlers = append(r.handlers, h)
	return h
}

// OnBatch 批量消费（不发布产出），beat 风格便捷 API。
func (r *Router) OnBatch(
	name string,
	topic string,
	subscriber message.Subscriber,
	handlerFunc func(msgs []*message.Message) error,
	batchSize int,
	batchTimeout time.Duration,
) *Handler {
	return r.HandleBatch(
		name, topic, subscriber, "", nil,
		func(msgs []*message.Message) ([]*message.Message, error) {
			return nil, handlerFunc(msgs)
		},
		batchSize, batchTimeout,
	)
}

// Run 启动路由器，阻塞直到 ctx 取消或发生致命错误。
//
// 流程：插件启动 → 构建中间件链 → 订阅全部主题 → 发出 Running 信号 → 消息循环。
// Running() channel 在所有订阅建立后才关闭，确保发布者可以安全使用。
func (r *Router) Run(ctx context.Context) error {
	// 启动插件
	for _, p := range r.plugins {
		if err := p.OnStart(ctx, r); err != nil {
			return err
		}
	}

	// 预建中间件链 + 订阅（在发出 Running 信号前完成）
	type handlerRun struct {
		handler *Handler
		fn      HandlerFunc
		msgCh   <-chan *message.Message
	}

	runs := make([]handlerRun, 0, len(r.handlers))
	for _, h := range r.handlers {
		// 仅对单条处理器构建中间件链（批量处理器跳过）
		var fn HandlerFunc
		if h.handlerFunc != nil {
			fn = h.handlerFunc
			for i := len(h.middlewares) - 1; i >= 0; i-- {
				fn = h.middlewares[i](fn)
			}
			for i := len(r.middlewares) - 1; i >= 0; i-- {
				fn = r.middlewares[i](fn)
			}
		}

		msgCh, err := h.subscriber.Subscribe(ctx, h.subscribeTopic)
		if err != nil {
			return err
		}

		h.logger.Info("handler subscribed", "topic", h.subscribeTopic)
		runs = append(runs, handlerRun{handler: h, fn: fn, msgCh: msgCh})
	}

	// 所有订阅就绪，发出 Running 信号
	r.mu.Lock()
	r.isRunning = true
	r.mu.Unlock()

	r.runningOnce.Do(func() { close(r.running) })

	// 启动消息循环
	var wg sync.WaitGroup
	errCh := make(chan error, len(runs))

	for _, run := range runs {
		wg.Add(1)
		go func(h *Handler, fn HandlerFunc, msgCh <-chan *message.Message) {
			defer wg.Done()
			var err error
			if h.batchFunc != nil {
				err = r.runBatchLoop(ctx, h, h.batchFunc, msgCh)
			} else if h.concurrency > 1 {
				err = r.runConcurrentLoop(ctx, h, fn, msgCh)
			} else {
				err = r.runSequentialLoop(ctx, h, fn, msgCh)
			}
			if err != nil {
				errCh <- err
			}
		}(run.handler, run.fn, run.msgCh)
	}

	// 等待 context 取消或致命错误
	select {
	case <-ctx.Done():
	case err := <-errCh:
		r.logger.Error("handler fatal error", "error", err)
	}

	// 关闭信号
	r.closedOnce.Do(func() { close(r.closed) })

	wg.Wait()

	// 清理插件
	for _, p := range r.plugins {
		p.OnStop(r)
	}

	r.mu.Lock()
	r.isRunning = false
	r.mu.Unlock()

	return nil
}

// Running 返回一个 channel，在 Router 开始运行后关闭。用于等待启动完成。
func (r *Router) Running() <-chan struct{} {
	return r.running
}

// Closed 返回一个 channel，在 Router 关闭后关闭。
func (r *Router) Closed() <-chan struct{} {
	return r.closed
}

// IsRunning 返回路由器是否正在运行。
func (r *Router) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isRunning
}

// Handlers 返回已注册的 Handler 列表（只读）。
func (r *Router) Handlers() []*Handler {
	return r.handlers
}

// ---------------------------------------------------------------------------
// 消息循环
// ---------------------------------------------------------------------------

// runSequentialLoop 顺序处理消息（单 goroutine，保证有序）。
func (r *Router) runSequentialLoop(ctx context.Context, h *Handler, fn HandlerFunc, msgCh <-chan *message.Message) error {
	h.logger.Info("sequential loop started", "topic", h.subscribeTopic)
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("handler stopped", "topic", h.subscribeTopic)
			return nil
		case msg, ok := <-msgCh:
			if !ok {
				h.logger.Info("subscription closed", "topic", h.subscribeTopic)
				return nil
			}
			r.processMessage(ctx, h, fn, msg)
		}
	}
}

// runConcurrentLoop 并发处理消息（多 goroutine，信号量背压）。
func (r *Router) runConcurrentLoop(ctx context.Context, h *Handler, fn HandlerFunc, msgCh <-chan *message.Message) error {
	limit := h.concurrency
	if h.maxInFlight > 0 && h.maxInFlight < limit {
		limit = h.maxInFlight
	}
	h.logger.Info("concurrent loop started", "topic", h.subscribeTopic, "workers", limit)

	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			h.logger.Info("handler stopped", "topic", h.subscribeTopic)
			return nil
		case msg, ok := <-msgCh:
			if !ok {
				wg.Wait()
				h.logger.Info("subscription closed", "topic", h.subscribeTopic)
				return nil
			}
			sem <- struct{}{} // 背压：达到上限时阻塞
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				r.processMessage(ctx, h, fn, msg)
			}()
		}
	}
}

// runBatchLoop 批量处理消息（累积 batchSize 条或 batchTimeout 超时后触发）。
func (r *Router) runBatchLoop(ctx context.Context, h *Handler, fn BatchFunc, msgCh <-chan *message.Message) error {
	h.logger.Info("batch loop started", "topic", h.subscribeTopic,
		"batchSize", h.batchSize, "batchTimeout", h.batchTimeout)

	batch := make([]*message.Message, 0, h.batchSize)
	timer := time.NewTimer(h.batchTimeout)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.processBatch(ctx, h, fn, batch)
		batch = batch[:0]
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(h.batchTimeout)
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			h.logger.Info("handler stopped", "topic", h.subscribeTopic)
			return nil
		case <-timer.C:
			flush()
			timer.Reset(h.batchTimeout)
		case msg, ok := <-msgCh:
			if !ok {
				flush()
				h.logger.Info("subscription closed", "topic", h.subscribeTopic)
				return nil
			}
			batch = append(batch, msg)
			if len(batch) >= h.batchSize {
				flush()
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 消息处理
// ---------------------------------------------------------------------------

// processMessage 处理单条消息：重试 → 执行 handler → 发布产出 → Ack/Nack → DLQ。
func (r *Router) processMessage(ctx context.Context, h *Handler, fn HandlerFunc, msg *message.Message) {
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("handler panic", "recovered", rec, "topic", h.subscribeTopic)
			h.sendToDLQ(ctx, msg, fmt.Errorf("panic: %v", rec))
			msg.Nack()
		}
	}()

	maxAttempts := h.maxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		producedMsgs, err := fn(msg)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts-1 {
				h.logger.Warn("handler retry", "error", err, "attempt", attempt+1, "uuid", msg.UUID)
			}
			continue
		}

		// 发布产出消息（使用消息自身的 context 传播超时等）
		if err := r.publishProduced(msg.Context(), h, producedMsgs); err != nil {
			lastErr = err
			if attempt < maxAttempts-1 {
				h.logger.Warn("publish retry", "error", err, "attempt", attempt+1)
			}
			continue
		}

		msg.Ack()
		return
	}

	// 所有重试耗尽
	h.logger.Error("handler failed", "error", lastErr, "uuid", msg.UUID, "retries", h.maxRetries)
	h.sendToDLQ(ctx, msg, lastErr)
	msg.Nack()
}

// processBatch 处理一批消息：执行 batchHandler → 发布产出 → 全部 Ack/Nack。
func (r *Router) processBatch(ctx context.Context, h *Handler, fn BatchFunc, msgs []*message.Message) {
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("batch handler panic", "recovered", rec, "count", len(msgs))
			for _, m := range msgs {
				m.Nack()
			}
		}
	}()

	producedMsgs, err := fn(msgs)
	if err != nil {
		h.logger.Error("batch handler error", "error", err, "count", len(msgs))
		for _, m := range msgs {
			m.Nack()
		}
		return
	}

	if err := r.publishProduced(ctx, h, producedMsgs); err != nil {
		h.logger.Error("batch publish error", "error", err)
		for _, m := range msgs {
			m.Nack()
		}
		return
	}

	for _, m := range msgs {
		m.Ack()
	}
}

// publishProduced 发布产出消息，支持动态路由。
func (r *Router) publishProduced(ctx context.Context, h *Handler, msgs []*message.Message) error {
	if h.publisher == nil || len(msgs) == 0 {
		return nil
	}

	// 无动态路由：一次性发布
	if h.topicFunc == nil {
		if h.publishTopic == "" {
			return nil
		}
		return h.publisher.Publish(ctx, h.publishTopic, msgs...)
	}

	// 动态路由：按 topic 分组
	groups := make(map[string][]*message.Message, 1)
	for _, m := range msgs {
		t := h.resolveTopic(m)
		if t != "" {
			groups[t] = append(groups[t], m)
		}
	}
	for topic, batch := range groups {
		if err := h.publisher.Publish(ctx, topic, batch...); err != nil {
			return err
		}
	}
	return nil
}
