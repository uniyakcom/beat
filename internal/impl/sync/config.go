// Package sync 提供同步/异步事件总线配置
package sync

import (
	"runtime"
	stdsync "sync"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/internal/support/pool"
	"github.com/uniyakcom/beat/util"

	"github.com/uniyakcom/beat/internal/support/wpool"
)

// Config 事件总线配置
type Config struct {
	// 预热
	Prewarm   bool     // 是否启用预热
	PreEvents []string // 预热的event types
	PreCnt    int      // 预热对象数量

	// CPU亲和性
	CPU   bool // 是否启用CPU绑定
	CPUId int  // 绑定CPU索引

	// 异步
	PoolSize int  // 异步池大小
	Async    bool // 是否启用异步模式

	// Arena
	EnableArena bool // 是否启用Arena自动分配Data
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Prewarm:     false,
		PreCnt:      1024,
		CPU:         false,
		CPUId:       0,
		PoolSize:    0,
		Async:       false,
		EnableArena: false, // 默认无Arena
	}
}

// OptConfig 返回优化配置（推荐用于高性能场景）
func OptConfig() *Config {
	numCPU := runtime.NumCPU()
	return &Config{
		Prewarm:     false,
		PreEvents:   []string{"event", "system", "user", "order", "log", "metric", "trace", "cmd"},
		PreCnt:      2048,
		CPU:         numCPU > 1,
		CPUId:       0,
		PoolSize:    numCPU * 10,
		Async:       false,
		EnableArena: false, // 同步场景关闭Arena
	}
}

// New 使用配置创建事件总线
func New(cfg *Config) (core.Bus, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// 配置全局 Pool 的 Arena 开关
	pool.SetEnableArena(cfg.EnableArena)

	e := &Bus{
		matcher:   core.NewTrieMatcher(),
		async:     cfg.Async,
		emitted:   util.NewPerCPUCounter(),
		processed: util.NewPerCPUCounter(),
		panics:    util.NewPerCPUCounter(),
	}

	m := make(map[string][]*sub)
	e.subs.Store(buildSnapshot(m))

	e.pool = stdsync.Pool{
		New: func() interface{} {
			return &core.Event{}
		},
	}

	// 异步模式
	if cfg.Async {
		poolSz := cfg.PoolSize
		if poolSz <= 0 {
			poolSz = optPoolSz()
		}
		pool := wpool.New(poolSz, 0)
		e.gPool = pool
		e.errChan = make(chan error, 1024)
		e.errDone = make(chan struct{})
		go e.errorHandler()
	}

	// 预热
	if cfg.Prewarm && len(cfg.PreEvents) > 0 {
		prewarmInternal(e, cfg.PreEvents, cfg.PreCnt)
	}

	return e, nil
}

// prewarmInternal 内部预热 - 最小化版本
func prewarmInternal(e *Bus, eventTypes []string, cnt int) {
	// stdsync.Pool会自动管理对象，无需手动预热
	// 仅在高并发场景（Conc>10000）才预热matcher缓存

	// 只预热最常用的2-3个事件类型，避免过度初始化
	limit := 2
	if len(eventTypes) < limit {
		limit = len(eventTypes)
	}

	for i := 0; i < limit; i++ {
		e.matcher.HasMatch(eventTypes[i])
	}
}
