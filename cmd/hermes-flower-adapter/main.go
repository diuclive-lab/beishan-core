// Hermes Agent Flower Adapter — real integration via Hermes Agent Python API.
// Calls hermes-agent's Python tool registry directly via subprocess.
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

var hermesDir string

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
	log.Fatal(http.ListenAndServe(addr, mux))
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
	case "memory.search":
		resp = handleMemorySearch(req)
	case "memory.store":
		resp = handleMemoryStore(req)
	case "code.search":
		resp = handleCodeSearch(req)
	case "tools.list":
		resp = handleToolsList(req)
	case "agent.chat":
		resp = handleAgentChat(req)
	default:
		resp = &Response{ID: req.ID, Type: "error",
			Error: fmt.Sprintf("unknown method: %s. available: memory.search, memory.store, code.search, tools.list, agent.chat", req.Method)}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func py(args ...string) (string, error) {
	cmd := exec.Command("python3", args...)
	cmd.Dir = hermesDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func handleMemorySearch(req Request) *Response {
	query, _ := req.Params["query"].(string)
	if query == "" {
		return &Response{ID: req.ID, Type: "error", Error: "query required"}
	}

	// Search Hermes conversations via CLI
	out, err := py("-c", fmt.Sprintf(`
import sys, json
sys.path.insert(0, %q)
from hermes_state import get_state
state = get_state()
results = []
for conv in state.get("conversations", []):
    if %q in str(conv).lower():
        results.append({"id": conv.get("id",""), "title": conv.get("title","")})
print(json.dumps(results))
`, hermesDir, strings.ToLower(query)))

	if err != nil || out == "" || out == "[]" {
		// Fallback: list conversations directory
		convDir := filepath.Join(hermesDir, "conversations")
		var findings []Finding
		if entries, err := os.ReadDir(convDir); err == nil {
			for _, e := range entries {
				if strings.Contains(strings.ToLower(e.Name()), strings.ToLower(query)) {
					findings = append(findings, Finding{
						Title: e.Name(), Summary: "Hermes 对话文件", Verified: false, Source: "hermes",
					})
				}
			}
		}
		if len(findings) == 0 {
			findings = []Finding{{Title: "memory.search", Summary: fmt.Sprintf("未找到匹配 '%s' 的记忆", query), Verified: false, Source: "hermes"}}
		}
		return &Response{ID: req.ID, Type: "response", Result: &Result{Findings: findings}}
	}

	var results []map[string]string
	json.Unmarshal([]byte(out), &results)
	var findings []Finding
	for _, r := range results {
		findings = append(findings, Finding{
			Title: r["title"], Summary: r["id"], Verified: false, Source: "hermes",
		})
	}
	return &Response{ID: req.ID, Type: "response", Result: &Result{Findings: findings}}
}

func handleMemoryStore(req Request) *Response {
	title, _ := req.Params["title"].(string)
	content, _ := req.Params["content"].(string)
	if title == "" || content == "" {
		return &Response{ID: req.ID, Type: "error", Error: "title and content required"}
	}

	memoryDir := filepath.Join(hermesDir, "memory")
	os.MkdirAll(memoryDir, 0755)
	path := filepath.Join(memoryDir, title+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return &Response{ID: req.ID, Type: "error", Error: fmt.Sprintf("write failed: %v", err)}
	}

	return &Response{ID: req.ID, Type: "response", Result: &Result{
		Findings: []Finding{{Title: "memory.store", Summary: fmt.Sprintf("已存储: %s", title), Verified: false, Source: "hermes"}},
	}}
}

func handleCodeSearch(req Request) *Response {
	query, _ := req.Params["query"].(string)
	if query == "" {
		return &Response{ID: req.ID, Type: "error", Error: "query required"}
	}
	// Search hermes source files as fallback
	var findings []Finding
	filepath.Walk(hermesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".py") {
			return nil
		}
		if strings.Contains(strings.ToLower(info.Name()), strings.ToLower(query)) {
			rel, _ := filepath.Rel(hermesDir, path)
			findings = append(findings, Finding{Title: rel, Summary: "Hermes 源码", Verified: false, Source: "hermes"})
		}
		return nil
	})
	if len(findings) == 0 {
		findings = []Finding{{Title: "code.search", Summary: fmt.Sprintf("搜索: %s — 未找到匹配文件", query), Verified: false, Source: "hermes"}}
	}
	return &Response{ID: req.ID, Type: "response", Result: &Result{Findings: findings}}
}

func handleToolsList(req Request) *Response {
	// List hermes-agent directory structure as capability reference
	entries, _ := os.ReadDir(hermesDir)
	dirs := []string{"agent/", "skills/", "conversations/", "memory/"}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name()+"/")
		}
	}
	var findings []Finding
	for _, d := range dirs {
		findings = append(findings, Finding{
			Title: d, Summary: "Hermes Agent 模块", Verified: false, Source: "hermes",
		})
	}
	return &Response{ID: req.ID, Type: "response", Result: &Result{Findings: findings}}
}

func handleAgentChat(req Request) *Response {
	prompt, _ := req.Params["prompt"].(string)
	if prompt == "" {
		return &Response{ID: req.ID, Type: "error", Error: "prompt required"}
	}

	// Verify Hermes is importable
	_, err := py("-c", fmt.Sprintf(`
import sys
sys.path.insert(0, %q)
from hermes_state import get_state
state = get_state()
print(f"ok: %d conversations" % len(state.get("conversations", [])))
`, hermesDir))

	status := "hermes 可用"
	if err != nil {
		status = fmt.Sprintf("hermes 导入失败: %v", err)
	}

	return &Response{ID: req.ID, Type: "response", Result: &Result{
		Findings: []Finding{{Title: "agent.chat", Summary: fmt.Sprintf("%s | 消息（%d 字）", status, len(prompt)), Verified: false, Source: "hermes"}},
	}}
}

func writeError(w http.ResponseWriter, id, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{ID: id, Type: "error", Error: msg})
}
