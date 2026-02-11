// Package local 提供基于 beat 引擎的本地 Publisher/Subscriber 实现。
//
// 将 beat 的高性能进程内事件总线包装为 message.Publisher/Subscriber 接口，
// 实现本地消息传递。这是 beat 消息框架的默认传输层：
//   - 零网络开销：消息在内存中流转
//   - 继承 beat 引擎性能：单线程 ~9ns、并发 ~83ns
//   - 三预设自动映射：通过 Handler.WithPriority() 选择 Sync/Async/Flow
//
// 用法：
//
//	bus := beat.ForSync()
//	pub := local.NewPublisher(bus)
//	sub := local.NewSubscriber(bus)
//
//	r := router.NewRouter()
//	r.On("order.created", sub, handleOrder)
//	r.Run(ctx)
package local

import (
	"context"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/message"
)

// Publisher 基于 beat Bus 的本地发布者
type Publisher struct {
	bus core.Bus
}

// NewPublisher 创建本地发布者。
func NewPublisher(bus core.Bus) *Publisher {
	return &Publisher{bus: bus}
}

// Publish 将消息转换为 core.Event 并通过 beat Bus 发布。
//
// 映射规则：
//   - topic → Event.Type
//   - msg.Payload → Event.Data
//   - msg.UUID → Event.ID
//   - msg.Metadata → Event.Metadata
func (p *Publisher) Publish(_ context.Context, topic string, messages ...*message.Message) error {
	for _, msg := range messages {
		evt := &core.Event{
			Type:     topic,
			Data:     msg.Payload,
			ID:       msg.UUID,
			Metadata: make(map[string]string, len(msg.Metadata)),
		}
		for k, v := range msg.Metadata {
			evt.Metadata[k] = v
		}
		if err := p.bus.Emit(evt); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭发布者。本地实现无需清理资源。
func (p *Publisher) Close() error {
	return nil
}
