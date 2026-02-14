// Package correlation 提供链路追踪 ID 传播中间件。
//
// 确保消息处理链中的 correlation_id 自动传播：
//
//   - 入站消息无 correlation_id → 自动生成
//
//   - 产出消息 → 继承入站消息的 correlation_id
//
//     r.Use(correlation.New())
package correlation

import (
	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

const (
	// HeaderCorrelationID 元数据中的 correlation ID key
	HeaderCorrelationID = "correlation_id"
)

// New 创建 correlation ID 传播中间件。
// 使用 FastUUID 生成高性能 correlation ID（非密码学安全但足够唯一）。
func New() router.Middleware {
	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			id := msg.Metadata.Get(HeaderCorrelationID)
			if id == "" {
				id = message.FastUUID() // 使用 Per-P 无锁 FastUUID
				msg.Metadata.Set(HeaderCorrelationID, id)
			}

			produced, err := h(msg)

			// 将 correlation_id 传播到所有产出消息
			for _, p := range produced {
				if p == nil {
					continue
				}
				if p.Metadata.Get(HeaderCorrelationID) == "" {
					p.Metadata.Set(HeaderCorrelationID, id)
				}
			}

			return produced, err
		}
	}
}
