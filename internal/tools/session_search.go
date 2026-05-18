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

func registerSessionSearchTools() {
	Register("session_search", "Search messages across all stored sessions by keyword.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"keyword"},
			"properties": map[string]interface{}{
				"keyword": stringParam("Keyword to search for in session messages"),
				"limit":   intParam("Max results (default 20)"),
			},
		},
		sessionSearchHandler,
	)

	Register("session_list", "List all stored sessions sorted by last update time.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		sessionListHandler,
	)
}

func sessionSearchHandler(args map[string]interface{}) *ToolResult {
	keyword, _ := args["keyword"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

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
			if strings.Contains(strings.ToLower(m.Payload), strings.ToLower(keyword)) ||
				strings.Contains(strings.ToLower(m.Type), strings.ToLower(keyword)) {
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
		sb.WriteString(fmt.Sprintf("  %s [%d msgs, %d ev] %s\n", s.id[:8], s.msgCount, s.evCount, ts))
	}
	return successResult(sb.String())
}
