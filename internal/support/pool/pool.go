// Package pool 提供高性能事件对象池 + 自动 Arena 管理
//
// 设计：
//   - Acquire/Release 管理 Event 对象复用
//   - EnableArena=true 时，AllocData 自动从 Arena 分配（无 malloc）
//   - Arena 满时自动切换新 chunk，对调用侧透明
package pool

import (
	"sync"
	"unsafe"

	"github.com/uniyakcom/beat/core"
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

	// Arena 配置和管理
	EnableArena  bool
	currentArena *ArenaChunk
	arenaLock    sync.Mutex
	arenaPool    sync.Pool
}

// New 创建事件对象池
func New() *EventPool {
	p := &EventPool{
		EnableArena: true, // 默认启用 Arena
		pool: sync.Pool{
			New: func() interface{} { return &core.Event{} },
		},
		arenaPool: sync.Pool{
			New: func() interface{} { return newArenaChunk() },
		},
	}
	// 初始化第一个 Arena
	p.currentArena = p.arenaPool.Get().(*ArenaChunk)
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
	*(*[24]byte)(unsafe.Pointer(&evt.Timestamp)) = [24]byte{}
	p.pool.Put(evt)
}

// AllocData 分配 Data
// 若 EnableArena=true，从 Arena 分配（无 malloc）
// 若 EnableArena=false，直接 make（传统方式）
func (p *EventPool) AllocData(n int) []byte {
	if !p.EnableArena {
		return make([]byte, n)
	}

	p.arenaLock.Lock()
	defer p.arenaLock.Unlock()

	buf := p.currentArena.Alloc(n)
	if buf == nil {
		// 当前 Arena 满，切换新 Arena
		p.arenaPool.Put(p.currentArena)
		p.currentArena = p.arenaPool.Get().(*ArenaChunk)
		buf = p.currentArena.Alloc(n)
	}
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
func SetEnableArena(enable bool) { global.EnableArena = enable }
