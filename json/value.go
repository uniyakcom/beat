package json

import "unsafe"

// Type JSON 值类型
type Type uint8

const (
	TypeNull   Type = iota // null
	TypeBool               // true / false
	TypeNumber             // 整数或浮点数
	TypeString             // 字符串
	TypeArray              // 数组
	TypeObject             // 对象
)

// String 返回类型名称
func (t Type) String() string {
	switch t {
	case TypeNull:
		return "null"
	case TypeBool:
		return "bool"
	case TypeNumber:
		return "number"
	case TypeString:
		return "string"
	case TypeArray:
		return "array"
	case TypeObject:
		return "object"
	default:
		return "unknown"
	}
}

// Value JSON 值（零分配设计）
//
// 所有字符串/数字都以原始 JSON 字节的切片形式存储，
// 不做额外内存分配。Value 由 Parser 的 cache 管理生命周期。
//
// 结构布局受 valyala/fastjson Value 启发（o/a/s/t 四字段模式），
// 差异: yak 将数字存储在独立的 n 字段（延迟解析），布尔值用 b 字段
// （fastjson 用 TypeTrue/TypeFalse 两个类型常量代替）。
//   - o: 对象的键值对（有序，借鉴 fastjson Object/kv 设计）
//   - a: 数组的子值
//   - s: 字符串值（指向原始 JSON）
//   - n: 数字的原始字节（延迟解析，原创）
//   - t: 值类型
//   - b: 布尔值（原创，fastjson 用两个类型常量代替）
type Value struct {
	o kvPairs  // TypeObject: 有序键值对
	a []*Value // TypeArray: 子值数组
	s string   // TypeString: 已解转义的字符串
	n string   // TypeNumber: 原始数字字面量（延迟解析）
	t Type     // 值类型
	b bool     // TypeBool: 布尔值
}

// kvPairs 有序键值对（保持 JSON 中的字段顺序）
type kvPairs struct {
	kvs           []kv
	keysUnescaped bool
}

type kv struct {
	k string // key（指向原始 JSON 的切片或解转义后的 copy）
	v *Value
}

func (o *kvPairs) reset() {
	o.kvs = o.kvs[:0]
	o.keysUnescaped = false
}

func (o *kvPairs) getKV() *kv {
	if cap(o.kvs) > len(o.kvs) {
		o.kvs = o.kvs[:len(o.kvs)+1]
	} else {
		o.kvs = append(o.kvs, kv{})
	}
	return &o.kvs[len(o.kvs)-1]
}

// ─── 类型判断 ───

// Type 返回值类型
func (v *Value) Type() Type {
	if v == nil {
		return TypeNull
	}
	return v.t
}

// IsNull 是否为 null
func (v *Value) IsNull() bool { return v == nil || v.t == TypeNull }

// IsObject 是否为对象
func (v *Value) IsObject() bool { return v != nil && v.t == TypeObject }

// IsArray 是否为数组
func (v *Value) IsArray() bool { return v != nil && v.t == TypeArray }

// ─── 值获取（安全: 类型不匹配返回零值） ───

// GetString 获取字符串值，支持嵌套路径: v.GetString("user", "name")
func (v *Value) GetString(keys ...string) string {
	v = v.Get(keys...)
	if v == nil || v.t != TypeString {
		return ""
	}
	return v.s
}

// GetStringBytes 获取字符串值的字节切片（零分配）
func (v *Value) GetStringBytes(keys ...string) []byte {
	s := v.GetString(keys...)
	if len(s) == 0 {
		return nil
	}
	return s2b(s)
}

// GetInt 获取整数值
func (v *Value) GetInt(keys ...string) int {
	v = v.Get(keys...)
	if v == nil || v.t != TypeNumber {
		return 0
	}
	n, _ := parseInt(v.n)
	return int(n)
}

// GetInt64 获取 64 位整数值
func (v *Value) GetInt64(keys ...string) int64 {
	v = v.Get(keys...)
	if v == nil || v.t != TypeNumber {
		return 0
	}
	n, _ := parseInt(v.n)
	return n
}

// GetFloat64 获取浮点数值
func (v *Value) GetFloat64(keys ...string) float64 {
	v = v.Get(keys...)
	if v == nil || v.t != TypeNumber {
		return 0
	}
	f, _ := parseFloat(v.n)
	return f
}

// GetBool 获取布尔值
func (v *Value) GetBool(keys ...string) bool {
	v = v.Get(keys...)
	if v == nil || v.t != TypeBool {
		return false
	}
	return v.b
}

// Get 按路径获取嵌套值
//
//	v.Get("user", "name")  // 获取 {"user":{"name":"..."}} 中的 name
//	v.Get("items", "0")    // 获取数组第 0 个元素
func (v *Value) Get(keys ...string) *Value {
	if v == nil {
		return nil
	}
	for _, key := range keys {
		if v == nil {
			return nil
		}
		switch v.t {
		case TypeObject:
			v = v.objGet(key)
		case TypeArray:
			idx, ok := parseIdx(key)
			if !ok || idx < 0 || idx >= len(v.a) {
				return nil
			}
			v = v.a[idx]
		default:
			return nil
		}
	}
	return v
}

// objGet 在对象中查找 key（线性扫描，JSON 对象通常字段少）
func (v *Value) objGet(key string) *Value {
	for i := range v.o.kvs {
		if v.o.kvs[i].k == key {
			return v.o.kvs[i].v
		}
	}
	return nil
}

// Len 返回数组或对象的元素数量
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	switch v.t {
	case TypeArray:
		return len(v.a)
	case TypeObject:
		return len(v.o.kvs)
	default:
		return 0
	}
}

// ArrayEach 遍历数组元素，返回 false 停止遍历
func (v *Value) ArrayEach(fn func(i int, val *Value) bool) {
	if v == nil || v.t != TypeArray {
		return
	}
	for i, elem := range v.a {
		if !fn(i, elem) {
			return
		}
	}
}

// ObjectEach 遍历对象键值对（保持 JSON 字段顺序），返回 false 停止遍历
func (v *Value) ObjectEach(fn func(key string, val *Value) bool) {
	if v == nil || v.t != TypeObject {
		return
	}
	for i := range v.o.kvs {
		if !fn(v.o.kvs[i].k, v.o.kvs[i].v) {
			return
		}
	}
}

// Raw 返回原始数字字面量（仅 TypeNumber）
func (v *Value) Raw() string {
	if v == nil {
		return ""
	}
	if v.t == TypeNumber {
		return v.n
	}
	if v.t == TypeString {
		return v.s
	}
	return ""
}

// ─── 辅助函数 ───

func parseIdx(s string) (int, bool) {
	if len(s) == 0 || len(s) > 10 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n < 0 {
			return 0, false // 溢出保护（32 位平台）
		}
	}
	return n, true
}

// s2b 零拷贝 string → []byte
func s2b(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// b2s 零拷贝 []byte → string
func b2s(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
