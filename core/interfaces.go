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

// Stats 事件总线运行时统计
type Stats struct {
	Emitted   int64 // 已发布事件总数
	Processed int64 // 已处理事件总数（handler 执行完成）
	Panics    int64 // handler panic 次数
	Depth     int64 // 当前队列积压深度（仅 Ring Buffer 实现有值）
}

// Bus 事件总线接口
type Bus interface {
	// On 订阅事件，返回订阅ID
	On(pattern string, handler Handler) uint64

	// Off 取消订阅
	Off(id uint64)

	// Emit 发布事件（带 panic 保护）
	Emit(evt *Event) error

	// UnsafeEmit 发布事件（零保护，不捕获 handler panic，极致性能）
	// handler panic 会直接传播到调用方 goroutine。
	// 不更新 Stats().Emitted 计数，以实现最低开销。
	// 仅在确信 handler 不会 panic 时使用。
	UnsafeEmit(evt *Event) error

	// EmitMatch 发布事件（支持通配符匹配）
	EmitMatch(evt *Event) error

	// UnsafeEmitMatch 发布事件（通配符匹配，零保护，极致性能）
	// handler panic 会直接传播到调用方 goroutine。
	// 不更新 Stats() 计数，以实现最低开销。
	// 仅在确信 handler 不会 panic 时使用。
	UnsafeEmitMatch(evt *Event) error

	// EmitBatch 批量发布事件
	EmitBatch(events []*Event) error

	// EmitMatchBatch 批量发布事件（支持通配符）
	EmitMatchBatch(events []*Event) error

	// Stats 返回运行时统计
	Stats() Stats

	// Close 关闭（立即关闭，不等待队列排空）
	Close()

	// Drain 优雅关闭（等待队列排空或超时）
	// timeout=0 时等效于 Close()
	Drain(timeout time.Duration) error
}

// ═══════════════════════════════════════════════════════════════════
// 扩展接口 — 按需类型断言使用
// ═══════════════════════════════════════════════════════════════════

// Flusher 支持手动刷新的 Bus（如 Flow 批处理模式）
//
// 用法:
//
//	if f, ok := bus.(core.Flusher); ok {
//	    f.Flush()
//	}
type Flusher interface {
	// Flush 立即刷新当前缓冲区/批次
	Flush() error
}

// ErrorReporter 支持异步错误查询的 Bus（如 Sync 异步模式）
//
// 用法:
//
//	if er, ok := bus.(core.ErrorReporter); ok {
//	    if err := er.LastError(); err != nil {
//	        log.Println("async error:", err)
//	        er.ClearError()
//	    }
//	}
type ErrorReporter interface {
	// LastError 返回最近一次异步错误（nil 表示无错误）
	LastError() error
	// ClearError 清除错误状态
	ClearError()
}

// Prewarmer 支持预热的 Bus（如 Sync 模式）
//
// 用法:
//
//	if pw, ok := bus.(core.Prewarmer); ok {
//	    pw.Prewarm([]string{"user.created", "order.placed"})
//	}
type Prewarmer interface {
	// Prewarm 预热对象池和匹配器缓存
	Prewarm(eventTypes []string)
}

// BatchStatter 支持批处理统计的 Bus（如 Flow 模式）
//
// 用法:
//
//	if bs, ok := bus.(core.BatchStatter); ok {
//	    processed, batches := bs.BatchStats()
//	}
type BatchStatter interface {
	// BatchStats 返回批处理统计（已处理事件数, 批次数）
	BatchStats() (processed, batches uint64)
}
