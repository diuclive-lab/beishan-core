// OpenHuman Flower Adapter — thin HTTP bridge between beishan-core Right Flower
// Protocol and OpenHuman's local API.
//
// Protocol: receives rightflower dispatch → translates → returns findings.
// OpenHuman is NOT allowed to write files directly; all writes go through
// beishan-core's hardening layer.
//
// Usage:
//   go run ./cmd/openhuman-flower-adapter &
//   # Core loads right_flowers/openhuman.yaml.example → adapter → OpenHuman
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const defaultOpenHumanEndpoint = "http://127.0.0.1:7788"

// RightFlowerRequest comes from beishan-core's rightflower package.
type RightFlowerRequest struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Method string `json:"method"`
}

// RightFlowerResponse goes back to beishan-core.
type RightFlowerResponse struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type Result struct {
	Diff     string    `json:"diff,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

type Finding struct {
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Verified bool   `json:"verified"`
	Source   string `json:"source"`
}

var (
	openHumanEndpoint string
	openHumanOK       bool
)

// probe checks if OpenHuman is reachable.
func probe() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(openHumanEndpoint + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req RightFlowerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[openhuman-adapter] dispatch: method=%s id=%s", req.Method, req.ID)

	if !openHumanOK {
		resp := RightFlowerResponse{
			ID:   req.ID,
			Type: "response",
			Result: &Result{
				Findings: []Finding{
					{Title: "OpenHuman 不可用", Summary: "OpenHuman 进程未运行或 API 不可达。请启动 OpenHuman 后重试。", Verified: false, Source: "openhuman_adapter"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Dispatch to OpenHuman
	openReq := map[string]interface{}{
		"method": req.Method,
		"params": map[string]interface{}{},
	}
	body, _ := json.Marshal(openReq)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	ohResp, err := httpClient.Post(openHumanEndpoint+"/api/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		resp := RightFlowerResponse{
			ID: req.ID, Type: "response",
			Result: &Result{Findings: []Finding{
				{Title: "OpenHuman 调用失败", Summary: fmt.Sprintf("调用 OpenHuman 失败: %v", err), Verified: false, Source: "openhuman_adapter"},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	defer ohResp.Body.Close()
	respBody, _ := io.ReadAll(ohResp.Body)

	// Return OpenHuman response as finding
	resp := RightFlowerResponse{
		ID: req.ID, Type: "response",
		Result: &Result{
			Findings: []Finding{
				{Title: "OpenHuman 结果", Summary: string(respBody[:min(len(respBody), 1000)]), Verified: false, Source: "openhuman"},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleProbe(w http.ResponseWriter, r *http.Request) {
	status := "unreachable"
	if probe() {
		status = "reachable"
		openHumanOK = true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"adapter":  "openhuman",
		"openhuman": status,
	})
}

func main() {
	openHumanEndpoint = os.Getenv("OPENHUMAN_ENDPOINT")
	if openHumanEndpoint == "" {
		openHumanEndpoint = defaultOpenHumanEndpoint
	}
	openHumanOK = probe()

	addr := ":9529"
	if p := os.Getenv("ADAPTER_PORT"); p != "" {
		addr = ":" + p
	}

	http.HandleFunc("/dispatch", handleDispatch)
	http.HandleFunc("/health", handleProbe)

	log.Printf("[openhuman-adapter] 启动于 %s → OpenHuman: %s (reachable=%v)", addr, openHumanEndpoint, openHumanOK)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
