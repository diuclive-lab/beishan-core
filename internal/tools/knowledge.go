package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"beishan/internal/observatory"
)

/* ─── TypedLink 有类型的知识关联 ──────────────── */

// LinkType 链接类型（代码定义的枚举，不是 LLM 决策）
type LinkType string

const (
	LinkRelated     LinkType = "related"     // 相关（现有 autoLink）
	LinkContradicts LinkType = "contradicts" // 矛盾
	LinkSupersedes  LinkType = "supersedes"  // 替代/演进
	LinkSupports    LinkType = "supports"    // 支持/佐证
)

// TypedLink 有类型的关联链接
type TypedLink struct {
	TargetID string   `json:"target_id"`
	Type     LinkType `json:"type"`
	Reason   string   `json:"reason"` // 为什么建这个链接（可审计）
}

/* ─── KnowledgeEntry 统一知识条目 ──────────────── */

type KnowledgeEntry struct {
	ID             string      `json:"id"`
	SourceType     string      `json:"source_type"` // chat|article|idea|web|file|note|codex|claude_memory
	Title          string      `json:"title"`
	Summary        string      `json:"summary"`
	Tags           []string    `json:"tags"`
	Topics         []string    `json:"topics"`
	Tasks          []string    `json:"tasks"` // 提取的任务/行动项
	CreatedAt      int64       `json:"created_at"`
	Links          []string    `json:"links"`                      // 关联的 memory/知识 ID（旧格式，兼容）
	TypedLinks     []TypedLink `json:"typed_links,omitempty"`      // 有类型的关联链接（新格式）
	RawRef         string      `json:"raw_ref"`                    // 原始来源引用
	Content        string      `json:"content,omitempty"`          // 完整内容（可选）
	Embedding      []float64   `json:"embedding,omitempty"`        // 语义嵌入向量，用于语义检索
	Ephemeral      bool        `json:"ephemeral,omitempty"`        // 临时记忆，到期不参与检索
	ExpiresAt      int64       `json:"expires_at,omitempty"`       // 过期时间戳，0=永久
	Status         string      `json:"status,omitempty"`           // active/archived/expired，空=active
	LastAccessedAt int64       `json:"last_accessed_at,omitempty"` // 最后被检索/引用的时间戳
	Namespace      string      `json:"namespace,omitempty"`        // 所属空间: default/workspace-a/project-b，空=default
	Verified       bool        `json:"verified,omitempty"`         // 是否经过事实核查
	VerifiedAt     int64       `json:"verified_at,omitempty"`      // 核查时间
	HitCount       int64       `json:"hit_count,omitempty"`        // 被检索命中次数，用于排序加权
	UtilityScore   float64     `json:"utility_score,omitempty"`    // 用户反馈评分: +1/-1/0，越用越准
	ContentType    string      `json:"content_type,omitempty"`     // work_record | decision | lesson | fact

	// BlockContents 块级存储的文档块内容列表（检索时匹配用，不序列化到 JSON 文件）。
	BlockContents []string `json:"-"`
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
	// 优先走当前 StorageAdapter（BlockStorage 时走内存 index）。
	// JSONStorage.GetEntry 内部调用此函数，用 isLoadingFromJSON 避免循环。
	if !isLoadingFromJSON.Load() {
		if entry := Storage().GetEntry(id); entry != nil {
			return entry
		}
	}
	// 降级：直接读 JSON 文件（JSONStorage 路径 / 兜底）
	data, err := os.ReadFile(knowledgePath(id))
	if err != nil {
		return nil
	}
	var entry KnowledgeEntry
	json.Unmarshal(data, &entry)
	return &entry
}

// isLoadingFromJSON 防止 JSONStorage.GetEntry → loadKnowledge → Storage().GetEntry 循环。
var isLoadingFromJSON atomic.Bool

// prepareEntry 入库前自动补全（SourceType/Tags/Embedding），纯副作用，不写磁盘。
func prepareEntry(entry *KnowledgeEntry) {
	if entry.SourceType == "" {
		entry.SourceType = inferSourceType(entry)
	}
	if len(entry.Tags) == 0 {
		entry.Tags = autoExtractTags(entry.Title, entry.Summary)
	}
	if embeddingEnabled() && len(entry.Embedding) == 0 {
		text := entry.Title + " " + entry.Summary
		if emb, ok := tryEmbedding(text); ok {
			entry.Embedding = emb
		}
	}
}

func saveKnowledge(entry *KnowledgeEntry) {
	initKnowledgeDir()
	prepareEntry(entry)
	// 委托给当前存储适配器（BlockStorage 或 JSONStorage）
	Storage().SaveEntry(entry) //nolint:errcheck
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
		observatory.SafeGo("knowledge.autoLink "+entry.ID, func() {
			autoLinkEntry(entry.ID, title, summary, tags, topics)
		})
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
	for _, entry := range Storage().AllEntries() {
		if entry.RawRef == rawRef {
			return entry
		}
	}
	return nil
}

func strFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// linksToTypedLinks 将旧格式的 string ID 列表转换为 TypedLinks。
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	entry := Storage().GetEntry(id)
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

func findEntry(entries []*KnowledgeEntry, id string) *KnowledgeEntry {
	for _, e := range entries {
		if e.ID == id {
			return e
		}
	}
	return nil
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
