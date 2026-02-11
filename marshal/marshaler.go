// Package marshal 提供消息序列化/反序列化接口和实现。
//
// Codec 将 message.Message 与 []byte 互相转换，用于跨进程传输和持久化。
// 内置 JSON 实现；Protobuf、MessagePack 等可作为外部扩展。
package marshal

import (
	"github.com/uniyakcom/beat/message"
)

// Codec 消息编解码器接口
type Codec interface {
	// Marshal 将消息序列化为字节。
	Marshal(topic string, msg *message.Message) ([]byte, error)

	// Unmarshal 将字节反序列化为消息。
	Unmarshal(topic string, data []byte) (*message.Message, error)
}
