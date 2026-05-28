package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"beishan/internal/retrieval"
)

/* ─── TypedLink 有类型的知识关联 ──────────────── */

// LinkType 链接类型（代码定义的枚举，不是 LLM 决策）
type LinkType string

const (
	LinkRelated     LinkType = "related"     // 相关（现有 autoLink）
	LinkContradicts LinkType = "contradicts"  // 矛盾
	LinkSupersedes  LinkType = "supersedes"   // 替代/演进
	LinkSupports    LinkType = "supports"     // 支持/佐证
)

// TypedLink 有类型的关联链接
type TypedLink struct {
	TargetID string   `json:"target_id"`
	Type     LinkType `json:"type"`
	Reason   string   `json:"reason"` // 为什么建这个链接（可审计）
}

/* ─── KnowledgeEntry 统一知识条目 ──────────────── */

type KnowledgeEntry struct {
	ID         string   `json:"id"`
	SourceType string   `json:"source_type"` // chat|article|idea|web|file|note|codex|claude_memory
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
	Topics     []string `json:"topics"`
	Tasks      []string `json:"tasks"` // 提取的任务/行动项
	CreatedAt  int64    `json:"created_at"`
	Links      []string  `json:"links"`                   // 关联的 memory/知识 ID（旧格式，兼容）
	TypedLinks []TypedLink `json:"typed_links,omitempty"` // 有类型的关联链接（新格式）
	RawRef     string    `json:"raw_ref"`                 // 原始来源引用
	Content    string    `json:"content,omitempty"`       // 完整内容（可选）
	Embedding  []float64 `json:"embedding,omitempty"`     // 语义嵌入向量，用于语义检索
	Ephemeral  bool      `json:"ephemeral,omitempty"`     // 临时记忆，到期不参与检索
	ExpiresAt  int64     `json:"expires_at,omitempty"`   // 过期时间戳，0=永久
	Status        string    `json:"status,omitempty"`         // active/archived/expired，空=active
	LastAccessedAt int64     `json:"last_accessed_at,omitempty"` // 最后被检索/引用的时间戳
	Namespace     string    `json:"namespace,omitempty"`      // 所属空间: default/workspace-a/project-b，空=default
	Verified      bool      `json:"verified,omitempty"`        // 是否经过事实核查
	VerifiedAt    int64     `json:"verified_at,omitempty"`     // 核查时间
	HitCount      int64     `json:"hit_count,omitempty"`       // 被检索命中次数，用于排序加权
	UtilityScore  float64   `json:"utility_score,omitempty"`   // 用户反馈评分: +1/-1/0，越用越准
	ContentType   string    `json:"content_type,omitempty"`    // work_record | decision | lesson | fact

	// BlockContents 块级存储的文档块内容列表（检索时匹配用，不序列化到 JSON 文件）。
	BlockContents []string `json:"-"`
}

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
	Query  string          `json:"query"`
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

/* ─── 存储引擎 ─────────────────────────────────── */

var (
	knowledgeMu  sync.RWMutex
	knowledgeDir string
)

func initKnowledgeDir() {
	if knowledgeDir == "" {
		knowledgeDir = filepath.Join(MemoryDir, "knowledge")
	}
	os.MkdirAll(knowledgeDir, 0755)
}

func knowledgePath(id string) string {
	return filepath.Join(knowledgeDir, id+".json")
}

func loadKnowledge(id string) *KnowledgeEntry {
	initKnowledgeDir()
	data, err := os.ReadFile(knowledgePath(id))
	if err != nil {
		return nil
	}
	var entry KnowledgeEntry
	json.Unmarshal(data, &entry)
	return &entry
}

func saveKnowledge(entry *KnowledgeEntry) {
	initKnowledgeDir()

	// 入库门禁：自动补全（不拒绝，但修正）
	if entry.SourceType == "" {
		entry.SourceType = inferSourceType(entry)
	}
	if len(entry.Tags) == 0 {
		entry.Tags = autoExtractTags(entry.Title, entry.Summary)
	}

	// 写入时顺带计算 embedding（失败不影响入库）
	if embeddingEnabled() && len(entry.Embedding) == 0 {
		text := entry.Title + " " + entry.Summary
		if emb, ok := tryEmbedding(text); ok {
			entry.Embedding = emb
		}
	}

	data, _ := json.MarshalIndent(entry, "", "  ")
	os.WriteFile(knowledgePath(entry.ID), data, 0644)
}

func deleteKnowledge(id string) error {
	initKnowledgeDir()
	return os.Remove(knowledgePath(id))
}

func newKnowledgeID() string {
	return fmt.Sprintf("kn_%d", time.Now().UnixNano())
}

/* ─── 公开 API ─────────────────────────────────── */

func KnowledgeAdd(sourceType, title, summary string, tags, topics, tasks, links []string, rawRef, content string, namespace string) *ToolResult {
	if sourceType == "" {
		return errorResult("source_type 不能为空")
	}
	if title == "" && summary == "" {
		return errorResult("title 和 summary 不能同时为空")
	}

	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	if existing := findKnowledgeByRawRefLocked(rawRef); existing != nil {
		return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","message":"知识已存在，跳过重复入库"}`, existing.ID, existing.Title))
	}

	now := time.Now().Unix()
	entry := &KnowledgeEntry{
		ID:         newKnowledgeID(),
		SourceType: sourceType,
		Title:      title,
		Summary:    summary,
		Tags:       tags,
		Topics:     topics,
		Tasks:      tasks,
		CreatedAt:  now,
		TypedLinks: linksToTypedLinks(links),
		RawRef:     rawRef,
		Content:    content,
		Namespace:  namespace,
	}
	// 保存，但保持 CreatedAt 为首次创建时间
	saveKnowledge(entry)

	// 后台自动建链：不阻塞入库
	if sourceType != "memory" && summary != "" {
		go autoLinkEntry(entry.ID, title, summary, tags, topics)
	}

	return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","message":"知识已入库"}`, entry.ID, title))
}

// KnowledgeRemember 轻量记忆写入：包装 KnowledgeAdd，固定 source_type="memory"。
// Agent 主动调用时用此工具记录关键事实、用户偏好、决策结果。
// contentType: work_record | decision | lesson | fact | ""（空=未分类）
// namespace: 条目所属空间，空=default。claude_dev 专用于 Claude Code 开发会话记忆，与智能体主知识库隔离。
func KnowledgeRemember(title, summary, contentType string, tags []string, expiresInDays int, namespace string) *ToolResult {
	if title == "" && summary == "" {
		return errorResult("title 和 summary 不能同时为空")
	}
	now := time.Now()
	entry := &KnowledgeEntry{
		ID:          newKnowledgeID(),
		SourceType:  "memory",
		ContentType: contentType,
		Title:       title,
		Summary:     summary,
		Tags:        tags,
		CreatedAt:   now.Unix(),
		Namespace:   namespace,
	}
	if expiresInDays > 0 {
		entry.Ephemeral = true
		entry.ExpiresAt = now.Unix() + int64(expiresInDays*86400)
	}
	saveKnowledge(entry)
	ts := now.Format("2006-01-02 15:04:05")
	ns := namespace
	if ns == "" {
		ns = "default"
	}
	return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","namespace":"%s","created_at":"%s","message":"记忆已记录"}`,
		entry.ID, title, ns, ts))
}

func findKnowledgeByRawRefLocked(rawRef string) *KnowledgeEntry {
	if rawRef == "" {
		return nil
	}
	initKnowledgeDir()
	entries, _ := os.ReadDir(knowledgeDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		entry := loadKnowledge(strings.TrimSuffix(e.Name(), ".json"))
		if entry != nil && entry.RawRef == rawRef {
			return entry
		}
	}
	return nil
}

func KnowledgeSearch(keyword string) *ToolResult {
	// 若关键词包含字段语法（tag: / type: / date:> 等），走结构化检索路径。
	q := retrieval.ParseQuery(keyword)
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
	results := SearchMemoryFull(keyword, 5, nil)
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
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Tags          []string `json:"tags"`
	SourceType    string   `json:"source_type"`
	Score         int      `json:"score"`
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


/* ─── embedding 引擎 ────────────────────────── */

var forcedEmbeddingEndpoint string

// SetEmbeddingEndpoint 程序化设置 embedding API 端点，覆盖环境变量。
// 由 main.go 在启动 embedding sidecar 后调用，使语义搜索可用。
func SetEmbeddingEndpoint(url string) {
	forcedEmbeddingEndpoint = url
}

func embeddingEndpoint() string {
	if forcedEmbeddingEndpoint != "" {
		return forcedEmbeddingEndpoint
	}
	return os.Getenv("EMBEDDING_ENDPOINT")
}
func embeddingModel() string {
	if m := os.Getenv("EMBEDDING_MODEL"); m != "" {
		return m
	}
	return "nomic-embed-text"
}

func embeddingEnabled() bool {
	return embeddingEndpoint() != ""
}

// tryEmbedding 调通用 embedding API 计算文本向量。
// 不绑定任何具体工具（Ollama/llama.cpp/glue 均可），靠环境变量配置端点。
func tryEmbedding(text string) ([]float64, bool) {
	if !embeddingEnabled() {
		return nil, false
	}
	// 截断到 300 字符（nomic-embed 的 embedding 上下文窗口实测 ~512 token）
	runes := []rune(text)
	if len(runes) > 300 {
		text = string(runes[:300])
	}
	body, err := json.Marshal(map[string]interface{}{
		"model": embeddingModel(),
		"input": text,
	})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequest("POST", embeddingEndpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("EMBEDDING_API_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	} else if k := os.Getenv("LOCAL_API_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, false
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, false
	}
	return result.Data[0].Embedding, true
}

// searchByEmbedding 向量相似度检索。
// 无 embedding 的条目异步触发惰性补全。
func searchByEmbedding(queryEmb []float64, limit int, namespace string) []ScoredEntry {
	all := loadAllKnowledge()
	var scored []ScoredEntry
	var pending []*KnowledgeEntry

	for _, entry := range all {
		// 跳过过期和已归档条目
		if entry.Status != "" && entry.Status != "active" {
			continue
		}
		if !matchNamespace(entry, namespace) {
			continue
		}
		if entry.Ephemeral && entry.ExpiresAt > 0 && time.Now().Unix() > entry.ExpiresAt {
			continue
		}
		if len(entry.Embedding) == 0 {
			pending = append(pending, entry)
			continue
		}
		sim := cosineSimilarity(queryEmb, entry.Embedding)
		if sim >= 0.4 {
			scored = append(scored, ScoredEntry{
				ID:         entry.ID,
				Title:      entry.Title,
				Summary:    entry.Summary,
				Tags:       entry.Tags,
				SourceType: entry.SourceType,
				Score:      int(sim * 100),
			})
		}
	}

	if len(pending) > 0 {
		go batchFillEmbedding(pending)
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

// batchFillEmbedding 批量补全 embedding，后台调用。
func batchFillEmbedding(entries []*KnowledgeEntry) {
	if !embeddingEnabled() {
		return
	}
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			continue
		}
		text := e.Title + " " + e.Summary
		if emb, ok := tryEmbedding(text); ok {
			e.Embedding = emb
			saveKnowledge(e)
		}
	}
}


/* ─── RetrievalResult 统一检索结果 ──────────────── */
// 已迁移至 internal/retrieval/contract.go

// RetrievalResult 统一检索结果（内部使用）
// 已迁移至 internal/retrieval/contract.go
// 使用 retrieval.RetrievalResult 代替

// loadLinkedEntries 按 Links 和 TypedLinks 字段加载关联条目。
// 返回条目列表和对应的链接类型（用于加权）
func loadLinkedEntries(id string) ([]*KnowledgeEntry, map[string]LinkType) {
	entry := loadKnowledge(id)
	if entry == nil {
		return nil, nil
	}

	linkTypes := make(map[string]LinkType)
	var linked []*KnowledgeEntry
	seen := make(map[string]bool)

	recordAccess(id)

	// 先加载 TypedLinks（新格式）
	for _, tl := range entry.TypedLinks {
		if seen[tl.TargetID] {
			continue
		}
		seen[tl.TargetID] = true
		if le := loadKnowledge(tl.TargetID); le != nil {
			linked = append(linked, le)
			linkTypes[tl.TargetID] = tl.Type
			recordAccess(tl.TargetID)
		}
	}

	// 再加载 Links（旧格式，标记为 related）
	for _, linkedID := range entry.Links {
		if seen[linkedID] {
			continue
		}
		seen[linkedID] = true
		if le := loadKnowledge(linkedID); le != nil {
			linked = append(linked, le)
			linkTypes[linkedID] = LinkRelated
			recordAccess(linkedID)
		}
	}

	return linked, linkTypes
}

// autoLinkEntry 为新条目自动建立双向关联链接。
// 两层建链：
//   1. 确定性建链：基于标签重叠和标题/摘要关键词匹配
//   2. 语义建链：LLM 分析关系类型（写入时离线，不违反宪法）
func autoLinkEntry(id, title, summary string, tags, topics []string) {
	all := loadAllKnowledge()
	titleWords := strings.ToLower(title)

	var candidates []string
	for _, e := range all {
		if e.ID == id || len(e.Title) == 0 {
			continue
		}
		score := 0
		// 标签重叠
		for _, t := range tags {
			for _, et := range e.Tags {
				if strings.ToLower(t) == strings.ToLower(et) || (len(t) > 2 && strings.Contains(strings.ToLower(et), strings.ToLower(t))) {
					score += 2
					break
				}
			}
		}
		for _, tp := range topics {
			for _, et := range e.Topics {
				if strings.ToLower(tp) == strings.ToLower(et) {
					score += 2
					break
				}
			}
		}
		// 标题/摘要关键词匹配
		et := strings.ToLower(e.Title)
		if len(title) > 3 && (strings.Contains(et, strings.ToLower(title)) || strings.Contains(strings.ToLower(title), et[:min(len(et), 8)])) {
			score += 1
		}
		if len(summary) > 5 && strings.Contains(titleWords, et[:min(len(et), 6)]) {
			score += 1
		}
		if score >= 3 && !containsStr(candidates, e.ID) {
			candidates = append(candidates, e.ID)
		}
	}

	// 双向写入 TypedLinks（确定性建链，替代旧的 Links 写入）
	entry := loadKnowledge(id)
	if entry == nil {
		return
	}
	for _, cid := range candidates {
		if !containsTypedLink(entry.TypedLinks, cid) {
			entry.TypedLinks = append(entry.TypedLinks, TypedLink{
				TargetID: cid,
				Type:     LinkRelated,
				Reason:   "标签/主题/关键词匹配",
			})
		}
		// 反向链接
		le := loadKnowledge(cid)
		if le != nil && !containsTypedLink(le.TypedLinks, id) {
			le.TypedLinks = append(le.TypedLinks, TypedLink{
				TargetID: id,
				Type:     LinkRelated,
				Reason:   "反向关联: 标签/主题/关键词匹配",
			})
			saveKnowledge(le)
		}
	}

	// 第二层：语义建链（代码判断，写入时离线）
	// 只对最近 50 条知识做对比，不是全量
	recent := getRecentEntries(all, 50)
	semanticLinks := findSemanticLinks(id, title, summary, recent)
	if len(semanticLinks) > 0 {
		entry.TypedLinks = mergeTypedLinks(entry.TypedLinks, semanticLinks)
		// 反向写入
		for _, link := range semanticLinks {
			le := loadKnowledge(link.TargetID)
			if le != nil {
				reverseLink := TypedLink{
					TargetID: id,
					Type:     reverseLinkType(link.Type),
					Reason:   "反向关联: " + link.Reason,
				}
				le.TypedLinks = mergeTypedLinks(le.TypedLinks, []TypedLink{reverseLink})
				saveKnowledge(le)
			}
		}
		fmt.Printf("[knowledge] 语义建链: %s → %d 条关联\n", id, len(semanticLinks))
	}

	saveKnowledge(entry)
}

// getRecentEntries 获取最近 N 条知识条目（按创建时间倒序）
func getRecentEntries(all []*KnowledgeEntry, limit int) []*KnowledgeEntry {
	sorted := make([]*KnowledgeEntry, len(all))
	copy(sorted, all)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt > sorted[j].CreatedAt
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// findSemanticLinks 用代码判断语义关系（写入时离线，零 LLM）。
// 基于关键词反义检测 + 时间戳，完全确定性。
func findSemanticLinks(id, title, summary string, candidates []*KnowledgeEntry) []TypedLink {
	if len(candidates) == 0 {
		return nil
	}
	entry := loadKnowledge(id)
	if entry == nil {
		return nil
	}
	lower := strings.ToLower

	var links []TypedLink
	for _, c := range candidates {
		if c.ID == id {
			continue
		}
		// 判断关系类型
		linkType := LinkRelated
		reason := "相关条目"

		t1 := lower(title + " " + summary)
		t2 := lower(c.Title + " " + c.Summary)

		// contradicts：同主题 + 一个有否定词一个没有（结论方向相反）
		hasNeg1 := hasNegKeyword(t1)
		hasNeg2 := hasNegKeyword(t2)
		if hasNeg1 != hasNeg2 && hasSharedTagOrTopic(entry, c) {
			linkType = LinkContradicts
			reason = "结论方向相反"
		} else if hasNeg1 && hasNeg2 && entry.CreatedAt > c.CreatedAt {
			// supersedes：同主题 + 新条目否定旧条目的肯定方向
			if hasPosKeyword(t2) {
				linkType = LinkSupersedes
				reason = "新结论替代旧结论"
			}
		} else if !hasNeg1 && !hasNeg2 && hasPosKeyword(t1) && hasPosKeyword(t2) {
			linkType = LinkSupports
			reason = "结论相互印证"
		}

		links = append(links, TypedLink{
			TargetID: c.ID,
			Type:     linkType,
			Reason:   reason,
		})
	}
	return links
}

// hasNegKeyword 检查文本是否包含否定/放弃类关键词。
func hasNegKeyword(text string) bool {
	neg := []string{"放弃", "不可", "不行", "禁止", "避免", "停止", "错误", "失败",
		"不用", "不能", "不要", "不推荐", "有问题", "复杂性", "瓶颈", "不实用", "太慢"}
	for _, kw := range neg {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// hasPosKeyword 检查文本是否包含肯定/推荐类关键词。
func hasPosKeyword(text string) bool {
	pos := []string{"采用", "使用", "支持", "可用", "推荐", "可以", "成功",
		"正确", "适配", "实现", "集成", "打通", "接入", "完成", "支持"}
	for _, kw := range pos {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// mergeTypedLinks 合并去重 TypedLink
func mergeTypedLinks(existing, newLinks []TypedLink) []TypedLink {
	seen := make(map[string]bool)
	for _, link := range existing {
		seen[link.TargetID+":"+string(link.Type)] = true
	}
	merged := make([]TypedLink, len(existing))
	copy(merged, existing)
	for _, link := range newLinks {
		key := link.TargetID + ":" + string(link.Type)
		if !seen[key] {
			merged = append(merged, link)
			seen[key] = true
		}
	}
	return merged
}

// hasSharedTagOrTopic 检查两条知识是否有共享的标签或主题
func hasSharedTagOrTopic(a, b *KnowledgeEntry) bool {
	for _, ta := range a.Tags {
		for _, tb := range b.Tags {
			if strings.ToLower(ta) == strings.ToLower(tb) {
				return true
			}
		}
	}
	for _, ta := range a.Topics {
		for _, tb := range b.Topics {
			if strings.ToLower(ta) == strings.ToLower(tb) {
				return true
			}
		}
	}
	return false
}

// reverseLinkType 反转链接类型
func reverseLinkType(t LinkType) LinkType {
	switch t {
	case LinkContradicts:
		return LinkContradicts // 矛盾是双向的
	case LinkSupersedes:
		return LinkRelated // 被替代方标记为 related
	default:
		return t
	}
}

func containsTypedLink(links []TypedLink, targetID string) bool {
	for _, l := range links {
		if l.TargetID == targetID {
			return true
		}
	}
	return false
}

// typedLinksFromArgs 将 JSON 反序列化后的 typed_links 参数转换为 []TypedLink。
func typedLinksFromArgs(raw interface{}) []TypedLink {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []TypedLink
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		tl := TypedLink{
			TargetID: strFromMap(m, "target_id"),
			Type:     LinkType(strFromMap(m, "type")),
			Reason:   strFromMap(m, "reason"),
		}
		if tl.TargetID != "" {
			result = append(result, tl)
		}
	}
	return result
}

func strFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// linksToTypedLinks 将旧格式的 string ID 列表转换为 TypedLinks。
func linksToTypedLinks(ids []string) []TypedLink {
	var tls []TypedLink
	for _, id := range ids {
		if id == "" {
			continue
		}
		tls = append(tls, TypedLink{
			TargetID: id,
			Type:     LinkRelated,
			Reason:   "知识关联",
		})
	}
	return tls
}

// matchNamespace 检查条目是否匹配指定空间。ns 为空时返回 true（不过滤）。
func matchNamespace(entry *KnowledgeEntry, ns string) bool {
	if ns == "" {
		return true
	}
	entryNs := entry.Namespace
	if entryNs == "" {
		entryNs = "default"
	}
	return entryNs == ns
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// recordAccess 记录条目被检索/引用的时间。
// 仅在检索命中时写入，不影响只读扫描。
func recordAccess(id string) {
	entry := loadKnowledge(id)
	if entry == nil {
		return
	}
	entry.LastAccessedAt = time.Now().Unix()
	entry.HitCount++
	saveKnowledge(entry)
}

/* ─── 版本控制 ──────────────────────────────────── */

// saveVersionSnapshot 在修改前保存当前版本快照。
// 历史文件存储在 history/{id}/v{timestamp}.json。
// 每个条目保留最近 50 个版本，超出时删除最旧的。
func saveVersionSnapshot(id string, entry *KnowledgeEntry) {
	initKnowledgeDir()
	historyDir := filepath.Join(knowledgeDir, "history", id)
	os.MkdirAll(historyDir, 0755)
	path := filepath.Join(historyDir, fmt.Sprintf("v%d.json", time.Now().UnixNano()))
	data, _ := json.MarshalIndent(entry, "", "  ")
	os.WriteFile(path, data, 0644)

	// 版本清理：保留最近 50 个
	if entries, err := os.ReadDir(historyDir); err == nil && len(entries) > 50 {
		sort.Slice(entries, func(i, j int) bool {
			ii, _ := entries[i].Info()
			ji, _ := entries[j].Info()
			return ii.ModTime().Before(ji.ModTime())
		})
		for i := 0; i < len(entries)-50; i++ {
			os.Remove(filepath.Join(historyDir, entries[i].Name()))
		}
	}
}

// KnowledgeHistory 查看指定条目的版本历史。
func KnowledgeHistory(id string) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}
	initKnowledgeDir()
	historyDir := filepath.Join(knowledgeDir, "history", id)

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		return successResult(fmt.Sprintf(`{"id":"%s","versions":[],"message":"暂无历史版本"}`, id))
	}

	type VersionInfo struct {
		Timestamp int64  `json:"timestamp"`
		File      string `json:"file"`
		Title     string `json:"title"`
		Summary   string `json:"summary"`
	}

	var versions []VersionInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(historyDir, e.Name()))
		var entry KnowledgeEntry
		if json.Unmarshal(data, &entry) != nil {
			continue
		}
		// 从文件名 v1712345678.json 提取时间戳
		var ts int64
		fmt.Sscanf(e.Name(), "v%d.json", &ts)
		versions = append(versions, VersionInfo{
			Timestamp: ts,
			File:      e.Name(),
			Title:     entry.Title,
			Summary:   truncateStr(entry.Summary, 100),
		})
	}

	if len(versions) == 0 {
		return successResult(fmt.Sprintf(`{"id":"%s","versions":[],"message":"暂无历史版本"}`, id))
	}

	// 按时间倒序
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Timestamp > versions[j].Timestamp
	})

	result := map[string]interface{}{
		"id":       id,
		"versions": versions,
		"count":    len(versions),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

// KnowledgeVersionGet 获取指定版本的内容。
func KnowledgeVersionGet(id, versionFile string) *ToolResult {
	if id == "" || versionFile == "" {
		return errorResult("id 和 version 不能为空")
	}
	initKnowledgeDir()
	path := filepath.Join(knowledgeDir, "history", id, versionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("版本文件 %s 未找到", versionFile))
	}
	return successResult(string(data))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
//   L0:   确定性关键词评分（不调 LLM）
//   L0.5: 图扩展（沿 Links/TypedLinks 遍历）
//   L2:   embedding 语义回退（仅当 L0 结果不足时）
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

/* ─── KnowledgeReindex 批量补全工具 ──────────── */

// embeddingText 为知识条目生成用于计算向量的文本。
// 优先使用 content（有实质内容时），回退到 title+summary。
// 检测 summary 是否为 macOS 系统噪声（形如 "darwin/20.x.x ..."），若是则忽略。
func embeddingText(e *KnowledgeEntry) string {
	if e.Content != "" {
		return e.Title + " " + e.Content
	}
	// 匹配 macOS 内核版本字符串噪声：小写 "darwin/" 后跟版本号
	if strings.Contains(strings.ToLower(e.Summary), "darwin/") && len(e.Summary) > 30 {
		return e.Title
	}
	return e.Title + " " + e.Summary
}

/* KnowledgeReindex 为所有无 embedding 或维度不匹配的知识条目重新计算语义向量。
   force=true 时强制重算所有条目（忽略已有 embedding）。 */
func KnowledgeReindex() *ToolResult {
	if !embeddingEnabled() {
		return successResult(`{"message":"EMBEDDING_ENDPOINT 未设置，跳过"}`)
	}
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"message":"知识库为空，跳过","count":0}`)
	}
	var count int
	// 先用一条文本探测当前 API 的向量维度
	probeText := all[0].Title
	probeEmb, ok := tryEmbedding(probeText)
	if !ok || len(probeEmb) == 0 {
		return successResult(`{"message":"embedding API 不可用，跳过"}`)
	}
	apiDim := len(probeEmb)

	for _, e := range all {
		// 跳过已有正确维度的 embedding
		if len(e.Embedding) == apiDim {
			continue
		}
		text := embeddingText(e)
		if emb, ok := tryEmbedding(text); ok && len(emb) == apiDim {
			e.Embedding = emb
			saveKnowledge(e)
			count++
		}
	}
	return successResult(fmt.Sprintf(`{"message":"重算完成","count":%d,"dim":%d}`, count, apiDim))
}

// KnowledgeBackup 将知识库目录备份到带时间戳的子目录。
//
// 备份内容：
//   - knowledgeDir（所有 .json 知识条目）
//   - calibration.jsonl（分类校准数据）
//
// 保留策略：最多保留最近 7 份，自动删除更早的备份。
// 默认目录：~/.hermes/backups/knowledge_YYYYMMDD_HHMMSS
/* ─── 检索质量探针 ─────────────────────────────────────────────────────────
   每次探针从知识库随机采样 10 条，测量 L0 关键词 + L1 语义的召回率@3。
   结果追加到 ~/.hermes/probe_history.jsonl，供趋势分析。
   已知基线（2026-05-27）：L0 6/10、L1 4/10。
──────────────────────────────────────────────────────────────────────────── */

// ProbeResult 记录单次检索质量探针的结果。
type ProbeResult struct {
	ProbeTime    string  `json:"probe_time"`
	TotalEntries int     `json:"total_entries"`
	TotalSampled int     `json:"total_sampled"`
	L0Found      int     `json:"l0_found"`
	L0Recall     float64 `json:"l0_recall_at_3"`
	L1Found      int     `json:"l1_found"`
	L1Recall     float64 `json:"l1_recall_at_3"`
	L1Available  bool    `json:"l1_available"`
}

// KnowledgeProbe 检索质量探针。
// 随机采样 min(10, total) 条 active 知识条目，分别用 L0（关键词）和
// L1（向量，需 EMBEDDING_ENDPOINT）搜索每条条目的标题，统计 recall@3。
// 结果追加写入 probe_history.jsonl，并以 JSON 形式返回。
func KnowledgeProbe() *ToolResult {
	initKnowledgeDir()

	all := loadAllKnowledge()
	var active []*KnowledgeEntry
	for _, e := range all {
		if e.Status == "" || e.Status == "active" {
			active = append(active, e)
		}
	}
	if len(active) < 3 {
		return successResult(fmt.Sprintf(
			`{"message":"知识库条目不足，跳过探针","total_entries":%d}`, len(active)))
	}

	const sampleSize = 10
	const topK = 3

	// 随机打乱后取前 sampleSize 条
	sample := make([]*KnowledgeEntry, len(active))
	copy(sample, active)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(sample), func(i, j int) { sample[i], sample[j] = sample[j], sample[i] })
	if len(sample) > sampleSize {
		sample = sample[:sampleSize]
	}

	l1Available := embeddingEndpoint() != ""
	var l0Found, l1Found int

	for _, entry := range sample {
		// L0：用标题关键词搜索，检查 entry.ID 是否出现在 top-K
		l0Results := SearchWithScore(entry.Title, topK, "")
		for _, res := range l0Results {
			if res.ID == entry.ID {
				l0Found++
				break
			}
		}

		// L1：向量搜索（仅在 EMBEDDING_ENDPOINT 可用时）
		if l1Available {
			if emb, ok := tryEmbedding(entry.Title); ok {
				l1Results := searchByEmbedding(emb, topK, "")
				for _, res := range l1Results {
					if res.ID == entry.ID {
						l1Found++
						break
					}
				}
			}
		}
	}

	n := len(sample)
	l0Recall := float64(l0Found) / float64(n)
	var l1Recall float64
	if l1Available && n > 0 {
		l1Recall = float64(l1Found) / float64(n)
	}

	result := ProbeResult{
		ProbeTime:    time.Now().UTC().Format(time.RFC3339),
		TotalEntries: len(active),
		TotalSampled: n,
		L0Found:      l0Found,
		L0Recall:     l0Recall,
		L1Found:      l1Found,
		L1Recall:     l1Recall,
		L1Available:  l1Available,
	}

	// 追加到历史记录（趋势分析用）
	historyPath := filepath.Join(HermesHome, "probe_history.jsonl")
	if line, err := json.Marshal(result); err == nil {
		if f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			_, _ = fmt.Fprintf(f, "%s\n", line)
			f.Close()
		}
	}

	out, _ := json.Marshal(result)
	return successResult(string(out))
}

func KnowledgeBackup(destParent string) *ToolResult {
	initKnowledgeDir()

	if destParent == "" {
		destParent = filepath.Join(HermesHome, "backups")
	}
	ts := time.Now().Format("20060102_150405")
	backupDir := filepath.Join(destParent, "knowledge_"+ts)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return errorResult(fmt.Sprintf("创建备份目录失败: %v", err))
	}

	// 复制所有知识条目 JSON
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		return errorResult(fmt.Sprintf("读取知识库目录失败: %v", err))
	}
	var copied int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		src := filepath.Join(knowledgeDir, e.Name())
		dst := filepath.Join(backupDir, e.Name())
		if data, err := os.ReadFile(src); err == nil {
			if err := os.WriteFile(dst, data, 0644); err == nil {
				copied++
			}
		}
	}

	// 复制校准数据
	calibSrc := filepath.Join(MemoryDir, "knowledge_calibration.jsonl")
	if data, err := os.ReadFile(calibSrc); err == nil {
		_ = os.WriteFile(filepath.Join(backupDir, "knowledge_calibration.jsonl"), data, 0644)
	}

	// 清理旧备份，保留最近 7 份
	pruned := pruneOldBackups(destParent, "knowledge_", 7)

	return successResult(fmt.Sprintf(
		`{"backup_dir":%q,"files_copied":%d,"old_backups_pruned":%d}`,
		backupDir, copied, pruned,
	))
}

// pruneOldBackups 删除目录下前缀匹配的旧备份，保留最新 keep 份。
func pruneOldBackups(parent, prefix string, keep int) int {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			dirs = append(dirs, e.Name())
		}
	}
	// ReadDir 按名字排序，时间戳格式保证字典序 = 时间序
	sort.Strings(dirs)
	var pruned int
	for len(dirs) > keep {
		old := dirs[0]
		dirs = dirs[1:]
		if err := os.RemoveAll(filepath.Join(parent, old)); err == nil {
			pruned++
		}
	}
	return pruned
}

func KnowledgeList(sourceType string, days int, contentType string) *ToolResult {
	return KnowledgeListNS(sourceType, days, contentType, "")
}

// KnowledgeListNS 列出知识条目，支持 namespace 过滤。
// namespace="" 返回所有 namespace（向后兼容）；非空时精确匹配。
func KnowledgeListNS(sourceType string, days int, contentType, namespace string) *ToolResult {
	initKnowledgeDir()
	entries, _ := os.ReadDir(knowledgeDir)

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	var kEntries []KnowledgeEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		entry := loadKnowledge(strings.TrimSuffix(e.Name(), ".json"))
		if entry == nil {
			continue
		}
		if sourceType != "" && entry.SourceType != sourceType {
			continue
		}
		if contentType != "" && entry.ContentType != contentType {
			continue
		}
		if days > 0 && time.Unix(entry.CreatedAt, 0).Before(cutoff) {
			continue
		}
		if namespace != "" && entry.Namespace != namespace {
			continue
		}
		kEntries = append(kEntries, *entry)
	}

	if len(kEntries) == 0 {
		return successResult("暂无知识条目。")
	}

	sort.Slice(kEntries, func(i, j int) bool {
		return kEntries[i].CreatedAt > kEntries[j].CreatedAt
	})

	var sb strings.Builder
	for _, e := range kEntries {
		created := time.Unix(e.CreatedAt, 0).Format("2006-01-02 15:04:05")
		ns := e.Namespace
		if ns == "" {
			ns = "default"
		}
		tags := strings.Join(e.Tags, ", ")
		sb.WriteString(fmt.Sprintf("%s [%s][%s][ns:%s] %s — %s (tags: %s)\n",
			e.ID, e.SourceType, e.ContentType, ns, e.Title, created, tags))
	}
	return successResult(sb.String())
}

func KnowledgeGet(id string) *ToolResult {
	knowledgeMu.RLock()
	defer knowledgeMu.RUnlock()

	entry := loadKnowledge(id)
	if entry == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}
	b, _ := json.MarshalIndent(entry, "", "  ")
	return successResult(string(b))
}

func KnowledgeUpdate(id string, fields map[string]interface{}) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}

	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	entry := loadKnowledge(id)
	if entry == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}

	changed := false
	if v, ok := fields["source_type"].(string); ok && v != "" {
		entry.SourceType = v
		changed = true
	}
	if v, ok := fields["title"].(string); ok && v != "" {
		entry.Title = v
		changed = true
	}
	if v, ok := fields["summary"].(string); ok && v != "" {
		entry.Summary = v
		changed = true
	}
	if v, ok := fields["tags"].([]string); ok {
		entry.Tags = v
		changed = true
	}
	if v, ok := fields["topics"].([]string); ok {
		entry.Topics = v
		changed = true
	}
	if v, ok := fields["tasks"].([]string); ok {
		entry.Tasks = v
		changed = true
	}
	if v, ok := fields["links"].([]string); ok {
		entry.Links = v
		changed = true
	}
	if rawTL, ok := fields["typed_links"]; ok {
		if tls := typedLinksFromArgs(rawTL); tls != nil {
			entry.TypedLinks = tls
			changed = true
		}
	}
	if v, ok := fields["raw_ref"].(string); ok {
		entry.RawRef = v
		changed = true
	}
	if v, ok := fields["content"].(string); ok {
		entry.Content = v
		changed = true
	}

	if !changed {
		return successResult(fmt.Sprintf(`{"id":"%s","message":"无需更新"}`, id))
	}

	// 保存版本快照（重新加载原始条目，避免保存修改后的内容）
	if orig := loadKnowledge(id); orig != nil {
		saveVersionSnapshot(id, orig)
	}

	// 保持原始创建时间不变
	saveKnowledge(entry)

	b, _ := json.MarshalIndent(entry, "", "  ")
	return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","message":"已更新","entry":%s}`, id, entry.Title, string(b)))
}

func KnowledgeDelete(id string) *ToolResult {
	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	if err := deleteKnowledge(id); err != nil {
		return errorResult(fmt.Sprintf("删除知识条目失败: %v", err))
	}
	return successResult(fmt.Sprintf("知识条目 %s 已删除", id))
}

/* ─── 查重 ──────────────────────────────────────── */

type DedupMatch struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Reason  string `json:"reason"`
	Score   int    `json:"score"`
}

func KnowledgeDedupe(id, rawRef string) *ToolResult {
	knowledgeMu.RLock()
	defer knowledgeMu.RUnlock()

	var all []*KnowledgeEntry
	for _, entry := range loadAllKnowledge() {
		all = append(all, entry)
	}

	var matches []DedupMatch

	if rawRef != "" {
		for _, entry := range all {
			if id != "" && entry.ID == id {
				continue
			}
			if entry.RawRef == rawRef {
				matches = append(matches, DedupMatch{
					ID: entry.ID, Title: entry.Title,
					Summary: entry.Summary,
					Reason:  fmt.Sprintf("相同 raw_ref: %s", rawRef),
					Score:   100,
				})
			}
		}
	}

	if id != "" {
		source := findEntry(all, id)
		if source == nil {
			if len(matches) == 0 {
				return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
			}
		} else {
			for _, entry := range all {
				if entry.ID == id {
					continue
				}
				score := 0
				var reasons []string

				if entry.RawRef != "" && entry.RawRef == source.RawRef {
					score += 80
					reasons = append(reasons, "相同 raw_ref")
				}
				if strings.EqualFold(entry.Title, source.Title) {
					score += 50
					reasons = append(reasons, "标题相同")
				} else if strings.Contains(strings.ToLower(entry.Title), strings.ToLower(source.Title)) ||
					strings.Contains(strings.ToLower(source.Title), strings.ToLower(entry.Title)) {
					score += 20
					reasons = append(reasons, "标题相似")
				}
				if strings.Contains(strings.ToLower(entry.Summary), strings.ToLower(source.Summary)) ||
					strings.Contains(strings.ToLower(source.Summary), strings.ToLower(entry.Summary)) {
					score += 10
					reasons = append(reasons, "摘要重叠")
				}
				// 检查 shared_tags 数量
				shared := intersectStrings(entry.Tags, source.Tags)
				if len(shared) > 0 {
					score += 10 * len(shared)
					if score > 50 {
						score = 50
					}
					reasons = append(reasons, fmt.Sprintf("共享 %d 个标签", len(shared)))
				}

				if score >= 20 {
					matches = append(matches, DedupMatch{
						ID: entry.ID, Title: entry.Title,
						Summary: entry.Summary,
						Reason:  strings.Join(reasons, "; "),
						Score:   score,
					})
				}
			}
		}
	}

	if len(matches) == 0 {
		return successResult(`{"matches":[],"count":0,"message":"未发现重复条目"}`)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	result := map[string]interface{}{
		"matches": matches,
		"count":   len(matches),
		"message": fmt.Sprintf("发现 %d 个可能重复的条目", len(matches)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func findEntry(entries []*KnowledgeEntry, id string) *KnowledgeEntry {
	for _, e := range entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

/* ─── 合并 ──────────────────────────────────────── */

func KnowledgeMerge(sourceID, targetID string) *ToolResult {
	if sourceID == "" || targetID == "" {
		return errorResult("source_id 和 target_id 不能为空")
	}
	if sourceID == targetID {
		return errorResult("source_id 和 target_id 不能相同")
	}

	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	source := loadKnowledge(sourceID)
	if source == nil {
		return errorResult(fmt.Sprintf("源条目 %s 未找到", sourceID))
	}
	target := loadKnowledge(targetID)
	if target == nil {
		return errorResult(fmt.Sprintf("目标条目 %s 未找到", targetID))
	}

	// 合并 tags: union
	target.Tags = unionStrings(target.Tags, source.Tags)
	// 合并 topics
	target.Topics = unionStrings(target.Topics, source.Topics)
	// 合并 tasks
	target.Tasks = unionStrings(target.Tasks, source.Tasks)
	// 合并 links（旧格式，兼容）
	target.Links = unionStrings(target.Links, source.Links)
	// 合并 typed_links（去重）
	for _, tl := range source.TypedLinks {
		if !containsTypedLink(target.TypedLinks, tl.TargetID) {
			target.TypedLinks = append(target.TypedLinks, tl)
		}
	}
	// 合并反馈指标：HitCount 累加，UtilityScore 取最大值
	target.HitCount += source.HitCount
	if source.UtilityScore > target.UtilityScore {
		target.UtilityScore = source.UtilityScore
	}
	// 合并 content: 如果 source 有额外内容
	if source.Content != "" {
		srcTrimmed := strings.TrimSpace(source.Content)
		tgtTrimmed := strings.TrimSpace(target.Content)
		if srcTrimmed != tgtTrimmed && !strings.Contains(tgtTrimmed, srcTrimmed) {
			target.Content = tgtTrimmed + "\n\n---\n\n" + srcTrimmed
		}
	}
	// 更新 Summary 取更长的
	if len(source.Summary) > len(target.Summary) {
		target.Summary = source.Summary
	}

	saveKnowledge(target)
	deleteKnowledge(sourceID)

	// 重定向其他条目中指向 source 的 TypedLinks，避免悬空引用
	allEntries := loadAllKnowledge()
	for _, entry := range allEntries {
		if entry.ID == targetID || entry.ID == sourceID {
			continue
		}
		changed := false
		for i, tl := range entry.TypedLinks {
			if tl.TargetID == sourceID {
				entry.TypedLinks[i].TargetID = targetID
				changed = true
			}
		}
		if changed {
			saveKnowledge(entry)
		}
	}

	b, _ := json.MarshalIndent(target, "", "  ")
	return successResult(fmt.Sprintf(`{"target_id":"%s","source_id":"%s","message":"已合并","entry":%s}`, targetID, sourceID, string(b)))
}

func unionStrings(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		set[s] = true
	}
	result := make([]string, 0, len(set))
	for s := range set {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}

/* ─── 关联确认写入 ──────────────────────────────── */

func KnowledgeConfirmLinks(id string, linkIDs []string) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}
	if len(linkIDs) == 0 {
		return errorResult("link_ids 不能为空")
	}

	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	entry := loadKnowledge(id)
	if entry == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}

	added := 0
	for _, lid := range linkIDs {
		lid = strings.TrimSpace(lid)
		if lid == "" || lid == id {
			continue
		}
		if !containsTypedLink(entry.TypedLinks, lid) {
			entry.TypedLinks = append(entry.TypedLinks, TypedLink{
				TargetID: lid,
				Type:     LinkRelated,
				Reason:   "用户确认关联",
			})
			added++
		}
	}

	if added == 0 {
		return successResult(fmt.Sprintf(`{"id":"%s","message":"所有链接已存在，无需添加","typed_links_count":%d}`, id, len(entry.TypedLinks)))
	}

	saveKnowledge(entry)

	b, _ := json.MarshalIndent(entry, "", "  ")
	return successResult(fmt.Sprintf(`{"id":"%s","message":"已确认 %d 条关联","typed_links_count":%d,"entry":%s}`, id, added, len(entry.TypedLinks), string(b)))
}

/* ─── 关联建议 ──────────────────────────────────── */

type LinkCandidate struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Score        float64  `json:"score"`
	SharedTags   []string `json:"shared_tags,omitempty"`
	SharedTopics []string `json:"shared_topics,omitempty"`
	KeywordMatch bool     `json:"keyword_match"`
	Reason       string   `json:"reason"`
}

func KnowledgeSuggestLinks(id string, maxResults int) *ToolResult {
	if maxResults <= 0 {
		maxResults = 10
	}

	knowledgeMu.RLock()
	source := loadKnowledge(id)
	knowledgeMu.RUnlock()

	if source == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}

	entries := loadAllKnowledge()
	var candidates []LinkCandidate

	for _, entry := range entries {
		if entry.ID == id {
			continue
		}
		if containsString(source.Links, entry.ID) {
			continue
		}

		sharedTags := intersectStrings(source.Tags, entry.Tags)
		sharedTopics := intersectStrings(source.Topics, entry.Topics)

		score := 0.0
		var reasons []string

		if len(sharedTags) > 0 {
			tagScore := float64(len(sharedTags)) * 0.35
			if tagScore > 0.7 {
				tagScore = 0.7
			}
			score += tagScore
			reasons = append(reasons, fmt.Sprintf("共享标签: %s", strings.Join(sharedTags, ", ")))
		}

		if len(sharedTopics) > 0 {
			topicScore := float64(len(sharedTopics)) * 0.30
			if topicScore > 0.6 {
				topicScore = 0.6
			}
			score += topicScore
			reasons = append(reasons, fmt.Sprintf("共享主题: %s", strings.Join(sharedTopics, ", ")))
		}

		// 关键词匹配：源条目的标签/主题是否出现在目标条目的标题/摘要中
		kwMatch := false
		searchTerms := append([]string{}, source.Tags...)
		searchTerms = append(searchTerms, source.Topics...)
		searchTerms = append(searchTerms, extractKnowledgeKeywords(source.Title)...)
		seen := make(map[string]bool)
		for _, term := range searchTerms {
			if seen[term] || len(term) < 2 {
				continue
			}
			seen[term] = true
			termLower := strings.ToLower(term)
			if strings.Contains(strings.ToLower(entry.Title), termLower) ||
				strings.Contains(strings.ToLower(entry.Summary), termLower) {
				kwMatch = true
				reasons = append(reasons, fmt.Sprintf("关键词'%s'出现在目标条目", term))
				break
			}
		}
		if kwMatch {
			score += 0.20
		}

		if score >= 0.20 {
			if score > 1.0 {
				score = 1.0
			}
			candidates = append(candidates, LinkCandidate{
				ID:           entry.ID,
				Title:        entry.Title,
				Summary:      entry.Summary,
				Score:        score,
				SharedTags:   sharedTags,
				SharedTopics: sharedTopics,
				KeywordMatch: kwMatch,
				Reason:       strings.Join(reasons, "; "),
			})
		}
	}

	if len(candidates) == 0 {
		return successResult(`{"candidates":[],"message":"未找到关联条目"}`)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	result := map[string]interface{}{
		"source_id":    id,
		"source_title": source.Title,
		"candidates":   candidates,
		"count":        len(candidates),
		"message":      fmt.Sprintf("找到 %d 个候选关联条目", len(candidates)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 内部辅助 ─────────────────────────────────── */

func loadAllKnowledge() []*KnowledgeEntry {
	return Storage().AllEntries()
}

func intersectStrings(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[strings.ToLower(s)] = true
	}
	var result []string
	for _, s := range b {
		if set[strings.ToLower(s)] {
			result = append(result, s)
		}
	}
	return result
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func extractKnowledgeKeywords(s string) []string {
	var kw []string
	for _, word := range strings.Fields(s) {
		word = strings.Trim(word, "，。！？、；：'（）,.!?;:'()[]{}")
		if len(word) >= 2 {
			kw = append(kw, word)
		}
	}
	return kw
}

func matchesTag(tags []string, keyword string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), keyword) {
			return true
		}
	}
	return false
}

/* ─── Tool 注册 ─────────────────────────────────── */


/* ─── 主题图谱 ──────────────────────────────────── */

type TopicNode struct {
	Name     string   `json:"name"`
	Count    int      `json:"count"`
	Entries  []string `json:"entries"` // IDs
	Tags     []string `json:"tags"`
	Children []TopicNode `json:"children,omitempty"`
}

func KnowledgeTopicMap() *ToolResult {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"topics":[],"message":"暂无知识条目"}`)
	}

	// 按 tag 聚类
	tagMap := make(map[string][]string)   // tag → entry IDs
	entryTagMap := make(map[string][]string) // entry ID → tags
	entryTitle := make(map[string]string)

	for _, entry := range all {
		entryTitle[entry.ID] = entry.Title
		entryTagMap[entry.ID] = entry.Tags
		for _, tag := range entry.Tags {
			tagMap[tag] = append(tagMap[tag], entry.ID)
		}
	}

	// 构建主题节点（按条目数降序）
	var topics []TopicNode
	for tag, ids := range tagMap {
		topics = append(topics, TopicNode{
			Name:    tag,
			Count:   len(ids),
			Entries: ids,
			Tags:    []string{tag},
		})
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].Count != topics[j].Count {
			return topics[i].Count > topics[j].Count
		}
		return topics[i].Name < topics[j].Name
	})

	// 取 top 15
	if len(topics) > 15 {
		topics = topics[:15]
	}

	// 为每个主题找关联主题（共享条目的其他 tag）
	for i := range topics {
		relatedSet := make(map[string]int)
		for _, eid := range topics[i].Entries {
			for _, t := range entryTagMap[eid] {
				if t != topics[i].Name {
					relatedSet[t]++
				}
			}
		}
		// 将关联主题作为子节点（共享条目≥2）
		for t, count := range relatedSet {
			if count >= 2 {
				var eids []string
				for _, eid := range topics[i].Entries {
					for _, et := range entryTagMap[eid] {
						if et == t {
							eids = append(eids, eid)
							break
						}
					}
				}
				topics[i].Children = append(topics[i].Children, TopicNode{
					Name:    t,
					Count:   count,
					Entries: eids,
				})
			}
		}
		sort.Slice(topics[i].Children, func(a, b int) bool {
			return topics[i].Children[a].Count > topics[i].Children[b].Count
		})
	}

	result := map[string]interface{}{
		"topics":   topics,
		"count":    len(topics),
		"total":    len(all),
		"message": fmt.Sprintf("%d 条条目，%d 个主题", len(all), len(topics)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 时间线 ──────────────────────────────────── */

type TimelineBucket struct {
	Date    string   `json:"date"`
	Count   int      `json:"count"`
	Entries []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"entries"`
}

func KnowledgeTimeline(groupBy string) *ToolResult {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"buckets":[],"message":"暂无知识条目"}`)
	}

	if groupBy == "" {
		groupBy = "day"
	}

	bucketMap := make(map[string]*TimelineBucket)
	var bucketOrder []string

	for _, entry := range all {
		t := time.Unix(entry.CreatedAt, 0)
		var key string
		switch groupBy {
		case "week":
			year, week := t.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
		case "month":
			key = t.Format("2006-01")
		default:
			key = t.Format("2006-01-02")
		}

		if _, ok := bucketMap[key]; !ok {
			bucketMap[key] = &TimelineBucket{Date: key, Count: 0}
			bucketOrder = append(bucketOrder, key)
		}
		bucketMap[key].Count++
		bucketMap[key].Entries = append(bucketMap[key].Entries, struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{ID: entry.ID, Title: entry.Title})
	}

	// 按时间排序（先收集所有bucket再排序）
	type kv struct {
		key string
		b   *TimelineBucket
	}
	var sorted []kv
	for k, v := range bucketMap {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].key > sorted[j].key // 最新的在前
	})

	var buckets []TimelineBucket
	for _, kv := range sorted {
		buckets = append(buckets, *kv.b)
	}

	result := map[string]interface{}{
		"group_by": groupBy,
		"buckets":  buckets,
		"count":    len(buckets),
		"total":    len(all),
		"message": fmt.Sprintf("%d 条条目，%d 个%s区间", len(all), len(buckets), groupBy),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 知识图谱 ──────────────────────────────────── */

type GraphNode struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	SourceType string   `json:"source_type,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Refs       int      `json:"refs,omitempty"`  // 入度（被引用次数）
	Defs       int      `json:"defs,omitempty"`  // 出度（引用其他次数）
	Size       float64  `json:"size,omitempty"`  // 节点大小（基于引用数对数缩放）
}

type GraphEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

func KnowledgeGraph() *ToolResult {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"nodes":[],"edges":[],"count":0}`)
	}

	nodes := make([]GraphNode, 0, len(all))
	nodeSet := make(map[string]bool)
	var edges []GraphEdge

	for _, entry := range all {
		if entry.ID == "" {
			continue
		}
		nodes = append(nodes, GraphNode{
			ID:         entry.ID,
			Title:      entry.Title,
			SourceType: entry.SourceType,
			Tags:       entry.Tags,
		})
		nodeSet[entry.ID] = true

		for _, tl := range entry.TypedLinks {
			if entry.ID > tl.TargetID {
				continue
			}
			edges = append(edges, GraphEdge{
				Source:   entry.ID,
				Target:   tl.TargetID,
				Relation: string(tl.Type),
			})
		}
	}

	result := map[string]interface{}{
		"nodes":  nodes,
		"edges":  edges,
		"count":  len(nodes),
		"links":  len(edges),
		"message": fmt.Sprintf("%d 个节点，%d 条边", len(nodes), len(edges)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 知识自愈 ──────────────────────────────────── */

type HealSuggestion struct {
	Type       string   `json:"type"`                 // "merge" / "link" / "review"
	EntryA     string   `json:"entry_a_id"`           // 条目 A ID
	EntryB     string   `json:"entry_b_id,omitempty"` // 条目 B ID（merge/link 需要两个）
	TitleA     string   `json:"title_a"`
	TitleB     string   `json:"title_b,omitempty"`
	Similarity float64  `json:"similarity,omitempty"`  // BOW 余弦相似度
	Reason     string   `json:"reason"`
	Action     string   `json:"action"`                // "建议合并" / "建议关联" / "需人工复核"
}

func KnowledgeHeal(threshold float64, autoMerge bool) *ToolResult {
	if threshold <= 0 {
		threshold = 0.6
	}

	all := loadAllKnowledge()
	if len(all) < 2 {
		return successResult(`{"suggestions":[],"count":0,"message":"条目太少，无需自愈"}`)
	}

	// 预计算 BOW 向量
	type entryVec struct {
		entry *KnowledgeEntry
		vec   []float64
	}
	var vecs []entryVec
	for _, e := range all {
		if e.Status != "" && e.Status != "active" {
			continue
		}
		if e.Title == "" {
			continue // 跳过空 title（通常是残留噪音）
		}
		text := buildEmbedText(e)
		vec := textToVector(text)
		vecs = append(vecs, entryVec{entry: e, vec: vec})
	}

	var suggestions []HealSuggestion
	var autoMerged []map[string]interface{}
	mergeThreshold := 0.85
	linkThreshold := threshold

	// 已被自动合并的 ID 集合（跳过后续配对）
	mergedIDs := make(map[string]bool)

	for i := 0; i < len(vecs); i++ {
		for j := i + 1; j < len(vecs); j++ {
			a, b := vecs[i], vecs[j]

			// 跳过同一条或已合并的
			if a.entry.ID == b.entry.ID || mergedIDs[a.entry.ID] || mergedIDs[b.entry.ID] {
				continue
			}

			sim := cosineSimilarity(a.vec, b.vec)

			// 极高相似度 → 重复候选
			if sim >= mergeThreshold {
				// 自动合并条件：两个条目 HitCount 均为 0（低访问量，低风险）
				if autoMerge && a.entry.HitCount == 0 && b.entry.HitCount == 0 {
					// 保留较长的条目作为 target
					sourceID, targetID := a.entry.ID, b.entry.ID
					if len(a.entry.Summary) > len(b.entry.Summary) {
						sourceID, targetID = b.entry.ID, a.entry.ID
					}
					mergeResult := KnowledgeMerge(sourceID, targetID)
					if mergeResult.Success {
						mergedIDs[sourceID] = true
						autoMerged = append(autoMerged, map[string]interface{}{
							"source_id": sourceID,
							"target_id": targetID,
							"title":     a.entry.Title,
							"similarity": sim,
						})
						fmt.Printf("[heal] 自动合并 %s → %s (%.0f%%)\n", sourceID, targetID, sim*100)
					}
					continue
				}

				// 不满足自动合并条件，生成建议
				if a.entry.ID < b.entry.ID {
					suggestions = append(suggestions, HealSuggestion{
						Type:       "merge",
						EntryA:     a.entry.ID,
						EntryB:     b.entry.ID,
						TitleA:     a.entry.Title,
						TitleB:     b.entry.Title,
						Similarity: sim,
						Reason:     fmt.Sprintf("BOW 相似度 %.0f%%，建议合并或确认是否为不同条目", sim*100),
						Action:     "建议合并",
					})
				}
				continue
			}

			// 高相似度 → 检查是否有 TypedLinks
			if sim >= linkThreshold {
				hasLink := false
				for _, tl := range a.entry.TypedLinks {
					if tl.TargetID == b.entry.ID {
						hasLink = true
						break
					}
				}
				if !hasLink {
					for _, tl := range b.entry.TypedLinks {
						if tl.TargetID == a.entry.ID {
							hasLink = true
							break
						}
					}
				}
				if !hasLink {
					if a.entry.ID < b.entry.ID {
						suggestions = append(suggestions, HealSuggestion{
							Type:       "link",
							EntryA:     a.entry.ID,
							EntryB:     b.entry.ID,
							TitleA:     a.entry.Title,
							TitleB:     b.entry.Title,
							Similarity: sim,
							Reason:     fmt.Sprintf("内容高度相关（%.0f%%），建议建立 TypedLinks", sim*100),
							Action:     "建议关联",
						})
					}
				}
			}
		}
	}

	// 按相似度降序
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Similarity > suggestions[j].Similarity
	})

	// 上限 20 条
	if len(suggestions) > 20 {
		suggestions = suggestions[:20]
	}

	msg := fmt.Sprintf("扫描 %d 个条目，发现 %d 个待处理项", len(all), len(suggestions))
	if len(autoMerged) > 0 {
		msg += fmt.Sprintf("，自动合并 %d 对", len(autoMerged))
	}

	result := map[string]interface{}{
		"suggestions":  suggestions,
		"auto_merged":  autoMerged,
		"count":        len(suggestions),
		"merged_count": len(autoMerged),
		"total":        len(all),
		"threshold":    threshold,
		"message":      msg,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 批量去重 ──────────────────────────────────── */

// KBDedup 一键去重：高阈值扫描 + 自动合并沉睡条目 + 返回待审查列表。
func KBDedup() *ToolResult {
	// 第一轮：90% 阈值 + 自动合并（沉睡条目）
	healResult := KnowledgeHeal(0.9, true)
	var healOut struct {
		Suggestions  []HealSuggestion    `json:"suggestions"`
		AutoMerged   []map[string]interface{} `json:"auto_merged"`
		Count        int                 `json:"count"`
		MergedCount  int                 `json:"merged_count"`
		Total        int                 `json:"total"`
	}
	json.Unmarshal([]byte(healResult.Output), &healOut)

	// 第二轮：80% 阈值，不自动合并，找出待审查项
	reviewResult := KnowledgeHeal(0.8, false)
	var reviewOut struct {
		Suggestions []HealSuggestion `json:"suggestions"`
		Count       int              `json:"count"`
	}
	json.Unmarshal([]byte(reviewResult.Output), &reviewOut)

	// 过滤掉已在第一轮合并的条目
	mergedIDs := make(map[string]bool)
	for _, m := range healOut.AutoMerged {
		if sid, ok := m["source_id"].(string); ok {
			mergedIDs[sid] = true
		}
	}

	var reviewItems []HealSuggestion
	for _, s := range reviewOut.Suggestions {
		if s.Type == "merge" && (mergedIDs[s.EntryA] || mergedIDs[s.EntryB]) {
			continue // 已合并，跳过
		}
		reviewItems = append(reviewItems, s)
	}

	// 上限 10 条待审查
	if len(reviewItems) > 10 {
		reviewItems = reviewItems[:10]
	}

	result := map[string]interface{}{
		"auto_merged":    healOut.AutoMerged,
		"merged_count":   healOut.MergedCount,
		"review_items":   reviewItems,
		"review_count":   len(reviewItems),
		"total":          healOut.Total,
		"message":        fmt.Sprintf("去重完成：自动合并 %d 对，待审查 %d 项", healOut.MergedCount, len(reviewItems)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 检索反馈 ──────────────────────────────────── */

// KnowledgeFeedback 记录用户对知识条目的反馈，调整 UtilityScore。
// direction: "up"=有用(+1), "down"=没用(-1), "reset"=归零。
func KnowledgeFeedback(id, direction string) *ToolResult {
	if id == "" {
		return errorResult("id 不能为空")
	}

	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()

	entry := loadKnowledge(id)
	if entry == nil {
		return errorResult(fmt.Sprintf("知识条目 %s 未找到", id))
	}

	switch direction {
	case "up":
		entry.UtilityScore++
	case "down":
		entry.UtilityScore--
	case "reset":
		entry.UtilityScore = 0
	default:
		return errorResult("direction 必须是 up/down/reset")
	}

	saveKnowledge(entry)
	return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","utility_score":%.0f,"message":"反馈已记录"}`, id, entry.Title, entry.UtilityScore))
}

func registerKnowledgeTools() {
	Register("knowledge_add", "添加结构化知识条目（统一 memory schema，含 tags/topics/tasks）。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"required": []string{"source_type", "title", "summary"},
			"properties": map[string]interface{}{
				"source_type": stringParam("来源类型: chat|article|idea|web|file|note|codex|claude_memory"),
				"title":       stringParam("知识条目标题"),
				"summary":     stringParam("内容摘要（一句话到一段话）"),
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "标签列表，用于分类和检索",
					"items":       map[string]interface{}{"type": "string"},
				},
				"topics": map[string]interface{}{
					"type":        "array",
					"description": "所属主题列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "从此内容中提取的行动项/待办",
					"items":       map[string]interface{}{"type": "string"},
				},
				"links": map[string]interface{}{
					"type":        "array",
					"description": "关联的 memory/知识 ID 列表（旧格式）",
					"items":       map[string]interface{}{"type": "string"},
				},
				"typed_links": map[string]interface{}{
					"type":        "array",
					"description": "有类型的关联链接（新格式）",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"target_id", "type"},
						"properties": map[string]interface{}{
							"target_id": stringParam("关联的目标条目 ID"),
							"type": stringParam("链接类型: related|contradicts|supersedes|supports"),
							"reason": stringParam("建链原因"),
						},
					},
				},
				"namespace": stringParam("所属空间: default/workspace/project。不同空间隔离，默认 default"),
				"raw_ref": stringParam("原始来源引用，如 URL 或文件路径"),
				"content": map[string]interface{}{
					"oneOf": []interface{}{
						map[string]interface{}{"type": "string"},
						map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"description": "完整内容（字符串或字符串数组）",
				},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeAdd(
				strArg(args, "source_type"),
				strArg(args, "title"),
				strArg(args, "summary"),
				strSliceArg(args, "tags"),
				strSliceArg(args, "topics"),
				strSliceArg(args, "tasks"),
				strSliceArg(args, "links"),
				strArg(args, "raw_ref"),
				contentOrJoin(args, "content"),
				strArg(args, "namespace"),
			)
		},
	)

	Register("knowledge_search", "按关键词搜索知识条目（匹配 title/summary/content/tags/topics）。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"required": []string{"keyword"},
			"properties": map[string]interface{}{
				"keyword": stringParam("搜索关键词"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeSearch(strArg(args, "keyword"))
		},
	)

		Register("knowledge_remember", "记录一条记忆（轻量写入知识库，source_type=memory）。Agent 自主记录关键事实、决策、偏好。",
			map[string]interface{}{
				"type":     "object",
				"required": []string{"title", "summary"},
				"properties": map[string]interface{}{
					"title":   stringParam("记忆标题，简洁描述这条事实"),
					"summary": stringParam("记忆内容（一句话到一段话）"),
					"tags": map[string]interface{}{
						"type":        "array",
						"description": "标签列表",
						"items":       map[string]interface{}{"type": "string"},
					},
					"expires_in_days": map[string]interface{}{
						"type":        "integer",
						"description": "过期天数，到期后不参与检索。0=永久（默认0）。",
					},
					"namespace": stringParam("所属空间：空=default（智能体主知识库），claude_dev=Claude Code 开发会话专用（与主库隔离）"),
				},
			},
			func(args map[string]interface{}) *ToolResult {
				expDays, _ := args["expires_in_days"].(float64)
				return KnowledgeRemember(
					strArg(args, "title"),
					strArg(args, "summary"),
					strArg(args, "content_type"),
					strSliceArg(args, "tags"),
					int(expDays),
					strArg(args, "namespace"),
				)
			},
		)

	Register("knowledge_list", "列出所有知识条目，可按来源类型、天数、namespace 过滤。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"source_type":  stringParam("可选的来源类型过滤"),
				"days":         intParam("最近 N 天（0=全部）"),
				"content_type": stringParam("内容类型过滤：work_record|decision|lesson|fact"),
				"namespace":    stringParam("空间过滤：留空=全部，claude_dev=仅 Claude Code 开发记忆"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			days, _ := args["days"].(float64)
			return KnowledgeListNS(strArg(args, "source_type"), int(days), strArg(args, "content_type"), strArg(args, "namespace"))
		},
	)

	Register("knowledge_get", "获取指定知识条目的完整内容。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": stringParam("知识条目 ID"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeGet(strArg(args, "id"))
		},
	)

	Register("knowledge_delete", "删除指定知识条目。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": stringParam("要删除的知识条目 ID"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeDelete(strArg(args, "id"))
		},
	)

	Register("knowledge_update", "更新现有知识条目的字段（保留未提供的字段）。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id":          stringParam("要更新的知识条目 ID"),
				"source_type": stringParam("来源类型: chat|article|idea|web|file|note|codex|claude_memory"),
				"title":       stringParam("知识条目标题"),
				"summary":     stringParam("内容摘要"),
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "标签列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"topics": map[string]interface{}{
					"type":        "array",
					"description": "所属主题列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "行动项/待办列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"links": map[string]interface{}{
					"type":        "array",
					"description": "关联 ID 列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"typed_links": map[string]interface{}{
					"type":        "array",
					"description": "有类型的关联链接（新格式）",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"target_id", "type"},
						"properties": map[string]interface{}{
							"target_id": stringParam("关联的目标条目 ID"),
							"type":      stringParam("链接类型: related|contradicts|supersedes|supports"),
							"reason":    stringParam("建链原因"),
						},
					},
				},
				"raw_ref": stringParam("原始来源引用"),
				"content": stringParam("完整内容"),
				"status":  stringParam("条目状态: active|archived|expired，空=active"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			id, _ := args["id"].(string)
			fields := knowledgeUpdateFields(args)
			return KnowledgeUpdate(id, fields)
		},
	)

	Register("knowledge_suggest_links", "为指定知识条目推荐关联条目（基于标签/主题/关键词匹配）。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id":          stringParam("知识条目 ID"),
				"max_results": intParam("最大返回候选数，默认 10"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			id, _ := args["id"].(string)
			maxResults, _ := args["max_results"].(float64)
			return KnowledgeSuggestLinks(id, int(maxResults))
		},
	)
	Register("knowledge_dedupe", "查找可能重复的知识条目（按 raw_ref/title/tags 匹配）。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"id":      stringParam("知识条目 ID（查找与此条目标题/标签相似的条目）"),
				"raw_ref": stringParam("原始来源引用（查找同一来源的条目）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeDedupe(strArg(args, "id"), strArg(args, "raw_ref"))
		},
	)

	Register("knowledge_merge", "合并两个知识条目（tags/topics/tasks/links/content 合并后删除源条目）。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"required": []string{"source_id", "target_id"},
			"properties": map[string]interface{}{
				"source_id": stringParam("源条目 ID（合并后将删除）"),
				"target_id": stringParam("目标条目 ID（合并到此处）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeMerge(strArg(args, "source_id"), strArg(args, "target_id"))
		},
	)
	Register("knowledge_confirm_links", "确认关联建议：将一个或多个目标条目 ID 写入源条目的 links 字段。",
		map[string]interface{}{
			"type":     "object",
			"additionalProperties": true,
			"required": []string{"id", "link_ids"},
			"properties": map[string]interface{}{
				"id":       stringParam("源知识条目 ID"),
				"link_ids": map[string]interface{}{
					"type":        "array",
					"description": "要关联的目标条目 ID 列表",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			id, _ := args["id"].(string)
			return KnowledgeConfirmLinks(id, strSliceArg(args, "link_ids"))
		},
	)


	Register("knowledge_topic_map", "生成知识条目主题图谱（按 tag 聚类，显示关联子主题）。",
		map[string]interface{}{
			"type":       "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeTopicMap()
		},
	)

	Register("knowledge_timeline", "按时间线查看知识条目（按 day/week/month 分组）。",
		map[string]interface{}{
			"type": "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"group_by": stringParam("分组方式: day | week | month，默认 day"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeTimeline(strArg(args, "group_by"))
		},
	)

	Register("knowledge_reindex", "为所有无 embedding 的知识条目计算语义向量。需要配置 EMBEDDING_ENDPOINT。",
		map[string]interface{}{
			"type":       "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeReindex()
		},
	)

	Register("knowledge_history", "查看指定知识条目的修改历史版本列表。每次 knowledge_update 自动保存快照。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": stringParam("知识条目 ID"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeHistory(strArg(args, "id"))
		},
	)

	Register("knowledge_version_get", "获取指定知识条目的特定历史版本内容。先用 knowledge_history 查看可用版本。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id", "version"},
			"properties": map[string]interface{}{
				"id":      stringParam("知识条目 ID"),
				"version": stringParam("版本文件名，如 v1712345678.json"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeVersionGet(strArg(args, "id"), strArg(args, "version"))
		},
	)

	Register("knowledge_heal", "知识自愈扫描：用BOW向量对比检测高相似度条目，找出应合并或应建立TypedLinks的候选。threshold默认0.6。auto_merge=true时自动合并HitCount=0的高相似条目。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"threshold":  intParam("相似度阈值 0-100，默认 60。越高越严格"),
				"auto_merge": boolParam("自动合并 HitCount=0 的高相似条目（默认 false）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			th := 0.6
			if t, ok := args["threshold"].(float64); ok && t > 0 {
				th = t / 100.0
			}
			am := false
			if v, ok := args["auto_merge"].(bool); ok {
				am = v
			}
			return KnowledgeHeal(th, am)
		},
	)

	Register("kb_dedup", "知识库去重：高阈值(90%)扫描+自动合并沉睡条目+返回待审查列表。一键完成去重。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KBDedup()
		},
	)

	Register("knowledge_feedback", "记录对知识条目的反馈。up=有用(+1分), down=没用(-1分), reset=归零。UtilityScore影响检索排名。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"id", "direction"},
			"properties": map[string]interface{}{
				"id":        stringParam("知识条目 ID"),
				"direction": stringParam("反馈方向: up（有用）/ down（没用）/ reset（归零）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeFeedback(strArg(args, "id"), strArg(args, "direction"))
		},
	)

	Register("knowledge_graph", "生成知识图谱（nodes+edges JSON），用于前端 D3.js 可视化。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeGraph()
		},
	)

	Register("knowledge_graph_local", "以指定条目为中心构建局部知识图谱（N 跳扩展）。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":    stringParam("条目 ID"),
				"depth": stringParam("递归深度（1-6，默认 2）"),
			},
			"required": []string{"id"},
		},
		func(args map[string]interface{}) *ToolResult {
			id, _ := args["id"].(string)
			depth := 2
			if v, ok := args["depth"].(float64); ok {
				depth = int(v)
			}
			nodes, links, err := BuildLocalGraph(id, depth)
			if err != nil {
				return ErrorResult(err.Error())
			}
			b, _ := json.MarshalIndent(map[string]interface{}{
				"nodes": nodes, "links": links, "count": len(nodes),
			}, "", "  ")
			return SuccessResult(string(b))
		},
	)

	Register("knowledge_graph_global", "构建全库知识图谱，可按最小引用数过滤。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"min_refs": stringParam("最小引用数（低于此值不显示，默认 0 全部显示）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			minRefs := 0
			if v, ok := args["min_refs"].(float64); ok {
				minRefs = int(v)
			}
			nodes, links, err := BuildGlobalGraph(minRefs)
			if err != nil {
				return ErrorResult(err.Error())
			}
			b, _ := json.MarshalIndent(map[string]interface{}{
				"nodes": nodes, "links": links, "count": len(nodes),
			}, "", "  ")
			return SuccessResult(string(b))
		},
	)

	Register("knowledge_probe", "检索质量探针：随机采样知识库条目，测量 L0 关键词和 L1 语义检索的 recall@3。结果追加到 probe_history.jsonl。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeProbe()
		},
	)

	Register("knowledge_backup", "备份知识库到带时间戳的目录。保留最近 7 份，自动清理旧备份。可选 dest 参数覆盖默认目录。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"dest": stringParam("备份目标父目录（可选，默认 ~/.hermes/backups）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeBackup(strArg(args, "dest"))
		},
	)
}

func knowledgeUpdateFields(args map[string]interface{}) map[string]interface{} {
	fields := make(map[string]interface{})
	for _, key := range []string{"source_type", "title", "summary", "raw_ref", "content", "status"} {
		raw, ok := args[key]
		if !ok || raw == nil {
			continue
		}
		if v, ok := raw.(string); ok && v != "" {
			fields[key] = v
		}
	}
	for _, key := range []string{"tags", "topics", "tasks", "links"} {
		raw, ok := args[key]
		if !ok || raw == nil {
			continue
		}
		fields[key] = strSliceArg(args, key)
	}
	if raw, ok := args["typed_links"]; ok && raw != nil {
		fields["typed_links"] = raw
	}
	return fields
}

func contentOrJoin(args map[string]interface{}, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	if arr, ok := raw.([]interface{}); ok {
		var parts []string
		for _, v := range arr {
			if s, ok := v.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", raw)
}

func strSliceArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
