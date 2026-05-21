package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"beishan/internal/llm"
)

// KnowledgeStats 返回知识库统计信息。
func KnowledgeStats() map[string]interface{} {
	all := loadAllKnowledgePtr()
	total := len(all)
	byType := make(map[string]int)
	byStatus := make(map[string]int)
	withLinks := 0
	withEmbedding := 0
	withBow := 0

	home, _ := os.UserHomeDir()
	kbDir := filepath.Join(home, ".hermes", "memory", "knowledge")

	for _, e := range all {
		byType[e.SourceType]++
		status := e.Status
		if status == "" {
			status = "active"
		}
		byStatus[status]++
		if len(e.TypedLinks) > 0 {
			withLinks++
		}
		if len(e.Embedding) > 0 {
			withEmbedding++
		}
		// 检查 BOW 向量文件
		bowPath := filepath.Join(kbDir, e.ID+".embed.json")
		if _, err := os.Stat(bowPath); err == nil {
			withBow++
		}
	}

	return map[string]interface{}{
		"total":            total,
		"by_type":          byType,
		"by_status":        byStatus,
		"with_links":       withLinks,
		"with_embedding":   withEmbedding,
		"with_bow":         withBow,
	}
}

func loadAllKnowledgePtr() []*KnowledgeEntry {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".hermes", "memory", "knowledge")
	entries, _ := os.ReadDir(dir)
	var result []*KnowledgeEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".embed.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var entry KnowledgeEntry
		if json.Unmarshal(data, &entry) == nil {
			result = append(result, &entry)
		}
	}
	return result
}

// SessionStats 返回会话统计信息。
func SessionStats() map[string]interface{} {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".hermes", "memory", "sessions")
	entries, _ := os.ReadDir(dir)
	total := len(entries)

	today := 0
	todayStr := time.Now().Format("20060102")
	for _, e := range entries {
		if len(e.Name()) >= 8 {
			// 文件名格式: session_YYYYMMDD_*.json 或直接 sessionID.json
			name := e.Name()
			if len(name) >= 8 && name[:8] == todayStr {
				today++
			}
		}
	}

	return map[string]interface{}{
		"total": total,
		"today": today,
	}
}

// UsageToday 返回今日 LLM 使用统计。
func UsageToday() map[string]interface{} {
	date := time.Now().Format("2006-01-02")
	s := llm.SummarizeUsage(date)
	return map[string]interface{}{
		"date":         s.Date,
		"total_calls":  s.TotalCalls,
		"total_tokens": s.TotalTokens,
		"by_caller":    s.ByCaller,
		"by_model":     s.ByModel,
	}
}
