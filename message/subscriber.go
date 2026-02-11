package message

import "context"

// Subscriber 消息订阅者接口
//
// 所有 Pub/Sub 适配器（本地、Kafka、NATS、Redis 等）必须实现此接口。
type Subscriber interface {
	// Subscribe 订阅指定 topic，返回消息通道。
	// 通道关闭表示订阅结束（Close 被调用或 context 取消）。
	//
	// 消费者应对每条消息调用 Ack() 或 Nack()。
	// 对于不支持确认机制的实现（如本地 Pub/Sub），Ack/Nack 可以是无操作。
	Subscribe(ctx context.Context, topic string) (<-chan *Message, error)

	// Close 关闭订阅者，停止所有订阅，释放资源。
	Close() error
}
