package rightflower

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"beishan/kernel"
)

func TestValidateManifest_Valid(t *testing.T) {
	m := &Manifest{
		Name: "test", Type: "testing", Protocol: "http",
		Endpoint: "http://localhost:9528", Capabilities: []string{"test"},
		OutputFormat: "json", SafetyLevel: "sandbox", Enabled: true,
	}
	if err := ValidateManifest(m); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateManifest_EmptyName(t *testing.T) {
	m := &Manifest{Protocol: "http", Endpoint: "http://localhost:9528", Capabilities: []string{"test"}}
	if err := ValidateManifest(m); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateManifest_RemoteEndpoint(t *testing.T) {
	m := &Manifest{
		Name: "test", Protocol: "http",
		Endpoint: "https://example.com/api", Capabilities: []string{"test"},
	}
	if err := ValidateManifest(m); err == nil {
		t.Fatal("expected error for remote endpoint")
	}
}

func TestValidateManifest_BadFormat(t *testing.T) {
	m := &Manifest{
		Name: "test", Protocol: "http",
		Endpoint: "http://localhost:9528", Capabilities: []string{"test"},
		OutputFormat: "binary", SafetyLevel: "sandbox",
	}
	if err := ValidateManifest(m); err == nil {
		t.Fatal("expected error for bad output_format")
	}
}

func TestHTTPError_Non2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer ts.Close()
	client := NewClient()
	_, err := client.Dispatch(ts.URL, &Request{ID: "1", Type: "dispatch"})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if he.StatusCode != 500 {
		t.Fatalf("expected status 500, got %d", he.StatusCode)
	}
}

func TestPayloadContract(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		params := body["params"].(map[string]interface{})
		p := params["payload"]
		if _, ok := p.(string); ok {
			t.Errorf("payload should be object, got string: %T", p)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{ID: "1", Type: "response", Result: &Result{}})
	}))
	defer ts.Close()
	client := NewClient()
	plugin := &Plugin{
		Name: "test", Client: client,
		Manifest: &Manifest{Endpoint: ts.URL},
	}
	msg := kernel.Message{
		Sender: "user", Recipient: "test", Type: "test",
		CorrelationID: "test-123",
		Payload:       json.RawMessage(`{"key":"value"}`),
	}
	resp, err := plugin.OnMessage(msg)
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}
	if resp.Type != "test.result" {
		t.Fatalf("expected test.result, got %s", resp.Type)
	}
}

func TestSecurityWrapper_VerifiedFalse(t *testing.T) {
	r := &Result{Findings: []Finding{
		{Title: "test", Verified: true},
	}}
	SecurityWrapper(r, "test_flower", "test_method")
	if r.Findings[0].Verified {
		t.Fatal("expected Verified=false after SecurityWrapper")
	}
}
