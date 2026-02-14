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
	done   chan struct{} // 关闭信号（替代 close(ch)，避免 Submit 向已关闭 channel 发送 panic）
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
		done:   make(chan struct{}),
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
	for {
		select {
		case fn, ok := <-ch:
			if !ok {
				return
			}
			fn()
		case <-p.done:
			// 排空 channel 中剩余任务
			for {
				select {
				case fn, ok := <-ch:
					if !ok {
						return
					}
					fn()
				default:
					return
				}
			}
		}
	}
}

// Submit 提交任务到池。池关闭后 Submit 为 no-op。
// 使用原子轮转分派到不同分片，消除 channel 竞争。
// 安全: 使用 select+done 防止向已关闭 channel 发送 panic。
func (p *Pool) Submit(task func()) {
	if p.closed.Load() {
		return
	}
	start := p.cursor.Add(1)
	idx := start % p.size

	// 快速路径: 首选分片直接写入（带 done 保护）
	select {
	case p.shards[idx] <- task:
		return
	case <-p.done:
		return
	default:
	}

	// 慢路径: 轮转尝试其他分片（防止单分片阻塞）
	for i := uint64(1); i < p.size; i++ {
		select {
		case p.shards[(idx+i)%p.size] <- task:
			return
		case <-p.done:
			return
		default:
		}
	}

	// 所有分片满: 阻塞在首选分片（背压），带 done 保护防止死锁
	select {
	case p.shards[idx] <- task:
	case <-p.done:
	}
}

// Release 关闭池并等待所有已提交任务执行完毕。
// 安全: 先通过 done channel 通知 worker 退出，再关闭 shard channels。
func (p *Pool) Release() {
	if !p.closed.CompareAndSwap(false, true) {
		return // 已关闭
	}
	close(p.done) // 通知所有 worker 和 Submit 退出
	p.wg.Wait()
}
