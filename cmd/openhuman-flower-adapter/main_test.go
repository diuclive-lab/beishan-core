package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestParamsForwarding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["method"] != "memory.search" {
			t.Errorf("method = %v, want memory.search", body["method"])
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
	openHumanToken = ""
	body, code, err := dispatchToOpenHuman(&RightFlowerRequest{
		ID: "1", Method: "memory.search",
		Params: map[string]any{"query": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	var resp map[string]any
	json.Unmarshal(body, &resp)
	if resp["ok"] != true {
		t.Fatalf("unexpected response: %v", resp)
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
	dispatchToOpenHuman(&RightFlowerRequest{ID: "1", Method: "ping", Params: map[string]any{}})
	openHumanToken = ""
}

func TestNon2xxStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	openHumanEndpoint = ts.URL
	body, code, err := dispatchToOpenHuman(&RightFlowerRequest{ID: "1", Method: "test"})
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
