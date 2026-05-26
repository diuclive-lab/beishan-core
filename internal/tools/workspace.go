package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func init() {
	registerWorkspaceTools()
}

func registerWorkspaceTools() {
	Register("workspace_save", "保存当前工作状态，下次对话自动加载，确保上下文连续。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project":    map[string]interface{}{"type": "string", "description": "项目名（如 beishan-core / FangLab / hermes）"},
				"task":       map[string]interface{}{"type": "string", "description": "当前正在做的任务简述"},
				"status":     map[string]interface{}{"type": "string", "description": "状态: 进行中 / 已完成 / 阻塞"},
				"next_steps": map[string]interface{}{"type": "string", "description": "下一步计划"},
				"key_info":   map[string]interface{}{"type": "string", "description": "关键信息（代码行号、文件路径、决策理由等）"},
			},
			"required": []string{"project", "task", "status"},
		},
		func(args map[string]interface{}) *ToolResult {
			project, _ := args["project"].(string)
			task, _ := args["task"].(string)
			status, _ := args["status"].(string)
			nextSteps, _ := args["next_steps"].(string)
			keyInfo, _ := args["key_info"].(string)

			if project == "" || task == "" {
				return ErrorResult("workspace_save requires project and task")
			}

			now := time.Now().Format("2006-01-02 15:04")
			summary := fmt.Sprintf("项目: %s | 任务: %s | 状态: %s | 更新时间: %s", project, task, status, now)
			if nextSteps != "" {
				summary += fmt.Sprintf(" | 下一步: %s", nextSteps)
			}

			var contentParts []string
			if keyInfo != "" {
				contentParts = append(contentParts, fmt.Sprintf("关键信息:\n%s", keyInfo))
			}
			if nextSteps != "" {
				contentParts = append(contentParts, fmt.Sprintf("下一步:\n%s", nextSteps))
			}
			contentParts = append(contentParts, fmt.Sprintf("更新时间: %s", now))

			tags := []string{"workspace_state", strings.ToLower(project)}

			result := KnowledgeRemember(fmt.Sprintf("[工作状态] %s / %s", project, task), summary, tags, 14)

			SuccessResult(fmt.Sprintf(`{"project":"%s","task":"%s","status":"%s","id":"%s"}`,
				project, task, status, extractID(result.Output)))
			return SuccessResult(fmt.Sprintf(`{"project":"%s","task":"%s","status":"%s"}`,
				project, task, status))
		},
	)

	Register("workspace_load", "加载最近的工作状态，获取当前项目上下文。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project": map[string]interface{}{"type": "string", "description": "可选，指定项目名过滤"},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			project, _ := args["project"].(string)
			results := SearchMemoryFull("[工作状态]", 50, nil)
			var matches []string
			for _, r := range results {
				if !hasTag(r.Tags, "workspace_state") {
					continue
				}
				if project != "" && !hasTag(r.Tags, strings.ToLower(project)) {
					continue
				}
				matches = append(matches, fmt.Sprintf("[%s] %s | %s", r.EntryID, r.Title, r.Summary))
			}
			if len(matches) == 0 {
				return ErrorResult("没有找到工作状态")
			}
			return SuccessResult(strings.Join(matches, "\n"))
		},
	)
}

// BuildWorkspaceContext 格式化工作状态为 LLM 上下文提示。
// 返回空字符串表示没有活跃的工作状态。
func BuildWorkspaceContext(project string) string {
	results := SearchMemoryFull("[工作状态]", 50, nil)
	if len(results) == 0 {
		return ""
	}
	for _, r := range results {
		if !hasTag(r.Tags, "workspace_state") {
			continue
		}
		if project != "" && !hasTag(r.Tags, strings.ToLower(project)) {
			continue
		}
		return "[上次工作状态] " + r.Title
	}
	return ""
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

// extractID 从 KnowledgeRemember 输出中提取知识条目 ID
func extractID(output string) string {
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil && result.ID != "" {
		return result.ID
	}
	return ""
}
