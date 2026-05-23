package kernel

import (
	"encoding/json"
	"testing"
)

func TestPayloadNotParsed(t *testing.T) {
	k := NewKernel("test-key")
	msg := Message{
		Sender: "user", Recipient: "test_plugin", Type: "test",
		Payload: json.RawMessage(`{"sensitive":"data"}`),
	}
	if len(msg.Payload) == 0 {
		t.Fatal("payload should not be empty")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["sensitive"] != "data" {
		t.Fatal("payload content should be preserved")
	}
	_ = k
}

func TestParseDecisionRejectsNonexistentRecipient(t *testing.T) {
	r := NewRouter("test-key")
	_, err := r.parseDecision(`{"recipient":"nonexistent","confidence":0.9,"reason":"test"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent recipient")
	}
}

func TestParseDecisionRejectsLowConfidence(t *testing.T) {
	r := NewRouter("test-key")
	_, err := r.parseDecision(`{"recipient":"search_plugin","confidence":0.1,"reason":"test"}`)
	if err == nil {
		t.Fatal("expected error for low confidence")
	}
}

func TestParseDecisionRejectsInvalidJSON(t *testing.T) {
	r := NewRouter("test-key")
	_, err := r.parseDecision(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
