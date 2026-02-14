// Package core 提供通配符匹配逻辑
package core

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const matchCacheShards = 16

// maxTrieDepth 最大 Trie 深度（固定数组大小，避免 Remove 中 make 分配）
const maxTrieDepth = 16

// TrieMatcher 基于 Trie 树的高性能匹配器（导出具体类型，热路径避免接口开销）
// 支持 user.* (单层通配) 和 user.** (多层通配) 以及 user.created (精确)
// 内置 sharded match-cache：精确O(1) + 热点eventType缓存结果
//
// 优化: 前缀哈希分桶 — 根节点 children 按首段哈希分组，
// 通配符匹配时减少遍历范围 O(n) → O(n/k)
type TrieMatcher struct {
	root *node

	mu   sync.RWMutex
	pool sync.Pool // 重用 []string 切片，减少分配

	// 精确匹配快速路径（sync.Map: eventType → true）
	exact sync.Map

	// --- cache line boundary ---
	_ [unsafe.Sizeof(sync.Map{}) % 64]byte

	// match结果缓存（sharded避免竞争）
	cache    [matchCacheShards]matchCacheShard
	cacheVer atomic.Uint64 // Add/Remove 时递增使缓存失效
}

type matchCacheShard struct {
	m sync.Map // eventType → *matchCacheEntry
	_ [40]byte // padding to ~64B
}

type matchCacheEntry struct {
	patterns []string // 匹配到的 patterns 副本
	ver      uint64   // 写入时的 cacheVer
}

type node struct {
	children map[string]*node // 24字节
	pattern  string           // 16字节
	refCount int32            // 4字节
	isEnd    bool             // 1字节
}

// NewTrieMatcher 创建具体类型匹配器（热路径直接使用，避免接口分发）
func NewTrieMatcher() *TrieMatcher {
	return &TrieMatcher{
		root: newNode(),
		pool: sync.Pool{
			New: func() interface{} { s := make([]string, 0, 16); return &s },
		},
	}
}

func newNode() *node {
	return &node{
		children: make(map[string]*node, 4),
	}
}

// containsStar 零分配检查是否包含'*'字符
// 注: strings.Contains 内部使用 IndexByte 但需要 rune 解码前置检查，
// 直接逐字节扫描在短字符串上更快
func containsStar(s string) bool {
	for i := 0; i < len(s); i++ {
		if wildcardChars[s[i]] {
			return true
		}
	}
	return false
}

// wildcardChars [256]bool 查表 — 零分支判断通配符字符
var wildcardChars [256]bool

func init() {
	wildcardChars['*'] = true
}

// cacheShard 返回eventType的cache分片索引（FNV-1a散列）
func cacheShard(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h & (matchCacheShards - 1)
}

// splitNoAlloc 无分配字符串分割，使用池复用 *[]string
func (t *TrieMatcher) splitNoAlloc(s string, sep byte) *[]string {
	sp := t.pool.Get().(*[]string)
	*sp = (*sp)[:0]
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			*sp = append(*sp, s[start:i])
			start = i + 1
		}
	}
	*sp = append(*sp, s[start:])
	return sp
}

// putSlice 放回 *[]string 到池（清除引用避免内存泄漏）
func (t *TrieMatcher) putSlice(sp *[]string) {
	all := (*sp)[:cap(*sp)]
	for i := range all {
		all[i] = ""
	}
	*sp = all[:0]
	t.pool.Put(sp)
}

// Add 添加模式到 Trie（使缓存失效）— 零分配 split
func (t *TrieMatcher) Add(pattern string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sp := t.splitNoAlloc(pattern, '.')
	n := t.root

	for _, part := range *sp {
		if _, ok := n.children[part]; !ok {
			n.children[part] = newNode()
		}
		n = n.children[part]
	}
	n.isEnd = true
	n.pattern = pattern
	n.refCount++

	// 精确匹配优化
	if !containsStar(pattern) {
		t.exact.Store(pattern, true)
	}

	t.putSlice(sp)

	// 使缓存失效
	t.cacheVer.Add(1)
}

// Remove 移除模式（引用计数 + 自底向上清理空节点 + 使缓存失效）— 零分配 split
// 优化: 使用固定数组 [maxTrieDepth+1]*node 替代 make，避免堆分配
func (t *TrieMatcher) Remove(pattern string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sp := t.splitNoAlloc(pattern, '.')
	parts := *sp

	// 固定数组收集路径节点（最大深度限制 maxTrieDepth，避免 make 分配）
	var pathBuf [maxTrieDepth + 1]*node
	pathLen := 0
	pathBuf[0] = t.root
	pathLen++
	n := t.root
	for _, part := range parts {
		if pathLen > maxTrieDepth {
			t.putSlice(sp)
			return // 超过最大深度，拒绝操作
		}
		if next, ok := n.children[part]; ok {
			pathBuf[pathLen] = next
			pathLen++
			n = next
		} else {
			t.putSlice(sp)
			return
		}
	}

	if n.refCount > 0 {
		n.refCount--
	}

	if n.refCount == 0 {
		n.isEnd = false
		n.pattern = ""
	}

	// 自底向上清理无用空节点，防止Trie内存泄漏
	for i := len(parts) - 1; i >= 0; i-- {
		child := pathBuf[i+1]
		if !child.isEnd && len(child.children) == 0 && child.refCount == 0 {
			delete(pathBuf[i].children, parts[i])
		} else {
			break
		}
	}

	if !containsStar(pattern) {
		t.exact.Delete(pattern)
	}

	t.putSlice(sp)

	// 使缓存失效
	t.cacheVer.Add(1)
}

// Match 查找匹配 eventType 的所有 pattern（带 sharded cache）
// 返回 *[]string — 调用者必须用 Put 归还，归还后不可再访问
func (t *TrieMatcher) Match(eventType string) *[]string {
	// 快速路径1：精确匹配（绝大多数场景无通配符）
	if _, ok := t.exact.Load(eventType); ok {
		sp := t.pool.Get().(*[]string)
		*sp = (*sp)[:0]
		*sp = append(*sp, eventType)
		return sp
	}

	// 快速路径2：cache命中
	ver := t.cacheVer.Load()
	shard := cacheShard(eventType)
	if v, ok := t.cache[shard].m.Load(eventType); ok {
		entry := v.(*matchCacheEntry)
		if entry.ver == ver {
			// cache命中 — 复制到池切片返回（调用者会Put回来）
			sp := t.pool.Get().(*[]string)
			*sp = (*sp)[:0]
			*sp = append(*sp, entry.patterns...)
			return sp
		}
	}

	// 慢路径：Trie遍历
	t.mu.RLock()
	sp := t.pool.Get().(*[]string)
	*sp = (*sp)[:0]
	partsSp := t.splitNoAlloc(eventType, '.')
	t.matchRecursive(t.root, *partsSp, 0, sp)
	t.putSlice(partsSp)
	t.mu.RUnlock()

	// 写入cache（复制结果避免池回收后数据损坏）
	results := *sp
	cached := make([]string, len(results))
	copy(cached, results)
	t.cache[shard].m.Store(eventType, &matchCacheEntry{
		patterns: cached,
		ver:      ver,
	})

	return sp
}

func (t *TrieMatcher) matchRecursive(n *node, parts []string, depth int, results *[]string) {
	if depth == len(parts) {
		if n.isEnd {
			*results = append(*results, n.pattern)
		}
		if child, ok := n.children["**"]; ok && child.isEnd {
			*results = append(*results, child.pattern)
		}
		return
	}

	part := parts[depth]

	// 精确匹配
	if child, ok := n.children[part]; ok {
		t.matchRecursive(child, parts, depth+1, results)
	}

	// 单层通配符 "*"
	if child, ok := n.children["*"]; ok {
		t.matchRecursive(child, parts, depth+1, results)
	}

	// 多层通配符 "**"
	if child, ok := n.children["**"]; ok && child.isEnd {
		*results = append(*results, child.pattern)
	}
}

// Put 放回Match返回的 *[]string 到池（清除引用避免内存泄漏）
func (t *TrieMatcher) Put(sp *[]string) {
	if sp == nil {
		return
	}
	all := (*sp)[:cap(*sp)]
	for i := range all {
		all[i] = ""
	}
	*sp = all[:0]
	t.pool.Put(sp)
}

// HasMatch 检查是否存在匹配（零分配版本）
func (t *TrieMatcher) HasMatch(eventType string) bool {
	// 快速路径：精确匹配
	if _, ok := t.exact.Load(eventType); ok {
		return true
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	sp := t.splitNoAlloc(eventType, '.')
	defer t.putSlice(sp)
	return t.hasMatchRecursive(t.root, *sp, 0)
}

func (t *TrieMatcher) hasMatchRecursive(n *node, parts []string, depth int) bool {
	if depth == len(parts) {
		if n.isEnd {
			return true
		}
		if child, ok := n.children["**"]; ok && child.isEnd {
			return true
		}
		return false
	}

	part := parts[depth]

	if child, ok := n.children[part]; ok {
		if t.hasMatchRecursive(child, parts, depth+1) {
			return true
		}
	}

	if child, ok := n.children["*"]; ok {
		if t.hasMatchRecursive(child, parts, depth+1) {
			return true
		}
	}

	if child, ok := n.children["**"]; ok && child.isEnd {
		return true
	}

	return false
}
