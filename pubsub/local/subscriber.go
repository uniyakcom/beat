package local

import (
	"context"
	"sync"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/message"
)

// Subscriber 基于 beat Bus 的本地订阅者
type Subscriber struct {
	bus    core.Bus
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	subIDs []uint64 // 跟踪订阅 ID，Close 时清理
}

// NewSubscriber 创建本地订阅者。
func NewSubscriber(bus core.Bus) *Subscriber {
	return &Subscriber{
		bus:  bus,
		done: make(chan struct{}),
	}
}

// Subscribe 订阅主题，返回消息通道。
//
// 内部注册 beat On handler，将 core.Event 转换为 message.Message 后写入通道。
// 通道在 ctx 取消或 Subscriber.Close() 时停止接收新消息。
func (s *Subscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	output := make(chan *message.Message, 256)

	id := s.bus.On(topic, func(e *core.Event) error {
		msg := message.New(e.ID, e.Data)
		msg.Metadata.Set("_topic", e.Type)
		msg.Metadata.Set("_source", e.Source)
		// 复制事件元数据
		for k, v := range e.Metadata {
			msg.Metadata.Set(k, v)
		}

		select {
		case output <- msg:
		case <-ctx.Done():
		case <-s.done:
		}
		return nil
	})

	// 跟踪订阅 ID，在 Close 时清理
	s.mu.Lock()
	s.subIDs = append(s.subIDs, id)
	s.mu.Unlock()

	return output, nil
}

// Close 关闭订阅者，停止所有订阅的消息接收。
func (s *Subscriber) Close() error {
	s.once.Do(func() {
		close(s.done)
		// 安全: 取消所有订阅，防止资源泄漏
		s.mu.Lock()
		for _, id := range s.subIDs {
			s.bus.Off(id)
		}
		s.subIDs = nil
		s.mu.Unlock()
	})
	return nil
}
