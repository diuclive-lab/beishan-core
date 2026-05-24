// OpenHuman Flower Adapter — thin HTTP bridge between beishan-core Right Flower
// Protocol and OpenHuman's local API.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOpenHumanEndpoint = "http://127.0.0.1:7788"

type RightFlowerRequest struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

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

func loadMethodMap() map[string]string {
	m := map[string]string{
		"memory.search":   "openhuman.memory_recall_memories",
		"memory.store":    "openhuman.memory_doc_put",
		"context.retrieve": "openhuman.memory_context_query",
		"code.review":     "openhuman.agent_chat",
	}
	// ENV override: OPENHUMAN_METHOD_MAP=memory.search=recall:cust.method=custom
	if env := os.Getenv("OPENHUMAN_METHOD_MAP"); env != "" {
		for _, pair := range strings.Split(env, ":") {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				m[parts[0]] = parts[1]
			}
		}
	}
	return m
}

var methodMap = loadMethodMap()

// translateMethod resolves method name with case-insensitive fallback.
func translateMethod(flowerMethod string) (string, bool) {
	// Direct match
	if oh, ok := methodMap[flowerMethod]; ok { return oh, ok }
	// Legacy aliases (formerly in internal/legacy)
	legacyAliases := map[string]string{
		"memory.search": "openhuman.memory_recall_memories",
		"memory.store":  "openhuman.memory_doc_put",
	}
	if canonical, ok := legacyAliases[flowerMethod]; ok {
		if _, ok := methodMap[canonical]; ok { return methodMap[canonical], true }
	}
	// Case-insensitive fallback
	for k, v := range methodMap {
		if strings.EqualFold(k, flowerMethod) { return v, true }
	}
	return "", false
}

var (
	openHumanEndpoint string
	openHumanToken    string
)

func probe() bool {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(openHumanEndpoint + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func probeAuth() (bool, bool) {
	// Returns: reachable, auth_ok
	body, code, err := dispatchToOpenHuman("ping", map[string]any{})
	if err != nil {
		return false, false
	}
	if code == 401 || code == 403 {
		return true, false
	}
	return code == 200, len(body) > 0
}

func dispatchToOpenHuman(method string, params map[string]any) ([]byte, int, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      "1",
	})
	hc := &http.Client{Timeout: 30 * time.Second}
	hreq, _ := http.NewRequest("POST", openHumanEndpoint+"/rpc", bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	if openHumanToken != "" {
		hreq.Header.Set("Authorization", "Bearer "+openHumanToken)
	}
	ohResp, err := hc.Do(hreq)
	if err != nil {
		return nil, 0, err
	}
	defer ohResp.Body.Close()
	respBody, _ := io.ReadAll(ohResp.Body)
	return respBody, ohResp.StatusCode, nil
}

func findingResult(id, title, summary, source string) RightFlowerResponse {
	return RightFlowerResponse{
		ID: id, Type: "response",
		Result: &Result{Findings: []Finding{
			{Title: title, Summary: summary, Verified: false, Source: source},
		}},
	}
}

func availableMethods() []string {
	keys := make([]string, 0, len(methodMap))
	for k := range methodMap { keys = append(keys, k) }
	return keys
}

func normalizeParams(method string, params map[string]any) map[string]any {
	p := make(map[string]any, len(params)+1)
	for k, v := range params { p[k] = v }
	if _, has := p["namespace"]; !has { p["namespace"] = "personal" }
	return p
}

func handleProbeMethods(w http.ResponseWriter, r *http.Request) {
	reachable, authOk := probeAuth()
	results := map[string]interface{}{"reachable": reachable, "auth_ok": authOk}
	for rf, oh := range methodMap {
		if !reachable {
			results[rf] = map[string]interface{}{"status": "unreachable"}
			continue
		}
		body, code, err := dispatchToOpenHuman(oh, map[string]any{})
		if err != nil {
			results[rf] = map[string]interface{}{"status": "error", "detail": err.Error()}
		} else {
			results[rf] = map[string]interface{}{"status": "responded", "code": code, "body": truncate(string(body), 200)}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req RightFlowerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	log.Printf("[openhuman-adapter] dispatch: method=%s id=%s", req.Method, req.ID)

	ohMethod, ok := translateMethod(req.Method)
	if !ok {
		json.NewEncoder(w).Encode(findingResult(req.ID,
			fmt.Sprintf("不支持的 method: %s。可用: " + strings.Join(availableMethods(), ", "), req.Method),
			"支持的 methods: memory.search, memory.store, context.retrieve, code.review",
			"openhuman_adapter"))
		return
	}

	if !probe() {
		json.NewEncoder(w).Encode(findingResult(req.ID,
			"OpenHuman 不可用", "OpenHuman 进程未运行或 API 不可达。请启动 OpenHuman 后重试。", "openhuman_adapter"))
		return
	}

	respBody, statusCode, err := dispatchToOpenHuman(ohMethod, req.Params)
	if err != nil {
		json.NewEncoder(w).Encode(findingResult(req.ID, "OpenHuman 调用失败",
			fmt.Sprintf("HTTP 请求失败: %v", err), "openhuman_adapter"))
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		snip := truncate(string(respBody), 300)
		json.NewEncoder(w).Encode(findingResult(req.ID,
			fmt.Sprintf("OpenHuman 返回 HTTP %d", statusCode), snip, "openhuman_adapter"))
		return
	}

	norm := NormalizeResponse(respBody, "openhuman")
	json.NewEncoder(w).Encode(RightFlowerResponse{ID: req.ID, Type: "response", Result: &norm})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// NormalizeResponse converts raw bytes into a standardized Result.
func NormalizeResponse(raw []byte, source string) Result {
	s := strings.TrimSpace(string(raw))
	if len(s) == 0 {
		return Result{Findings: []Finding{{Title: "空响应", Summary: "右花返回了空响应", Verified: false, Source: source}}}
	}
	// Try JSON-RPC result
	var rpc struct {
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct{ Message string `json:"message"` } `json:"error,omitempty"`
	}
	if json.Unmarshal(raw, &rpc) == nil && rpc.Error != nil {
		return Result{Findings: []Finding{{Title: "OpenHuman 错误", Summary: rpc.Error.Message, Verified: false, Source: source}}}
	}
	if json.Unmarshal(raw, &rpc) == nil && len(rpc.Result) > 0 {
		return Result{Findings: []Finding{{Title: "OpenHuman 结果", Summary: truncate(string(rpc.Result), 1000), Verified: false, Source: source}}}
	}
	// Plain JSON
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		summary, _ := json.Marshal(obj)
		return Result{Findings: []Finding{{Title: "JSON 响应", Summary: truncate(string(summary), 1000), Verified: false, Source: source}}}
	}
	// Plain text / markdown
	return Result{Findings: []Finding{{Title: "文本响应", Summary: truncate(s, 1000), Verified: false, Source: source}}}
}

func handleProbe(w http.ResponseWriter, r *http.Request) {
	status := "reachable"
	if !probe() {
		status = "unreachable"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"adapter": "openhuman", "openhuman": status})
}

type config struct {
	endpoint string
	token    string
	addr     string
}

func loadConfigFromEnv() config {
	endpoint := os.Getenv("OPENHUMAN_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultOpenHumanEndpoint
	}
	addr := ":9529"
	if p := os.Getenv("ADAPTER_PORT"); p != "" {
		addr = ":" + p
	}
	return config{endpoint: endpoint, token: os.Getenv("OPENHUMAN_TOKEN"), addr: addr}
}

func main() {
	cfg := loadConfigFromEnv()
	openHumanEndpoint = cfg.endpoint
	openHumanToken = cfg.token
	http.HandleFunc("/dispatch", handleDispatch)
	http.HandleFunc("/probe-methods", handleProbeMethods)
	http.HandleFunc("/health", handleProbe)
	log.Printf("[openhuman-adapter] 启动于 %s → OpenHuman: %s", cfg.addr, cfg.endpoint)
	log.Fatal(http.ListenAndServe(cfg.addr, nil))
}
