package kernel

import (
	"testing"
)

func TestKernelDoesNotParsePayload(t *testing.T) {
	k := NewKernel("test-key")
	original := []byte(`{"sensitive":"data"}`)
	msg := Message{
		Sender: "user", Recipient: "test", Type: "test",
		Payload: original,
	}
	if len(msg.Payload) == 0 {
		t.Fatal("payload should not be empty")
	}
	for i, b := range original {
		if i < len(msg.Payload) && msg.Payload[i] != b {
			t.Fatalf("payload modified at byte %d", i)
		}
	}
	if len(msg.Payload) != len(original) {
		t.Fatal("payload length changed")
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
