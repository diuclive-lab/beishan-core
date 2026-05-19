package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ─── KnowledgeEntry 统一知识条目 ──────────────── */

type KnowledgeEntry struct {
	ID           string   `json:"id"`
	SourceType   string   `json:"source_type"`              // 来源：chat|article|idea|web|file|note|codex|claude_memory
	KnowledgeType string  `json:"knowledge_type,omitempty"` // 认知类型：Principle|ADR|Lesson|AntiPattern|Hotspot|Telemetry
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Tags         []string `json:"tags"`
	Topics       []string `json:"topics"`
	Tasks        []string `json:"tasks"` // 提取的任务/行动项
	Confidence   float64  `json:"confidence,omitempty"` // 置信度 0-1
	Importance   string   `json:"importance,omitempty"` // 重要性：high|medium|low
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at,omitempty"`
	Links        []string `json:"links"`             // 关联的 memory/知识 ID
	RawRef       string   `json:"raw_ref"`           // 原始来源引用
	Content      string   `json:"content,omitempty"` // 完整内容（可选）
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

func KnowledgeAdd(sourceType, knowledgeType, title, summary string, tags, topics, tasks, links []string, rawRef, content string, confidence float64, importance string) *ToolResult {
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
		ID:            newKnowledgeID(),
		SourceType:    sourceType,
		KnowledgeType: knowledgeType,
		Title:         title,
		Summary:       summary,
		Tags:          tags,
		Topics:        topics,
		Tasks:         tasks,
		Confidence:    confidence,
		Importance:    importance,
		CreatedAt:     now,
		Links:         links,
		RawRef:        rawRef,
		Content:       content,
	}
	saveKnowledge(entry)

	return successResult(fmt.Sprintf(`{"id":"%s","title":"%s","message":"知识已入库"}`, entry.ID, title))
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
	initKnowledgeDir()
	entries, _ := os.ReadDir(knowledgeDir)

	var results []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		entry := loadKnowledge(strings.TrimSuffix(e.Name(), ".json"))
		if entry == nil {
			continue
		}

		kw := strings.ToLower(keyword)
		if strings.Contains(strings.ToLower(entry.Title), kw) ||
			strings.Contains(strings.ToLower(entry.Summary), kw) ||
			strings.Contains(strings.ToLower(entry.Content), kw) ||
			matchesTag(entry.Tags, kw) ||
			matchesTag(entry.Topics, kw) {
			results = append(results, fmt.Sprintf("[%s] %s | %s", entry.ID, entry.Title, truncateStr(entry.Summary, 120)))
		}
	}

	if len(results) == 0 {
		return successResult("未找到匹配的知识条目。")
	}

	sort.Strings(results)
	return successResult(strings.Join(results, "\n"))
}

func KnowledgeList(sourceType, knowledgeType string, days int) *ToolResult {
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
		if knowledgeType != "" && entry.KnowledgeType != knowledgeType {
			continue
		}
		if days > 0 && time.Unix(entry.CreatedAt, 0).Before(cutoff) {
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
		created := time.Unix(e.CreatedAt, 0).Format("01-02 15:04")
		tags := strings.Join(e.Tags, ", ")
		ktype := e.KnowledgeType
		if ktype == "" {
			ktype = "-"
		}
		sb.WriteString(fmt.Sprintf("%s [%s|%s] %s — %s (tags: %s)\n",
			e.ID, e.SourceType, ktype, e.Title, created, tags))
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
	if v, ok := fields["knowledge_type"].(string); ok && v != "" {
		entry.KnowledgeType = v
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
	if v, ok := fields["confidence"].(float64); ok {
		entry.Confidence = v
		changed = true
	}
	if v, ok := fields["importance"].(string); ok && v != "" {
		entry.Importance = v
		changed = true
	}
	if v, ok := fields["raw_ref"].(string); ok {
		entry.RawRef = v
		changed = true
	}
	if v, ok := fields["content"].(string); ok {
		entry.Content = v
		changed = true
	}
	if changed {
		entry.UpdatedAt = time.Now().Unix()
	}

	if !changed {
		return successResult(fmt.Sprintf(`{"id":"%s","message":"无需更新"}`, id))
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
	// 合并 links
	target.Links = unionStrings(target.Links, source.Links)
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
		found := false
		for _, existing := range entry.Links {
			if existing == lid {
				found = true
				break
			}
		}
		if !found {
			entry.Links = append(entry.Links, lid)
			added++
		}
	}

	if added == 0 {
		return successResult(fmt.Sprintf(`{"id":"%s","message":"所有链接已存在，无需添加","links_count":%d}`, id, len(entry.Links)))
	}

	saveKnowledge(entry)

	b, _ := json.MarshalIndent(entry, "", "  ")
	return successResult(fmt.Sprintf(`{"id":"%s","message":"已确认 %d 条关联","links_count":%d,"entry":%s}`, id, added, len(entry.Links), string(b)))
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
	initKnowledgeDir()
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		return nil
	}
	var result []*KnowledgeEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		entry := loadKnowledge(strings.TrimSuffix(e.Name(), ".json"))
		if entry != nil {
			result = append(result, entry)
		}
	}
	return result
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
func registerKnowledgeTools() {
	Register("knowledge_add", "添加结构化知识条目（统一 memory schema，含 tags/topics/tasks/knowledge_type）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"source_type", "title", "summary"},
			"properties": map[string]interface{}{
				"source_type": stringParam("来源类型: chat|article|idea|web|file|note|codex|claude_memory"),
				"knowledge_type": stringParam("认知类型: Principle|ADR|Lesson|AntiPattern|Hotspot|Telemetry"),
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
					"description": "关联的 memory/知识 ID 列表",
					"items":       map[string]interface{}{"type": "string"},
				},
				"confidence": map[string]interface{}{
					"type":        "number",
					"description": "置信度 0-1",
				},
				"importance": stringParam("重要性: high|medium|low"),
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
			confidence, _ := args["confidence"].(float64)
			return KnowledgeAdd(
				strArg(args, "source_type"),
				strArg(args, "knowledge_type"),
				strArg(args, "title"),
				strArg(args, "summary"),
				strSliceArg(args, "tags"),
				strSliceArg(args, "topics"),
				strSliceArg(args, "tasks"),
				strSliceArg(args, "links"),
				strArg(args, "raw_ref"),
				contentOrJoin(args, "content"),
				confidence,
				strArg(args, "importance"),
			)
		},
	)

	Register("knowledge_search", "按关键词搜索知识条目（匹配 title/summary/content/tags/topics）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"keyword"},
			"properties": map[string]interface{}{
				"keyword": stringParam("搜索关键词"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeSearch(strArg(args, "keyword"))
		},
	)

	Register("knowledge_list", "列出所有知识条目，可按来源类型、认知类型和天数过滤。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_type":    stringParam("可选的来源类型过滤"),
				"knowledge_type": stringParam("可选的认知类型过滤: Principle|ADR|Lesson|AntiPattern|Hotspot|Telemetry"),
				"days":           intParam("最近 N 天（0=全部）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			days, _ := args["days"].(float64)
			return KnowledgeList(strArg(args, "source_type"), strArg(args, "knowledge_type"), int(days))
		},
	)

	Register("knowledge_get", "获取指定知识条目的完整内容。",
		map[string]interface{}{
			"type":     "object",
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
			"type":     "object",
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
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id":            stringParam("要更新的知识条目 ID"),
				"source_type":   stringParam("来源类型: chat|article|idea|web|file|note|codex|claude_memory"),
				"knowledge_type": stringParam("认知类型: Principle|ADR|Lesson|AntiPattern|Hotspot|Telemetry"),
				"title":         stringParam("知识条目标题"),
				"summary":       stringParam("内容摘要"),
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
				"confidence": map[string]interface{}{
					"type":        "number",
					"description": "置信度 0-1",
				},
				"importance": stringParam("重要性: high|medium|low"),
				"raw_ref":    stringParam("原始来源引用"),
				"content":    stringParam("完整内容"),
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
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeTopicMap()
		},
	)

	Register("knowledge_timeline", "按时间线查看知识条目（按 day/week/month 分组）。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"group_by": stringParam("分组方式: day | week | month，默认 day"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeTimeline(strArg(args, "group_by"))
		},
	)
}

func knowledgeUpdateFields(args map[string]interface{}) map[string]interface{} {
	fields := make(map[string]interface{})
	for _, key := range []string{"source_type", "knowledge_type", "title", "summary", "importance", "raw_ref", "content"} {
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
	if v, ok := args["confidence"].(float64); ok {
		fields["confidence"] = v
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
