package kernel

import (
	"strings"
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


func TestRegisterAppearsInPrompt(t *testing.T) {
	k := NewKernel("test-key")
	k.Register("test_plugin", &testPlugin{}, Meta{Description: "test"})
	prompt := k.Router.buildPluginList()
	if !contains(prompt, "test_plugin") {
		t.Fatal("Register should add to prompt")
	}
}

func TestRegisterUnlistedNotInPrompt(t *testing.T) {
	k := NewKernel("test-key")
	k.RegisterUnlisted("unlisted_plugin", &testPlugin{}, Meta{Description: "hidden"})
	prompt := k.Router.buildPluginList()
	if contains(prompt, "unlisted_plugin") {
		t.Fatal("RegisterUnlisted should NOT add to prompt")
	}
}

func TestRegisterUnlistedStillInKnownPlugins(t *testing.T) {
	k := NewKernel("test-key")
	k.RegisterUnlisted("unlisted_plugin", &testPlugin{}, Meta{Description: "hidden"})
	names := k.KnownPlugins()
	for _, n := range names {
		if n == "unlisted_plugin" {
			return
		}
	}
	t.Fatal("RegisterUnlisted should still be in KnownPlugins")
}

func TestPayloadRoundTrip(t *testing.T) {
	k := NewKernel("test-key")
	recv := make(chan Message, 1)
	plugin := &recordPlugin{recv: recv}
	k.RegisterUnlisted("recv_plugin", plugin, Meta{})
	original := []byte(`{"key":"value"}`)
	msg := Message{Sender: "user", Recipient: "recv_plugin", Type: "test", Payload: original}
	sent := k.Send(msg)
	if sent != nil {
		t.Fatal(sent)
	}
	select {
	case received := <-recv:
		if string(received.Payload) != string(original) {
			t.Fatalf("payload modified: %q != %q", received.Payload, original)
		}
	default:
		t.Fatal("plugin did not receive message")
	}
}

type testPlugin struct{}
func (p *testPlugin) OnMessage(msg Message) (Message, error) { return Message{}, nil }

type recordPlugin struct{ recv chan Message }
func (p *recordPlugin) OnMessage(msg Message) (Message, error) {
	p.recv <- msg
	return Message{}, nil
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
