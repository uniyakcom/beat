// Package message 提供消息框架核心类型定义。
//
// Message 是 beat 消息框架的基本传输单元，在 Publisher/Subscriber/Router 之间流转。
// 与 core.Event（进程内高速事件）不同，Message 面向跨进程/跨服务的可靠消息传递场景，
// 提供 UUID、Metadata、Ack/Nack 语义。
package message

import (
	"context"
	"sync"
)

// Message 消息传输单元
//
// 在 Router 中流转：Subscriber 产出 → 中间件链 → HandlerFunc → Publisher 发布。
// 支持 Ack/Nack 确认机制，确保消息可靠处理。
type Message struct {
	// UUID 消息唯一标识（自动生成或外部指定）
	UUID string

	// Metadata 消息元数据（链路追踪 ID、来源、优先级等）
	Metadata Metadata

	// Payload 消息负载（业务数据）
	Payload []byte

	ctx      context.Context
	ackCh    chan struct{}
	nackCh   chan struct{}
	ackOnce  sync.Once
	nackOnce sync.Once
}

// NewMessage 创建新消息。uuid 为空时自动生成。
func NewMessage(uuid string, payload []byte) *Message {
	if uuid == "" {
		uuid = NewUUID()
	}
	return &Message{
		UUID:     uuid,
		Metadata: make(Metadata),
		Payload:  payload,
		ctx:      context.Background(),
		ackCh:    make(chan struct{}),
		nackCh:   make(chan struct{}),
	}
}

// Ack 确认消息已成功处理。只能调用一次，重复调用安全但无效。
func (m *Message) Ack() {
	m.ackOnce.Do(func() {
		close(m.ackCh)
	})
}

// Nack 拒绝消息（处理失败）。只能调用一次，重复调用安全但无效。
func (m *Message) Nack() {
	m.nackOnce.Do(func() {
		close(m.nackCh)
	})
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

	msg := NewMessage("", payload)
	for k, v := range m.Metadata {
		msg.Metadata[k] = v
	}
	return msg
}
