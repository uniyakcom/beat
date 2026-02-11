package router

// Middleware 中间件函数签名
//
// 中间件包装 HandlerFunc，在消息处理前后添加逻辑（日志、重试、超时、追踪等）。
// 类似 HTTP 中间件模式。
//
//	func myMiddleware(next HandlerFunc) HandlerFunc {
//	    return func(msg *message.Message) ([]*message.Message, error) {
//	        // 前置逻辑
//	        result, err := next(msg)
//	        // 后置逻辑
//	        return result, err
//	    }
//	}
type Middleware func(h HandlerFunc) HandlerFunc
