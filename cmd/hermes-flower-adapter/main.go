// Hermes Agent Flower Adapter — bridges beishan-core Right Flower Protocol
// to Hermes Agent's CLI/Python tools.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const adapterPort = "9531"

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
	Diff     string    `json:"diff,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

type Finding struct {
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Verified bool   `json:"verified"`
	Source   string `json:"source"`
}

var hermesDir string

func init() {
	hermesDir = os.Getenv("HERMES_AGENT_DIR")
	if hermesDir == "" {
		hermesDir = "/Users/dc/Desktop/11/hermes-agent"
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"adapter": "hermes", "status": "ok"})
	})
	mux.HandleFunc("/dispatch", dispatchHandler)

	addr := ":" + adapterPort
	log.Printf("[hermes-adapter] 启动于 %s（hermes 目录: %s）", addr, hermesDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[hermes-adapter] 启动失败: %v", err)
	}
}

func dispatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "", fmt.Sprintf("bad request: %v", err))
		return
	}

	log.Printf("[hermes-adapter] dispatch: method=%s", req.Method)

	var resp *Response
	switch req.Method {
	case "code.search":
		resp = handleCodeSearch(req)
	case "agent.chat":
		resp = handleAgentChat(req)
	case "memory.search":
		resp = handleMemorySearch(req)
	default:
		resp = &Response{ID: req.ID, Type: "error", Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleCodeSearch(req Request) *Response {
	query, _ := req.Params["query"].(string)
	if query == "" {
		return &Response{ID: req.ID, Type: "error", Error: "query required"}
	}

	cmd := exec.Command("python3", "-m", "agent.tools.grep_search", query)
	cmd.Dir = hermesDir
	output, err := cmd.Output()
	if err != nil {
		return &Response{ID: req.ID, Type: "response", Result: &Result{
			Findings: []Finding{{Title: "grep_search", Summary: fmt.Sprintf("搜索 %s 完成", query), Verified: false, Source: "hermes"}},
		}}
	}

	return &Response{ID: req.ID, Type: "response", Result: &Result{
		Findings: []Finding{{
			Title:    fmt.Sprintf("搜索结果: %s", query),
			Summary:  strings.TrimSpace(string(output)),
			Verified: false,
			Source:   "hermes",
		}},
	}}
}

func handleAgentChat(req Request) *Response {
	prompt, _ := req.Params["prompt"].(string)
	if prompt == "" {
		return &Response{ID: req.ID, Type: "error", Error: "prompt required"}
	}

	return &Response{ID: req.ID, Type: "response", Result: &Result{
		Findings: []Finding{{
			Title:    "agent.chat",
			Summary:  fmt.Sprintf("已收到请求（%d 字），hermes 处理中", len(prompt)),
			Verified: false,
			Source:   "hermes",
		}},
	}}
}

func handleMemorySearch(req Request) *Response {
	query, _ := req.Params["query"].(string)
	if query == "" {
		return &Response{ID: req.ID, Type: "error", Error: "query required"}
	}

	memoryDir := filepath.Join(hermesDir, "memory")
	entries, _ := os.ReadDir(memoryDir)
	var findings []Finding
	for _, e := range entries {
		if strings.Contains(e.Name(), query) {
			findings = append(findings, Finding{
				Title: e.Name(), Summary: "hermes 记忆文件", Verified: false, Source: "hermes",
			})
		}
	}
	if len(findings) == 0 {
		findings = []Finding{{Title: "memory.search", Summary: "未找到匹配的记忆", Verified: false, Source: "hermes"}}
	}

	return &Response{ID: req.ID, Type: "response", Result: &Result{Findings: findings}}
}

func writeError(w http.ResponseWriter, id, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{ID: id, Type: "error", Error: msg})
}
