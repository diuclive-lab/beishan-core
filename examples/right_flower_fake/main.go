// Package main implements a minimal fake right flower for protocol testing.
// Runs an HTTP server that accepts dispatch requests and returns fake results.
//
// Usage:
//   go run ./examples/right_flower_fake &
//   curl -X POST http://localhost:9528 \
//     -H 'Content-Type: application/json' \
//     -d '{"id":"1","type":"dispatch","method":"code.review","params":{"instruction":"test"}}'
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// Request matches rightflower.Request wire format.
type Request struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Method string `json:"method"`
}

// Response matches rightflower.Response wire format.
type Response struct {
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

func handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[fake] dispatch: method=%s id=%s", req.Method, req.ID)

	resp := Response{
		ID:   req.ID,
		Type: "response",
		Result: &Result{
			Findings: []Finding{
				{
					Title:    "Fake 审查结果",
					Summary:  fmt.Sprintf("来自 fake 右花对 %s 的处理结果", req.Method),
					Verified: false,
					Source:   "fake_right_flower",
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	http.HandleFunc("/", handleDispatch)
	addr := ":9528"
	log.Printf("[fake-right-flower] 启动于 %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
