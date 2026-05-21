package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	usageMu   sync.Mutex
	usageDir  string
	usageOnce sync.Once
)

// UsageRecord 写入日志的单条记录。
type UsageRecord struct {
	Timestamp    int64  `json:"ts"`
	Caller       string `json:"caller"` // "router" / "think" / "workflow:content_review.classify"
	Model        string `json:"model"`
	PromptTokens int    `json:"prompt_tokens"`
	CompTokens   int    `json:"completion_tokens"`
	TotalTokens  int    `json:"total_tokens"`
}

// RecordUsage 将一次 LLM 调用的 token 消耗追加到当日日志文件。
// 线程安全，每日一个 JSONL 文件：~/.hermes/usage/2026-05-21.json
func RecordUsage(caller string, usage *Usage) {
	if usage == nil || usage.TotalTokens == 0 {
		return
	}

	usageOnce.Do(func() {
		if usageDir == "" {
			home, _ := os.UserHomeDir()
			usageDir = filepath.Join(home, ".hermes", "usage")
		}
		os.MkdirAll(usageDir, 0o755)
	})

	rec := UsageRecord{
		Timestamp:    time.Now().Unix(),
		Caller:       caller,
		Model:        usage.Model,
		PromptTokens: usage.PromptTokens,
		CompTokens:   usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}

	data, _ := json.Marshal(rec)

	usageMu.Lock()
	defer usageMu.Unlock()

	date := time.Now().Format("2006-01-02")
	path := filepath.Join(usageDir, date+".json")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("[usage] 写入失败: %v\n", err)
		return
	}
	defer f.Close()
	f.Write(data)
	f.WriteString("\n")
}

// LoadUsage 读取指定日期的使用记录。
func LoadUsage(date string) ([]UsageRecord, error) {
	if usageDir == "" {
		home, _ := os.UserHomeDir()
		usageDir = filepath.Join(home, ".hermes", "usage")
	}
	path := filepath.Join(usageDir, date+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []UsageRecord
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var rec UsageRecord
		if json.Unmarshal(line, &rec) == nil {
			records = append(records, rec)
		}
	}
	return records, nil
}

// UsageSummary 按 caller 聚合的统计。
type UsageSummary struct {
	Date         string            `json:"date"`
	TotalCalls   int               `json:"total_calls"`
	TotalTokens  int               `json:"total_tokens"`
	ByCaller     map[string]int    `json:"by_caller_tokens"`
	ByModel      map[string]int    `json:"by_model_tokens"`
	Records      int               `json:"records"`
}

// SummarizeUsage 汇总指定日期的使用统计。
func SummarizeUsage(date string) UsageSummary {
	records, _ := LoadUsage(date)
	s := UsageSummary{
		Date:     date,
		ByCaller: make(map[string]int),
		ByModel:  make(map[string]int),
		Records:  len(records),
	}
	for _, r := range records {
		s.TotalCalls++
		s.TotalTokens += r.TotalTokens
		s.ByCaller[r.Caller] += r.TotalTokens
		s.ByModel[r.Model] += r.TotalTokens
	}
	return s
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
