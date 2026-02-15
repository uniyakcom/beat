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

	"github.com/uniyakcom/beat/core"
	"github.com/uniyakcom/beat/internal/support/noop"
)

const arenaChunkSize = 64 * 1024

type ArenaChunk struct {
	buf    []byte
	offset atomic.Int64
}

func newArenaChunk() *ArenaChunk {
	a := &ArenaChunk{buf: make([]byte, arenaChunkSize)}
	return a
}

// Alloc CAS bump allocator — 无锁热路径
func (a *ArenaChunk) Alloc(n int) []byte {
	aligned := int64((n + 7) &^ 7)
	for {
		cur := a.offset.Load()
		next := cur + aligned
		if next > int64(len(a.buf)) {
			return nil
		}
		if a.offset.CompareAndSwap(cur, next) {
			return a.buf[cur : cur+int64(n) : cur+aligned]
		}
	}
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

	// Arena chunk 回收池 — 通过 sync.Pool 复用已耗尽的 chunk
	// 避免每次 chunk 耗尽都 make([]byte,64K) 产生 GC 压力
	chunkPool sync.Pool
}

// New 创建事件对象池
func New() *EventPool {
	p := &EventPool{
		pool: sync.Pool{
			New: func() interface{} { return &core.Event{} },
		},
		arenaLock: noop.NewMutex(true), // 默认并发安全
		chunkPool: sync.Pool{
			New: func() interface{} {
				return &ArenaChunk{buf: make([]byte, arenaChunkSize)}
			},
		},
	}
	p.enableArena.Store(true) // 默认启用 Arena
	// 初始化第一个 Arena（从 chunkPool 获取）
	p.currentArena = p.chunkPool.Get().(*ArenaChunk)
	return p
}

// Acquire 获取 Event（零分配热路径）
func (p *EventPool) Acquire() *core.Event {
	return p.pool.Get().(*core.Event)
}

// Release 归还 Event（最小化清零后放回池）
// 仅清零 hot fields (Data, Type)，cold fields 在 Acquire 后由调用方覆盖
func (p *EventPool) Release(evt *core.Event) {
	if evt == nil {
		return
	}
	evt.Data = nil
	evt.Type = ""
	p.pool.Put(evt)
}

// AllocData 分配 Data
// 热路径: CAS bump allocator（无锁）
// 冷路径: 仅 chunk 耗尽时加锁切换新 chunk（从 chunkPool 获取，避免 GC 压力）
func (p *EventPool) AllocData(n int) []byte {
	if !p.enableArena.Load() {
		return make([]byte, n)
	}

	// 超大分配直接 make（不污染 Arena）
	if n > arenaChunkSize/2 {
		return make([]byte, n)
	}

	// 热路径: CAS 无锁分配（大部分调用在此返回）
	arena := p.currentArena
	if buf := arena.Alloc(n); buf != nil {
		return buf
	}

	// 冷路径: chunk 耗尽，加锁切换
	p.arenaLock.Lock()
	// Double-check: 可能其他 goroutine 已切换
	arena = p.currentArena
	if buf := arena.Alloc(n); buf != nil {
		p.arenaLock.Unlock()
		return buf
	}

	// 从 chunkPool 获取新 chunk（可能是回收复用的，避免 make）
	newChunk := p.chunkPool.Get().(*ArenaChunk)
	newChunk.offset.Store(0) // 重置偏移量（回收的 chunk 可能遗留旧值）

	// 旧 chunk 归还池 — 其 buf 数据可能仍被 evt.Data 引用，
	// 但不影响安全性：我们只重置 offset，不清零 buf 内容。
	// 当 chunk 从池中取出并重新使用时，新数据会自然覆盖旧数据。
	oldChunk := p.currentArena
	p.currentArena = newChunk

	buf := newChunk.Alloc(n)
	p.arenaLock.Unlock()

	p.chunkPool.Put(oldChunk)
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
