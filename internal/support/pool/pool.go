// Package pool 提供高性能事件对象池 + 自动 Arena 管理
//
// 设计：
//   - Acquire/Release 管理 Event 对象复用
//   - EnableArena=true 时，AllocData 自动从 Arena 分配（无 malloc）
//   - Arena 满时自动切换新 chunk，对调用侧透明
package pool

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/internal/support/noop"
)

const arenaChunkSize = 64 * 1024

type ArenaChunk struct {
	buf    []byte
	offset int
}

func newArenaChunk() *ArenaChunk {
	return &ArenaChunk{buf: make([]byte, arenaChunkSize)}
}

func (a *ArenaChunk) Alloc(n int) []byte {
	aligned := (n + 7) &^ 7
	if a.offset+aligned > len(a.buf) {
		return nil
	}
	s := a.buf[a.offset : a.offset+n : a.offset+aligned]
	a.offset += aligned
	return s
}

// ─── Event Pool ──────────────────────────────────────────────────────

// EventPool 高性能事件对象池 + 自动 Arena 管理
type EventPool struct {
	// Event 对象复用
	pool sync.Pool

	// Arena 配置和管理（enableArena 使用 atomic.Bool 保证并发安全）
	enableArena  atomic.Bool
	currentArena *ArenaChunk
	arenaLock    noop.Mutex // 可切换锁: 单线程场景零开销

	// Arena 活跃 chunk 计数（大小保护，防止内存膨胀）
	activeChunks atomic.Int32
	maxChunks    int32 // 超过此值降级 make（默认 256 = 16MB）
}

// New 创建事件对象池
func New() *EventPool {
	p := &EventPool{
		maxChunks: 256, // 最多 256 个 64KB chunk = 16MB
		pool: sync.Pool{
			New: func() interface{} { return &core.Event{} },
		},
		arenaLock: noop.NewMutex(true), // 默认并发安全
	}
	p.enableArena.Store(true) // 默认启用 Arena
	// 初始化第一个 Arena
	p.currentArena = newArenaChunk()
	p.activeChunks.Store(1)
	return p
}

// Acquire 获取 Event（零分配热路径）
func (p *EventPool) Acquire() *core.Event {
	return p.pool.Get().(*core.Event)
}

// Release 归还 Event（清零后放回池）
func (p *EventPool) Release(evt *core.Event) {
	if evt == nil {
		return
	}
	evt.Data = nil
	evt.Type = ""
	evt.ID = ""
	evt.Source = ""
	evt.Metadata = nil
	evt.Timestamp = time.Time{} // 安全的零值赋值（替代 unsafe 清零）
	p.pool.Put(evt)
}

// AllocData 分配 Data
// 若 EnableArena=true，从 Arena 分配（无 malloc）
// 若 EnableArena=false 或活跃 chunk 超限，直接 make（传统方式）
func (p *EventPool) AllocData(n int) []byte {
	if !p.enableArena.Load() {
		return make([]byte, n)
	}

	// 超大分配直接 make（不污染 Arena）
	if n > arenaChunkSize/2 {
		return make([]byte, n)
	}

	p.arenaLock.Lock()

	buf := p.currentArena.Alloc(n)
	if buf == nil {
		// 当前 Arena 满，检查是否超过 chunk 上限
		if p.activeChunks.Load() >= p.maxChunks {
			p.arenaLock.Unlock()
			return make([]byte, n) // 降级 make，防止内存膨胀
		}
		// 安全修复: 不回收旧 chunk — 已分配的切片仍引用其缓冲区，
		// 回收会导致 use-after-free 数据损坏。由 GC 在所有引用释放后回收。
		p.currentArena = newArenaChunk()
		p.activeChunks.Add(1)
		buf = p.currentArena.Alloc(n)
	}

	p.arenaLock.Unlock()
	return buf
}

// ─── 全局便捷接口 ───────────────────────────────────────────────────

var global = New()

// Acquire 全局获取 Event
func Acquire() *core.Event { return global.Acquire() }

// Release 全局归还 Event
func Release(evt *core.Event) { global.Release(evt) }

// AllocData 全局分配 Data
func AllocData(n int) []byte { return global.AllocData(n) }

// Global 获取全局池引用
func Global() *EventPool { return global }

// SetEnableArena 全局设置是否启用 Arena
func SetEnableArena(enable bool) { global.enableArena.Store(enable) }
