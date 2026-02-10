// Package optimize factory工厂
package optimize

import (
	"time"

	"github.com/uniyakcom/beat/core"
	implasync "github.com/uniyakcom/beat/internal/impl/async"
	"github.com/uniyakcom/beat/internal/impl/flow"
	implsync "github.com/uniyakcom/beat/internal/impl/sync"
	"github.com/uniyakcom/beat/internal/support/pool"
)

// Build 根据推荐配置构建Bus
func Build(advised *Advised) (core.Bus, error) {
	impl := advised.Impl

	// 提取全局 Arena 配置
	enableArena := false
	if v, ok := advised.Params["arena"]; ok && v.(bool) {
		enableArena = true
	}
	pool.SetEnableArena(enableArena)

	switch impl {
	case "sync":
		return buildSync(advised, enableArena)
	case "async":
		return buildAsync(advised)
	case "flow":
		return buildFlow(advised, enableArena)
	default:
		return buildSync(advised, enableArena)
	}
}

// buildSync 构建同步 Bus（用于sync场景）
func buildSync(advised *Advised, enableArena bool) (core.Bus, error) {
	cfg := &implsync.Config{
		Async:       false,
		PoolSize:    0,
		EnableArena: enableArena,
	}

	// 处理推荐参数
	if prewarm, ok := advised.Params["prewarm"]; ok && prewarm.(bool) {
		cfg.Prewarm = true
		if events, ok := advised.Params["preevents"]; ok {
			cfg.PreEvents = events.([]string)
		}
	}

	if poolsz, ok := advised.Params["poolsz"]; ok {
		cfg.PoolSize = poolsz.(int)
		cfg.Async = true
	}

	return implsync.New(cfg)
}

// buildAsync 构建 Per-P SPSC 零分配 Bus
func buildAsync(advised *Advised) (core.Bus, error) {
	cfg := implasync.DefaultConfig()

	if v, ok := advised.Params["workers"]; ok {
		cfg.Workers = v.(int)
	}
	if v, ok := advised.Params["ringSize"]; ok {
		cfg.RingSize = v.(uint64)
	}

	return implasync.New(cfg), nil
}

// buildFlow 构建流处理（用于stream/batch场景）
func buildFlow(advised *Advised, enableArena bool) (core.Bus, error) {
	batchsz := 100
	if v, ok := advised.Params["batchsz"]; ok {
		batchsz = v.(int)
	}

	timeout := 100 * time.Millisecond
	if v, ok := advised.Params["batchTimeout"]; ok {
		timeout = v.(time.Duration)
	}

	// 创建简单的透传Stage
	stages := []flow.Stage{
		func(events []*core.Event) error {
			return nil
		},
	}

	bus := flow.New(stages, batchsz, timeout)
	return bus, nil
}
