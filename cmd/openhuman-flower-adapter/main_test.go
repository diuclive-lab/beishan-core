package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMethodMap_Known(t *testing.T) {
	tests := []struct{ flower, want string }{
		{"memory.search", "openhuman.memory_recall_memories"},
		{"memory.store", "openhuman.memory_doc_put"},
		{"context.retrieve", "openhuman.memory_context_query"},
		{"code.review", "openhuman.agent_chat"},
	}
	for _, tc := range tests {
		got, ok := translateMethod(tc.flower)
		if !ok {
			t.Errorf("translateMethod(%q) = _, false, want %q", tc.flower, tc.want)
		}
		if got != tc.want {
			t.Errorf("translateMethod(%q) = %q, want %q", tc.flower, got, tc.want)
		}
	}
}

func TestMethodMap_Unknown(t *testing.T) {
	_, ok := translateMethod("unknown.method")
	if ok {
		t.Fatal("expected false for unknown method")
	}
}

func TestMethodMap_CaseSensitive(t *testing.T) {
	_, ok := translateMethod("Memory.Search")
	if ok {
		t.Fatal("expected false for wrong case")
	}
}

func TestParamsForwarding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["method"] != "openhuman.memory_recall_memories" {
			t.Errorf("method = %v, want recall", body["method"])
		}
		params := body["params"].(map[string]any)
		if params["query"] != "test" {
			t.Errorf("params.query = %v, want test", params["query"])
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	openHumanEndpoint = ts.URL
	body, code, err := dispatchToOpenHuman("openhuman.memory_recall_memories", map[string]any{"query": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["ok"] != true {
		t.Fatalf("unexpected: %v", resp)
	}
}

func TestBearerToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", auth)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	openHumanEndpoint = ts.URL
	openHumanToken = "test-token"
	dispatchToOpenHuman("ping", map[string]any{})
	openHumanToken = ""
}

func TestNon2xxStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()
	openHumanEndpoint = ts.URL
	body, code, err := dispatchToOpenHuman("test", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if code != 401 {
		t.Fatalf("status = %d, want 401", code)
	}
	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["error"] != "unauthorized" {
		t.Fatalf("body = %v", resp)
	}
}

func TestProbeReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()
	openHumanEndpoint = ts.URL
	if !probe() {
		t.Fatal("expected reachable")
	}
}

func TestProbeUnreachable(t *testing.T) {
	openHumanEndpoint = "http://127.0.0.1:1"
	if probe() {
		t.Fatal("expected unreachable")
	}
}

func TestLoadConfig(t *testing.T) {
	os.Setenv("OPENHUMAN_ENDPOINT", "http://test:9999")
	os.Setenv("OPENHUMAN_TOKEN", "test-token")
	os.Setenv("ADAPTER_PORT", "9999")
	defer func() {
		os.Unsetenv("OPENHUMAN_ENDPOINT")
		os.Unsetenv("OPENHUMAN_TOKEN")
		os.Unsetenv("ADAPTER_PORT")
	}()
	cfg := loadConfigFromEnv()
	if cfg.endpoint != "http://test:9999" {
		t.Fatalf("endpoint = %q", cfg.endpoint)
	}
	if cfg.token != "test-token" {
		t.Fatalf("token = %q", cfg.token)
	}
	if cfg.addr != ":9999" {
		t.Fatalf("addr = %q", cfg.addr)
	}
}

func TestTruncate(t *testing.T) {
	if s := truncate("hello", 3); s != "hel..." {
		t.Fatalf("got = %q", s)
	}
	if s := truncate("hello", 10); s != "hello" {
		t.Fatalf("got = %q", s)
	}
}

func TestNormalizeResponseJSONRPCResult(t *testing.T) {
	r := NormalizeResponse([]byte(`{"jsonrpc":"2.0","result":{"data":"test"}}`), "openhuman")
	if len(r.Findings) == 0 { t.Fatal("expected findings") }
	if r.Findings[0].Title != "OpenHuman 结果" { t.Fatalf("title=%q", r.Findings[0].Title) }
	if r.Findings[0].Verified { t.Fatal("should be unverified") }
}

func TestNormalizeResponseJSONRPCError(t *testing.T) {
	r := NormalizeResponse([]byte(`{"jsonrpc":"2.0","error":{"message":"not found"}}`), "openhuman")
	if len(r.Findings) == 0 { t.Fatal("expected findings") }
	if r.Findings[0].Title != "OpenHuman 错误" { t.Fatalf("title=%q", r.Findings[0].Title) }
}

func TestNormalizeResponsePlainText(t *testing.T) {
	r := NormalizeResponse([]byte("hello world"), "test")
	if r.Findings[0].Title != "文本响应" { t.Fatalf("title=%q", r.Findings[0].Title) }
}

func TestNormalizeResponseEmpty(t *testing.T) {
	r := NormalizeResponse([]byte{}, "test")
	if r.Findings[0].Title != "空响应" { t.Fatalf("title=%q", r.Findings[0].Title) }
}

func TestNormalizeResponseLongBody(t *testing.T) {
	body := make([]byte, 2000)
	for i := range body { body[i] = byte('a') }
	r := NormalizeResponse(body, "test")
	if len(r.Findings[0].Summary) > 1100 { t.Fatal("summary should be truncated") }
}
