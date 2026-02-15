package json

import (
	"fmt"
	"sync"
)

// Parser JSON 解析器（零分配，可复用）
//
// Parser 维护一个 Value 对象缓存池，避免逐个分配。
// 注意: Parser 不是并发安全的，并发场景请使用 ParserPool。
//
// 用法:
//
//	var p json.Parser
//	v, err := p.Parse(`{"key":"value"}`)
//	fmt.Println(v.GetString("key")) // "value"
type Parser struct {
	c cache
}

// cache Value 对象缓存
//
// 灵感来源: valyala/fastjson cache — 预分配 []Value 切片，
// 通过 append 增长而非逐个 new，解析结束后 reset 长度为 0 复用底层数组。
// yak 在此基础上将解析引擎改为索引模式 (s, i) 而非 fastjson 的字符串切片模式。
type cache struct {
	vs []Value
}

func (c *cache) reset() { c.vs = c.vs[:0] }

func (c *cache) getVal() *Value {
	if cap(c.vs) > len(c.vs) {
		c.vs = c.vs[:len(c.vs)+1]
	} else {
		c.vs = append(c.vs, Value{})
	}
	return &c.vs[len(c.vs)-1]
}

// Parse 解析 JSON 字符串，返回根 Value
//
// 返回的 Value 的生命周期绑定到 Parser。
// 下次调用 Parse 时之前的 Value 会失效。
func (p *Parser) Parse(s string) (*Value, error) {
	p.c.reset()
	n := len(s)
	i := 0
	for i < n && s[i] <= ' ' {
		i++
	}
	if i >= n {
		return nil, fmt.Errorf("json: empty input")
	}
	v, i, err := parseVal(s, i, &p.c, 0)
	if err != nil {
		return nil, err
	}
	for i < n && s[i] <= ' ' {
		i++
	}
	if i < n {
		return nil, fmt.Errorf("json: unexpected trailing data: %.32q", s[i:])
	}
	return v, nil
}

// ParseBytes 解析 JSON 字节切片
func (p *Parser) ParseBytes(b []byte) (*Value, error) {
	return p.Parse(b2s(b))
}

// ─── ParserPool（并发安全） ───

// ParserPool 并发安全的 Parser 池
var ParserPool = sync.Pool{
	New: func() any { return new(Parser) },
}

// AcquireParser 从池中获取 Parser
func AcquireParser() *Parser {
	return ParserPool.Get().(*Parser)
}

// ReleaseParser 归还 Parser 到池中
func ReleaseParser(p *Parser) {
	ParserPool.Put(p)
}

// ─── 全局单例: true/false/null（不消耗 cache 槽位） ───
// 灵感来源: valyala/fastjson valueTrue/valueFalse/valueNull 单例

var (
	valueTrue  = &Value{t: TypeBool, b: true}
	valueFalse = &Value{t: TypeBool, b: false}
	valueNull  = &Value{t: TypeNull}
)

// ─── 核心解析引擎（索引模式 + 内联 WS 跳过） ───

// parseVal 解析任意 JSON 值
//
// 索引模式: 接受 (s, i) 返回 (*Value, newI, error)
// 避免字符串切片，减少 StringHeader 拷贝开销
func parseVal(s string, i int, c *cache, depth int) (*Value, int, error) {
	if i >= len(s) {
		return nil, i, fmt.Errorf("json: unexpected end of input")
	}
	if depth > MaxDepth {
		return nil, i, fmt.Errorf("json: max depth %d exceeded", MaxDepth)
	}
	switch s[i] {
	case '{':
		return parseObj(s, i+1, c, depth+1)
	case '[':
		return parseArr(s, i+1, c, depth+1)
	case '"':
		return parseStr(s, i, c)
	case 't':
		if i+3 < len(s) && s[i+1] == 'r' && s[i+2] == 'u' && s[i+3] == 'e' {
			return valueTrue, i + 4, nil
		}
		return nil, i, fmt.Errorf("json: invalid value at offset %d", i)
	case 'f':
		if i+4 < len(s) && s[i+1] == 'a' && s[i+2] == 'l' && s[i+3] == 's' && s[i+4] == 'e' {
			return valueFalse, i + 5, nil
		}
		return nil, i, fmt.Errorf("json: invalid value at offset %d", i)
	case 'n':
		if i+3 < len(s) && s[i+1] == 'u' && s[i+2] == 'l' && s[i+3] == 'l' {
			return valueNull, i + 4, nil
		}
		return nil, i, fmt.Errorf("json: invalid value at offset %d", i)
	default:
		if s[i] == '-' || (s[i] >= '0' && s[i] <= '9') {
			return parseNum(s, i, c)
		}
		return nil, i, fmt.Errorf("json: unexpected character %q at offset %d", s[i], i)
	}
}

// parseObj 解析 JSON 对象（s[i-1] == '{'，i 指向 '{' 后）
//
// 优化:
//   - 内联 key 解析（避免独立函数调用）
//   - 内联 WS 跳过（消除 skipWS 函数调用开销）
//   - 快速路径: 短无转义 key 直接零拷贝
func parseObj(s string, i int, c *cache, depth int) (*Value, int, error) {
	v := c.getVal()
	v.t = TypeObject
	v.s = ""
	v.n = ""
	v.a = nil
	v.o.reset()
	n := len(s)
	for i < n && s[i] <= ' ' {
		i++
	}
	if i >= n {
		return nil, i, fmt.Errorf("json: unexpected end of object")
	}
	if s[i] == '}' {
		return v, i + 1, nil
	}
	for {
		// skip WS before key
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n {
			return nil, i, fmt.Errorf("json: unexpected end of object")
		}
		if s[i] != '"' {
			return nil, i, fmt.Errorf("json: object key must be string, got %q", s[i])
		}
		kv := v.o.getKV()
		// 安全: 检查对象键数上限，防止 O(n²) 查找退化
		if len(v.o.kvs) > MaxObjectKeys {
			return nil, i, fmt.Errorf("json: object has too many keys (%d > %d)", len(v.o.kvs), MaxObjectKeys)
		}

		// 内联 key 解析: 短无转义 key 直接零拷贝
		i++ // skip opening '"'
		ks := i
		for i < n {
			if s[i] == '"' {
				kv.k = s[ks:i]
				i++ // skip closing '"'
				goto keyDone
			}
			if s[i] == '\\' {
				// fallback 到完整字符串解析
				var err error
				kv.k, i, _, err = rawStr(s, ks-1)
				if err != nil {
					return nil, i, fmt.Errorf("json: invalid object key: %w", err)
				}
				goto keyDone
			}
			i++
		}
		return nil, i, fmt.Errorf("json: unterminated key")
	keyDone:
		if len(kv.k) > MaxKeyLength {
			return nil, i, fmt.Errorf("json: key too long (%d > %d)", len(kv.k), MaxKeyLength)
		}
		// skip WS + colon
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n || s[i] != ':' {
			return nil, i, fmt.Errorf("json: missing ':' after key")
		}
		i++ // skip ':'
		for i < n && s[i] <= ' ' {
			i++
		}
		// parse value
		var err error
		kv.v, i, err = parseVal(s, i, c, depth)
		if err != nil {
			return nil, i, err
		}
		// skip WS after value
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n {
			return nil, i, fmt.Errorf("json: unexpected end of object")
		}
		if s[i] == ',' {
			i++
			continue
		}
		if s[i] == '}' {
			return v, i + 1, nil
		}
		return nil, i, fmt.Errorf("json: expected ',' or '}' in object, got %q", s[i])
	}
}

// parseArr 解析 JSON 数组（s[i-1] == '['，i 指向 '[' 后）
func parseArr(s string, i int, c *cache, depth int) (*Value, int, error) {
	v := c.getVal()
	v.t = TypeArray
	v.s = ""
	v.n = ""
	v.a = v.a[:0]
	v.o.reset()
	n := len(s)

	for i < n && s[i] <= ' ' {
		i++
	}
	if i >= n {
		return nil, i, fmt.Errorf("json: unexpected end of array")
	}
	if s[i] == ']' {
		return v, i + 1, nil
	}
	for {
		for i < n && s[i] <= ' ' {
			i++
		}
		var elem *Value
		var err error
		elem, i, err = parseVal(s, i, c, depth)
		if err != nil {
			return nil, i, err
		}
		v.a = append(v.a, elem)
		// 安全: 检查数组长度上限，防止百万元素导致内存耗尽
		if len(v.a) > MaxArrayLength {
			return nil, i, fmt.Errorf("json: array too long (%d > %d)", len(v.a), MaxArrayLength)
		}
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n {
			return nil, i, fmt.Errorf("json: unexpected end of array")
		}
		if s[i] == ',' {
			i++
			continue
		}
		if s[i] == ']' {
			return v, i + 1, nil
		}
		return nil, i, fmt.Errorf("json: expected ',' or ']' in array, got %q", s[i])
	}
}

// parseStr 解析 JSON 字符串值
func parseStr(s string, i int, c *cache) (*Value, int, error) {
	content, end, _, err := rawStr(s, i)
	if err != nil {
		return nil, end, err
	}
	if len(content) > MaxStringLength {
		return nil, end, fmt.Errorf("json: string too long (%d > %d)", len(content), MaxStringLength)
	}
	v := c.getVal()
	v.t = TypeString
	v.s = content
	v.a = nil
	return v, end, nil
}

// rawStr 解析引号字符串，返回内容（不含引号）、结束位置、是否含转义
//
// 优化技巧来源:
//   - > '\\' 比较技巧: 受 gjson parseString 中字符范围比较启发，
//     利用 '\\' (0x5C) 居于 ASCII 中间的特点，一次比较覆盖所有安全字符
//   - 8 字节批量展开: 原创设计，将上述技巧展开为 8 路检查
//   - 零拷贝快速路径: 受 fastjson parseRawString 启发，无转义时直接返回切片
//   - 栈 buffer: 受 buger/jsonparser unescapeStackBufSize 启发，
//     rawStrSlow 使用 [64]byte 栈缓冲避免小字符串堆分配
func rawStr(s string, i int) (string, int, bool, error) {
	if i >= len(s) || s[i] != '"' {
		return "", i, false, fmt.Errorf("json: expected '\"'")
	}
	i++ // skip opening '"'
	start := i
	n := len(s)

	// 8 字节批量扫描: > '\\' (0x5C) 覆盖 a-z、UTF-8 高字节、{、}
	// 仅 '"' (0x22)、'\\' (0x5C)、控制字符 (<0x20) 需要特殊处理
	for n-i >= 8 {
		if s[i] <= '\\' {
			goto ch
		}
		if s[i+1] <= '\\' {
			i++
			goto ch
		}
		if s[i+2] <= '\\' {
			i += 2
			goto ch
		}
		if s[i+3] <= '\\' {
			i += 3
			goto ch
		}
		if s[i+4] <= '\\' {
			i += 4
			goto ch
		}
		if s[i+5] <= '\\' {
			i += 5
			goto ch
		}
		if s[i+6] <= '\\' {
			i += 6
			goto ch
		}
		if s[i+7] <= '\\' {
			i += 7
			goto ch
		}
		i += 8
		continue
	ch:
		if s[i] == '"' {
			return s[start:i], i + 1, false, nil
		}
		if s[i] == '\\' {
			return rawStrSlow(s, start-1)
		}
		if s[i] < 0x20 {
			return "", i, false, fmt.Errorf("json: invalid control character 0x%02x in string", s[i])
		}
		i++
	}
	// 尾部: 逐字节（剩余 < 8 字节）
	for i < n {
		if s[i] > '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return s[start:i], i + 1, false, nil
		}
		if s[i] == '\\' {
			return rawStrSlow(s, start-1)
		}
		if s[i] < 0x20 {
			return "", i, false, fmt.Errorf("json: invalid control character 0x%02x in string", s[i])
		}
		i++
	}
	return "", n, false, fmt.Errorf("json: unterminated string")
}

// rawStrSlow 慢速路径: 解析含转义字符的字符串
//
// s[i] == '"'，从头重新解析并解转义
// 优化: 栈上 [64]byte 缓冲避免小字符串堆分配
func rawStrSlow(s string, i int) (string, int, bool, error) {
	i++ // skip opening '"'
	n := len(s)
	var stk [64]byte
	var buf []byte
	if n-i <= 64 {
		buf = stk[:0]
	} else {
		buf = make([]byte, 0, n-i)
	}
	for i < n {
		c := s[i]
		if c == '"' {
			return string(buf), i + 1, true, nil
		}
		if c < 0x20 {
			return "", i, false, fmt.Errorf("json: invalid control character 0x%02x in string", c)
		}
		if c != '\\' {
			buf = append(buf, c)
			i++
			continue
		}
		// 转义序列
		i++
		if i >= n {
			return "", i, false, fmt.Errorf("json: unterminated escape sequence")
		}
		switch s[i] {
		case '"', '\\', '/':
			buf = append(buf, s[i])
		case 'b':
			buf = append(buf, '\b')
		case 'f':
			buf = append(buf, '\f')
		case 'n':
			buf = append(buf, '\n')
		case 'r':
			buf = append(buf, '\r')
		case 't':
			buf = append(buf, '\t')
		case 'u':
			if i+4 >= n {
				return "", i, false, fmt.Errorf("json: truncated unicode escape")
			}
			r, sz, err := hexRune(s[i+1:])
			if err != nil {
				return "", i, false, err
			}
			var ubuf [4]byte
			un := encRune(ubuf[:], r)
			buf = append(buf, ubuf[:un]...)
			i += sz
		default:
			return "", i, false, fmt.Errorf("json: invalid escape character %q", s[i])
		}
		i++
	}
	return "", n, false, fmt.Errorf("json: unterminated string")
}

// hexRune 解析 \uXXXX（含 surrogate pair）
func hexRune(s string) (rune, int, error) {
	if len(s) < 4 {
		return 0, 0, fmt.Errorf("json: truncated unicode escape")
	}
	r1 := hexDig(s[:4])
	if r1 < 0 {
		return 0, 0, fmt.Errorf("json: invalid unicode escape: \\u%s", s[:4])
	}
	if r1 < 0xD800 || r1 > 0xDFFF {
		return r1, 4, nil
	}
	if r1 > 0xDBFF {
		return 0, 0, fmt.Errorf("json: invalid high surrogate: \\u%s", s[:4])
	}
	if len(s) < 10 || s[4] != '\\' || s[5] != 'u' {
		return 0, 0, fmt.Errorf("json: missing low surrogate after \\u%s", s[:4])
	}
	r2 := hexDig(s[6:10])
	if r2 < 0xDC00 || r2 > 0xDFFF {
		return 0, 0, fmt.Errorf("json: invalid low surrogate: \\u%s", s[6:10])
	}
	return 0x10000 + (r1-0xD800)*0x400 + (r2 - 0xDC00), 10, nil
}

// hexDig 解析 4 位十六进制数
func hexDig(s string) rune {
	var r rune
	for i := 0; i < 4; i++ {
		c := s[i]
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return -1
		}
	}
	return r
}

// encRune UTF-8 编码（避免 import unicode/utf8）
func encRune(buf []byte, r rune) int {
	if r < 0x80 {
		buf[0] = byte(r)
		return 1
	}
	if r < 0x800 {
		buf[0] = byte(0xC0 | (r >> 6))
		buf[1] = byte(0x80 | (r & 0x3F))
		return 2
	}
	if r < 0x10000 {
		buf[0] = byte(0xE0 | (r >> 12))
		buf[1] = byte(0x80 | ((r >> 6) & 0x3F))
		buf[2] = byte(0x80 | (r & 0x3F))
		return 3
	}
	buf[0] = byte(0xF0 | (r >> 18))
	buf[1] = byte(0x80 | ((r >> 12) & 0x3F))
	buf[2] = byte(0x80 | ((r >> 6) & 0x3F))
	buf[3] = byte(0x80 | (r & 0x3F))
	return 4
}

// parseNum 解析 JSON 数字（延迟解析: 仅保存原始字面量）
func parseNum(s string, i int, c *cache) (*Value, int, error) {
	n := len(s)
	start := i
	if i < n && s[i] == '-' {
		i++
	}
	if i >= n {
		return nil, i, fmt.Errorf("json: unexpected end of number")
	}
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return nil, i, fmt.Errorf("json: invalid number character %q", s[i])
	}
	if i < n && s[i] == '.' {
		i++
		if i >= n || s[i] < '0' || s[i] > '9' {
			return nil, i, fmt.Errorf("json: invalid number: missing digit after '.'")
		}
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= n || s[i] < '0' || s[i] > '9' {
			return nil, i, fmt.Errorf("json: invalid number: missing digit in exponent")
		}
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	v := c.getVal()
	v.t = TypeNumber
	v.s = ""
	v.n = s[start:i]
	v.a = nil
	return v, i, nil
}
