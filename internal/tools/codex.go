package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxCodexExtractChars = 120000

var sensitiveCodexPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret|password)\s*[:=]\s*['"]?[^'"\s]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{16,}`),
}

/* ─── 数据结构 ─────────────────────────────────── */

type CodexSession struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
	Path       string `json:"path,omitempty"`
	MsgCount   int    `json:"msg_count,omitempty"`
}

type CodexMessage struct {
	Role    string `json:"role"`    // user / assistant
	Content string `json:"content"` // message text
}

type CodexConversation struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Messages []CodexMessage `json:"messages"`
}

/* ─── 目录查找 ─────────────────────────────────── */

func codexDir() string {
	if d := os.Getenv("CODEX_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

/* ─── Session List ─────────────────────────────── */

func CodexSessionList(keyword string, limit int, since, until string) *ToolResult {
	idxPath := filepath.Join(codexDir(), "session_index.jsonl")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return codexSessionListByScan(keyword)
	}
	if limit <= 0 {
		limit = 50
	}

	var sessions []CodexSession
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s CodexSession
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(s.ThreadName), strings.ToLower(keyword)) {
			continue
		}
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		return successResult(`{"sessions":[],"count":0,"message":"无匹配会话"}`)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	result := map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
		"message":  fmt.Sprintf("找到 %d 个会话", len(sessions)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func codexSessionListByScan(keyword string) *ToolResult {
	root := codexDir()
	sessionsDir := filepath.Join(root, "sessions")

	var sessions []CodexSession
	filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		// Extract ID from filename: rollout-2026-05-11T08-08-18-{uuid}.jsonl
		parts := strings.SplitN(info.Name(), "-", 7)
		id := ""
		if len(parts) >= 7 {
			id = strings.TrimSuffix(parts[6], ".jsonl")
		} else {
			id = strings.TrimSuffix(info.Name(), ".jsonl")
		}
		s := CodexSession{
			ID:         id,
			ThreadName: id,
			UpdatedAt:  info.ModTime().Format(time.RFC3339),
			Path:       path,
		}
		if keyword == "" || strings.Contains(strings.ToLower(s.ThreadName), strings.ToLower(keyword)) {
			sessions = append(sessions, s)
		}
		return nil
	})

	if len(sessions) == 0 {
		return successResult(`{"sessions":[],"count":0,"message":"无匹配会话"}`)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	result := map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
		"message":  fmt.Sprintf("找到 %d 个会话（来自文件扫描）", len(sessions)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── Session Extract ──────────────────────────── */

func CodexSessionExtract(id string) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}

	sessionPath := findSessionFile(id)
	if sessionPath == "" {
		return errorResult(fmt.Sprintf("会话 %s 未找到（搜索 sessions/ 和 archived_sessions/）", id))
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return errorResult(fmt.Sprintf("读取会话文件失败: %v", err))
	}

	var conv CodexConversation
	conv.ID = id
	conv.Title = id
	var truncated bool
	conv.Messages, truncated = extractMessages(data)

	if len(conv.Messages) == 0 {
		return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","messages":[],"count":0,"message":"未提取到对话内容"}`, id, id))
	}

	// 尝试从 session_index 获取标题
	if title := lookupSessionTitle(id); title != "" {
		conv.Title = title
	}

	result := map[string]interface{}{
		"id":              conv.ID,
		"title":           conv.Title,
		"messages":        conv.Messages,
		"count":           len(conv.Messages),
		"path":            sessionPath,
		"truncated":       truncated,
		"max_chars":       maxCodexExtractChars,
		"extracted_chars": conversationCharCount(conv.Messages),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func findSessionFile(id string) string {
	root := codexDir()
	// 搜索 sessions/ 下所有子目录
	var found string
	filepath.Walk(filepath.Join(root, "sessions"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(info.Name(), id) && strings.HasSuffix(info.Name(), ".jsonl") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found
	}
	// 搜索 archived_sessions/
	filepath.Walk(filepath.Join(root, "archived_sessions"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(info.Name(), id) && strings.HasSuffix(info.Name(), ".jsonl") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func extractMessages(data []byte) ([]CodexMessage, bool) {
	type rawLine struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	type eventPayload struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}

	var messages []CodexMessage
	seen := make(map[string]bool) // dedup near-identical messages
	totalChars := 0
	truncated := false

	for _, line := range strings.Split(string(data), "\n") {
		if truncated {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rl rawLine
		if err := json.Unmarshal([]byte(line), &rl); err != nil {
			continue
		}
		if rl.Type != "event_msg" {
			continue
		}
		var ep eventPayload
		if err := json.Unmarshal(rl.Payload, &ep); err != nil {
			continue
		}

		role := ""
		switch ep.Type {
		case "user_message":
			role = "user"
		case "agent_message":
			role = "assistant"
		default:
			continue
		}

		msg := strings.TrimSpace(ep.Message)
		if msg == "" || seen[msg] {
			continue
		}
		msg = redactSensitiveCodexText(msg)
		msgChars := len([]rune(msg))
		if totalChars+msgChars > maxCodexExtractChars {
			remaining := maxCodexExtractChars - totalChars
			if remaining <= 0 {
				break
			}
			msg = truncateRunes(msg, remaining) + "\n\n[已截断：对话过长，请分段导入]"
			truncated = true
		}
		seen[msg] = true
		messages = append(messages, CodexMessage{Role: role, Content: msg})
		totalChars += len([]rune(msg))
	}
	return messages, truncated
}

func conversationCharCount(messages []CodexMessage) int {
	total := 0
	for _, m := range messages {
		total += len([]rune(m.Content))
	}
	return total
}

func redactSensitiveCodexText(s string) string {
	for _, re := range sensitiveCodexPatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			if idx := strings.IndexAny(match, "=:"); idx >= 0 {
				return match[:idx+1] + " [REDACTED]"
			}
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [REDACTED]"
			}
			return "[REDACTED_SECRET]"
		})
	}
	return s
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if n >= len(runes) {
		return s
	}
	return string(runes[:n])
}

func lookupSessionTitle(id string) string {
	idxPath := filepath.Join(codexDir(), "session_index.jsonl")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s CodexSession
		if json.Unmarshal([]byte(line), &s) != nil {
			continue
		}
		if strings.Contains(s.ID, id) || strings.Contains(id, s.ID) {
			return s.ThreadName
		}
	}
	return ""
}

/* ─── Tool 注册 ─────────────────────────────────── */

func registerCodexTools() {
	Register("codex_session_list", "列出 Codex 对话历史（支持关键词、数量、日期过滤）。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keyword": stringParam("可选的关键词过滤"),
				"limit":   intParam("最大返回数，默认 50"),
				"since":   stringParam("起始日期（ISO格式，如 2026-05-01）"),
				"until":   stringParam("截止日期（ISO格式）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			limit, _ := args["limit"].(float64)
			return CodexSessionList(strArg(args, "keyword"), int(limit), strArg(args, "since"), strArg(args, "until"))
		},
	)

	Register("codex_session_extract", "提取指定 Codex 对话的完整文本内容。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": stringParam("会话 ID（UUID 或文件名中的 ID 片段）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return CodexSessionExtract(strArg(args, "id"))
		},
	)
}
