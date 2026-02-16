// Package beat 统一API入口
package beat

import (
	"time"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/optimize"
)

// Bus 导出Bus接口
type Bus = core.Bus

// Event 导出Event类型
type Event = core.Event

// Handler 导出Handler类型
type Handler = core.Handler

// PanicHandler 导出PanicHandler类型
type PanicHandler = core.PanicHandler

// Profile 导出Profile
type Profile = optimize.Profile

// Auto 导出Auto配置
type Auto = optimize.Auto

// ═══════════════════════════════════════════════════════════════════
// 第零层：New() 零配置入口
// ═══════════════════════════════════════════════════════════════════

// New 零配置创建 Bus（自动检测运行时环境，选择最优实现）
//   - >= 4 核: Async（高并发最优）
//   - <  4 核: Sync（低延迟最优）
//
// 用法:
//
//	bus, _ := event.New()
//	defer bus.Close()
func New() (Bus, error) {
	return Option(optimize.AutoDetect())
}

// ═══════════════════════════════════════════════════════════════════
// 第一层：ForXxx() 三大核心（推荐使用）
// ═══════════════════════════════════════════════════════════════════

// ForSync 创建同步直调 Bus
// 用途: RPC调用、API中间件、权限验证
// 性能: Emit ~15ns/op，UnsafeEmit ~4.4ns/op，error 返回，零开销
func ForSync() (Bus, error) {
	return Option(optimize.Sync())
}

// ForAsync 创建 Per-P SPSC 异步高吞吐 Bus
// 用途: 发布订阅、日志聚合、实时推送、高频交易
// 性能: ~33ns/op 单线程，~32ns/op 高并发，零 CAS，零分配
func ForAsync() (Bus, error) {
	return Option(optimize.Async())
}

// ForFlow 创建多阶段 Pipeline 流处理 Bus
// 用途: 实时ETL、窗口聚合、批量数据加载
// 性能: ~70ns/op 单线程，多阶段 Pipeline，per-shard 精准唤醒
func ForFlow() (Bus, error) {
	return Option(optimize.Flow())
}

// ═══════════════════════════════════════════════════════════════════
// 第二层：Scenario() 字符串配置
// ═══════════════════════════════════════════════════════════════════

// Scenario 预设场景快速创建
// name: "sync", "async", "flow"
func Scenario(name string) (Bus, error) {
	p := optimize.Preset(name)
	return Option(p)
}

// ═══════════════════════════════════════════════════════════════════
// 第三层：Option() 完全控制
// ═══════════════════════════════════════════════════════════════════

// Option 生成优化Bus（完全控制）
func Option(p *Profile) (Bus, error) {
	if p == nil {
		p = optimize.Sync()
	}
	advisor := optimize.NewAdvisor()
	advised := advisor.Advise(p)
	return optimize.Build(advised)
}

// ═══════════════════════════════════════════════════════════════════
// 包级便捷 API（Sync 语义，零初始化，无需 Close）
// ═══════════════════════════════════════════════════════════════════

// defaultBus 包级默认 Bus（Sync 实现，惰性初始化安全：init 阶段单线程）
// Sync 无后台 goroutine，无需 Close，天然适合全局单例。
var defaultBus Bus

func init() {
	b, err := ForSync()
	if err != nil {
		panic("beat: failed to init default bus: " + err.Error())
	}
	defaultBus = b
}

// Default 返回包级默认 Bus 实例（Sync 语义）
// 适用于需要将 Bus 作为参数传递但又想使用全局默认实例的场景。
func Default() Bus {
	return defaultBus
}

// On 包级订阅事件（Sync 语义）
//
// 用法:
//
//	beat.On("user.created", func(e *beat.Event) error {
//	    fmt.Println(string(e.Data))
//	    return nil
//	})
func On(pattern string, handler Handler) uint64 {
	return defaultBus.On(pattern, handler)
}

// Off 包级取消订阅
func Off(id uint64) {
	defaultBus.Off(id)
}

// Emit 包级发布事件（Sync 语义，返回 handler error）
//
// 用法:
//
//	err := beat.Emit(&beat.Event{Type: "user.created", Data: []byte("alice")})
func Emit(evt *Event) error {
	return defaultBus.Emit(evt)
}

// UnsafeEmit 包级发布事件（零保护，极致性能，不捕获 handler panic）
// 注意: 不更新 Stats().Emitted 计数，以实现最低开销。
func UnsafeEmit(evt *Event) error {
	return defaultBus.UnsafeEmit(evt)
}

// EmitMatch 包级发布事件（支持通配符匹配）
func EmitMatch(evt *Event) error {
	return defaultBus.EmitMatch(evt)
}

// UnsafeEmitMatch 包级发布事件（通配符匹配，零保护，极致性能）
// 注意: 不更新 Stats() 计数，以实现最低开销。
func UnsafeEmitMatch(evt *Event) error {
	return defaultBus.UnsafeEmitMatch(evt)
}

// EmitBatch 包级批量发布事件
func EmitBatch(events []*Event) error {
	return defaultBus.EmitBatch(events)
}

// EmitMatchBatch 包级批量发布事件（支持通配符）
func EmitMatchBatch(events []*Event) error {
	return defaultBus.EmitMatchBatch(events)
}

// Stats 包级获取运行时统计
func Stats() core.Stats {
	return defaultBus.Stats()
}

// Drain 包级优雅关闭默认 Bus
// 注意: 关闭后包级 API 将不可用，通常仅在进程退出前调用。
func Drain(timeout time.Duration) error {
	return defaultBus.Drain(timeout)
}
