package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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
//  1. 确定性建链：基于标签重叠和标题/摘要关键词匹配
//  2. 语义建链：LLM 分析关系类型（写入时离线，不违反宪法）
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
