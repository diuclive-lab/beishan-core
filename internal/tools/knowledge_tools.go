package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

func registerKnowledgeTools() {
	Register("knowledge_add", "添加结构化知识条目（统一 memory schema，含 tags/topics/tasks）。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"source_type", "title", "summary"},
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
							"type":      stringParam("链接类型: related|contradicts|supersedes|supports"),
							"reason":    stringParam("建链原因"),
						},
					},
				},
				"namespace": stringParam("所属空间: default/workspace/project。不同空间隔离，默认 default"),
				"raw_ref":   stringParam("原始来源引用，如 URL 或文件路径"),
				"content": map[string]interface{}{
					"oneOf": []interface{}{
						map[string]interface{}{"type": "string"},
						map[string]interface{}{
							"type":  "array",
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
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"keyword"},
			"properties": map[string]interface{}{
				"keyword":   stringParam("搜索关键词"),
				"namespace": stringParam("命名空间过滤：留空=全库，claude_dev=仅 Claude Code 开发记忆"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeSearch(strArg(args, "keyword"), strArg(args, "namespace"))
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
			"required":             []string{"id"},
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
			"required":             []string{"id"},
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
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"id"},
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
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"id"},
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
			"type":                 "object",
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
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"source_id", "target_id"},
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
			"type":                 "object",
			"additionalProperties": true,
			"required":             []string{"id", "link_ids"},
			"properties": map[string]interface{}{
				"id": stringParam("源知识条目 ID"),
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
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return KnowledgeTopicMap()
		},
	)

	Register("knowledge_timeline", "按时间线查看知识条目（按 day/week/month 分组）。",
		map[string]interface{}{
			"type":                 "object",
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
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
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
