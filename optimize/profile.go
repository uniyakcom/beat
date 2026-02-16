// Package optimize 提供优化配置和推荐
package optimize

import (
	"runtime"
	"time"
)

// Auto 自动配置结构
type Auto struct {
	Enabled      bool // 总开关（默认true）
	Batch        bool // 批处理自适应
	Backpressure bool // 背压控制
	Degradation  bool // 自动降级
}

// Profile 优化场景Profile
type Profile struct {
	Name         string        // 场景名称
	Conc         int           // 预期并发
	TPS          int           // 目标吞吐(M/s)
	Lat          string        // "low"/"med"/"hi"/"ultra_low"
	Mem          string        // "min"/"balance"/"unlimited"
	Arch         string        // "amd64"/"arm64"/"generic"
	Cores        int           // CPU核心数
	Impl         string        // "sync"/"async"/"flow"
	EnableArena  bool          // 是否启用 Arena（0分配数据分配）
	BatchTimeout time.Duration // Flow 批处理超时（0=默认100ms）
	Auto         Auto          // 自动配置
}

// ═══════════════════════════════════════════════════════════════════
// 三大核心 Profile
// ═══════════════════════════════════════════════════════════════════

// Sync 同步直调场景
// 用途: RPC调用链路、API网关中间件、权限验证拦截
// 特点: Emit ~15ns/op，UnsafeEmit ~4.4ns/op，~175ns/op 高并发（低核），error 返回
func Sync() *Profile {
	return &Profile{
		Name:        "sync",
		Conc:        1000,
		TPS:         10000,
		Lat:         "low",
		Mem:         "balance",
		Arch:        runtime.GOARCH,
		Cores:       runtime.NumCPU(),
		Impl:        "sync",
		EnableArena: false,
		Auto: Auto{
			Enabled:      true,
			Batch:        false,
			Backpressure: false,
			Degradation:  true,
		},
	}
}

// Async Per-P SPSC 异步高吞吐场景
// 用途: 发布订阅、日志聚合、实时推送、高频交易、微服务事件总线
// 特点: ~33ns/op 单线程，~32ns/op 高并发，零 CAS，零分配，Per-P SPSC ring
func Async() *Profile {
	return &Profile{
		Name:        "async",
		Conc:        100000,
		TPS:         500000,
		Lat:         "ultra_low",
		Mem:         "min",
		Arch:        runtime.GOARCH,
		Cores:       runtime.NumCPU(),
		Impl:        "async",
		EnableArena: false,
		Auto: Auto{
			Enabled:      true,
			Batch:        false,
			Backpressure: false,
			Degradation:  false,
		},
	}
}

// Flow 多阶段 Pipeline 流处理场景
// 用途: 实时ETL、窗口聚合、批量数据加载、数据格式转换
// 特点: ~70ns/op 单线程，多阶段 Pipeline，per-shard 精准唤醒，自适应分片，At-least-once
func Flow() *Profile {
	return &Profile{
		Name:         "flow",
		Conc:         5000,
		TPS:          50000,
		Lat:          "med",
		Mem:          "balance",
		Arch:         runtime.GOARCH,
		Cores:        runtime.NumCPU(),
		Impl:         "flow",
		EnableArena:  true,
		BatchTimeout: 100 * time.Millisecond,
		Auto: Auto{
			Enabled:      true,
			Batch:        true,
			Backpressure: true,
			Degradation:  true,
		},
	}
}

// ═══════════════════════════════════════════════════════════════════
// Presets
// ═══════════════════════════════════════════════════════════════════

// Presets 所有预设场景
var Presets = map[string]*Profile{
	"sync":  Sync(),
	"async": Async(),
	"flow":  Flow(),
}

// Preset 获取预设Profile
func Preset(name string) *Profile {
	if p, ok := Presets[name]; ok {
		// 返回副本，避免共享状态
		return &Profile{
			Name:         p.Name,
			Conc:         p.Conc,
			TPS:          p.TPS,
			Lat:          p.Lat,
			Mem:          p.Mem,
			Arch:         p.Arch,
			Cores:        p.Cores,
			Impl:         p.Impl,
			EnableArena:  p.EnableArena,
			BatchTimeout: p.BatchTimeout,
			Auto:         p.Auto,
		}
	}
	return Sync() // 默认使用sync场景
}

// ═════════════════════════════════════════════════════════════════
// 自动检测
// ═════════════════════════════════════════════════════════════════

// AutoDetect 根据运行时环境自动选择最优实现
//   - 多核 (>= 4 cores) → Async（高并发最优）
//   - 少核 (< 4 cores ) → Sync（低延迟最优）
//
// Flow 不会被自动选择 — 它是专用 Pipeline，需显式指定。
func AutoDetect() *Profile {
	cores := runtime.NumCPU()
	if cores >= 4 {
		p := Async()
		p.Name = "auto"
		p.Cores = cores
		return p
	}
	p := Sync()
	p.Name = "auto"
	p.Cores = cores
	return p
}
