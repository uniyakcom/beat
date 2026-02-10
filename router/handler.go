package router

import (
	"log/slog"

	"github.com/uniyakcom/beat/message"
)

// HandlerFunc 消息处理函数签名
//
// 接收一条消息，返回零或多条产出消息和可选 error。
// 产出消息将自动发布到 Handler 配置的 publishTopic。
// 返回 nil, nil 表示消息已处理完毕，无需转发。
type HandlerFunc func(msg *message.Message) ([]*message.Message, error)

// NoPublishHandlerFunc 无发布的处理函数签名（仅消费，不产出）
//
// 用于 Router.On() 便捷方法，适合纯消费场景（写DB、发通知等）。
type NoPublishHandlerFunc func(msg *message.Message) error

// Handler 路由处理器配置
type Handler struct {
	name           string
	subscribeTopic string
	subscriber     message.Subscriber
	publishTopic   string
	publisher      message.Publisher
	handlerFunc    HandlerFunc
	middlewares    []Middleware
	logger         *slog.Logger

	// beat 特有选项
	priority     Priority
	backpressure bool
}

// AddMiddleware 为此 Handler 添加专属中间件（不影响其他 Handler）。
func (h *Handler) AddMiddleware(m ...Middleware) *Handler {
	h.middlewares = append(h.middlewares, m...)
	return h
}

// WithPriority 设置处理优先级。
//
//	PriorityHigh   → 低延迟直调（对应 beat Sync）
//	PriorityNormal → 高吞吐异步（对应 beat Async）
//	PriorityLow    → 批量聚合（对应 beat Flow）
func (h *Handler) WithPriority(p Priority) *Handler {
	h.priority = p
	return h
}

// WithBackpressure 启用自适应背压。
// 消费者变慢时自动降低拉取速率，避免内存堆积。
func (h *Handler) WithBackpressure() *Handler {
	h.backpressure = true
	return h
}

// Priority 处理优先级
type Priority int

const (
	PriorityNormal Priority = iota // 默认：均衡
	PriorityHigh                   // 高优先：低延迟直调
	PriorityLow                    // 低优先：批量聚合
)
