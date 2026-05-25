package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ─── 数据结构 ─────────────────────────────────── */

type SessionMessage struct {
	Role      string `json:"role"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	CreatedAt int64  `json:"created_at"`
}

type Evidence struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Detail    string `json:"detail"`
	CreatedAt int64  `json:"created_at"`
}

type SessionData struct {
	SessionID string           `json:"session_id"`
	Messages  []SessionMessage `json:"messages"`
	Evidence  []Evidence       `json:"evidence"`
	CreatedAt int64            `json:"created_at"`
	UpdatedAt int64            `json:"updated_at"`
}

/* ─── 存储引擎 ─────────────────────────────────── */

var (
	sessionMu  sync.RWMutex
	sessionDir string
)

func initSessionDir() {
	if sessionDir == "" {
		sessionDir = filepath.Join(MemoryDir, "sessions")
	}
	os.MkdirAll(sessionDir, 0755)
}

func sessionPath(id string) string {
	return filepath.Join(sessionDir, id+".json")
}

func loadSession(id string) *SessionData {
	initSessionDir()
	data, err := os.ReadFile(sessionPath(id))
	if err != nil {
		return &SessionData{
			SessionID: id,
			Messages:  []SessionMessage{},
			Evidence:  []Evidence{},
			CreatedAt: time.Now().Unix(),
			UpdatedAt: time.Now().Unix(),
		}
	}
	var s SessionData
	json.Unmarshal(data, &s)
	return &s
}

func saveSession(s *SessionData) {
	initSessionDir()
	s.UpdatedAt = time.Now().Unix()
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(sessionPath(s.SessionID), data, 0644)
}

/* ─── 安全扫描 ─────────────────────────────────── */

var sessionThreatPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|above|prior|foregoing)\s+(instructions?|directions?|prompts?|messages?)`),
	regexp.MustCompile(`(?i)system\s*prompt\s*(override|reset|change|replace|delete)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(a\s+)?different`),
	regexp.MustCompile(`(?i)<\|im_start\|>`),
	regexp.MustCompile(`(?i)<\|im_end\|>`),
}

func scanThreats(content string) bool {
	for _, p := range sessionThreatPatterns {
		if p.MatchString(content) {
			return true
		}
	}
	return false
}

/* ─── 公开 API ─────────────────────────────────── */

func SessionAdd(sessionID, role, msgType, payload string) *ToolResult {
	if scanThreats(payload) {
		return errorResult("content blocked by threat scanner")
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	s := loadSession(sessionID)
	s.Messages = append(s.Messages, SessionMessage{
		Role:      role,
		Type:      msgType,
		Payload:   payload,
		CreatedAt: time.Now().Unix(),
	})
	saveSession(s)

	return successResult(fmt.Sprintf("session %s: 已记录 %s/%s", sessionID, role, msgType))
}

func SessionGet(sessionID string) *ToolResult {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	s := loadSession(sessionID)
	b, _ := json.MarshalIndent(s, "", "  ")
	return successResult(string(b))
}

func SessionSearch(keyword string) *ToolResult {
	initSessionDir()
	entries, _ := os.ReadDir(sessionDir)

	var results []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".json")
		s := loadSession(sessionID)

		for _, m := range s.Messages {
			if strings.Contains(strings.ToLower(m.Payload), strings.ToLower(keyword)) ||
				strings.Contains(strings.ToLower(m.Type), strings.ToLower(keyword)) {
				results = append(results, fmt.Sprintf("[%s] %s/%s: %s",
					sessionID, m.Role, m.Type, truncateStr(m.Payload, 100)))
			}
		}
		for _, ev := range s.Evidence {
			if strings.Contains(strings.ToLower(ev.Summary), strings.ToLower(keyword)) ||
				strings.Contains(strings.ToLower(ev.Detail), strings.ToLower(keyword)) {
				results = append(results, fmt.Sprintf("[%s][证据] %s: %s",
					sessionID, ev.Type, ev.Summary))
			}
		}
	}

	if len(results) == 0 {
		return successResult("No matching sessions found.")
	}

	sort.Strings(results)
	return successResult(strings.Join(results, "\n"))
}

func SessionList() *ToolResult {
	initSessionDir()
	entries, _ := os.ReadDir(sessionDir)

	var sessions []SessionData
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".json")
		s := loadSession(sessionID)
		sessions = append(sessions, *s)
	}

	if len(sessions) == 0 {
		return successResult("No sessions found.")
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	var lines []string
	for _, s := range sessions {
		msgCount := len(s.Messages)
		evCount := len(s.Evidence)
		updated := time.Unix(s.UpdatedAt, 0).Format("01-02 15:04")
		lines = append(lines, fmt.Sprintf("%s [%d msgs, %d ev] updated %s",
			s.SessionID, msgCount, evCount, updated))
	}

	return successResult(strings.Join(lines, "\n"))
}

func SessionDelete(sessionID string) *ToolResult {
	initSessionDir()
	if err := os.Remove(sessionPath(sessionID)); err != nil {
		return errorResult(fmt.Sprintf("delete session: %v", err))
	}
	return successResult(fmt.Sprintf("session %s deleted", sessionID))
}

/* ─── Evidence ──────────────────────────────────── */

func EvidenceAdd(sessionID, evType, summary, detail string) *ToolResult {
	if scanThreats(summary) {
		return errorResult("evidence content blocked")
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	s := loadSession(sessionID)
	s.Evidence = append(s.Evidence, Evidence{
		ID:        fmt.Sprintf("ev_%d", time.Now().UnixNano()),
		Type:      evType,
		Summary:   summary,
		Detail:    detail,
		CreatedAt: time.Now().Unix(),
	})
	saveSession(s)

	return successResult(fmt.Sprintf("evidence added to session %s: %s", sessionID, summary))
}

func EvidenceSearch(keyword string) *ToolResult {
	initSessionDir()
	entries, _ := os.ReadDir(sessionDir)

	var results []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".json")
		s := loadSession(sessionID)

		for _, ev := range s.Evidence {
			if strings.Contains(strings.ToLower(ev.Summary), strings.ToLower(keyword)) ||
				strings.Contains(strings.ToLower(ev.Detail), strings.ToLower(keyword)) {
				results = append(results, fmt.Sprintf("[%s] %s: %s",
					sessionID, ev.Type, ev.Summary))
			}
		}
	}

	if len(results) == 0 {
		return successResult("No matching evidence found.")
	}

	sort.Strings(results)
	return successResult(strings.Join(results, "\n"))
}

/* ─── Tool 注册 ─────────────────────────────────── */

func registerMemoryTools() {
	Register("session_add", "Add a message to a session timeline.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"session_id", "role", "type", "payload"},
			"properties": map[string]interface{}{
				"session_id": stringParam("Session identifier"),
				"role":       stringParam("Who sent this (user, search_plugin, write_plugin, etc.)"),
				"type":       stringParam("Message type (chat, web_search, write_file, etc.)"),
				"payload":    stringParam("Message content text"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return SessionAdd(
				strArg(args, "session_id"),
				strArg(args, "role"),
				strArg(args, "type"),
				strArg(args, "payload"),
			)
		},
	)

	Register("session_get", "Get all messages and evidence for a session.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"session_id"},
			"properties": map[string]interface{}{
				"session_id": stringParam("Session identifier"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return SessionGet(strArg(args, "session_id"))
		},
	)



	Register("session_delete", "Delete a session and all its evidence.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"session_id"},
			"properties": map[string]interface{}{
				"session_id": stringParam("Session identifier to delete"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return SessionDelete(strArg(args, "session_id"))
		},
	)

	Register("evidence_add", "Add structured evidence to a session.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"session_id", "type", "summary"},
			"properties": map[string]interface{}{
				"session_id": stringParam("Session identifier"),
				"type":       stringParam("Evidence type (legal_search_result, clause_analysis, risk_assessment, etc.)"),
				"summary":    stringParam("Short summary of the evidence"),
				"detail":     stringParam("Detailed content or reference"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return EvidenceAdd(
				strArg(args, "session_id"),
				strArg(args, "type"),
				strArg(args, "summary"),
				strArg(args, "detail"),
			)
		},
	)

	Register("evidence_search", "Search evidence across all sessions.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"keyword"},
			"properties": map[string]interface{}{
				"keyword": stringParam("Search keyword"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return EvidenceSearch(strArg(args, "keyword"))
		},
	)
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}
