package tools

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

/* ─── 知识自愈 ──────────────────────────────────── */

type HealSuggestion struct {
	Type       string  `json:"type"`                 // "merge" / "link" / "review"
	EntryA     string  `json:"entry_a_id"`           // 条目 A ID
	EntryB     string  `json:"entry_b_id,omitempty"` // 条目 B ID（merge/link 需要两个）
	TitleA     string  `json:"title_a"`
	TitleB     string  `json:"title_b,omitempty"`
	Similarity float64 `json:"similarity,omitempty"` // BOW 余弦相似度
	Reason     string  `json:"reason"`
	Action     string  `json:"action"` // "建议合并" / "建议关联" / "需人工复核"
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
							"source_id":  sourceID,
							"target_id":  targetID,
							"title":      a.entry.Title,
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
		Suggestions []HealSuggestion         `json:"suggestions"`
		AutoMerged  []map[string]interface{} `json:"auto_merged"`
		Count       int                      `json:"count"`
		MergedCount int                      `json:"merged_count"`
		Total       int                      `json:"total"`
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
		"auto_merged":  healOut.AutoMerged,
		"merged_count": healOut.MergedCount,
		"review_items": reviewItems,
		"review_count": len(reviewItems),
		"total":        healOut.Total,
		"message":      fmt.Sprintf("去重完成：自动合并 %d 对，待审查 %d 项", healOut.MergedCount, len(reviewItems)),
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
