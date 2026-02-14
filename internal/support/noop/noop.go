// Package noop 提供可切换锁
//
// 当 safe=false 时 Lock/Unlock 为空操作（零开销），
// 适用于 per-goroutine 独占数据结构或单线程场景。
//
//	mu := noop.NewMutex(true)  // 并发安全模式
//	mu := noop.NewMutex(false) // 单线程模式（零开销）
package noop

import "sync"

// Mutex 可切换互斥锁 — nil 时 Lock/Unlock 为空操作
type Mutex struct {
	mu *sync.Mutex
}

// NewMutex 创建可切换互斥锁。safe=true 启用真实锁，false 则零开销。
func NewMutex(safe bool) Mutex {
	if safe {
		return Mutex{mu: &sync.Mutex{}}
	}
	return Mutex{}
}

// Lock 加锁（nil 时为空操作）
func (m Mutex) Lock() {
	if m.mu != nil {
		m.mu.Lock()
	}
}

// Unlock 解锁（nil 时为空操作）
func (m Mutex) Unlock() {
	if m.mu != nil {
		m.mu.Unlock()
	}
}

// RWMutex 可切换读写锁 — nil 时所有操作为空操作
type RWMutex struct {
	mu *sync.RWMutex
}

// NewRWMutex 创建可切换读写锁。safe=true 启用真实锁，false 则零开销。
func NewRWMutex(safe bool) RWMutex {
	if safe {
		return RWMutex{mu: &sync.RWMutex{}}
	}
	return RWMutex{}
}

// Lock 写锁
func (m RWMutex) Lock() {
	if m.mu != nil {
		m.mu.Lock()
	}
}

// Unlock 写解锁
func (m RWMutex) Unlock() {
	if m.mu != nil {
		m.mu.Unlock()
	}
}

// RLock 读锁
func (m RWMutex) RLock() {
	if m.mu != nil {
		m.mu.RLock()
	}
}

// RUnlock 读解锁
func (m RWMutex) RUnlock() {
	if m.mu != nil {
		m.mu.RUnlock()
	}
}
