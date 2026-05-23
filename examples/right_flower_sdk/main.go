package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Request struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type Response struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type Result struct {
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
	log.Printf("dispatch: method=%s id=%s", req.Method, req.ID)
	resp := Response{
		ID: req.ID, Type: "response",
		Result: &Result{Findings: []Finding{
			{Title: "SDK 结果", Summary: "来自 SDK 模板右花", Verified: false, Source: "sdk_template"},
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	http.HandleFunc("/", handleDispatch)
	log.Fatal(http.ListenAndServe(":9530", nil))
}
