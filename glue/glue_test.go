package glue

import (
	"encoding/json"
	"testing"
)

func TestProtocolMessageMarshal(t *testing.T) {
	msg := ProtocolMessage{Type: "dispatch", ID: "1", Sender: "user", MsgType: "test", Payload: json.RawMessage(`{"key":"val"}`)}
	data, err := json.Marshal(msg)
	if err != nil { t.Fatal(err) }
	if len(data) == 0 { t.Fatal("empty marshal") }
}

func TestProtocolMessageUnmarshal(t *testing.T) {
	data := `{"type":"register","name":"test_plugin"}`
	var msg ProtocolMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil { t.Fatal(err) }
	if msg.Type != "register" { t.Fatalf("type=%q", msg.Type) }
	if msg.Name != "test_plugin" { t.Fatalf("name=%q", msg.Name) }
}

func TestProtocolMessageTypes(t *testing.T) {
	for _, tt := range []string{"register", "dispatch", "response", "event", "shutdown"} {
		msg := ProtocolMessage{Type: tt}
		data, _ := json.Marshal(msg)
		var decoded ProtocolMessage
		json.Unmarshal(data, &decoded)
		if decoded.Type != tt { t.Fatalf("expected %q got %q", tt, decoded.Type) }
	}
}
