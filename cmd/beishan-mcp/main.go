// cmd/beishan-mcp — beishan 知识库 MCP Server
//
// 用途（双向）：
//   1. Claude Code 开发会话：通过 MCP 协议直接读写知识库，
//      开发决策、踩坑记录在对话中自然沉淀，跨会话可检索。
//   2. 智能体自身能力：beishan-core 通过 internal/mcp/client.go
//      连接本 server，知识库操作走统一 MCP 接口，与外部 MCP 生态对齐。
//
// 协议：MCP 2024-11-05，stdio 传输（一行一条 JSON-RPC 2.0 消息）
//
// 暴露的工具（6 个）：
//   knowledge_search   — 关键词搜索（L0+L1）
//   knowledge_remember — 轻量写入（记录决策/笔记，source_type=memory）
//   knowledge_add      — 完整写入（含 tags/topics/tasks/links 等结构化字段）
//   knowledge_get      — 按 ID 读取完整条目
//   knowledge_list     — 列出条目（可按类型/天数过滤）
//   knowledge_probe    — 检索质量探针（L0/L1 recall@3）
//
// 启动方式：
//   go build -o beishan-mcp ./cmd/beishan-mcp/
//   # 注册到 .claude/settings.json 的 mcpServers 后由 Claude Code 自动管理

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"beishan/internal/tools"
)

// ─── JSON-RPC 2.0 ────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP 协议类型 ────────────────────────────────────────────────

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpContent struct {
	Type string `json:"type"` // 固定 "text"
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// ─── 入口 ────────────────────────────────────────────────────────

func main() {
	tools.Init()

	// 4 MB 缓冲——知识条目内容可能很大
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	out := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue // 忽略格式错误的行
		}

		resp := dispatch(req)
		if resp == nil {
			continue // notification 无需回复
		}

		data, _ := json.Marshal(resp)
		out.Write(data)
		out.WriteByte('\n')
		out.Flush()
	}
}

// ─── 请求分发 ─────────────────────────────────────────────────────

func dispatch(req rpcRequest) *rpcResponse {
	switch req.Method {

	case "initialize":
		return ok(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "beishan-knowledge", "version": "1.0.0"},
		})

	case "initialized":
		return nil // notification，不回复

	case "ping":
		return ok(req.ID, map[string]interface{}{})

	case "tools/list":
		return ok(req.ID, map[string]interface{}{"tools": knowledgeTools()})

	case "tools/call":
		return handleToolCall(req)

	case "shutdown":
		return ok(req.ID, nil)

	default:
		if req.ID == nil {
			return nil // 未知 notification，静默忽略
		}
		return errResp(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// ─── 工具定义 ─────────────────────────────────────────────────────

func knowledgeTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "knowledge_search",
			Description: "搜索 beishan 知识库（关键词匹配 title/summary/content/tags）。查历史决策、架构说明、踩坑记录首选此工具。",
			InputSchema: mustJSON(`{
				"type": "object",
				"required": ["keyword"],
				"properties": {
					"keyword": {"type": "string", "description": "搜索关键词，支持中英文"}
				}
			}`),
		},
		{
			Name:        "knowledge_remember",
			Description: "轻量写入知识库（source_type=memory）。开发会话中记录决策、笔记、重要发现，优先用此工具，比 knowledge_add 简单。返回值含 created_at 时间戳。",
			InputSchema: mustJSON(`{
				"type": "object",
				"required": ["title", "summary"],
				"properties": {
					"title":   {"type": "string", "description": "标题，简洁描述这条记录"},
					"summary": {"type": "string", "description": "内容正文"},
					"tags": {
						"type": "array",
						"items": {"type": "string"},
						"description": "标签，如 [\"architecture\",\"decision\",\"beishan-core\"]"
					},
					"expires_in_days": {
						"type": "integer",
						"description": "过期天数，0 = 永久（默认）"
					},
					"namespace": {
						"type": "string",
						"description": "所属空间：留空 = default（智能体主库），\"claude_dev\" = Claude Code 开发会话专用（与主库隔离，避免污染）"
					}
				}
			}`),
		},
		{
			Name:        "knowledge_add",
			Description: "完整写入知识库条目，支持 tags/topics/tasks/links 等结构化字段。适合从文章、文件、对话中批量入库。",
			InputSchema: mustJSON(`{
				"type": "object",
				"required": ["source_type", "title", "summary"],
				"properties": {
					"source_type": {"type": "string", "description": "来源: chat|article|idea|web|file|note|codex|claude_memory"},
					"title":       {"type": "string", "description": "条目标题"},
					"summary":     {"type": "string", "description": "内容摘要"},
					"content":     {"type": "string", "description": "完整正文（可选）"},
					"tags": {
						"type": "array",
						"items": {"type": "string"},
						"description": "标签列表"
					},
					"topics": {
						"type": "array",
						"items": {"type": "string"},
						"description": "主题列表"
					},
					"tasks": {
						"type": "array",
						"items": {"type": "string"},
						"description": "提取出的行动项/待办"
					},
					"namespace": {"type": "string", "description": "命名空间: default|workspace|project（默认 default）"},
					"raw_ref":   {"type": "string", "description": "原始来源引用，如 URL 或文件路径"}
				}
			}`),
		},
		{
			Name:        "knowledge_get",
			Description: "按 ID 获取知识库条目的完整内容。ID 格式为 kn_xxxx，通常来自 knowledge_search 的结果。",
			InputSchema: mustJSON(`{
				"type": "object",
				"required": ["id"],
				"properties": {
					"id": {"type": "string", "description": "条目 ID，格式 kn_xxxx"}
				}
			}`),
		},
		{
			Name:        "knowledge_list",
			Description: "列出知识库条目，可按来源类型、时间和 namespace 过滤。用于浏览知识库全貌或找某类条目。",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"source_type":  {"type": "string", "description": "来源类型过滤（留空=全部）"},
					"days":         {"type": "integer", "description": "仅显示最近 N 天（0=全部）"},
					"content_type": {"type": "string", "description": "内容类型过滤：work_record|decision|lesson|fact"},
					"namespace":    {"type": "string", "description": "空间过滤：留空=全部，\"claude_dev\"=仅 Claude Code 开发记忆"}
				}
			}`),
		},
		{
			Name:        "knowledge_probe",
			Description: "检索质量探针：随机采样知识库，测量 L0 关键词和 L1 语义的 recall@3。返回 ProbeResult JSON，用于诊断知识库健康。",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {}
			}`),
		},
	}
}

// ─── 工具调用 ─────────────────────────────────────────────────────

func handleToolCall(req rpcRequest) *rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, -32602, "invalid params: "+err.Error())
	}

	var result *tools.ToolResult

	switch params.Name {
	case "knowledge_search", "knowledge_add", "knowledge_remember",
		"knowledge_get", "knowledge_list":
		result = tools.ValidateAndExecute(params.Name, params.Arguments)

	case "knowledge_probe":
		result = tools.KnowledgeProbe()

	default:
		return errResp(req.ID, -32602, "unknown tool: "+params.Name)
	}

	if result == nil {
		return errResp(req.ID, -32603, "tool returned nil")
	}

	return ok(req.ID, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: result.Output}},
		IsError: !result.Success,
	})
}

// ─── 辅助函数 ─────────────────────────────────────────────────────

func ok(id interface{}, result interface{}) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errResp(id interface{}, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func mustJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}
