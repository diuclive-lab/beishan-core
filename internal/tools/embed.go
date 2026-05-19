package tools

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	vectorDim      = 512
	semanticCutoff = 0.25
)

type Embedding struct {
	ID        string    `json:"id"`
	Vector    []float64 `json:"vector"`
	Model     string    `json:"model"`
	CreatedAt int64     `json:"created_at"`
}

var embedMu sync.RWMutex

func embedPath(id string) string {
	return filepath.Join(knowledgeDir, id+".embed.json")
}

func loadEmbedding(id string) *Embedding {
	data, err := os.ReadFile(embedPath(id))
	if err != nil {
		return nil
	}
	var emb Embedding
	json.Unmarshal(data, &emb)
	return &emb
}

func saveEmbedding(emb *Embedding) {
	data, _ := json.MarshalIndent(emb, "", "  ")
	os.WriteFile(embedPath(emb.ID), data, 0644)
}

func textToVector(text string) []float64 {
	vec := make([]float64, vectorDim)
	text = strings.ToLower(text)

	var asciiBuf []rune
	for _, r := range text {
		if r > 127 {
			if len(asciiBuf) > 0 {
				hashToken(string(asciiBuf), vec)
				asciiBuf = nil
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				hashToken(string(r), vec)
			}
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			asciiBuf = append(asciiBuf, r)
		} else {
			if len(asciiBuf) > 0 {
				hashToken(string(asciiBuf), vec)
				asciiBuf = nil
			}
		}
	}
	if len(asciiBuf) > 0 {
		hashToken(string(asciiBuf), vec)
	}

	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

func hashToken(s string, vec []float64) {
	h := fnv.New32a()
	h.Write([]byte(s))
	idx := int(h.Sum32()) % len(vec)
	vec[idx]++

	runes := []rune(s)
	if len(runes) <= 4 && len(runes) >= 2 {
		for n := 2; n <= len(runes); n++ {
			for i := 0; i <= len(runes)-n; i++ {
				sub := string(runes[i : i+n])
				h := fnv.New32a()
				h.Write([]byte(sub))
				idx := int(h.Sum32()) % len(vec)
				vec[idx]++
			}
		}
	}
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func buildEmbedText(entry *KnowledgeEntry) string {
	var parts []string
	if entry.Title != "" {
		parts = append(parts, entry.Title)
	}
	if entry.Summary != "" {
		parts = append(parts, entry.Summary)
	}
	if len(entry.Tags) > 0 {
		parts = append(parts, strings.Join(entry.Tags, " "))
	}
	if len(entry.Topics) > 0 {
		parts = append(parts, strings.Join(entry.Topics, " "))
	}
	if entry.Content != "" {
		maxContent := 3000
		if len(entry.Content) > maxContent {
			parts = append(parts, entry.Content[:maxContent])
		} else {
			parts = append(parts, entry.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func KnowledgeEmbed(id string) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}
	knowledgeMu.RLock()
	entry := loadKnowledge(id)
	knowledgeMu.RUnlock()
	if entry == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}
	text := buildEmbedText(entry)
	vector := textToVector(text)
	embedMu.Lock()
	saveEmbedding(&Embedding{
		ID:        id,
		Vector:    vector,
		Model:     "bow-v1",
		CreatedAt: time.Now().Unix(),
	})
	embedMu.Unlock()
	return successResult(fmt.Sprintf(`{"id":"%s","model":"bow-v1","dimensions":%d}`, id, len(vector)))
}

func KnowledgeEmbedAll() *ToolResult {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"count":0,"message":"暂无知识条目可 embed"}`)
	}
	var succeeded, failed int
	var results []map[string]interface{}
	for _, entry := range all {
		r := KnowledgeEmbed(entry.ID)
		res := map[string]interface{}{"id": entry.ID, "title": entry.Title}
		if r.Success {
			succeeded++
			res["status"] = "ok"
		} else {
			failed++
			res["status"] = "error"
			res["error"] = r.Output
		}
		results = append(results, res)
	}
	result := map[string]interface{}{
		"total": len(all), "succeeded": succeeded, "failed": failed,
		"results": results,
		"message": fmt.Sprintf("Embed 完成: %d 成功, %d 失败", succeeded, failed),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

type SemanticMatch struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Score   float64  `json:"score"`
	Source  string   `json:"source_type"`
	Tags    []string `json:"tags"`
}

func KnowledgeSemanticSearch(query string, limit int) *ToolResult {
	if query == "" {
		return errorResult("query 不能为空")
	}
	if limit <= 0 {
		limit = 10
	}
	queryVec := textToVector(query)
	all := loadAllKnowledge()
	var matches []SemanticMatch
	for _, entry := range all {
		embedMu.RLock()
		emb := loadEmbedding(entry.ID)
		embedMu.RUnlock()
		if emb == nil {
			continue
		}
		score := cosineSimilarity(queryVec, emb.Vector)
		if score >= semanticCutoff {
			matches = append(matches, SemanticMatch{
				ID: entry.ID, Title: entry.Title, Summary: entry.Summary,
				Score: score, Source: entry.SourceType, Tags: entry.Tags,
			})
		}
	}
	if len(matches) == 0 {
		return successResult(fmt.Sprintf(`{"query":"%s","matches":[],"count":0,"message":"未找到语义匹配的条目"}`, query))
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	result := map[string]interface{}{
		"query": query, "matches": matches, "count": len(matches),
		"message": fmt.Sprintf("找到 %d 个语义匹配条目（Bow 向量，阈值 %.2f）", len(matches), semanticCutoff),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── KnowledgeRetrieve 硬化检索 ────────────────────

   确定性代码检索，不走 LLM。
   1. BOW 向量相似度
   2. knowledge_type 权重加成
   3. 返回格式化文本，直接注入 system prompt
*/

// typeWeight 定义各认知类型的检索权重。
// 权重越高，排序时越靠前。
var typeWeight = map[string]float64{
	"Principle":   1.3, // 稳定原则，优先参考
	"ADR":         1.2, // 架构决策，重要参考
	"Lesson":      1.1, // 经验总结，有参考价值
	"AntiPattern": 1.1, // 禁止行为，有参考价值
	"Hotspot":     1.0, // 代码风险点
	"Telemetry":   0.9, // 指标反馈，优先级略低
	"":            1.0, // 未分类，默认权重
}

// RetrieveMatch 是硬化检索的返回结构。
type RetrieveMatch struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Type    string   `json:"knowledge_type"`
	Score   float64  `json:"score"`
	Tags    []string `json:"tags"`
}

// KnowledgeRetrieve 硬化检索：BOW 向量 + 类型权重。
// 返回排序后的匹配列表，用于注入 system prompt。
func KnowledgeRetrieve(query string, limit int) []RetrieveMatch {
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}

	queryVec := textToVector(query)
	all := loadAllKnowledge()

	var matches []RetrieveMatch
	for _, entry := range all {
		embedMu.RLock()
		emb := loadEmbedding(entry.ID)
		embedMu.RUnlock()
		if emb == nil {
			continue
		}

		baseScore := cosineSimilarity(queryVec, emb.Vector)
		if baseScore < 0.15 {
			continue
		}

		// 类型权重加成
		weight := typeWeight[entry.KnowledgeType]
		adjustedScore := baseScore * weight

		matches = append(matches, RetrieveMatch{
			ID:      entry.ID,
			Title:   entry.Title,
			Summary: entry.Summary,
			Type:    entry.KnowledgeType,
			Score:   adjustedScore,
			Tags:    entry.Tags,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// FormatKnowledgeContext 将检索结果格式化为 system prompt 注入文本。
// 硬化输出格式，不依赖 LLM 生成。
func FormatKnowledgeContext(matches []RetrieveMatch) string {
	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 相关知识\n")
	for i, m := range matches {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, m.Type, m.Title))
		if m.Summary != "" {
			// 限制摘要长度，避免 context 过长
			summary := m.Summary
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf(" — %s", summary))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func registerEmbedTools() {
	Register("knowledge_embed", "生成并保存指定知识条目的词袋向量嵌入（本地语义搜索）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": stringParam("知识条目 ID"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeEmbed(strArg(args, "id"))
		},
	)
	Register("knowledge_embed_all", "为所有知识条目批量生成向量嵌入。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeEmbedAll()
		},
	)
	Register("knowledge_semantic_search", "语义搜索知识条目（本地词袋向量 + 余弦相似度，无需外部 API）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]interface{}{
				"query": stringParam("自然语言搜索查询"),
				"limit": intParam("最大返回数，默认 10"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			limit, _ := args["limit"].(float64)
			return KnowledgeSemanticSearch(strArg(args, "query"), int(limit))
		},
	)
}
