package notify

import (
	"encoding/json"
	"testing"
)

func TestSendViaChannel_InvalidChannel(t *testing.T) {
	err := SendViaChannel("invalid", "", "", nil)
	if err == nil { t.Fatal("expected error for invalid channel") }
}

func TestSendViaChannel_EmailMissingTarget(t *testing.T) {
	err := SendViaChannel("email", "", "test", json.RawMessage(`{}`))
	if err == nil { t.Fatal("expected error for missing target") }
}

func TestFormatPayload(t *testing.T) {
	s := formatPayload(json.RawMessage(`{"key":"value"}`))
	if len(s) == 0 { t.Fatal("expected formatted string") }
}

func TestFormatPayload_Invalid(t *testing.T) {
	s := formatPayload(json.RawMessage(`not json`))
	if s == "" { t.Fatal("expected fallback string") }
}
