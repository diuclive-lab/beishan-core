package tools

import (
	"encoding/json"
	"testing"
)

func FuzzValidateParams(f *testing.F) {
	f.Add(`{"name":"test"}`, `{"name":"test","extra":"invalid"}`)
	f.Add(`{"query":"hello"}`, `{}`)
	f.Add(`{"op":"read","path":"/tmp/test.txt"}`, `{"op":"read","path":"/etc/passwd"}`)

	f.Fuzz(func(t *testing.T, schemaStr, payloadStr string) {
		var schema map[string]interface{}
		var payload json.RawMessage
		if json.Unmarshal([]byte(schemaStr), &schema) != nil {
			return
		}
		if json.Unmarshal([]byte(payloadStr), &payload) != nil {
			return
		}
		ts := ToolSchema{Schema: schema}
		_ = ValidateParams(ts, payload)
	})
}

