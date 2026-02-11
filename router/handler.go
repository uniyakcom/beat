package router

import (
	"context"
	"log/slog"
	"time"

	"github.com/uniyakcom/beat/message"
)

// HandlerFunc 消息处理函数（单条）
//
// 接收一条消息，返回零或多条产出消息和可选 error。
// 产出消息发布到 Handler 配置的 publishTopic（或 topicFunc 动态路由）。
type HandlerFunc func(msg *message.Message) ([]*message.Message, error)

// ConsumerFunc 纯消费处理函数（无产出）
type ConsumerFunc func(msg *message.Message) error

// BatchFunc 批量处理函数
//
// 接收一批消息，返回零或多条产出消息和可选 error。
// 配合 Batch 使用，适用于批量入库、聚合计算等场景。
type BatchFunc func(msgs []*message.Message) ([]*message.Message, error)

// DLQConfig 死信队列配置
type DLQConfig struct {
	// Topic 死信队列 topic
	Topic string
	// Publisher 死信队列发布者（可与主 Publisher 不同）
	Publisher message.Publisher
}

// Handler 消息处理器
type Handler struct {
	name           string
	subscribeTopic string
	subscriber     message.Subscriber
	publishTopic   string
	publisher      message.Publisher
	handlerFunc    HandlerFunc
	batchFunc      BatchFunc
	middlewares    []Middleware
	logger         *slog.Logger

	// 并发
	concurrency int // worker 数（0/1 = 顺序）

	// 批量
	batchSize    int           // 批量大小（0 = 单条模式）
	batchTimeout time.Duration // 批量超时

	// 动态路由
	topicFunc func(*message.Message) string // 动态 topic（nil = 配置的 publishTopic）

	// 容错
	maxRetries int        // 处理失败最大重试（0 = 不重试，由 retry 中间件控制）
	dlq        *DLQConfig // 死信队列

	// 流控
	maxInFlight int // 最大在途消息（0 = 无限）
}

// AddMiddleware 添加 Handler 专属中间件。
func (h *Handler) AddMiddleware(m ...Middleware) *Handler {
	h.middlewares = append(h.middlewares, m...)
	return h
}

// Workers 设置并发 worker 数。
// n > 1 时多 goroutine 并行处理，消息间无序。
// n <= 1 时顺序处理（默认），保证消息有序。
func (h *Handler) Workers(n int) *Handler {
	h.concurrency = n
	return h
}

// Batch 启用批量处理模式。
// 累积 size 条消息或 timeout 超时后，将一批消息交给 BatchFunc 处理。
func (h *Handler) Batch(size int, timeout time.Duration) *Handler {
	h.batchSize = size
	h.batchTimeout = timeout
	return h
}

// Route 设置动态路由函数。
// 对每条产出消息调用 fn，返回目标 topic；返回空字符串时使用默认 publishTopic。
func (h *Handler) Route(fn func(*message.Message) string) *Handler {
	h.topicFunc = fn
	return h
}

// Retry 设置路由器级最大重试次数。
// 超过次数后 Nack 消息（或发送到 DLQ）。
// 注意：与 retry 中间件独立，此为路由器层面的兆底重试。
func (h *Handler) Retry(n int) *Handler {
	h.maxRetries = n
	return h
}

// DLQ 配置死信队列。
// 处理失败且超过重试次数的消息将被发送到 DLQ。
func (h *Handler) DLQ(cfg DLQConfig) *Handler {
	h.dlq = &cfg
	return h
}

// InFlight 设置最大在途消息数（背压控制）。
// 达到上限时暂停从 Subscriber 拉取，直到有消息处理完成。
func (h *Handler) InFlight(n int) *Handler {
	h.maxInFlight = n
	return h
}

// resolveTopic 解析产出消息的目标 topic。
func (h *Handler) resolveTopic(msg *message.Message) string {
	if h.topicFunc != nil {
		if t := h.topicFunc(msg); t != "" {
			return t
		}
	}
	return h.publishTopic
}

// sendToDLQ 将消息发送到死信队列。
func (h *Handler) sendToDLQ(ctx context.Context, msg *message.Message, reason error) {
	if h.dlq == nil || h.dlq.Publisher == nil {
		return
	}
	msg.Metadata.Set("dlq_reason", reason.Error())
	msg.Metadata.Set("dlq_handler", h.name)
	msg.Metadata.Set("dlq_topic", h.subscribeTopic)
	if err := h.dlq.Publisher.Publish(ctx, h.dlq.Topic, msg); err != nil {
		h.logger.Error("DLQ 发布失败", "error", err, "handler", h.name)
	}
}
