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
	"log/slog"
	"sync"

	"github.com/uniyakcom/beat/message"
)

// Router 消息路由器
type Router struct {
	handlers    []*Handler
	middlewares []Middleware
	plugins     []RouterPlugin
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

// AddMiddleware 添加全局中间件（对所有 Handler 生效）。
func (r *Router) AddMiddleware(m ...Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

// AddPlugin 添加路由器插件（生命周期钩子）。
func (r *Router) AddPlugin(p ...RouterPlugin) {
	r.plugins = append(r.plugins, p...)
}

// AddHandler 添加完整处理管道：subscribeTopic → handler → publishTopic。
//
//	r.AddHandler(
//	    "order.process",
//	    "order.created", sub,
//	    "order.processed", pub,
//	    func(msg *message.Message) ([]*message.Message, error) {
//	        // 处理订单
//	        return []*message.Message{result}, nil
//	    },
//	)
func (r *Router) AddHandler(
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
	handlerFunc NoPublishHandlerFunc,
) *Handler {
	return r.AddHandler(
		name, topic, subscriber, "", nil,
		func(msg *message.Message) ([]*message.Message, error) {
			return nil, handlerFunc(msg)
		},
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
		fn := h.handlerFunc
		for i := len(h.middlewares) - 1; i >= 0; i-- {
			fn = h.middlewares[i](fn)
		}
		for i := len(r.middlewares) - 1; i >= 0; i-- {
			fn = r.middlewares[i](fn)
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
			if err := r.runHandlerLoop(ctx, h, fn, msgCh); err != nil {
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

// runHandlerLoop 运行单个 Handler 的消息循环（订阅已在 Run 中完成）。
func (r *Router) runHandlerLoop(ctx context.Context, h *Handler, fn HandlerFunc, msgCh <-chan *message.Message) error {
	h.logger.Info("handler started", "topic", h.subscribeTopic)

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
			r.processMessage(h, fn, msg)
		}
	}
}

// processMessage 处理单条消息：执行 handler、发布产出、Ack/Nack。
func (r *Router) processMessage(h *Handler, fn HandlerFunc, msg *message.Message) {
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("handler panic", "recovered", rec, "topic", h.subscribeTopic)
			msg.Nack()
		}
	}()

	producedMsgs, err := fn(msg)
	if err != nil {
		h.logger.Error("handler error", "error", err, "uuid", msg.UUID)
		msg.Nack()
		return
	}

	// 发布产出消息
	if h.publisher != nil && h.publishTopic != "" && len(producedMsgs) > 0 {
		if err := h.publisher.Publish(h.publishTopic, producedMsgs...); err != nil {
			h.logger.Error("publish error", "error", err, "topic", h.publishTopic)
			msg.Nack()
			return
		}
	}

	msg.Ack()
}
