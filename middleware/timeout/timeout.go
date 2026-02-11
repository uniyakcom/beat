// Package timeout 提供消息处理超时中间件。
//
// 为每条消息设置处理截止时间，超时后 context 取消。
//
//	r.Use(timeout.New(5 * time.Second))
package timeout

import (
	"context"
	"time"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

// New 创建超时中间件。
//
// 在消息 context 上设置 deadline，handler 可通过 msg.Context().Done() 感知超时。
func New(d time.Duration) router.Middleware {
	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			ctx, cancel := context.WithTimeout(msg.Context(), d)
			defer cancel()

			msg.SetContext(ctx)
			return h(msg)
		}
	}
}
