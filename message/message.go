// Package message 提供消息框架核心类型定义。
//
// Message 是 beat 消息框架的基本传输单元，在 Publisher/Subscriber/Router 之间流转。
// 与 core.Event（进程内高速事件）不同，Message 面向跨进程/跨服务的可靠消息传递场景，
// 提供 UUID、Metadata、Ack/Nack 语义。
package message

import (
	"context"
	"sync/atomic"
	"time"
)

// 消息确认状态常量（CAS 替代 sync.Once，节省 ~16 字节/消息）
const (
	msgPending uint32 = 0 // 待处理
	msgAcked   uint32 = 1 // 已确认
	msgNacked  uint32 = 2 // 已拒绝
)

// Message 消息传输单元
//
// 在 Router 中流转：Subscriber 产出 → 中间件链 → HandlerFunc → Publisher 发布。
// 支持 Ack/Nack 确认机制，确保消息可靠处理。
type Message struct {
	// UUID 消息唯一标识（自动生成或外部指定）
	UUID string

	// Key 分区/路由键（Kafka 分区、Redis Stream 分组等）
	Key string

	// Metadata 消息元数据（链路追踪 ID、来源、优先级等）
	Metadata Metadata

	// Payload 消息负载（业务数据）
	Payload []byte

	// Timestamp 消息创建时间
	Timestamp time.Time

	ctx    context.Context
	ackCh  chan struct{}
	nackCh chan struct{}
	state  atomic.Uint32 // CAS 状态: pending/acked/nacked（替代 2 个 sync.Once）
}

// New 创建新消息。uuid 为空时自动生成。
func New(uuid string, payload []byte) *Message {
	if uuid == "" {
		uuid = NewUUID()
	}
	return &Message{
		UUID:      uuid,
		Metadata:  make(Metadata),
		Payload:   payload,
		Timestamp: time.Now(),
		ctx:       context.Background(),
		ackCh:     make(chan struct{}),
		nackCh:    make(chan struct{}),
	}
}

// NewPub 创建用于发布的轻量消息（不分配 Ack/Nack channel）。
//
// 适用于纯发布场景（不需要 Subscriber 端的 Ack/Nack 确认），
// 相比 New 减少 2 次 channel 分配，降低 GC 压力。
//
// 注意：对该消息调用 Ack()/Nack() 会 panic。如需 Ack 语义请使用 New。
func NewPub(uuid string, payload []byte) *Message {
	if uuid == "" {
		uuid = NewUUID()
	}
	return &Message{
		UUID:      uuid,
		Metadata:  make(Metadata),
		Payload:   payload,
		Timestamp: time.Now(),
		ctx:       context.Background(),
	}
}

// Ack 确认消息已成功处理。只能调用一次，重复调用安全但无效。
// 使用 CAS 替代 sync.Once，节省 ~16 字节/消息。
// 对 NewPub 创建的消息（无 ackCh）安全调用，直接忽略。
func (m *Message) Ack() {
	if m.ackCh == nil {
		return
	}
	if m.state.CompareAndSwap(msgPending, msgAcked) {
		close(m.ackCh)
	}
}

// Nack 拒绝消息（处理失败）。只能调用一次，重复调用安全但无效。
// 对 NewPub 创建的消息（无 nackCh）安全调用，直接忽略。
func (m *Message) Nack() {
	if m.nackCh == nil {
		return
	}
	if m.state.CompareAndSwap(msgPending, msgNacked) {
		close(m.nackCh)
	}
}

// Acked 返回一个 channel，在 Ack() 被调用后关闭。
func (m *Message) Acked() <-chan struct{} {
	return m.ackCh
}

// Nacked 返回一个 channel，在 Nack() 被调用后关闭。
func (m *Message) Nacked() <-chan struct{} {
	return m.nackCh
}

// Context 返回消息关联的 context。
func (m *Message) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// SetContext 设置消息关联的 context（用于超时、取消传播）。
func (m *Message) SetContext(ctx context.Context) {
	m.ctx = ctx
}

// Copy 深拷贝消息（新 UUID，独立的 Metadata 和 Payload）。
func (m *Message) Copy() *Message {
	payload := make([]byte, len(m.Payload))
	copy(payload, m.Payload)

	msg := New("", payload)
	msg.Key = m.Key
	msg.Timestamp = m.Timestamp
	for k, v := range m.Metadata {
		msg.Metadata[k] = v
	}
	return msg
}
