// Package core 提供事件总线核心接口定义
package core

import (
	"time"
)

// Event 事件（字段按大小降序排列 → 减少编译器padding → 最小化结构体体积）
// Hot fields (Type, Data) 在前，Cold fields (Metadata, Timestamp) 在后
type Event struct {
	Data      []byte            // 24 bytes (hot: 事件数据，读频率最高)
	Type      string            // 16 bytes (hot: 事件类型，用于路由)
	ID        string            // 16 bytes (warm)
	Source    string            // 16 bytes (cold)
	Metadata  map[string]string // 8 bytes  (cold: map指针，含GC扫描开销)
	Timestamp time.Time         // 24 bytes (cold: 含wall+ext+loc指针)
}

// Handler 事件处理器
type Handler func(*Event) error

// PanicHandler panic 回调（可选，用户注册后接收 panic 通知）
type PanicHandler func(recovered interface{}, evt *Event)

// BusStats 事件总线运行时统计
type BusStats struct {
	EventsEmitted   int64 // 已发布事件总数
	EventsProcessed int64 // 已处理事件总数（handler 执行完成）
	Panics          int64 // handler panic 次数
	QueueDepth      int64 // 当前队列积压深度（仅 Ring Buffer 实现有值）
}

// Bus 事件总线接口
type Bus interface {
	// On 订阅事件，返回订阅ID
	On(pattern string, handler Handler) uint64

	// Off 取消订阅
	Off(id uint64)

	// Emit 发布事件
	Emit(evt *Event) error

	// EmitMatch 发布事件（支持通配符匹配）
	EmitMatch(evt *Event) error

	// EmitBatch 批量发布事件
	EmitBatch(events []*Event) error

	// EmitMatchBatch 批量发布事件（支持通配符）
	EmitMatchBatch(events []*Event) error

	// Stats 返回运行时统计
	Stats() BusStats

	// Close 关闭（立即关闭，不等待队列排空）
	Close()

	// GracefulClose 优雅关闭（等待队列排空或超时）
	// timeout=0 时等效于 Close()
	GracefulClose(timeout time.Duration) error
}
