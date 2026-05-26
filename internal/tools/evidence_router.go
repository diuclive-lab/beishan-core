// Package tools provides tool registration, validation, and schema management.
//
// EvidenceRouter performs evidence-based routing using keyword/prefix matching
// with EWMA-adaptive keyword weights. Designed as a fast-path pre-router
// before the LLM-based Router, not a replacement.
//
// Absorbed from FangLab internal/tools/evidence_router.go (2026-05-26).
package tools

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// RouteRule defines a routing rule with evidence-based scoring.
type RouteRule struct {
	Name      string   // route identifier
	Tool      string   // target tool name
	MsgType   string   // message type to set (e.g. "knowledge_search")
	Prefixes  []string // fast-path prefix matches
	Keywords  []string // positive evidence keywords
	Negatives []string // negative evidence keywords
	Threshold float64  // minimum score to match (0.0 = any match)
	Priority  int      // higher priority wins on tie
}

// RouteEvidence records one piece of routing evidence.
type RouteEvidence struct {
	Kind  string  // "prefix", "keyword", "negative"
	Value string  // the matched text
	Score float64 // contribution to total score
}

// RouteResult is the output of evidence-based routing.
type RouteResult struct {
	Tool       string          // selected tool (plugin name)
	MsgType    string          // message type to set
	Confidence float64         // total score
	Evidence   []RouteEvidence // what contributed to the decision
}

type candidate struct {
	tool       string
	msgType    string
	confidence float64
	evidence   []RouteEvidence
}

const ewmaAlpha = 0.3

// EvidenceRouter performs evidence-based routing using a registry of rules.
// Supports Top-N candidate retrieval and EWMA-based keyword weight updates.
// Does NOT replace the LLM-based Router — it's a fast-path pre-router for
// high-confidence deterministic patterns.
type EvidenceRouter struct {
	mu             sync.RWMutex
	rules          []RouteRule
	prefix         *RadixTree
	keywordWeights map[string]float64 // EWMA-smoothed keyword success rates
	history        []RouteOutcome    // recent routing outcomes
}

// RouteOutcome records a single routing decision and its result.
type RouteOutcome struct {
	Input    string   `json:"input"`
	Selected string   `json:"selected"`
	Success  bool     `json:"success"`
	TopCands []string `json:"top_cands,omitempty"`
}

// NewEvidenceRouter creates a router with the given rules.
func NewEvidenceRouter(rules []RouteRule) *EvidenceRouter {
	r := &EvidenceRouter{
		rules:          rules,
		prefix:         NewRadixTree(),
		keywordWeights: make(map[string]float64),
	}
	for i, rule := range rules {
		for _, p := range rule.Prefixes {
			r.prefix.Insert(p, i)
		}
		for _, kw := range rule.Keywords {
			if _, exists := r.keywordWeights[kw]; !exists {
				r.keywordWeights[kw] = 1.0
			}
		}
	}
	return r
}

// Route evaluates input against all rules and returns the best match.
func (r *EvidenceRouter) Route(input string) *RouteResult {
	cands := r.RouteTopN(input, 1)
	if len(cands) == 0 {
		return nil
	}
	return cands[0]
}

// RouteTopN evaluates input and returns the top N candidates above threshold.
func (r *EvidenceRouter) RouteTopN(input string, n int) []*RouteResult {
	if r == nil || input == "" || n <= 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(strings.TrimSpace(input))
	var candidates []candidate

	for _, rule := range r.rules {
		score := 0.0
		var evidence []RouteEvidence

		for _, p := range rule.Prefixes {
			if strings.HasPrefix(lower, p) {
				score += 0.4
				evidence = append(evidence, RouteEvidence{Kind: "prefix", Value: p, Score: 0.4})
				break
			}
		}

		for _, kw := range rule.Keywords {
			if strings.Contains(lower, kw) {
				weight := r.keywordWeights[kw]
				score += 0.3 * weight
				evidence = append(evidence, RouteEvidence{Kind: "keyword", Value: kw, Score: 0.3 * weight})
			}
		}

		for _, neg := range rule.Negatives {
			if strings.Contains(lower, neg) {
				score -= 0.3
				evidence = append(evidence, RouteEvidence{Kind: "negative", Value: neg, Score: -0.3})
			}
		}

		if score < rule.Threshold {
			continue
		}

		candidates = append(candidates, candidate{
			tool:       rule.Tool,
			confidence: score,
			evidence:   evidence,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].confidence-candidates[j].confidence) > 0.001 {
			return candidates[i].confidence > candidates[j].confidence
		}
		return rulePriority(r, candidates[i].tool) > rulePriority(r, candidates[j].tool)
	})

	if len(candidates) > n {
		candidates = candidates[:n]
	}

	result := make([]*RouteResult, len(candidates))
	for i, c := range candidates {
		result[i] = &RouteResult{
			Tool:       c.tool,
			Confidence: c.confidence,
			Evidence:   c.evidence,
		}
	}
	return result
}

// RecordOutcome records a routing outcome and updates keyword EWMA weights.
func (r *EvidenceRouter) RecordOutcome(input string, selected string, success bool) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	lower := strings.ToLower(strings.TrimSpace(input))
	observation := 0.0
	if success {
		observation = 1.0
	}

	for _, rule := range r.rules {
		if rule.Tool != selected {
			continue
		}
		for _, kw := range rule.Keywords {
			if strings.Contains(lower, kw) {
				old := r.keywordWeights[kw]
				r.keywordWeights[kw] = ewmaAlpha*observation + (1-ewmaAlpha)*old
			}
		}
	}

	r.history = append(r.history, RouteOutcome{
		Input:    truncateString(input, 80),
		Selected: selected,
		Success:  success,
	})
	if len(r.history) > 1000 {
		r.history = r.history[len(r.history)-500:]
	}
}

// KeywordWeight returns the current EWMA weight for a keyword.
func (r *EvidenceRouter) KeywordWeight(keyword string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.keywordWeights[keyword]
}

// KeywordWeights returns a copy of all keyword weights.
func (r *EvidenceRouter) KeywordWeights() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]float64, len(r.keywordWeights))
	for k, v := range r.keywordWeights {
		out[k] = v
	}
	return out
}

// History returns recent routing outcomes.
func (r *EvidenceRouter) History() []RouteOutcome {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RouteOutcome, len(r.history))
	copy(out, r.history)
	return out
}

// RouteHistoryStats aggregates routing history statistics.
type RouteHistoryStats struct {
	Total        int                `json:"total"`
	Successes    int                `json:"successes"`
	Failures     int                `json:"failures"`
	Accuracy     float64            `json:"accuracy"`
	ByTool       map[string]int    `json:"by_tool"`
	ToolAccuracy map[string]float64 `json:"tool_accuracy"`
}

// Stats returns aggregate statistics from routing history.
func (r *EvidenceRouter) Stats() *RouteHistoryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s := &RouteHistoryStats{
		Total:        len(r.history),
		ByTool:       make(map[string]int),
		ToolAccuracy: make(map[string]float64),
	}
	toolSuccess := make(map[string]int)

	for _, h := range r.history {
		s.ByTool[h.Selected]++
		if h.Success {
			s.Successes++
			toolSuccess[h.Selected]++
		} else {
			s.Failures++
		}
	}
	if s.Total > 0 {
		s.Accuracy = float64(s.Successes) / float64(s.Total)
	}
	for tool, total := range s.ByTool {
		if total > 0 {
			s.ToolAccuracy[tool] = float64(toolSuccess[tool]) / float64(total)
		}
	}
	return s
}

func rulePriority(r *EvidenceRouter, tool string) int {
	for _, rule := range r.rules {
		if rule.Tool == tool {
			return rule.Priority
		}
	}
	return 0
}

func truncateString(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// Rules returns the registered rules (for inspection).
func (r *EvidenceRouter) Rules() []RouteRule {
	if r == nil {
		return nil
	}
	return r.rules
}

// SortRulesByPriority sorts rules by priority (highest first).
func SortRulesByPriority(rules []RouteRule) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})
}

// DefaultRoutingRules returns the default set of pre-routing rules
// matching beishan-core's common intent patterns.
func DefaultRoutingRules() []RouteRule {
	return []RouteRule{
		{
			Name:      "desktop_screen",
			Tool:      "memory_plugin",
			MsgType:   "desktop_actuator",
			Keywords:  []string{"看桌面", "屏幕", "截屏", "截图", "屏幕上"},
			Negatives: []string{"文件", "目录", "md", "有哪些"},
			Threshold: 0.3,
			Priority:  1,
		},
		{
			Name:      "desktop_files",
			Tool:      "terminal_plugin",
			MsgType:   "terminal_exec",
			Keywords:  []string{"桌面文件", "桌面目录", "桌面上", "桌面.*文件", "桌面上都有"},
			Negatives: []string{"屏幕", "截图", "窗口"},
			Threshold: 0.3,
			Priority:  2,
		},
		{
			Name:      "knowledge_search",
			Tool:      "memory_plugin",
			MsgType:   "knowledge_search",
			Keywords:  []string{"搜索知识库", "查知识", "我的笔记", "知识.*搜索"},
			Threshold: 0.3,
			Priority:  3,
		},
		{
			Name:      "web_search",
			Tool:      "search_plugin",
			MsgType:   "web_search",
			Keywords:  []string{"搜一下", "帮我查", "查查", "搜索", "查找资料"},
			Negatives: []string{"知识库", "笔记", "记忆"},
			Threshold: 0.3,
			Priority:  4,
		},
		{
			Name:      "create_workflow",
			Tool:      "skill_factory_plugin",
			MsgType:   "skill_create",
			Keywords:  []string{"创建工作流", "新建工作流", "生成工作流"},
			Threshold: 0.3,
			Priority:  5,
		},
		{
			Name:      "todo_add",
			Tool:      "todo_plugin",
			MsgType:   "todo_add",
			Keywords:  []string{"添加待办", "新建待办", "新增待办"},
			Threshold: 0.3,
			Priority:  6,
		},
		{
			Name:      "todo_list",
			Tool:      "todo_plugin",
			MsgType:   "todo_list",
			Keywords:  []string{"查看待办", "待办列表", "列出待办", "我的待办"},
			Threshold: 0.3,
			Priority:  7,
		},
		{
			Name:      "stock_query",
			Tool:      "memory_plugin",
			MsgType:   "stock_multi_quote",
			Keywords:  []string{"股价", "行情", "股票"},
			Threshold: 0.3,
			Priority:  8,
		},
		{
			Name:      "chat_greeting",
			Tool:      "think_plugin",
			MsgType:   "chat",
			Keywords:  []string{"聊聊", "聊天", "你好", "在吗"},
			Threshold: 0.2,
			Priority:  9,
		},
	}
}

func MatchIntent(input string) (tool string, confidence float64) {
	r := NewEvidenceRouter(DefaultRoutingRules())
	result := r.Route(input)
	if result == nil {
		return "", 0
	}
	return result.Tool, result.Confidence
}
