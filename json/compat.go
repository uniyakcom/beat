package json

import (
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ─── Marshal ───

// marshalBuf 复用序列化 buffer
var marshalBuf = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

// Marshal 将 Go 值序列化为 JSON（兼容 encoding/json.Marshal）
//
// 支持:
//   - 基础类型: string, bool, int*, uint*, float*
//   - 复合类型: struct, map[string]T, slice, array, pointer
//   - 接口: json.Marshaler
//   - struct tag: `json:"name,omitempty"`、`json:"-"` 跳过
//
// 与标准库的差异:
//   - 不对 HTML 字符(<, >, &)转义（性能优先）
//   - NaN/Inf 输出为 null 而非报错
func Marshal(v any) ([]byte, error) {
	bp := marshalBuf.Get().(*[]byte)
	buf := (*bp)[:0]
	var err error
	buf, err = appendMarshal(buf, reflect.ValueOf(v))
	if err != nil {
		*bp = buf
		marshalBuf.Put(bp)
		return nil, err
	}
	// 复制结果（调用方拥有返回值，pool buffer 将被复用）
	result := make([]byte, len(buf))
	copy(result, buf)
	*bp = buf
	marshalBuf.Put(bp)
	return result, nil
}

// MarshalTo 将 Go 值序列化并追加到 dst（零分配序列化）
//
// 适用于已有 buffer 的场景（如 Response 写 JSON）。
func MarshalTo(dst []byte, v any) ([]byte, error) {
	return appendMarshal(dst, reflect.ValueOf(v))
}

// MarshalAppend 将 Go 值序列化并追加到 dst（导出的零分配序列化入口）
//
// 与 MarshalTo 功能相同，外部包（如 response.go）可直接使用
// pool buffer 避免 Marshal 的 make+copy 二次分配。
func MarshalAppend(dst []byte, v any) ([]byte, error) {
	return appendMarshal(dst, reflect.ValueOf(v))
}

// AcquireBuf 从 marshalBuf pool 获取缓冲区
func AcquireBuf() *[]byte {
	return marshalBuf.Get().(*[]byte)
}

// ReleaseBuf 将缓冲区归还到 marshalBuf pool
func ReleaseBuf(bp *[]byte) {
	marshalBuf.Put(bp)
}

// appendMarshal 核心序列化递归
func appendMarshal(dst []byte, rv reflect.Value) ([]byte, error) {
	return appendMarshalDepth(dst, rv, 0)
}

// appendMarshalDepth 带深度限制的核心序列化递归
//
// 安全: 递归深度限制 MaxMarshalDepth，防止自引用指针链导致栈溢出
// （encoding/json 标准库使用 ptrLevel=1000 做同样的保护）
func appendMarshalDepth(dst []byte, rv reflect.Value, depth int) ([]byte, error) {
	// nil interface / invalid
	if !rv.IsValid() {
		return append(dst, "null"...), nil
	}
	if depth > MaxMarshalDepth {
		return dst, fmt.Errorf("json: max marshal depth %d exceeded", MaxMarshalDepth)
	}

	// 快速路径: 先用 interface 做具体类型匹配（避免 Kind() 的 reflect 开销）
	if rv.CanInterface() {
		switch val := rv.Interface().(type) {
		case string:
			return appendQuotedString(dst, val), nil
		case int:
			return appendInt(dst, int64(val)), nil
		case int64:
			return appendInt(dst, val), nil
		case bool:
			if val {
				return append(dst, "true"...), nil
			}
			return append(dst, "false"...), nil
		case map[string]string:
			// 直接操作 map 避免 reflect.MapKeys + reflect.ValueOf 开销
			return appendMapStringString(dst, val), nil
		case map[string]any:
			// 跳过通用 appendMap 的反射路径
			return appendMapStringAny(dst, val, depth+1)
		case Marshaler:
			b, err := val.MarshalJSON()
			if err != nil {
				return dst, err
			}
			return append(dst, b...), nil
		}
	}

	// 解引用指针
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return append(dst, "null"...), nil
		}
		rv = rv.Elem()
	}

	// json.Marshaler 接口（注意: 指针或值接收者都可能实现）
	if rv.CanInterface() {
		if m, ok := rv.Interface().(Marshaler); ok {
			b, err := m.MarshalJSON()
			if err != nil {
				return dst, err
			}
			return append(dst, b...), nil
		}
	}
	// 检查指针接收者
	if rv.CanAddr() {
		if m, ok := rv.Addr().Interface().(Marshaler); ok {
			b, err := m.MarshalJSON()
			if err != nil {
				return dst, err
			}
			return append(dst, b...), nil
		}
	}

	switch rv.Kind() {
	case reflect.String:
		dst = appendQuotedString(dst, rv.String())
		return dst, nil

	case reflect.Bool:
		if rv.Bool() {
			return append(dst, "true"...), nil
		}
		return append(dst, "false"...), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendInt(dst, rv.Int()), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return appendUint(dst, rv.Uint()), nil

	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return append(dst, "null"...), nil
		}
		if f == math.Trunc(f) && f >= -1e15 && f <= 1e15 {
			return appendInt(dst, int64(f)), nil
		}
		bits := 64
		if rv.Kind() == reflect.Float32 {
			bits = 32
		}
		return strconv.AppendFloat(dst, f, 'f', -1, bits), nil

	case reflect.Slice:
		if rv.IsNil() {
			return append(dst, "null"...), nil
		}
		// []byte → base64（兼容 encoding/json）
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return appendByteSlice(dst, rv.Bytes()), nil
		}
		// []string 快速路径（避免反射逐元素 boxing）
		if rv.Type().Elem().Kind() == reflect.String {
			return appendStringSlice(dst, rv), nil
		}
		return appendArray(dst, rv, depth+1)

	case reflect.Array:
		return appendArray(dst, rv, depth+1)

	case reflect.Map:
		if rv.IsNil() {
			return append(dst, "null"...), nil
		}
		return appendMap(dst, rv, depth+1)

	case reflect.Struct:
		return appendStruct(dst, rv, depth+1)

	case reflect.Interface:
		if rv.IsNil() {
			return append(dst, "null"...), nil
		}
		return appendMarshalDepth(dst, rv.Elem(), depth+1)

	default:
		return append(dst, "null"...), nil
	}
}

// appendQuotedString 追加带引号的 JSON 字符串
func appendQuotedString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	// 快速路径: 无需转义
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			// 有需要转义的字符，走慢速路径
			return appendQuotedStringSlow(dst, s)
		}
	}
	dst = append(dst, s...)
	dst = append(dst, '"')
	return dst
}

func appendQuotedStringSlow(dst []byte, s string) []byte {
	// dst 已经包含了开头的 "
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigit[c>>4], hexDigit[c&0xF])
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
}

// appendByteSlice 编码 []byte 为 base64 字符串（兼容 encoding/json）
func appendByteSlice(dst []byte, b []byte) []byte {
	if len(b) == 0 {
		return append(dst, `""`...)
	}
	dst = append(dst, '"')
	encodedLen := base64.StdEncoding.EncodedLen(len(b))
	pos := len(dst)
	dst = append(dst, make([]byte, encodedLen)...)
	base64.StdEncoding.Encode(dst[pos:], b)
	dst = append(dst, '"')
	return dst
}

// appendStringSlice []string 快速路径（避免反射逐元素 boxing）
func appendStringSlice(dst []byte, rv reflect.Value) []byte {
	dst = append(dst, '[')
	n := rv.Len()
	for i := 0; i < n; i++ {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendQuotedString(dst, rv.Index(i).String())
	}
	dst = append(dst, ']')
	return dst
}

// appendArray 编码数组/切片
func appendArray(dst []byte, rv reflect.Value, depth int) ([]byte, error) {
	dst = append(dst, '[')
	n := rv.Len()
	for i := 0; i < n; i++ {
		if i > 0 {
			dst = append(dst, ',')
		}
		var err error
		dst, err = appendMarshalDepth(dst, rv.Index(i), depth)
		if err != nil {
			return dst, err
		}
	}
	dst = append(dst, ']')
	return dst, nil
}

// appendMap 编码 map（键排序以保持输出一致性）
func appendMap(dst []byte, rv reflect.Value, depth int) ([]byte, error) {
	keys := rv.MapKeys()
	// 排序键
	strKeys := make([]string, len(keys))
	for i, k := range keys {
		strKeys[i] = k.String()
	}
	sort.Strings(strKeys)

	dst = append(dst, '{')
	for i, key := range strKeys {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendQuotedString(dst, key)
		dst = append(dst, ':')
		var err error
		dst, err = appendMarshalDepth(dst, rv.MapIndex(reflect.ValueOf(key)), depth)
		if err != nil {
			return dst, err
		}
	}
	dst = append(dst, '}')
	return dst, nil
}

// appendMapStringString map[string]string 专用快速路径
// 无反射: 直接 range map 取 k/v，单 key 不排序（省 alloc+sort）
func appendMapStringString(dst []byte, m map[string]string) []byte {
	dst = append(dst, '{')
	if len(m) == 0 {
		dst = append(dst, '}')
		return dst
	}
	if len(m) == 1 {
		for k, v := range m {
			dst = appendQuotedString(dst, k)
			dst = append(dst, ':')
			dst = appendQuotedString(dst, v)
		}
		dst = append(dst, '}')
		return dst
	}
	// 多 key: 排序保持一致性
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendQuotedString(dst, k)
		dst = append(dst, ':')
		dst = appendQuotedString(dst, m[k])
	}
	dst = append(dst, '}')
	return dst
}

// appendMapStringAny map[string]any 专用快速路径
// 避免 reflect.MapKeys + reflect.ValueOf(key) 的额外分配
func appendMapStringAny(dst []byte, m map[string]any, depth int) ([]byte, error) {
	dst = append(dst, '{')
	if len(m) == 0 {
		dst = append(dst, '}')
		return dst, nil
	}
	if len(m) == 1 {
		for k, v := range m {
			dst = appendQuotedString(dst, k)
			dst = append(dst, ':')
			var err error
			dst, err = appendMarshalDepth(dst, reflect.ValueOf(v), depth)
			if err != nil {
				return dst, err
			}
		}
		dst = append(dst, '}')
		return dst, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendQuotedString(dst, k)
		dst = append(dst, ':')
		var err error
		dst, err = appendMarshalDepth(dst, reflect.ValueOf(m[k]), depth)
		if err != nil {
			return dst, err
		}
	}
	dst = append(dst, '}')
	return dst, nil
}

// ─── Struct 编码 ───

// structFieldInfo 缓存的结构体字段元数据
type structFieldInfo struct {
	name      string // JSON 键名
	nameJSON  string // 预编码: "name": （含引号和冒号和逗号前缀）
	index     []int  // reflect 字段索引
	omitempty bool   // 是否省略零值
}

// structFields 缓存（避免反复反射）
var structCache sync.Map // map[reflect.Type][]structFieldInfo

func getStructFields(t reflect.Type) []structFieldInfo {
	if cached, ok := structCache.Load(t); ok {
		return cached.([]structFieldInfo)
	}
	fields := buildStructFields(t)
	structCache.Store(t, fields)
	return fields
}

func buildStructFields(t reflect.Type) []structFieldInfo {
	var fields []structFieldInfo
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		// 匿名嵌入结构体展开
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			embedded := buildStructFields(f.Type)
			for j := range embedded {
				embedded[j].index = append([]int{i}, embedded[j].index...)
			}
			fields = append(fields, embedded...)
			continue
		}

		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := f.Name
		omitempty := false
		if tag != "" {
			parts := strings.SplitN(tag, ",", 2)
			if parts[0] != "" {
				name = parts[0]
			}
			if len(parts) > 1 && strings.Contains(parts[1], "omitempty") {
				omitempty = true
			}
		}
		fields = append(fields, structFieldInfo{
			name:      name,
			nameJSON:  `"` + name + `":`,
			index:     f.Index,
			omitempty: omitempty,
		})
	}
	return fields
}

func appendStruct(dst []byte, rv reflect.Value, depth int) ([]byte, error) {
	fields := getStructFields(rv.Type())
	dst = append(dst, '{')
	first := true
	for i := range fields {
		fi := &fields[i]
		fv := rv.FieldByIndex(fi.index)
		if fi.omitempty && isZeroValue(fv) {
			continue
		}
		if !first {
			dst = append(dst, ',')
		}
		first = false
		// 使用预编码的 "name": 避免运行时转义
		dst = append(dst, fi.nameJSON...)
		var err error
		dst, err = appendMarshalDepth(dst, fv, depth)
		if err != nil {
			return dst, err
		}
	}
	dst = append(dst, '}')
	return dst, nil
}

// isZeroValue 检查是否为零值（omitempty 用）
func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map:
		return v.IsNil()
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Array:
		return v.Len() == 0
	case reflect.Struct:
		return false // struct 不省略
	}
	return false
}

// ─── Unmarshal ───

// Unmarshal 将 JSON 反序列化到 Go 值（兼容 encoding/json.Unmarshal）
//
// 支持:
//   - *struct: 按 json tag 映射字段
//   - *map[string]any: 通用对象解析
//   - *[]any: 通用数组解析
//   - *string, *bool, *int*, *float*: 基础类型
//   - json.Unmarshaler 接口
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}

	// json.Unmarshaler 接口
	if u, ok := v.(Unmarshaler); ok {
		return u.UnmarshalJSON(data)
	}

	var p Parser
	jv, err := p.ParseBytes(data)
	if err != nil {
		return err
	}
	return unmarshalValue(jv, rv.Elem())
}

func unmarshalValue(jv *Value, rv reflect.Value) error {
	// 解引用指针（自动创建）
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}

	// Unmarshaler 接口
	if rv.CanAddr() {
		if u, ok := rv.Addr().Interface().(Unmarshaler); ok {
			// 重新序列化 Value → JSON
			raw := marshalValue(jv)
			return u.UnmarshalJSON(raw)
		}
	}

	switch jv.t {
	case TypeNull:
		rv.SetZero()
		return nil

	case TypeBool:
		if rv.Kind() == reflect.Bool {
			rv.SetBool(jv.b)
		} else if rv.Kind() == reflect.Interface {
			rv.Set(reflect.ValueOf(jv.b))
		}
		return nil

	case TypeNumber:
		return unmarshalNumber(jv, rv)

	case TypeString:
		if rv.Kind() == reflect.String {
			rv.SetString(jv.s)
		} else if rv.Kind() == reflect.Interface {
			rv.Set(reflect.ValueOf(jv.s))
		}
		return nil

	case TypeArray:
		return unmarshalArray(jv, rv)

	case TypeObject:
		return unmarshalObject(jv, rv)
	}
	return nil
}

func unmarshalNumber(jv *Value, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := parseInt(jv.n)
		if err != nil {
			return err
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := parseInt(jv.n)
		if err != nil {
			return err
		}
		rv.SetUint(uint64(n))
	case reflect.Float32, reflect.Float64:
		f, err := parseFloat(jv.n)
		if err != nil {
			return err
		}
		rv.SetFloat(f)
	case reflect.Interface:
		// 默认: 尝试整数，失败则浮点
		if n, err := parseInt(jv.n); err == nil {
			rv.Set(reflect.ValueOf(n))
		} else if f, err := parseFloat(jv.n); err == nil {
			rv.Set(reflect.ValueOf(f))
		}
	}
	return nil
}

func unmarshalArray(jv *Value, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Slice:
		slice := reflect.MakeSlice(rv.Type(), len(jv.a), len(jv.a))
		for i, elem := range jv.a {
			if err := unmarshalValue(elem, slice.Index(i)); err != nil {
				return err
			}
		}
		rv.Set(slice)
	case reflect.Array:
		for i := 0; i < rv.Len() && i < len(jv.a); i++ {
			if err := unmarshalValue(jv.a[i], rv.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Interface:
		arr := make([]any, len(jv.a))
		for i, elem := range jv.a {
			val := reflect.ValueOf(&arr[i]).Elem()
			if err := unmarshalValue(elem, val); err != nil {
				return err
			}
		}
		rv.Set(reflect.ValueOf(arr))
	}
	return nil
}

func unmarshalObject(jv *Value, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(rv.Type()))
		}
		valType := rv.Type().Elem()
		for i := range jv.o.kvs {
			kv := &jv.o.kvs[i]
			val := reflect.New(valType).Elem()
			if err := unmarshalValue(kv.v, val); err != nil {
				return err
			}
			rv.SetMapIndex(reflect.ValueOf(kv.k), val)
		}
	case reflect.Struct:
		return unmarshalStruct(jv, rv)
	case reflect.Interface:
		m := make(map[string]any, len(jv.o.kvs))
		for i := range jv.o.kvs {
			kv := &jv.o.kvs[i]
			var val any
			vv := reflect.ValueOf(&val).Elem()
			if err := unmarshalValue(kv.v, vv); err != nil {
				return err
			}
			m[kv.k] = val
		}
		rv.Set(reflect.ValueOf(m))
	}
	return nil
}

func unmarshalStruct(jv *Value, rv reflect.Value) error {
	fields := getStructFields(rv.Type())
	// 构建 name → index 映射
	for i := range jv.o.kvs {
		kv := &jv.o.kvs[i]
		for _, fi := range fields {
			if fi.name == kv.k {
				fv := rv.FieldByIndex(fi.index)
				if err := unmarshalValue(kv.v, fv); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// marshalValue 将 Value 序列化为 JSON 字节（用于 Unmarshaler 回调）
func marshalValue(v *Value) []byte {
	if v == nil {
		return []byte("null")
	}
	switch v.t {
	case TypeNull:
		return []byte("null")
	case TypeBool:
		if v.b {
			return []byte("true")
		}
		return []byte("false")
	case TypeNumber:
		return []byte(v.n)
	case TypeString:
		buf := appendQuotedString(nil, v.s)
		return buf
	case TypeArray:
		buf := []byte{'['}
		for i, elem := range v.a {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, marshalValue(elem)...)
		}
		buf = append(buf, ']')
		return buf
	case TypeObject:
		buf := []byte{'{'}
		for i := range v.o.kvs {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendQuotedString(buf, v.o.kvs[i].k)
			buf = append(buf, ':')
			buf = append(buf, marshalValue(v.o.kvs[i].v)...)
		}
		buf = append(buf, '}')
		return buf
	}
	return []byte("null")
}
