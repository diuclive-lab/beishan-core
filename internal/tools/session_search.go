package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ─── 会话摘要 ────────────────────────────────────────

// SessionSummary 会话摘要，用于快速检索。
type SessionSummary struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Topics    []string `json:"topics"`
	MsgCount  int      `json:"msg_count"`
	UpdatedAt int64    `json:"updated_at"`
}

func summaryPath(sessionID string) string {
	return filepath.Join(MemoryDir, "sessions", sessionID+".summary.json")
}

// GenerateSessionSummary 为单个 session 生成摘要（确定性，无 LLM）。
// 从用户消息中提取主题词和首条消息作为摘要。
func GenerateSessionSummary(sessionID string) *SessionSummary {
	sessionDir := filepath.Join(MemoryDir, "sessions")
	path := filepath.Join(sessionDir, sessionID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		Messages []struct {
			Role    string `json:"role"`
			Type    string `json:"type"`
			Payload string `json:"payload"`
		} `json:"messages"`
	}
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	if len(s.Messages) == 0 {
		return nil
	}

	// 提取用户消息
	var userMsgs []string
	for _, m := range s.Messages {
		if m.Role == "user" && m.Type == "chat" {
			var obj map[string]interface{}
			txt := m.Payload
			if json.Unmarshal([]byte(m.Payload), &obj) == nil {
				if msg, ok := obj["message"].(string); ok {
					txt = msg
				}
			}
			if txt != "" {
				userMsgs = append(userMsgs, txt)
			}
		}
	}
	if len(userMsgs) == 0 {
		return nil
	}

	// 摘要 = 首条用户消息（截断到 80 字符）
	summary := userMsgs[0]
	if len([]rune(summary)) > 80 {
		summary = string([]rune(summary)[:80]) + "..."
	}

	// 主题词提取：去停用词，取高频词
	topics := extractTopics(userMsgs)

	return &SessionSummary{
		ID:        sessionID,
		Summary:   summary,
		Topics:    topics,
		MsgCount:  len(s.Messages),
		UpdatedAt: time.Now().Unix(),
	}
}

// extractTopics 从消息列表中提取主题词（简单分词 + 去停用词 + 频率排序）。
func extractTopics(msgs []string) []string {
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "我": true, "你": true, "他": true,
		"她": true, "它": true, "们": true, "这": true, "那": true, "在": true,
		"有": true, "和": true, "与": true, "或": true, "不": true, "都": true,
		"也": true, "就": true, "把": true, "被": true, "让": true, "给": true,
		"从": true, "到": true, "对": true, "用": true, "为": true, "会": true,
		"能": true, "可以": true, "什么": true, "怎么": true, "如何": true,
		"请": true, "帮": true, "帮我": true, "一下": true, "看看": true,
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "can": true, "shall": true, "to": true, "of": true,
		"in": true, "for": true, "on": true, "with": true, "at": true, "by": true,
		"i": true, "you": true, "he": true, "she": true, "it": true, "we": true,
		"they": true, "this": true, "that": true, "and": true, "or": true,
		"but": true, "not": true, "no": true, "yes": true, "so": true,
	}

	freq := make(map[string]int)
	for _, msg := range msgs {
		words := strings.Fields(strings.ToLower(msg))
		for _, w := range words {
			w = strings.Trim(w, ".,!?;:\"'()[]{}")
			if len(w) < 2 || stopWords[w] {
				continue
			}
			freq[w]++
		}
	}

	type wordFreq struct {
		word string
		freq int
	}
	var wf []wordFreq
	for w, f := range freq {
		if f >= 2 {
			wf = append(wf, wordFreq{w, f})
		}
	}
	sort.Slice(wf, func(i, j int) bool { return wf[i].freq > wf[j].freq })

	var topics []string
	for i, w := range wf {
		if i >= 5 {
			break
		}
		topics = append(topics, w.word)
	}
	return topics
}

// SaveSessionSummary 保存摘要到文件。
func SaveSessionSummary(sum *SessionSummary) {
	if sum == nil {
		return
	}
	data, _ := json.MarshalIndent(sum, "", "  ")
	os.WriteFile(summaryPath(sum.ID), data, 0644)
}

// LoadSessionSummary 加载摘要文件。
func LoadSessionSummary(sessionID string) *SessionSummary {
	data, err := os.ReadFile(summaryPath(sessionID))
	if err != nil {
		return nil
	}
	var s SessionSummary
	json.Unmarshal(data, &s)
	return &s
}

// SessionSummarizeAll 为所有无摘要的 session 生成摘要。
func SessionSummarizeAll() *ToolResult {
	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, _ := os.ReadDir(sessionDir)
	var generated, skipped int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".summary.json") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		if LoadSessionSummary(sid) != nil {
			skipped++
			continue
		}
		sum := GenerateSessionSummary(sid)
		if sum != nil {
			SaveSessionSummary(sum)
			generated++
		}
	}
	return successResult(fmt.Sprintf(`{"generated":%d,"skipped":%d,"total":%d}`, generated, skipped, generated+skipped))
}

// SessionCleanup 清理超过 maxAge 天的旧会话文件。
func SessionCleanup(maxAgeDays int) *ToolResult {
	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return successResult(fmt.Sprintf(`{"deleted":0,"message":"%s"}`, err))
	}
	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	var deleted int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(sessionDir, e.Name()))
			deleted++
		}
	}
	return successResult(fmt.Sprintf(`{"deleted":%d,"max_age_days":%d}`, deleted, maxAgeDays))
}

func registerSessionSearchTools() {
	Register("session_search", "Search messages across all stored sessions by keyword.",
		map[string]interface{}{
			"type":                 "object",
			"required":             []string{"keyword"},
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"keyword": stringParam("Keyword to search for in session messages"),
				"limit":   intParam("Max results (default 20)"),
				"role":    stringParam("Filter by role (e.g., 'user', 'think_plugin'). Empty = all roles"),
			},
		},
		sessionSearchHandler,
	)

	Register("session_cleanup", "清理超过指定天数的旧会话记录。减少磁盘占用和检索噪音。默认 max_age_days=30。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"max_age_days": map[string]interface{}{
					"type":        "integer",
					"description": "保留最近 N 天的会话，超过的删除（默认 30）",
				},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			days := 30
			if d, ok := args["max_age_days"].(float64); ok && d > 0 {
				days = int(d)
			}
			return SessionCleanup(days)
		},
	)

	Register("session_list", "List all stored sessions sorted by last update time.",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
		},
		sessionListHandler,
	)
	Register("session_summarize", "为所有无摘要的会话生成摘要，提升会话检索速度。确定性提取，无需 LLM。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return SessionSummarizeAll()
		},
	)
}

/* ─── SessionSearchStructured 结构化会话检索（供 retrieval pipe 调用）── */

// SessionMatch 结构化会话搜索结果
type SessionMatch struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	MsgType   string `json:"msg_type"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

// SessionSearchStructured 结构化会话搜索，返回按时间倒序排列的匹配结果。
// 供 retrieval pipe 的 Episodic 管道调用。
// maxAgeDays: 只检索最近 N 天的会话，<=0 时默认 30。
func SessionSearchStructured(keyword string, limit int, maxAgeDays int) []SessionMatch {
	if limit <= 0 {
		limit = 10
	}
	if maxAgeDays <= 0 {
		maxAgeDays = 30
	}
	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	keywords := strings.Fields(strings.ToLower(keyword))
	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, _ := os.ReadDir(sessionDir)

	// Phase 1: 摘要快速匹配（优先）
	var summaryMatches []string // session IDs matched via summary
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".summary.json") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		sum := LoadSessionSummary(sid)
		if sum == nil {
			continue
		}
		sumText := strings.ToLower(sum.Summary + " " + strings.Join(sum.Topics, " "))
		for _, kw := range keywords {
			if strings.Contains(sumText, kw) {
				summaryMatches = append(summaryMatches, sid)
				break
			}
		}
	}

	// Phase 2: 全文搜索（摘要匹配的 session 优先）
	var results []SessionMatch
	seen := make(map[string]bool)

	// 先搜摘要命中的 session
	for _, sid := range summaryMatches {
		path := filepath.Join(sessionDir, sid+".json")
		data, _ := os.ReadFile(path)
		var s struct {
			Messages []struct {
				Role      string `json:"role"`
				Type      string `json:"type"`
				Payload   string `json:"payload"`
				CreatedAt int64  `json:"created_at"`
			} `json:"messages"`
		}
		json.Unmarshal(data, &s)
		for _, m := range s.Messages {
			if m.Role == "workflow_plugin" || m.Type == "workflow.result" {
				continue
			}
			payloadLower := strings.ToLower(m.Payload)
			for _, kw := range keywords {
				if strings.Contains(payloadLower, kw) {
					results = append(results, SessionMatch{
						SessionID: sid,
						Role:      m.Role,
						MsgType:   m.Type,
						Payload:   m.Payload,
						Timestamp: m.CreatedAt,
					})
					seen[sid+":"+m.Payload[:min(20, len(m.Payload))]] = true
					break
				}
			}
			if len(results) >= limit {
				goto done
			}
		}
	}

	// 再搜剩余 session（全文扫描）
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".summary.json") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		// 跳过已搜过的摘要匹配 session
		alreadyMatched := false
		for _, ms := range summaryMatches {
			if ms == sid {
				alreadyMatched = true
				break
			}
		}
		if alreadyMatched {
			continue
		}

		data, _ := os.ReadFile(filepath.Join(sessionDir, e.Name()))
		var s struct {
			Messages []struct {
				Role      string `json:"role"`
				Type      string `json:"type"`
				Payload   string `json:"payload"`
				CreatedAt int64  `json:"created_at"`
			} `json:"messages"`
		}
		json.Unmarshal(data, &s)

		for _, m := range s.Messages {
			if m.Role == "workflow_plugin" || m.Type == "workflow.result" {
				continue
			}
			payloadLower := strings.ToLower(m.Payload)
			for _, kw := range keywords {
				if strings.Contains(payloadLower, kw) {
					results = append(results, SessionMatch{
						SessionID: sid,
						Role:      m.Role,
						MsgType:   m.Type,
						Payload:   m.Payload,
						Timestamp: m.CreatedAt,
					})
					break
				}
			}
			if len(results) >= limit {
				goto done
			}
		}
	}
done:

	// 时间倒序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp > results[j].Timestamp
	})

	return results
}

func sessionSearchHandler(args map[string]interface{}) *ToolResult {
	keyword, _ := args["keyword"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	roleFilter, _ := args["role"].(string)

	// 拆分关键词（空格分隔 → OR 匹配）
	keywords := strings.Fields(strings.ToLower(keyword))

	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, _ := os.ReadDir(sessionDir)

	type result struct {
		sessionID string
		role      string
		msgType   string
		payload   string
		timestamp int64
	}
	var results []result

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		data, _ := os.ReadFile(filepath.Join(sessionDir, e.Name()))
		var s struct {
			Messages []struct {
				Role      string `json:"role"`
				Type      string `json:"type"`
				Payload   string `json:"payload"`
				CreatedAt int64  `json:"created_at"`
			} `json:"messages"`
		}
		json.Unmarshal(data, &s)

		for _, m := range s.Messages {
			// Role 过滤
			if roleFilter != "" && m.Role != roleFilter {
				continue
			}
			// 排除 workflow 执行结果
			if m.Role == "workflow_plugin" || m.Type == "workflow.result" {
				continue
			}
			// OR 匹配：任一关键词命中即匹配
			payloadLower := strings.ToLower(m.Payload)
			typeLower := strings.ToLower(m.Type)
			matched := false
			for _, kw := range keywords {
				if strings.Contains(payloadLower, kw) || strings.Contains(typeLower, kw) {
					matched = true
					break
				}
			}
			if matched {
				results = append(results, result{sid, m.Role, m.Type, m.Payload, m.CreatedAt})
				if len(results) >= limit {
					goto done
				}
			}
		}
	}
done:

	if len(results) == 0 {
		return successResult(fmt.Sprintf("No results for: '%s'", keyword))
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].timestamp > results[j].timestamp
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results for '%s':\n", keyword))
	for _, r := range results {
		ts := time.Unix(r.timestamp, 0).Format("01-02 15:04")
		sb.WriteString(fmt.Sprintf("  [%s] [%s] %s/%s: %s\n", ts, r.sessionID[:8], r.role, r.msgType, truncateStr(r.payload, 100)))
	}
	return successResult(sb.String())
}

func sessionListHandler(args map[string]interface{}) *ToolResult {
	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, _ := os.ReadDir(sessionDir)

	if len(entries) == 0 {
		return successResult("No sessions found.")
	}

	type sInfo struct {
		id        string
		msgCount  int
		evCount   int
		updatedAt int64
	}

	var sessions []sInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		data, _ := os.ReadFile(filepath.Join(sessionDir, e.Name()))
		var s struct {
			Messages  []interface{} `json:"messages"`
			Evidence  []interface{} `json:"evidence"`
			UpdatedAt int64         `json:"updated_at"`
		}
		json.Unmarshal(data, &s)
		sessions = append(sessions, sInfo{
			id: sid, msgCount: len(s.Messages),
			evCount: len(s.Evidence), updatedAt: s.UpdatedAt,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].updatedAt > sessions[j].updatedAt
	})

	var sb strings.Builder
	for _, s := range sessions {
		ts := time.Unix(s.updatedAt, 0).Format("01-02 15:04")
		truncID := s.id
		if len(truncID) > 8 {
			truncID = truncID[:8]
		}
		sb.WriteString(fmt.Sprintf("  %s [%d msgs, %d ev] %s\n", truncID, s.msgCount, s.evCount, ts))
	}
	return successResult(sb.String())
}
