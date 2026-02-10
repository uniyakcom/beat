// Package optimize advisor推荐引擎
package optimize

// Advised 推荐配置
type Advised struct {
	Profile *Profile
	Params  map[string]interface{}
	Impl    string
}

// Advisor 推荐引擎
type Advisor struct{}

// NewAdvisor 创建推荐引擎
func NewAdvisor() *Advisor {
	return &Advisor{}
}

// Advise 根据Profile推荐配置
func (a *Advisor) Advise(p *Profile) *Advised {
	advised := &Advised{
		Profile: p,
		Params:  make(map[string]interface{}),
		Impl:    p.Impl,
	}

	// 根据场景选择实现（三预设：sync / async / flow）
	switch p.Name {
	case "sync":
		advised.Impl = "sync"

	case "async":
		advised.Impl = "async"
		advised.Params["ringSize"] = uint64(8192)
		w := p.Cores / 2
		if w < 1 {
			w = 1
		}
		advised.Params["workers"] = w

	case "flow":
		advised.Impl = "flow"
		batchsz := 200 + p.Conc/50 // 延迟-吞吐平衡（Conc=5000 → 300）
		advised.Params["batchsz"] = batchsz
		if p.BatchTimeout > 0 {
			advised.Params["batchTimeout"] = p.BatchTimeout
		}

	default:
		// 自动检测：根据运行时特征选择最优实现
		if p.TPS > 50000 || p.Lat == "ultra_low" || p.Cores >= 4 {
			advised.Impl = "async"
			advised.Params["ringSize"] = uint64(8192)
			w := p.Cores / 2
			if w < 1 {
				w = 1
			}
			advised.Params["workers"] = w
		} else {
			advised.Impl = "sync"
		}
	}

	// 预热配置（高并发场景）
	if p.Conc > 5000 {
		advised.Params["prewarm"] = true
		advised.Params["preevents"] = []string{
			"event", "system", "user", "order",
			"log", "metric", "trace", "cmd",
		}
	}

	// Arena 配置
	if p.EnableArena {
		advised.Params["arena"] = true
	}

	return advised
}
