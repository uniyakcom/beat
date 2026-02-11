// Package retry 提供消息处理失败重试中间件。
//
// 支持指数退避、最大重试次数、自定义判断函数。
//
//	r.Use(retry.New(retry.Config{
//	    MaxRetries:      3,
//	    InitialInterval: 100 * time.Millisecond,
//	}))
package retry

import (
	"time"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

// Config 重试配置
type Config struct {
	// MaxRetries 最大重试次数（不含首次执行）。默认 3。
	MaxRetries int

	// InitialInterval 首次重试间隔。默认 100ms。
	InitialInterval time.Duration

	// MaxInterval 最大重试间隔（指数退避上限）。默认 10s。
	MaxInterval time.Duration

	// Multiplier 退避乘数。默认 2.0。
	Multiplier float64

	// ShouldRetry 自定义是否重试判断。为 nil 时所有 error 都重试。
	ShouldRetry func(err error) bool
}

func (c *Config) defaults() {
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.InitialInterval <= 0 {
		c.InitialInterval = 100 * time.Millisecond
	}
	if c.MaxInterval <= 0 {
		c.MaxInterval = 10 * time.Second
	}
	if c.Multiplier <= 0 {
		c.Multiplier = 2.0
	}
}

// New 创建重试中间件。
func New(cfg Config) router.Middleware {
	cfg.defaults()

	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			interval := cfg.InitialInterval

			for attempt := 0; ; attempt++ {
				produced, err := h(msg)
				if err == nil {
					return produced, nil
				}

				if attempt >= cfg.MaxRetries {
					return nil, err
				}

				if cfg.ShouldRetry != nil && !cfg.ShouldRetry(err) {
					return nil, err
				}

				// 检查 context 是否已取消
				select {
				case <-msg.Context().Done():
					return nil, msg.Context().Err()
				default:
				}

				time.Sleep(interval)

				// 指数退避
				interval = time.Duration(float64(interval) * cfg.Multiplier)
				if interval > cfg.MaxInterval {
					interval = cfg.MaxInterval
				}
			}
		}
	}
}
