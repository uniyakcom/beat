// SPSC Sharded Scheduler 已迁移到 internal/support/sched 包。
// 本文件保留转发函数以保持向后兼容。
package async

import (
	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/internal/support/sched"
)

// NewShardedScheduler 转发到共享 sched 包
func NewShardedScheduler(ringSize uint64, workers int) *sched.ShardedScheduler[*core.Event] {
	return sched.NewShardedScheduler[*core.Event](ringSize, workers)
}
