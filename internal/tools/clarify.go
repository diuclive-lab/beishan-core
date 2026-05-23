package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"sync"

	"beishan/internal/clarify"
)

/* ─── 用户习惯学习 ───────────────────────────────

   clarify 触发时记录原始输入 → 用户澄清后的意图。
   下次收到相似输入时，如果匹配到已学习的模式且置信度足够，
   自动推断意图，不再询问用户。

   模式文件：~/.hermes/memory/clarify_patterns.json
*/

type userPattern struct {
	Keywords  []string `json:"keywords"`   // 触发 clarify 时的关键词
	Intent    string   `json:"intent"`     // 用户最终指定的意图
	Count     int      `json:"count"`      // 命中次数
	Threshold int      `json:"threshold"`  // 多少次后自动推断（默认 3）
	LastSeen  int64    `json:"last_seen"`  // 最近一次观察的 Unix 时间戳（EWMA 衰减用）
}

type patternStore struct {
	Patterns []userPattern `json:"patterns"`
	mu       sync.Mutex
}

var clarifyPatterns = &patternStore{}

func patternPath() string {
	dir := filepath.Join(MemoryDir, "memory")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "clarify_patterns.json")
}

func (ps *patternStore) load() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	data, err := os.ReadFile(patternPath())
	if err != nil {
		ps.Patterns = []userPattern{}
		return
	}
	json.Unmarshal(data, &ps.Patterns)
	if ps.Patterns == nil {
		ps.Patterns = []userPattern{}
	}
}

func (ps *patternStore) save() {
	data, _ := json.MarshalIndent(ps.Patterns, "", "  ")
	os.WriteFile(patternPath(), data, 0644)
}

// learn 记录一次澄清：原始输入的 keywords → 用户选择的 intent
func (ps *patternStore) learn(input, intent string) {
	ps.load()
	keywords := extractKeywords(input)

	for i := range ps.Patterns {
		if matchKeywords(ps.Patterns[i].Keywords, keywords) && ps.Patterns[i].Intent == intent {
			ps.Patterns[i].Count++
			ps.Patterns[i].LastSeen = time.Now().Unix()
			if ps.Patterns[i].Threshold == 0 {
				ps.Patterns[i].Threshold = 3
			}
			ps.save()
			return
		}
	}

	ps.Patterns = append(ps.Patterns, userPattern{
		Keywords:  keywords,
		Intent:    intent,
		Count:     1,
		Threshold: 3,
		LastSeen:  time.Now().Unix(),
	})
	ps.save()
}

// resolve 尝试自动推断意图。置信度 = count / threshold，>= 1.0 自动推断
func (ps *patternStore) resolve(input string) (intent string, confidence float64) {
	ps.load()
	keywords := extractKeywords(input)

	for _, p := range ps.Patterns {
		if matchKeywords(p.Keywords, keywords) {
			conf := float64(p.Count) / float64(p.Threshold)
			if p.LastSeen > 0 {
				daysSince := float64(time.Now().Unix()-p.LastSeen) / 86400
				if daysSince > 7 {
					conf *= 0.5 * (7.0 / daysSince)
				}
			}
			if conf >= 1.0 {
				return p.Intent, conf
			}
			return "", conf
		}
	}
	return "", 0
}

func extractKeywords(input string) []string {
	// 提取有意义的关键词：去掉常见停用词，只保留 3 字以上或特殊词
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "在": true, "有": true,
		"我": true, "他": true, "她": true, "它": true, "们": true,
		"这": true, "那": true, "和": true, "与": true, "就": true,
		"也": true, "都": true, "要": true, "会": true, "可": true,
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
	}

	words := strings.Fields(strings.ToLower(input))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ",.!?，。！？、；：\"'")
		if !stopWords[w] && len(w) >= 2 {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func matchKeywords(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// 检查是否有足够多的重叠关键词
	matchCount := 0
	for _, ka := range a {
		for _, kb := range b {
			if ka == kb {
				matchCount++
				break
			}
		}
	}
	minMatch := (len(a) + len(b)) / 3
	if minMatch < 1 {
		minMatch = 1
	}
	return matchCount >= minMatch
}

/* ─── clarify 工具注册 ──────────────────────────── */

func registerClarifyTools() {
	Register("clarify", "向用户提问以澄清模糊请求。支持多选和开放式问题。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"question"},
			"properties": map[string]interface{}{
				"question": stringParam("向用户提出的问题"),
				"choices": map[string]interface{}{
					"type":        "array",
					"description": "最多 4 个选项供用户选择",
					"items":       map[string]interface{}{"type": "string"},
					"maxItems":    4,
				},
				"_input": stringParam("用户原始输入，用于自动推断（内部使用）"),
			"format": stringParam("返回格式：plain（默认）或 structured"),
			},
		},
		clarifyHandler,
	)

	Register("clarify_learn", "记录一次用户澄清，用于模式学习。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"input", "intent"},
			"properties": map[string]interface{}{
				"input":  stringParam("用户原始输入"),
				"intent": stringParam("用户最终指定的意图"),
			},
		},
		clarifyLearnHandler,
	)
}

func clarifyHandler(args map[string]interface{}) *ToolResult {
	question, _ := args["question"].(string)
	if question == "" {
		return errorResult("question is required")
	}

	var choices []string
	if raw, ok := args["choices"].([]interface{}); ok {
		for _, c := range raw {
			if s, ok := c.(string); ok {
				choices = append(choices, s)
			}
		}
	}

	output := fmt.Sprintf("[需要澄清] %s", question)
	if len(choices) > 0 {
		output += "\n选项:\n"
		for i, c := range choices {
			output += fmt.Sprintf("  %d. %s\n", i+1, c)
		}
	}

	// 尝试自动推断（仅当插件上下文有 input 字段时）
	if input, ok := args["_input"].(string); ok && input != "" {
		if intent, conf := clarifyPatterns.resolve(input); conf >= 1.0 && intent != "" {
			return successResult(fmt.Sprintf("[自动推断] %s (置信度 %.0f%%)", intent, conf*100))
		}
	}

	// 结构化返回格式
	if format, ok := args["format"].(string); ok && format == "structured" {
		req := clarify.Request{
			NeedsClarify: true,
			Question:     output,
			Candidates:   choices,
			Confidence:   0.0,
		}
		data, _ := json.Marshal(req)
		return &ToolResult{Success: true, Output: string(data)}
	}

	return successResult(output)
}

func clarifyLearnHandler(args map[string]interface{}) *ToolResult {
	input, _ := args["input"].(string)
	intent, _ := args["intent"].(string)
	if input == "" || intent == "" {
		return errorResult("input and intent required")
	}

	clarifyPatterns.learn(input, intent)
	return successResult(fmt.Sprintf("已学习模式: %s → %s", input, intent))
}
