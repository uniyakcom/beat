// Package logging 提供消息处理日志中间件。
//
// 记录每条消息的处理耗时、产出数量和错误信息。使用 log/slog 零外部依赖。
//
//	r.AddMiddleware(logging.New(slog.Default()))
package logging

import (
	"log/slog"
	"time"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

// New 创建日志中间件。
func New(logger *slog.Logger) router.Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			start := time.Now()

			produced, err := h(msg)

			duration := time.Since(start)
			attrs := []any{
				"uuid", msg.UUID,
				"duration", duration,
				"produced", len(produced),
			}

			if err != nil {
				logger.Error("message handler failed", append(attrs, "error", err)...)
			} else {
				logger.Debug("message processed", attrs...)
			}

			return produced, err
		}
	}
}
