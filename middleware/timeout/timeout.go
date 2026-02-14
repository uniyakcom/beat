// Package timeout 提供消息处理超时中间件。
//
// 为每条消息设置处理截止时间，超时后 context 取消。
// 支持 Abandoned Context 模式：handler 超时后
// 通过 detached context 保留 value 但切断 cancel 传播。
//
//	r.Use(timeout.New(5 * time.Second))
//	r.Use(timeout.NewWithConfig(Config{Timeout: 5*time.Second, AllowOverrun: true}))
package timeout

import (
	"context"
	"time"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

// Config 超时中间件配置
type Config struct {
	// Timeout 处理超时时间（必填）
	Timeout time.Duration

	// AllowOverrun 是否允许 handler 超时后继续执行（Abandoned Context 模式）。
	// true: handler 返回后不立即 cancel context（适用于启动后台工作的 handler）
	// false: handler 返回后 defer cancel（默认，适用于同步 handler）
	AllowOverrun bool
}

// New 创建超时中间件（默认模式：handler 返回后立即 cancel）。
//
// 在消息 context 上设置 deadline，handler 可通过 msg.Context().Done() 感知超时。
func New(d time.Duration) router.Middleware {
	return NewWithConfig(Config{Timeout: d})
}

// NewWithConfig 创建带配置的超时中间件。
func NewWithConfig(cfg Config) router.Middleware {
	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			ctx, cancel := context.WithTimeout(msg.Context(), cfg.Timeout)

			if cfg.AllowOverrun {
				// Abandoned Context 模式: 使用 detached context 传给 handler，
				// defer cancel 仅释放 timer 资源，不影响 handler 的后续工作
				detached := detachedContext{ctx}
				msg.SetContext(detached)
				result, err := h(msg)
				cancel() // 释放 timer（context value 仍可通过 detached 访问）
				return result, err
			}

			// 标准模式: defer cancel 同时停止 handler 感知的 context
			defer cancel()
			msg.SetContext(ctx)
			return h(msg)
		}
	}
}

// detachedContext 保留 parent 的 Value 但切断 cancel/deadline 传播
type detachedContext struct {
	parent context.Context
}

func (d detachedContext) Deadline() (time.Time, bool)       { return time.Time{}, false }
func (d detachedContext) Done() <-chan struct{}             { return nil }
func (d detachedContext) Err() error                        { return nil }
func (d detachedContext) Value(key interface{}) interface{} { return d.parent.Value(key) }
