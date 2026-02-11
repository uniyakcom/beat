package message

import "context"

// Publisher 消息发布者接口
//
// 所有适配器（本地、Redis、Kafka、NATS、HTTP、SQL 等）必须实现此接口。
type Publisher interface {
	// Publish 发布消息到指定 topic。
	// ctx 用于传递超时、取消、链路追踪等信号。
	Publish(ctx context.Context, topic string, messages ...*Message) error

	// Close 关闭发布者，释放资源。
	Close() error
}
