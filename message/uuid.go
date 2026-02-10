package message

import (
	"crypto/rand"
	"encoding/hex"
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

// UUIDGenerator UUID 生成器接口（可替换为确定性生成器用于测试）。
type UUIDGenerator interface {
	NewUUID() string
}

// defaultGenerator 默认使用 crypto/rand。
type defaultGenerator struct{}

func (defaultGenerator) NewUUID() string { return NewUUID() }

// DefaultUUIDGenerator 返回默认 UUID 生成器。
func DefaultUUIDGenerator() UUIDGenerator {
	return defaultGenerator{}
}
