package message

// Metadata 消息元数据（键值对，用于链路追踪、路由标签等）
type Metadata map[string]string

// Get 获取元数据值，key 不存在返回空字符串。
func (m Metadata) Get(key string) string {
	if m == nil {
		return ""
	}
	return m[key]
}

// Set 设置元数据值。
func (m Metadata) Set(key, value string) {
	m[key] = value
}

// Has 检查 key 是否存在。
func (m Metadata) Has(key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// Copy 深拷贝 Metadata。
func (m Metadata) Copy() Metadata {
	if m == nil {
		return nil
	}
	cp := make(Metadata, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
