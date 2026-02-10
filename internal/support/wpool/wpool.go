// Package wpool 提供轻量级固定大小 worker pool，替代第三方依赖。
//
// 设计：分片 channel（每 worker 独占一个 chan）消除多 worker 竞争，
// Submit 按轮转（atomic counter）分派任务，无锁无 defer。
// Release 关闭全部 chan 并等待所有 worker 退出。
package wpool

import (
	"sync"
	"sync/atomic"
)

// Pool 固定大小 goroutine 池，分片 channel 架构
type Pool struct {
	shards []chan func()
	size   uint64
	cursor atomic.Uint64
	wg     sync.WaitGroup
	closed atomic.Bool
}

// New 创建 worker pool，size 为 worker 数量，queueSize 为每个分片的缓冲区大小。
// queueSize <= 0 时默认为 256。
func New(size, queueSize int) *Pool {
	if size <= 0 {
		size = 1
	}
	if queueSize <= 0 {
		queueSize = 256
	}
	p := &Pool{
		shards: make([]chan func(), size),
		size:   uint64(size),
	}
	p.wg.Add(size)
	for i := 0; i < size; i++ {
		p.shards[i] = make(chan func(), queueSize)
		go p.worker(p.shards[i])
	}
	return p
}

func (p *Pool) worker(ch <-chan func()) {
	defer p.wg.Done()
	for fn := range ch {
		fn()
	}
}

// Submit 提交任务到池。池关闭后 Submit 为 no-op。
// 使用原子轮转分派到不同分片，消除 channel 竞争。
func (p *Pool) Submit(task func()) {
	if p.closed.Load() {
		return
	}
	idx := p.cursor.Add(1) % p.size
	p.shards[idx] <- task
}

// Release 关闭池并等待所有已提交任务执行完毕。
func (p *Pool) Release() {
	p.closed.Store(true)
	for _, ch := range p.shards {
		close(ch)
	}
	p.wg.Wait()
}
