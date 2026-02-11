// Package recoverer 提供 panic 恢复中间件。
//
// 捕获 handler 内的 panic 并转化为 error 返回，防止单条消息的 panic 影响整个 Router。
//
//	r.Use(recoverer.New())
package recoverer

import (
	"fmt"

	"github.com/uniyakcom/beat/message"
	"github.com/uniyakcom/beat/router"
)

// PanicError 包装 panic 恢复值的 error 类型
type PanicError struct {
	Value interface{}
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("handler panic: %v", e.Value)
}

// New 创建 panic 恢复中间件。
func New() router.Middleware {
	return func(h router.HandlerFunc) router.HandlerFunc {
		return func(msg *message.Message) (produced []*message.Message, err error) {
			defer func() {
				if r := recover(); r != nil {
					produced = nil
					err = &PanicError{Value: r}
				}
			}()
			return h(msg)
		}
	}
}
