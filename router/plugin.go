package router

import "context"

// RouterPlugin 路由器插件接口（生命周期钩子）
//
// 插件可在 Router 启动/停止时执行初始化和清理逻辑，
// 例如注册 Prometheus 指标、启动健康检查端点等。
type RouterPlugin interface {
	// OnStart 在 Router.Run() 时调用。返回 error 将阻止 Router 启动。
	OnStart(ctx context.Context, r *Router) error

	// OnStop 在 Router 关闭时调用。
	OnStop(r *Router)
}
