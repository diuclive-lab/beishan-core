package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

/* ─── 主题图谱 ──────────────────────────────────── */

type TopicNode struct {
	Name     string      `json:"name"`
	Count    int         `json:"count"`
	Entries  []string    `json:"entries"` // IDs
	Tags     []string    `json:"tags"`
	Children []TopicNode `json:"children,omitempty"`
}

func KnowledgeTopicMap() *ToolResult {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"topics":[],"message":"暂无知识条目"}`)
	}

	// 按 tag 聚类
	tagMap := make(map[string][]string)      // tag → entry IDs
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
		"topics":  topics,
		"count":   len(topics),
		"total":   len(all),
		"message": fmt.Sprintf("%d 条条目，%d 个主题", len(all), len(topics)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── 时间线 ──────────────────────────────────── */

type TimelineBucket struct {
	Date    string `json:"date"`
	Count   int    `json:"count"`
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
		"message":  fmt.Sprintf("%d 条条目，%d 个%s区间", len(all), len(buckets), groupBy),
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
	Refs       int      `json:"refs,omitempty"` // 入度（被引用次数）
	Defs       int      `json:"defs,omitempty"` // 出度（引用其他次数）
	Size       float64  `json:"size,omitempty"` // 节点大小（基于引用数对数缩放）
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
		"nodes":   nodes,
		"edges":   edges,
		"count":   len(nodes),
		"links":   len(edges),
		"message": fmt.Sprintf("%d 个节点，%d 条边", len(nodes), len(edges)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}
