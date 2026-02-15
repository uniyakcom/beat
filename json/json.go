// Package json 高性能 JSON 解析与序列化库
//
// 设计原则（综合 fastjson、jsonparser、gjson、encoding/json 最佳实践）:
//   - 零分配解析: 所有字符串指向原始 JSON 字节，不做额外拷贝
//   - 池化复用: Parser/Writer 通过 sync.Pool 复用，并发安全
//   - 兼容标准库: 支持 json.Marshaler/Unmarshaler 接口
//   - 安全校验: 完整的 JSON 语法校验，防止注入和畸形数据
//   - 流式序列化: Writer 直接向 []byte 追加，避免中间缓冲
//
// 致谢 (Acknowledgments):
//
//	本库的部分设计模式和优化技巧受以下优秀开源项目启发：
//	- valyala/fastjson (MIT License): Parser+cache 缓存池架构、Value 结构体布局、
//	  kv 有序键值对设计、全局 true/false/null 单例模式
//	- tidwall/gjson (MIT License): vch[256] 字符分类查找表、int(c)-2 深度追踪公式、
//	  8 字节批量 skipNested 扫描技巧（源自 parseSquash）
//	- buger/jsonparser (MIT License): 栈上 [64]byte 缓冲避免小字符串堆分配
//	所有代码均为独立重写，核心创新包括：索引模式解析引擎、内联 WS 跳过、
//	8 字节批量 rawStr（> '\' 技巧）、Get() 零分配懒查询 API、
//	objFind key 长度预判 + 字节级比较、Writer 回调构建器模式。
//
// 用法:
//
//	// 解析
//	var p json.Parser
//	v, err := p.Parse(`{"name":"yak","version":1}`)
//	name := v.GetString("name")    // "yak"
//	ver  := v.GetInt("version")    // 1
//
//	// 序列化（零分配追加到已有 buffer）
//	w := json.AcquireWriter()
//	w.Object(func(w *json.Writer) {
//	    w.Field("name", "yak")
//	    w.FieldInt("version", 1)
//	})
//	data := w.Bytes()  // {"name":"yak","version":1}
//	json.ReleaseWriter(w)
//
//	// 兼容标准库
//	data, err := json.Marshal(myStruct)
//	err := json.Unmarshal(data, &myStruct)
package json

import (
	"fmt"
	"reflect"
)

// MaxDepth JSON 嵌套最大深度（防栈溢出攻击）
const MaxDepth = 512

// MaxKeyLength 单个键名最大长度
const MaxKeyLength = 1 << 16 // 64KB

// MaxStringLength 单个字符串值最大长度
const MaxStringLength = 1 << 24 // 16MB

// MaxArrayLength 单个数组最大元素数（防内存耗尽攻击）
const MaxArrayLength = 1 << 20 // 1M 元素

// MaxObjectKeys 单个对象最大键数（防 O(n²) 查找退化）
const MaxObjectKeys = 1 << 16 // 64K 键

// MaxMarshalDepth Marshal 序列化最大递归深度（防栈溢出）
const MaxMarshalDepth = 1000

// ─── 兼容接口 ───

// Marshaler JSON 序列化接口（兼容 encoding/json.Marshaler）
type Marshaler interface {
	MarshalJSON() ([]byte, error)
}

// Unmarshaler JSON 反序列化接口（兼容 encoding/json.Unmarshaler）
type Unmarshaler interface {
	UnmarshalJSON([]byte) error
}

// RawMessage 原始 JSON 消息（兼容 encoding/json.RawMessage）
//
// 它实现了 Marshaler 和 Unmarshaler 接口，
// 可用于延迟 JSON 解码或预计算 JSON 编码。
type RawMessage []byte

// MarshalJSON 返回 m 的 JSON 编码。
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON 设置 *m 为 data 的副本。
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return fmt.Errorf("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[:0], data...)
	return nil
}

// InvalidUnmarshalError 描述传递给 Unmarshal 的无效参数。
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "json: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "json: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "json: Unmarshal(nil " + e.Type.String() + ")"
}
