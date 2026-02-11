package marshal

import (
	"encoding/json"

	"github.com/uniyakcom/beat/message"
)

// jsonEnvelope JSON 序列化信封
type jsonEnvelope struct {
	UUID     string            `json:"uuid"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Payload  json.RawMessage   `json:"payload"`
}

// JSON JSON 编解码器（零外部依赖）
type JSON struct{}

// Marshal 将消息序列化为 JSON。
func (j JSON) Marshal(_ string, msg *message.Message) ([]byte, error) {
	env := jsonEnvelope{
		UUID:     msg.UUID,
		Metadata: msg.Metadata,
		Payload:  msg.Payload,
	}
	return json.Marshal(env)
}

// Unmarshal 将 JSON 反序列化为消息。
func (j JSON) Unmarshal(_ string, data []byte) (*message.Message, error) {
	var env jsonEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}

	msg := message.New(env.UUID, env.Payload)
	for k, v := range env.Metadata {
		msg.Metadata.Set(k, v)
	}
	return msg, nil
}
