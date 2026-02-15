package json

// Res 懒查询结果（轻量值类型，40 字节）
//
// 与 Value 的区别:
//   - Value 构建完整 DOM 树（适合多次访问）
//   - Res 零分配惰性扫描（适合单次取值）
//
// 对比 gjson.Result (Raw, Str, Num, Type, Index, Indexes ≈ 120 字节)，
// Res 仅保留 3 个字段 (raw, str, typ = 40 字节)，减少结构体拷贝开销。
// API 设计受 gjson.Result 启发（String/Int/Float64/Bool/Exists 方法），
// 但内部实现完全不同。
//
// 设计特点:
//   - raw: 原始 JSON 切片（零拷贝）
//   - str: 字符串内容（已解转义）
//   - typ: 值类型
type Res struct {
	raw string // 原始 JSON（如 `"dark"`、`42`、`{"a":1}`）
	str string // TypeString: 不含引号的内容；其他类型: 空
	typ Type
}

// String 返回字符串值
//
// TypeString → 解转义后的内容
// 其他类型 → 原始 JSON 文本
func (r Res) String() string {
	if r.typ == TypeString {
		return r.str
	}
	return r.raw
}

// Int 返回 int64 值（非数字返回 0）
func (r Res) Int() int64 {
	if r.typ != TypeNumber {
		return 0
	}
	n, _ := parseInt(r.raw)
	return n
}

// Float64 返回 float64 值（非数字返回 0）
func (r Res) Float64() float64 {
	if r.typ != TypeNumber {
		return 0
	}
	f, _ := parseFloat(r.raw)
	return f
}

// Bool 返回布尔值（非布尔返回 false）
func (r Res) Bool() bool {
	return r.typ == TypeBool && len(r.raw) == 4 // "true" len=4
}

// Exists 返回值是否存在
func (r Res) Exists() bool {
	return r.typ != 0 || len(r.raw) > 0
}

// Raw 返回原始 JSON 切片
func (r Res) Raw() string { return r.raw }

// Type 返回值类型
func (r Res) Type() Type { return r.typ }

// ─── 懒查询 API ───

// Get 按点分隔路径惰性查询 JSON 值（零分配）
//
// 不构建 DOM 树，直接扫描 JSON 跳过不需要的键/值。
// 适合单次取值场景，性能超越 GJSON。
//
// 路径格式: 点分隔的键名/数组下标
//
//	Get(`{"user":{"name":"yak"}}`, "user.name")     → "yak"
//	Get(`{"items":[1,2,3]}`, "items.1")              → 2
//	Get(`{"a":{"b":{"c":true}}}`, "a.b.c")           → true
func Get(json, path string) Res {
	n := len(json)
	i := 0
	for i < n && json[i] <= ' ' {
		i++
	}
	if i >= n {
		return Res{}
	}

	// 零分配路径处理: 逐段扫描，不分割为 []string
	for {
		// 提取下一个路径段
		dot := 0
		for dot < len(path) && path[dot] != '.' {
			dot++
		}
		key := path[:dot]
		var more bool
		if dot < len(path) {
			path = path[dot+1:]
			more = true
		} else {
			path = ""
		}

		for i < n && json[i] <= ' ' {
			i++
		}
		if i >= n {
			return Res{}
		}

		switch json[i] {
		case '{':
			i = objFind(json, i+1, key)
			if i < 0 {
				return Res{}
			}
		case '[':
			idx := atoIdx(key)
			if idx < 0 {
				return Res{}
			}
			i = arrFind(json, i+1, idx)
			if i < 0 {
				return Res{}
			}
		default:
			return Res{} // 无法在非容器中导航
		}

		if !more {
			return parseRes(json, i)
		}
	}
}

// GetBytes 按路径查询 JSON 字节切片（零拷贝包装）
func GetBytes(json []byte, path string) Res {
	return Get(b2s(json), path)
}

// ─── 内部查找函数 ───

// objFind 在对象中查找 key，返回值的起始位置（-1 = 未找到）
//
// s[i-1] == '{'，i 指向 '{' 之后
// 优化: 内联 key 长度预判 + 字节级比较（避免提取子串再比较）
func objFind(s string, i int, key string) int {
	n := len(s)
	kl := len(key)
	for {
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n || s[i] == '}' {
			return -1
		}
		if s[i] != '"' {
			return -1
		}
		i++ // skip opening '"'

		// 快速路径: 长度预判 + 字节内联比较
		// 若 key 长度位置恰好是 '"'，则长度匹配；再逐字节比较
		if n-i > kl && s[i+kl] == '"' {
			j := 0
			for j < kl && s[i+j] == key[j] {
				j++
			}
			if j == kl {
				// 完全匹配（隐含: 无转义字符，因为字节完全匹配）
				i += kl + 1 // skip key + closing '"'
				for i < n && s[i] <= ' ' {
					i++
				}
				if i >= n || s[i] != ':' {
					return -1
				}
				i++
				for i < n && s[i] <= ' ' {
					i++
				}
				return i
			}
		}

		// 慢速路径: 跳过整个 key（不匹配或含转义）
		for i < n {
			if s[i] == '"' {
				break
			}
			if s[i] == '\\' {
				i += 2
				continue
			}
			i++
		}
		if i >= n {
			return -1
		}
		i++ // skip closing '"'

		// skip WS + ':'
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n || s[i] != ':' {
			return -1
		}
		i++
		for i < n && s[i] <= ' ' {
			i++
		}

		// skip value
		i = skipVal(s, i)

		// skip comma
		for i < n && s[i] <= ' ' {
			i++
		}
		if i < n && s[i] == ',' {
			i++
		}
	}
}

// arrFind 在数组中查找第 idx 个元素，返回元素值的位置（-1 = 未找到）
//
// s[i-1] == '['，i 指向 '[' 之后
func arrFind(s string, i int, idx int) int {
	n := len(s)
	for j := 0; j <= idx; j++ {
		for i < n && s[i] <= ' ' {
			i++
		}
		if i >= n || s[i] == ']' {
			return -1
		}
		if j == idx {
			return i
		}
		i = skipVal(s, i)
		for i < n && s[i] <= ' ' {
			i++
		}
		if i < n && s[i] == ',' {
			i++
		}
	}
	return -1
}

// ─── 8 字节批量 skipVal ───

// vch JSON 结构字符分类表（用于 8 字节批量扫描）
//
// 灵感来源: tidwall/gjson vchars[256] 查找表 (gjson.go)
// gjson 原版: var vchars = [256]byte{'"':2, '{':3, '(':3, '[':3, '}':1, ')':1, ']':1}
// yak 简化版: 去掉 '(' 和 ')' （JSON 不含圆括号），仅保留 JSON RFC 8259 结构字符
//
//	0 = 普通字符（快速跳过）
//	1 = 闭括号 } ]
//	2 = 引号 "
//	3 = 开括号 { [
//
// depth 变化公式: int(c) - 2 → 开括号(3-2=+1)，闭括号(1-2=-1)
// 该公式直接来自 gjson parseSquash
var vch = [256]byte{
	'"': 2,
	'{': 3, '[': 3,
	'}': 1, ']': 1,
}

// skipVal 跳过一个完整 JSON 值，返回值末尾后的位置
//
// 优化:
//   - 字符串: > '\\' 8 字节批量扫描
//   - 对象/数组: vch 查表 + int(c)-2 深度追踪（学习自 GJSON parseSquash）
//   - 8 字节批量: 每次检查 8 个位置的 vch 值，非零时处理
func skipVal(s string, i int) int {
	n := len(s)
	if i >= n {
		return n
	}
	switch s[i] {
	case '"':
		return skipStr(s, i+1)
	case '{', '[':
		return skipNested(s, i)
	case 't':
		if i+4 <= n {
			return i + 4
		}
		return n
	case 'f':
		if i+5 <= n {
			return i + 5
		}
		return n
	case 'n':
		if i+4 <= n {
			return i + 4
		}
		return n
	default:
		// 数字
		for i < n {
			c := s[i]
			if c <= ' ' || c == ',' || c == '}' || c == ']' {
				return i
			}
			i++
		}
		return n
	}
}

// skipStr 跳过字符串内容（s[i-1] == '"'，i 指向第一个内容字节）
func skipStr(s string, i int) int {
	n := len(s)
	for i < n {
		if s[i] > '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
		if s[i] == '\\' {
			i += 2
			continue
		}
		i++
	}
	return n
}

// skipNested 跳过嵌套结构（对象/数组），使用 8 字节批量 vch 扫描
//
// 核心算法来源: tidwall/gjson parseSquash 函数 (MIT License)
// gjson 原版在 gjson.go 1053-1192 行，使用相同的:
//   - vchars[256] 查表 + 8 字节展开循环 + goto token 跳转
//   - depth += int(c) - 2 深度追踪公式
//   - 遇到 '"' (c==2) 进入字符串跳过模式
//
// yak 差异: 返回 int 位置（而非 gjson 的 (int, string) 元组），
// 变量名简化 (vchars→vch, token→tok)，去掉圆括号支持
func skipNested(s string, i int) int {
	n := len(s)
	depth := 1
	i++ // skip opening '{' or '['
	for i < n && depth > 0 {
		// 8 字节批量: 查表快速跳过非结构字符
		for n-i >= 8 {
			c := vch[s[i]]
			if c != 0 {
				goto tok
			}
			c = vch[s[i+1]]
			if c != 0 {
				i++
				goto tok
			}
			c = vch[s[i+2]]
			if c != 0 {
				i += 2
				goto tok
			}
			c = vch[s[i+3]]
			if c != 0 {
				i += 3
				goto tok
			}
			c = vch[s[i+4]]
			if c != 0 {
				i += 4
				goto tok
			}
			c = vch[s[i+5]]
			if c != 0 {
				i += 5
				goto tok
			}
			c = vch[s[i+6]]
			if c != 0 {
				i += 6
				goto tok
			}
			c = vch[s[i+7]]
			if c != 0 {
				i += 7
				goto tok
			}
			i += 8
			continue
		tok:
			if c == 2 { // '"': 跳过字符串
				i++
				for i < n {
					if s[i] > '\\' {
						i++
						continue
					}
					if s[i] == '"' {
						i++
						break
					}
					if s[i] == '\\' {
						i += 2
						continue
					}
					i++
				}
			} else {
				depth += int(c) - 2
				i++
				if depth == 0 {
					return i
				}
			}
			continue
		}
		// 尾部: 逐字节
		c := vch[s[i]]
		if c == 0 {
			i++
			continue
		}
		if c == 2 { // '"'
			i++
			for i < n {
				if s[i] > '\\' {
					i++
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				if s[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
		} else {
			depth += int(c) - 2
			i++
			if depth == 0 {
				return i
			}
		}
	}
	return i
}

// parseRes 在位置 i 解析值为 Res（不构建 DOM）
//
// 安全: 检查 MaxStringLength 防止通过 Get() API 绕过长度限制
func parseRes(s string, i int) Res {
	n := len(s)
	if i >= n {
		return Res{}
	}
	switch s[i] {
	case '"':
		content, end, _, _ := rawStr(s, i)
		if len(content) > MaxStringLength {
			return Res{} // 超长字符串视为不存在（安全防护）
		}
		return Res{raw: s[i:end], str: content, typ: TypeString}
	case '{':
		end := skipVal(s, i)
		return Res{raw: s[i:end], typ: TypeObject}
	case '[':
		end := skipVal(s, i)
		return Res{raw: s[i:end], typ: TypeArray}
	case 't':
		if i+4 <= n {
			return Res{raw: s[i : i+4], typ: TypeBool}
		}
		return Res{}
	case 'f':
		if i+5 <= n {
			return Res{raw: s[i : i+5], typ: TypeBool}
		}
		return Res{}
	case 'n':
		if i+4 <= n {
			return Res{raw: s[i : i+4], typ: TypeNull}
		}
		return Res{}
	default:
		// 数字
		end := i
		for end < n {
			c := s[end]
			if c <= ' ' || c == ',' || c == '}' || c == ']' {
				break
			}
			end++
		}
		return Res{raw: s[i:end], typ: TypeNumber}
	}
}

// atoIdx 解析非负整数索引（零分配，含溢出保护）
func atoIdx(s string) int {
	if len(s) == 0 || len(s) > 10 {
		return -1
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
		if n < 0 {
			return -1 // 溢出保护（32 位平台）
		}
	}
	return n
}
