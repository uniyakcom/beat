package json

import (
	"math"
	"strconv"
	"sync"
)

// Writer 高性能 JSON 序列化器（零分配追加到 buffer）
//
// 设计特点:
//   - 直接向 []byte 追加 JSON 字节，无中间 io.Writer 层
//   - 支持 pool 复用（AcquireWriter/ReleaseWriter）
//   - 支持嵌套 Object/Array 构建
//   - 自动处理字符串转义
//   - 整数使用快速路径避免 strconv 开销
//
// 用法:
//
//	w := json.AcquireWriter()
//	defer json.ReleaseWriter(w)
//	w.Object(func(w *json.Writer) {
//	    w.Field("name", "yak")
//	    w.FieldInt("ver", 1)
//	})
//	data := w.Bytes() // {"name":"yak","ver":1}
type Writer struct {
	buf     []byte
	scratch [20]byte // strconv 缓冲（避免分配）
}

// ─── Pool ───

var writerPool = sync.Pool{
	New: func() any { return &Writer{buf: make([]byte, 0, 256)} },
}

// AcquireWriter 从池中获取 Writer
func AcquireWriter() *Writer {
	w := writerPool.Get().(*Writer)
	w.buf = w.buf[:0]
	return w
}

// ReleaseWriter 归还 Writer 到池中
func ReleaseWriter(w *Writer) {
	// 保留小 buffer，释放大 buffer（防内存泄漏）
	if cap(w.buf) > 1<<16 {
		w.buf = make([]byte, 0, 256)
	}
	writerPool.Put(w)
}

// ─── 结果获取 ───

// Bytes 返回已生成的 JSON 字节（声明周期绑定到 Writer）
func (w *Writer) Bytes() []byte {
	return w.buf
}

// String 返回已生成的 JSON 字符串
func (w *Writer) String() string {
	return b2s(w.buf)
}

// Len 返回已写入的字节数
func (w *Writer) Len() int {
	return len(w.buf)
}

// Reset 重置 Writer 以复用
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
}

// AppendTo 将当前内容追加到外部 buffer（零拷贝）
func (w *Writer) AppendTo(dst []byte) []byte {
	return append(dst, w.buf...)
}

// ─── 对象构建 ───

// Object 构建 JSON 对象 {}
func (w *Writer) Object(fn func(w *Writer)) {
	w.buf = append(w.buf, '{')
	mark := len(w.buf)
	fn(w)
	// 如果写了字段，最后一个逗号要保留（已在 Field* 中写入）
	// 检查是否有尾部逗号需要移除
	if len(w.buf) > mark && w.buf[len(w.buf)-1] == ',' {
		w.buf[len(w.buf)-1] = '}'
	} else {
		w.buf = append(w.buf, '}')
	}
}

// Field 写入字符串字段: "key":"value",
func (w *Writer) Field(key, value string) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.writeQuotedString(value)
	w.buf = append(w.buf, ',')
}

// FieldBytes 写入字节切片字段（值会被转义为 JSON 字符串）
func (w *Writer) FieldBytes(key string, value []byte) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.writeQuotedBytes(value)
	w.buf = append(w.buf, ',')
}

// FieldInt 写入整数字段: "key":123,
func (w *Writer) FieldInt(key string, value int) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.buf = appendInt(w.buf, int64(value))
	w.buf = append(w.buf, ',')
}

// FieldInt64 写入 int64 字段
func (w *Writer) FieldInt64(key string, value int64) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.buf = appendInt(w.buf, value)
	w.buf = append(w.buf, ',')
}

// FieldUint64 写入 uint64 字段
func (w *Writer) FieldUint64(key string, value uint64) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.buf = appendUint(w.buf, value)
	w.buf = append(w.buf, ',')
}

// FieldFloat 写入浮点数字段: "key":1.23,
func (w *Writer) FieldFloat(key string, value float64) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.writeFloat(value)
	w.buf = append(w.buf, ',')
}

// FieldBool 写入布尔字段: "key":true,
func (w *Writer) FieldBool(key string, value bool) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	if value {
		w.buf = append(w.buf, "true"...)
	} else {
		w.buf = append(w.buf, "false"...)
	}
	w.buf = append(w.buf, ',')
}

// FieldNull 写入 null 字段: "key":null,
func (w *Writer) FieldNull(key string) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.buf = append(w.buf, "null"...)
	w.buf = append(w.buf, ',')
}

// FieldObject 写入嵌套对象字段: "key":{...},
func (w *Writer) FieldObject(key string, fn func(w *Writer)) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.Object(fn)
	w.buf = append(w.buf, ',')
}

// FieldArray 写入数组字段: "key":[...],
func (w *Writer) FieldArray(key string, fn func(w *Writer)) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.Array(fn)
	w.buf = append(w.buf, ',')
}

// FieldRaw 写入预编码的 JSON 原始值: "key":rawJSON,
func (w *Writer) FieldRaw(key string, rawJSON []byte) {
	w.writeQuotedString(key)
	w.buf = append(w.buf, ':')
	w.buf = append(w.buf, rawJSON...)
	w.buf = append(w.buf, ',')
}

// ─── 数组构建 ───

// Array 构建 JSON 数组 []
func (w *Writer) Array(fn func(w *Writer)) {
	w.buf = append(w.buf, '[')
	mark := len(w.buf)
	fn(w)
	if len(w.buf) > mark && w.buf[len(w.buf)-1] == ',' {
		w.buf[len(w.buf)-1] = ']'
	} else {
		w.buf = append(w.buf, ']')
	}
}

// Item 写入数组字符串元素: "value",
func (w *Writer) Item(value string) {
	w.writeQuotedString(value)
	w.buf = append(w.buf, ',')
}

// ItemInt 写入数组整数元素: 123,
func (w *Writer) ItemInt(value int) {
	w.buf = appendInt(w.buf, int64(value))
	w.buf = append(w.buf, ',')
}

// ItemFloat 写入数组浮点数元素
func (w *Writer) ItemFloat(value float64) {
	w.writeFloat(value)
	w.buf = append(w.buf, ',')
}

// ItemBool 写入数组布尔元素
func (w *Writer) ItemBool(value bool) {
	if value {
		w.buf = append(w.buf, "true"...)
	} else {
		w.buf = append(w.buf, "false"...)
	}
	w.buf = append(w.buf, ',')
}

// ItemNull 写入数组 null 元素
func (w *Writer) ItemNull() {
	w.buf = append(w.buf, "null"...)
	w.buf = append(w.buf, ',')
}

// ItemObject 写入数组中的对象元素
func (w *Writer) ItemObject(fn func(w *Writer)) {
	w.Object(fn)
	w.buf = append(w.buf, ',')
}

// ItemArray 写入数组中的子数组元素
func (w *Writer) ItemArray(fn func(w *Writer)) {
	w.Array(fn)
	w.buf = append(w.buf, ',')
}

// ─── 字符串转义 ───

// writeQuotedString 写入带引号和转义的 JSON 字符串
//
// 优化: 先扫描是否需要转义（大部分字符串不需要）
func (w *Writer) writeQuotedString(s string) {
	w.buf = append(w.buf, '"')

	// 快速路径: 无需转义
	needsEscape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			needsEscape = true
			break
		}
	}

	if !needsEscape {
		w.buf = append(w.buf, s...)
		w.buf = append(w.buf, '"')
		return
	}

	// 慢速路径: 逐字符转义
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			w.buf = append(w.buf, '\\', '"')
		case c == '\\':
			w.buf = append(w.buf, '\\', '\\')
		case c == '\n':
			w.buf = append(w.buf, '\\', 'n')
		case c == '\r':
			w.buf = append(w.buf, '\\', 'r')
		case c == '\t':
			w.buf = append(w.buf, '\\', 't')
		case c < 0x20:
			// 控制字符: \u00XX
			w.buf = append(w.buf, '\\', 'u', '0', '0')
			w.buf = append(w.buf, hexDigit[c>>4], hexDigit[c&0xF])
		default:
			w.buf = append(w.buf, c)
		}
	}
	w.buf = append(w.buf, '"')
}

// writeQuotedBytes 写入带引号和转义的 JSON 字节切片
func (w *Writer) writeQuotedBytes(b []byte) {
	w.writeQuotedString(b2s(b))
}

var hexDigit = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

// ─── 数字序列化 ───

// appendInt 快速 int64 追加（避免 strconv.AppendInt 的反射路径）
func appendInt(dst []byte, v int64) []byte {
	if v >= 0 && v < 100 {
		return appendSmallInt(dst, int(v))
	}
	return strconv.AppendInt(dst, v, 10)
}

// appendUint 快速 uint64 追加
func appendUint(dst []byte, v uint64) []byte {
	if v < 100 {
		return appendSmallInt(dst, int(v))
	}
	return strconv.AppendUint(dst, v, 10)
}

// appendSmallInt 小整数快速路径（0-99 查表）
func appendSmallInt(dst []byte, v int) []byte {
	if v < 10 {
		return append(dst, byte('0'+v))
	}
	return append(dst, byte('0'+v/10), byte('0'+v%10))
}

// writeFloat 写入浮点数
func (w *Writer) writeFloat(f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// JSON 不支持 NaN/Inf，输出 null
		w.buf = append(w.buf, "null"...)
		return
	}
	// 尝试整数快速路径
	if f == math.Trunc(f) && f >= -1e15 && f <= 1e15 {
		w.buf = appendInt(w.buf, int64(f))
		return
	}
	w.buf = strconv.AppendFloat(w.buf, f, 'f', -1, 64)
}
