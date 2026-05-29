package tools

import (
	"beishan/internal/retrieval"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ─── Retrieval Trace ──────────────────────────── */

type RetrievalStage struct {
	Stage  string         `json:"stage"`  // "embedding" | "keyword_expand" | "score" | "graph_expand" | "ranking"
	Method string         `json:"method"` // 具体方法
	Input  string         `json:"input"`  // 输入摘要
	Output map[string]any `json:"output"` // 结构化输出
	Score  float64        `json:"score"`  // 分数（如有）
	Reason string         `json:"reason"` // 原因
}

type RetrievalTrace struct {
	Query  string           `json:"query"`
	Stages []RetrievalStage `json:"stages"`
}

func NewRetrievalTrace(query string) *RetrievalTrace {
	return &RetrievalTrace{Query: query}
}

func (t *RetrievalTrace) Add(stage RetrievalStage) {
	t.Stages = append(t.Stages, stage)
}

func (t *RetrievalTrace) Log() {
	if len(t.Stages) == 0 {
		return
	}
	fmt.Printf("[retrieval] query=%q stages=%d\n", t.Query, len(t.Stages))
	for _, s := range t.Stages {
		fmt.Printf("  stage=%s method=%s input=%q output=%+v score=%.2f reason=%q\n",
			s.Stage, s.Method, s.Input, s.Output, s.Score, s.Reason)
	}
}

// FormatTrace 将检索过程格式化为可读文本，追加到回答末尾。
func FormatTrace(trace *RetrievalTrace) string {
	if trace == nil || len(trace.Stages) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n---\n")
	sb.WriteString("*检索过程*\n\n")
	for _, s := range trace.Stages {
		stageName := s.Stage
		sb.WriteString(fmt.Sprintf("**%s**", stageName))
		if s.Method != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", s.Method))
		}
		sb.WriteString("\n")
		if s.Reason != "" {
			sb.WriteString(fmt.Sprintf("> %s\n", s.Reason))
		}
		if total, ok := s.Output["total"]; ok {
			if f, isFloat := total.(float64); isFloat {
				sb.WriteString(fmt.Sprintf("  命中: %d 条\n", int(f)))
			}
		}
		if intent, ok := s.Output["intent"]; ok {
			sb.WriteString(fmt.Sprintf("  分流: %s\n", intent))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func KnowledgeSearch(keyword, namespace string) *ToolResult {
	// 若关键词包含字段语法（tag: / type: / date:> 等），走结构化检索路径。
	q := retrieval.ParseQuery(keyword)
	if namespace != "" {
		q.Namespace = namespace
	}
	if q.HasFieldFilters() {
		scored := SearchWithQuery(q, 10)
		if len(scored) == 0 {
			return successResult("未找到匹配的知识条目。")
		}
		var lines []string
		for _, r := range scored {
			lines = append(lines, fmt.Sprintf("[%s] %s | %s", r.ID, r.Title, truncateStr(r.Summary, 120)))
		}
		return successResult(strings.Join(lines, "\n"))
	}
	// 普通关键词走全文检索路径
	results := searchMemoryFull(keyword, 5, nil, namespace)
	if len(results) == 0 {
		return successResult("未找到匹配的知识条目。")
	}
	var lines []string
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("[%s] %s | %s", r.EntryID, r.Title, truncateStr(r.Summary, 120)))
	}
	return successResult(strings.Join(lines, "\n"))
}

/* ─── ScoredEntry 加权检索结果 ────────────── */

type ScoredEntry struct {
	ID             string                              `json:"id"`
	Title          string                              `json:"title"`
	Summary        string                              `json:"summary"`
	Tags           []string                            `json:"tags"`
	SourceType     string                              `json:"source_type"`
	Score          int                                 `json:"score"`
	Contradictions []retrieval.ContradictionAnnotation `json:"contradictions,omitempty"`
}

// ContradictionAnnotation 矛盾标注（供 FormatForPrompt 渲染）
// 已迁移至 internal/retrieval/contract.go

// decisionKeywords 决策类标签关键词（结构化加权用）
var decisionKeywords = []string{"决策", "决定", "架构", "方案", "结论", "教训", "放弃", "最终"}

func SearchWithScore(query string, limit int, namespace string) []ScoredEntry {
	if limit <= 0 {
		limit = 3
	}

	all := loadAllKnowledge()
	var scored []ScoredEntry
	q := strings.ToLower(query)

	for _, entry := range all {
		if entry.Status != "" && entry.Status != "active" {
			continue
		}
		if !matchNamespace(entry, namespace) {
			continue
		}
		score := 0
		title := strings.ToLower(entry.Title)
		summary := strings.ToLower(entry.Summary)

		// 正向：query 是否在条目字段中（完全匹配字段子串）
		if strings.Contains(title, q) {
			score += 3
		} else if stringContainsAny(title, q) {
			score += 3
		}

		for _, tag := range entry.Tags {
			if strings.Contains(strings.ToLower(tag), q) || strings.Contains(q, strings.ToLower(tag)) {
				score += 2
				break
			}
		}

		if strings.Contains(summary, q) {
			score += 1
		} else if stringContainsAny(summary, q) {
			score += 1
		}

		// 块级内容匹配加分（BlockStorage 迁移后可用）。
		for _, block := range entry.BlockContents {
			blockLower := strings.ToLower(block)
			if strings.Contains(blockLower, q) || stringContainsAny(blockLower, q) {
				score += 2
				break
			}
		}

		if score > 0 {
			// ── 结构化加权（L1 层） ──────────────────
			structuralBoost, contradictions := computeStructuralBoost(entry)
			score += structuralBoost
			recordAccess(entry.ID)

			scored = append(scored, ScoredEntry{
				ID:             entry.ID,
				Title:          entry.Title,
				Summary:        entry.Summary,
				Tags:           entry.Tags,
				SourceType:     entry.SourceType,
				Score:          score,
				Contradictions: contradictions,
			})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		// 同分数时 memory 优先
		if scored[i].Score == scored[j].Score {
			im := scored[i].SourceType == "memory"
			jm := scored[j].SourceType == "memory"
			if im != jm {
				return im
			}
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

/* SearchWithQuery 使用 Query DSL 结构进行检索。*/
func SearchWithQuery(q *retrieval.Query, limit int) []ScoredEntry {
	if q == nil || q.IsEmpty() {
		return nil
	}
	keyword := ""
	if len(q.Keywords) > 0 {
		keyword = q.Keywords[0]
	}
	results := SearchWithScore(keyword, limit*3, q.Namespace)

	// 按 content_type 过滤
	if len(q.Types) > 0 {
		typeSet := make(map[string]bool)
		for _, t := range q.Types {
			typeSet[t] = true
		}
		var filtered []ScoredEntry
		for _, r := range results {
			entry := loadKnowledge(r.ID)
			if entry != nil && typeSet[entry.ContentType] {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// 按 tags 过滤（条目须包含 q.Tags 中至少一个标签）
	if len(q.Tags) > 0 {
		tagSet := make(map[string]bool)
		for _, t := range q.Tags {
			tagSet[strings.ToLower(t)] = true
		}
		var filtered []ScoredEntry
		for _, r := range results {
			entry := loadKnowledge(r.ID)
			if entry == nil {
				continue
			}
			for _, tag := range entry.Tags {
				if tagSet[strings.ToLower(tag)] {
					filtered = append(filtered, r)
					break
				}
			}
		}
		results = filtered
	}

	// 按 DateAfter 过滤（格式 YYYY-MM-DD 或 YYYY-MM）
	if q.DateAfter != "" {
		if cutoff := parseDateCutoff(q.DateAfter); cutoff > 0 {
			var filtered []ScoredEntry
			for _, r := range results {
				entry := loadKnowledge(r.ID)
				if entry != nil && entry.CreatedAt >= cutoff {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}
	}

	// 按 DateBefore 过滤
	if q.DateBefore != "" {
		if cutoff := parseDateCutoff(q.DateBefore); cutoff > 0 {
			var filtered []ScoredEntry
			for _, r := range results {
				entry := loadKnowledge(r.ID)
				if entry != nil && entry.CreatedAt <= cutoff {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}
	}

	// 按 Status 过滤（空 = 不过滤）
	if q.Status != "" {
		var filtered []ScoredEntry
		for _, r := range results {
			entry := loadKnowledge(r.ID)
			if entry == nil {
				continue
			}
			entryStatus := entry.Status
			if entryStatus == "" {
				entryStatus = "active"
			}
			if entryStatus == q.Status {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// parseDateCutoff 将日期字符串解析为 Unix 时间戳（支持 YYYY-MM-DD 和 YYYY-MM）
func parseDateCutoff(s string) int64 {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.Unix()
	}
	return 0
}

// computeStructuralBoost 计算结构化加权分数 + 矛盾标注。
// 加权规则：
//   - 决策类标签（决策/决定/架构/方案/结论/教训/放弃）: +1
//   - 有 TypedLinks: +1
//   - 经过事实核查（Verified）: +2（已验证的事实优先展示）
//   - 有 contradicts 链接: +2 + 收集矛盾标注
func computeStructuralBoost(entry *KnowledgeEntry) (int, []retrieval.ContradictionAnnotation) {
	boost := 0
	var contradictions []retrieval.ContradictionAnnotation

	// 决策类标签加权
	for _, tag := range entry.Tags {
		tagLower := strings.ToLower(tag)
		for _, kw := range decisionKeywords {
			if strings.Contains(tagLower, kw) {
				boost += 1
				break
			}
		}
	}

	// TypedLink 存在加权
	if len(entry.TypedLinks) > 0 {
		boost += 1
	}

	// 事实核查加权：已验证事实优先展示
	if entry.Verified {
		boost += 2
	}

	// 检索反馈加权：UtilityScore 影响排名，上限 ±2
	us := int(entry.UtilityScore * 2)
	if us > 2 {
		us = 2
	} else if us < -2 {
		us = -2
	}
	boost += us

	// 矛盾链接加权 + 收集标注
	for _, tl := range entry.TypedLinks {
		if tl.Type == LinkContradicts {
			boost += 2
			target := loadKnowledge(tl.TargetID)
			if target != nil {
				contradictions = append(contradictions, retrieval.ContradictionAnnotation{
					TargetTitle: target.Title,
					Reason:      tl.Reason,
				})
			}
		}
	}

	return boost, contradictions
}

// stringContainsAny 检查 query 的语义词是否出现在 target 中。
// 方向：将 query 按空格/标点切词，对每个词检查是否作为子串出现在 target 中。
//
// 中文专用滑动窗口：只对 CJK 词生成 ≥3 字符子串（因中文不依赖空格分词）。
// ASCII 词要求精确整词匹配（ASCII 已有空格分词，不需要子串拆分）。
// 这样避免 "xyzzy_nonexistent_12345" 的 "ent"/"one" 等 3 字符片段误命中无关条目。
func stringContainsAny(target, query string) bool {
	qTokens := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '　' || r == '，' || r == '。' || r == '、' ||
			r == '：' || r == '（' || r == '）' || r == '—' || r == '|'
	})
	targetLower := strings.ToLower(target)
	for _, token := range qTokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		// 整个 token 出现在 target 中
		if strings.Contains(targetLower, lower) {
			return true
		}
		// 滑动窗口仅适用于含 CJK 字符的词（避免 ASCII 片段误命中）
		if isCJKToken(lower) {
			runes := []rune(lower)
			if len(runes) > 3 {
				for i := 0; i < len(runes)-2; i++ {
					for j := i + 3; j <= len(runes) && j-i < 6; j++ {
						seg := string(runes[i:j])
						if strings.Contains(targetLower, seg) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// isCJKToken 判断字符串是否主要包含 CJK（中日韩）字符。
// 阈值：CJK 字符占比 > 50% 视为 CJK 词，适用滑动窗口分词。
func isCJKToken(s string) bool {
	runes := []rune(s)
	if len(runes) == 0 {
		return false
	}
	cjk := 0
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF || // CJK 统一汉字
			r >= 0x3400 && r <= 0x4DBF || // CJK 扩展 A
			r >= 0x20000 && r <= 0x2A6DF || // CJK 扩展 B
			r >= 0x3000 && r <= 0x303F { // CJK 符号和标点
			cjk++
		}
	}
	return cjk*2 > len(runes)
}

// FormatForPrompt 将检索结果格式化为 <background> 文本。
// 每条两行：标题+标签一行，summary 一行。矛盾条目追加 ⚠️ 标注。
func FormatForPrompt(entries []ScoredEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString("<background>\n")
	for i, e := range entries {
		tags := strings.Join(e.Tags, ", ")
		if tags == "" {
			tags = e.SourceType
		}
		summary := e.Summary
		runes := []rune(summary)
		if len(runes) > 100 {
			summary = string(runes[:100]) + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s（%s）\n", i+1, e.Title, tags))
		sb.WriteString(fmt.Sprintf("   %s\n", summary))
		// 矛盾标注
		for _, c := range e.Contradictions {
			reason := c.Reason
			if reason == "" {
				reason = "存在矛盾观点"
			}
			sb.WriteString(fmt.Sprintf("   ⚠️ 矛盾: \"%s\" — %s\n", c.TargetTitle, reason))
		}
	}
	sb.WriteString("</background>")
	return sb.String()
}

// FormatForPromptFull 已迁移至 internal/retrieval/contract.go
// 使用 retrieval.FormatForPromptFull 代替

const maxGraphExpand = 5 // 图扩展最多返回条数，防爆炸

// followSupersedesChain 沿 supersedes 链追溯历史版本（最多 maxHops 跳）。
// 纯确定性遍历，不调 LLM。
func followSupersedesChain(startID string, seen map[string]bool, maxHops int) []*KnowledgeEntry {
	var chain []*KnowledgeEntry
	currentID := startID
	for hop := 0; hop < maxHops; hop++ {
		entry := loadKnowledge(currentID)
		if entry == nil {
			break
		}
		foundNext := false
		for _, tl := range entry.TypedLinks {
			if tl.Type == LinkSupersedes && !seen[tl.TargetID] {
				target := loadKnowledge(tl.TargetID)
				if target != nil {
					seen[tl.TargetID] = true
					chain = append(chain, target)
					currentID = tl.TargetID
					foundNext = true
					break
				}
			}
		}
		if !foundNext {
			break
		}
	}
	return chain
}

// entryToResult 将 KnowledgeEntry 转换为 retrieval.RetrievalResult
func entryToResult(entry *KnowledgeEntry, source retrieval.RetrievalKind, score int, linkType LinkType, linkFrom string, contradictions []retrieval.ContradictionAnnotation) retrieval.RetrievalResult {
	return retrieval.RetrievalResult{
		Source:         source,
		EntryID:        entry.ID,
		Title:          entry.Title,
		Summary:        entry.Summary,
		Tags:           entry.Tags,
		SourceType:     entry.SourceType,
		Score:          score,
		LinkType:       string(linkType),
		LinkFrom:       linkFrom,
		Contradictions: contradictions,
	}
}

// ─── 检索缓存 (LRU) ────────────────────────────────────────

const (
	searchCacheSize = 50              // 最大缓存条目数
	searchCacheTTL  = 2 * time.Minute // 缓存过期时间
)

type searchCacheEntry struct {
	results   []retrieval.RetrievalResult
	createdAt time.Time
}

var (
	searchCache   = make(map[string]searchCacheEntry)
	searchCacheMu sync.RWMutex
)

func searchCacheKey(query string, limit int, namespace string) string {
	return fmt.Sprintf("%s|%d|%s", query, limit, namespace)
}

func searchCacheGet(key string) ([]retrieval.RetrievalResult, bool) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	e, ok := searchCache[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.createdAt) > searchCacheTTL {
		delete(searchCache, key)
		return nil, false
	}
	return e.results, true
}

func searchCacheSet(key string, results []retrieval.RetrievalResult) {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	// 超限清理：删除最旧的一半
	if len(searchCache) >= searchCacheSize {
		oldest := time.Now()
		var oldestKey string
		for k, v := range searchCache {
			if v.createdAt.Before(oldest) {
				oldest = v.createdAt
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(searchCache, oldestKey)
		}
	}
	searchCache[key] = searchCacheEntry{results: results, createdAt: time.Now()}
}

// searchMemoryFull 内部统一检索入口，返回 []retrieval.RetrievalResult。
// 检索阶梯（宪法优先）：
//
//	L0:   确定性关键词评分（不调 LLM）
//	L0.5: 图扩展（沿 Links/TypedLinks 遍历）
//	L2:   embedding 语义回退（仅当 L0 结果不足时）
func searchMemoryFull(query string, limit int, trace *RetrievalTrace, namespace string) []retrieval.RetrievalResult {
	// 缓存命中检查（跳过 trace 模式，避免缓存污染调试数据）
	if trace == nil {
		key := searchCacheKey(query, limit, namespace)
		if cached, ok := searchCacheGet(key); ok {
			return cached
		}
	}
	if limit <= 0 {
		limit = 3
	}

	// ── L0: 确定性关键词评分 ──────────────────────
	directScored := SearchWithScore(query, limit*2, namespace)
	var direct []retrieval.RetrievalResult
	for _, se := range directScored {
		entry := loadKnowledge(se.ID)
		if entry == nil {
			continue
		}
		direct = append(direct, entryToResult(entry, retrieval.KindDirect, se.Score, "", "", se.Contradictions))
	}
	if trace != nil {
		trace.Add(RetrievalStage{
			Stage:  "L0_keyword",
			Method: "deterministic_score",
			Input:  query,
			Output: map[string]any{"candidates": len(direct)},
			Reason: "deterministic first, no LLM",
		})
	}

	// ── L0.5: 图扩展（增强版：supersedes 链式跟踪 + 上限防爆）──
	seen := make(map[string]bool)
	for _, r := range direct {
		seen[r.EntryID] = true
	}
	var expanded []retrieval.RetrievalResult
	expanded = append(expanded, direct...)
	var graphLinked int
	for _, r := range direct {
		if graphLinked >= maxGraphExpand {
			break
		}
		linked, linkTypes := loadLinkedEntries(r.EntryID)
		for _, le := range linked {
			if seen[le.ID] || graphLinked >= maxGraphExpand {
				continue
			}
			seen[le.ID] = true
			graphLinked++

			lt := linkTypes[le.ID]
			weight := linkTypeWeight(lt)
			_, contradictions := computeStructuralBoost(le)
			expanded = append(expanded, entryToResult(le, retrieval.KindLinked, int(float64(r.Score)*weight), lt, r.EntryID, contradictions))

			// supersedes 链式跟踪：沿 supersedes 链往回追溯（最多 3 跳）
			if lt == LinkSupersedes && graphLinked < maxGraphExpand {
				chain := followSupersedesChain(le.ID, seen, 3)
				for _, chainEntry := range chain {
					if graphLinked >= maxGraphExpand {
						break
					}
					graphLinked++
					_, chainContradictions := computeStructuralBoost(chainEntry)
					expanded = append(expanded, entryToResult(chainEntry, retrieval.KindLinked, int(float64(r.Score)*0.4), LinkSupersedes, le.ID, chainContradictions))
				}
			}
		}
	}
	if trace != nil && graphLinked > 0 {
		trace.Add(RetrievalStage{
			Stage:  "L0.5_graph",
			Method: "link_traversal",
			Input:  fmt.Sprintf("direct=%d", len(direct)),
			Output: map[string]any{"linked": graphLinked, "total": len(expanded)},
			Reason: "typed link traversal",
		})
	}

	// ── L1: embedding 语义检索（与 L0 并行，非回退）──
	if embeddingEnabled() {
		if emb, ok := tryEmbedding(query); ok {
			embeddingResults := searchByEmbedding(emb, limit*2, namespace)
			for _, se := range embeddingResults {
				if seen[se.ID] {
					// 合并语义分数：取 max(关键词, 语义) 提升混合命中
					for i, r := range expanded {
						if r.EntryID == se.ID && se.Score > r.Score {
							expanded[i].Score = se.Score
							break
						}
					}
					continue
				}
				seen[se.ID] = true
				entry := loadKnowledge(se.ID)
				if entry == nil {
					continue
				}
				_, contradictions := computeStructuralBoost(entry)
				expanded = append(expanded, entryToResult(entry, retrieval.KindSemantic, se.Score, "", "", contradictions))
			}
			if trace != nil {
				trace.Add(RetrievalStage{
					Stage:  "L1_embedding",
					Method: "cosine_similarity",
					Input:  query,
					Output: map[string]any{"semantic_candidates": len(embeddingResults), "total": len(expanded)},
					Reason: "parallel semantic search, hybrid scoring",
				})
			}
		}
	}

	// 分数归一化：关键词分数 (1-15) 与语义分数 (40-100) 尺度不同。
	// 将非语义来源的分数乘以 6 映射到 0-90 范围，使排序公平。
	if embeddingEnabled() {
		for i, r := range expanded {
			if r.Source != retrieval.KindSemantic && r.Score < 40 {
				expanded[i].Score = r.Score * 6
			}
		}
	}
	// ── 排序 + 低分过滤 + 截断 ─────────────────────
	sort.Slice(expanded, func(i, j int) bool {
		if expanded[i].Score == expanded[j].Score {
			im := expanded[i].SourceType == "memory"
			jm := expanded[j].SourceType == "memory"
			if im != jm {
				return im
			}
		}
		return expanded[i].Score > expanded[j].Score
	})
	// 低分噪音过滤：丢弃 score < 5 的结果，但至少保留 1 条
	const minScore = 5
	if len(expanded) > 1 {
		cutoff := len(expanded)
		for i, r := range expanded {
			if r.Score < minScore {
				cutoff = i
				break
			}
		}
		if cutoff == 0 {
			cutoff = 1 // 至少保留 1 条最高分结果
		}
		expanded = expanded[:cutoff]
	}
	if len(expanded) > limit {
		expanded = expanded[:limit]
	}
	if trace != nil {
		trace.Add(RetrievalStage{
			Stage:  "ranking",
			Method: "sort+limit",
			Input:  fmt.Sprintf("candidates=%d", len(expanded)),
			Output: map[string]any{"final": len(expanded)},
			Reason: "score desc, memory priority",
		})
	}
	// 写入缓存（非 trace 模式）
	if trace == nil {
		searchCacheSet(searchCacheKey(query, limit, namespace), expanded)
	}
	return expanded
}

// linkTypeWeight 返回链接类型的权重系数
func linkTypeWeight(lt LinkType) float64 {
	switch lt {
	case LinkContradicts:
		return 0.8 // 矛盾优先展示（灵感来源）
	case LinkSupersedes:
		return 0.7 // 演进重要
	case LinkSupports:
		return 0.3 // 佐证降权
	default:
		return 0.5 // related 默认
	}
}

// SearchMemory 统一记忆检索入口（对外接口，保持 []ScoredEntry 兼容）。
// 内部委托 searchMemoryFull 执行确定性优先的检索阶梯。
func SearchMemory(query string, limit int, trace *RetrievalTrace) []ScoredEntry {
	results := searchMemoryFull(query, limit, trace, "")
	var out []ScoredEntry
	for _, r := range results {
		out = append(out, ScoredEntry{
			ID:             r.EntryID,
			Title:          r.Title,
			Summary:        r.Summary,
			Tags:           r.Tags,
			SourceType:     r.SourceType,
			Score:          r.Score,
			Contradictions: r.Contradictions,
		})
	}
	return out
}

/* ─── Phase 3: 多跳检索导出接口 ──────────────── */

// SearchMemoryFull 检索入口（导出版），返回 []retrieval.RetrievalResult 供多跳使用。
func SearchMemoryFull(query string, limit int, trace *RetrievalTrace) []retrieval.RetrievalResult {
	return searchMemoryFull(query, limit, trace, "")
}

// NeedsSecondHop 判断是否需要第二轮检索（纯代码，零 LLM）。
// 三选一触发：稀疏结果 / 矛盾发现 / 演进链。
func NeedsSecondHop(results []retrieval.RetrievalResult) (bool, string) {
	if len(results) == 0 {
		return false, ""
	}
	// 条件 1：结果稀疏（只有 1 条直接命中）
	directCount := 0
	for _, r := range results {
		if r.Source == retrieval.KindDirect {
			directCount++
		}
	}
	if directCount == 1 {
		return true, "sparse"
	}
	// 条件 2：发现矛盾
	for _, r := range results {
		if r.LinkType == string(LinkContradicts) {
			return true, "contradiction"
		}
	}
	// 条件 3：发现演进链
	for _, r := range results {
		if r.LinkType == string(LinkSupersedes) {
			return true, "evolution"
		}
	}
	return false, ""
}

// DeriveSecondQuery 从 round 1 结果中提取第二轮查询（纯代码，零 LLM）。
func DeriveSecondQuery(results []retrieval.RetrievalResult, reason string) string {
	switch reason {
	case "sparse":
		// 用 round 1 结果的 tags 做展开
		var tags []string
		for _, r := range results {
			tags = append(tags, r.Tags...)
		}
		return strings.Join(dedupStrings(tags, 3), " ")
	case "contradiction":
		// 用矛盾双方的标题关键词
		var keywords []string
		for _, r := range results {
			if r.LinkType == string(LinkContradicts) {
				keywords = append(keywords, r.Title)
			}
		}
		return strings.Join(keywords, " ")
	case "evolution":
		// 用被替代条目的标题
		for _, r := range results {
			if r.LinkType == string(LinkSupersedes) {
				return r.Title
			}
		}
	}
	return ""
}

// MergeResults 合并两轮检索结果，去重，总上限 5 条。
func MergeResults(a, b []retrieval.RetrievalResult) []retrieval.RetrievalResult {
	seen := make(map[string]bool)
	var merged []retrieval.RetrievalResult
	for _, r := range a {
		if !seen[r.EntryID] {
			seen[r.EntryID] = true
			merged = append(merged, r)
		}
	}
	for _, r := range b {
		if !seen[r.EntryID] {
			seen[r.EntryID] = true
			merged = append(merged, r)
		}
	}
	// 按分数排序
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	if len(merged) > maxGraphExpand {
		merged = merged[:maxGraphExpand]
	}
	return merged
}

// dedupStrings 去重取前 n 个
func dedupStrings(ss []string, n int) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
			if len(result) >= n {
				break
			}
		}
	}
	return result
}
