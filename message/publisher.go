package message

// Publisher 消息发布者接口
//
// 所有 Pub/Sub 适配器（本地、Kafka、NATS、Redis 等）必须实现此接口。
type Publisher interface {
	// Publish 发布消息到指定 topic。
	// 消息必须在 Publish 返回前完成投递（同步语义）或入队（异步语义）。
	Publish(topic string, messages ...*Message) error

	// Close 关闭发布者，释放资源。
	Close() error
}
