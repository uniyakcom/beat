package message

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	mrand "math/rand"
	"sync"
)

// NewUUID 生成 UUID v4（零外部依赖，基于 crypto/rand）。
func NewUUID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10

	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf[:])
}

// fastRand 为 NewFastUUID 提供的无锁 RNG 源。
// 使用 crypto/rand 种子初始化，之后通过 math/rand 生成。
var fastRand struct {
	once sync.Once
	src  *mrand.Rand
	mu   sync.Mutex
}

func initFastRand() {
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	s := int64(binary.LittleEndian.Uint64(seed[:]))
	fastRand.src = mrand.New(mrand.NewSource(s))
}

// FastUUID 生成 UUID v4（math/rand，非密码学安全，高吞吐场景使用）。
//
// 适用于不需要密码学随机性的消息 ID，比 NewUUID (crypto/rand) 快约 10 倍。
// 首次调用时使用 crypto/rand 初始化种子。
func FastUUID() string {
	fastRand.once.Do(initFastRand)

	var u [16]byte
	fastRand.mu.Lock()
	binary.LittleEndian.PutUint64(u[0:8], fastRand.src.Uint64())
	binary.LittleEndian.PutUint64(u[8:16], fastRand.src.Uint64())
	fastRand.mu.Unlock()

	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10

	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf[:])
}

// IDGen UUID 生成器接口（可替换为确定性生成器用于测试）。
type IDGen interface {
	NewUUID() string
}

// defaultGenerator 默认使用 crypto/rand。
type defaultGenerator struct{}

func (defaultGenerator) NewUUID() string { return NewUUID() }

// DefaultIDGen 返回默认 UUID 生成器。
func DefaultIDGen() IDGen {
	return defaultGenerator{}
}
