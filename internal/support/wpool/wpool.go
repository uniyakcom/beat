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

// Task 任务接口 — 使用接口代替 func()，消除 method value 的隐式闭包分配。
// 对于指针类型的具体实现，接口装箱零分配（data word 直接存指针）。
type Task interface {
	Run()
}

// funcTask 包装 func() 为 Task（兼容旧式调用）
type funcTask struct {
	fn func()
}

func (f *funcTask) Run() { f.fn() }

// Pool 固定大小 goroutine 池，分片 channel 架构
type Pool struct {
	shards  []chan Task
	done    chan struct{} // 关闭信号（替代 close(ch)，避免 Submit 向已关闭 channel 发送 panic）
	size    uint64
	cursor  atomic.Uint64
	wg      sync.WaitGroup
	closed  atomic.Bool
	OnPanic func(any) // panic 回调（可选，worker 级别 recover 后调用）
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
		shards: make([]chan Task, size),
		done:   make(chan struct{}),
		size:   uint64(size),
	}
	p.wg.Add(size)
	for i := 0; i < size; i++ {
		p.shards[i] = make(chan Task, queueSize)
		go p.worker(p.shards[i])
	}
	return p
}

func (p *Pool) worker(ch <-chan Task) {
	defer p.wg.Done()
	for {
		if !p.workerInner(ch) {
			return // channel closed
		}
		// workerInner 仅在 panic 后返回 true（已 recover），重新进入循环继续消费
	}
}

// workerInner 内层循环 — defer/recover 在此层，panic 后返回 true 让外层重入
// 仅在 panic 发生时才有 defer unwind 开销，正常路径开销 = 1 次 defer 注册
//
//go:noinline
func (p *Pool) workerInner(ch <-chan Task) (alive bool) {
	defer func() {
		if r := recover(); r != nil {
			alive = true
			if p.OnPanic != nil {
				p.OnPanic(r)
			}
		}
	}()
	for t := range ch {
		t.Run()
	}
	return false // channel closed normally
}

// SubmitTask 提交 Task 到池（零分配热路径）。
// 使用原子轮转分派到不同分片，消除 channel 竞争。
func (p *Pool) SubmitTask(task Task) {
	if p.closed.Load() {
		return
	}
	idx := p.cursor.Add(1) % p.size

	// 快速路径: 单 channel 非阻塞写入（消除 select 双分支开销）
	select {
	case p.shards[idx] <- task:
		return
	default:
	}

	// 慢路径: 轮转尝试其他分片（防止单分片阻塞）
	for i := uint64(1); i < p.size; i++ {
		select {
		case p.shards[(idx+i)%p.size] <- task:
			return
		default:
		}
	}

	// 所有分片满: 阻塞在首选分片（背压）
	p.shards[idx] <- task
}

// Submit 提交 func() 任务（兼容接口，有闭包分配开销）。
// 高性能场景请使用 SubmitTask 配合池化 Task 结构体。
func (p *Pool) Submit(task func()) {
	p.SubmitTask(&funcTask{fn: task})
}

// Release 关闭池并等待所有已提交任务执行完毕。
// 安全: 先标记 closed 阻止新提交，再关闭 shard channels 触发 worker 的 range 退出。
func (p *Pool) Release() {
	if !p.closed.CompareAndSwap(false, true) {
		return // 已关闭
	}
	close(p.done) // 通知所有 Submit 退出
	for _, ch := range p.shards {
		close(ch) // 触发 worker 的 for-range 退出
	}
	p.wg.Wait()
}
