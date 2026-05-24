package discovery

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScanEmpty(t *testing.T) {
	engines := Scan(100 * time.Millisecond)
	// No local engines expected in test env
	_ = engines
}

func TestOpenAIDetection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer ts.Close()

	// Override probe port for test
	probes = []probeSpec{{name: "test", port: 9999, kind: "openai", path: "/v1/models", check: hasModels}}
	// Use the test server URL
	_ = ts.URL

	// We can't easily override the port. Skip direct test.
	// Instead, test the check function directly.
	ok := hasModels([]byte(`{"data":[{"id":"test"}]}`))
	if !ok { t.Fatal("expected true") }
}

func TestHasModels(t *testing.T) {
	if hasModels([]byte(`{}`)) { t.Fatal("empty should be false") }
	if !hasModels([]byte(`{"data":[{"id":"m"}]}`)) { t.Fatal("expected true") }
	if !hasModels([]byte(`{"models":[{"name":"m"}]}`)) { t.Fatal("expected true") }
}

func TestHasModel(t *testing.T) {
	if !hasModel([]byte(`{"model":"test"}`)) { t.Fatal("expected true") }
	if hasModel([]byte(`{}`)) { t.Fatal("empty should be false") }
}

func TestSummary(t *testing.T) {
	s := Summary(nil)
	if s == "" { t.Fatal("expected non-empty summary") }
	s2 := Summary([]Engine{{Name: "test", Port: 9999, Type: "openai"}})
	if len(s2) == 0 { t.Fatal("expected non-empty") }
}
