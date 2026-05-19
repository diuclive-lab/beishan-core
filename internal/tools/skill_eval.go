package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type SkillEvalIssue struct {
	Severity string `json:"severity"`
	Step     string `json:"step,omitempty"`
	Message  string `json:"message"`
}

type SkillEvalReport struct {
	WorkflowID          string           `json:"workflow_id"`
	StepCount           int              `json:"step_count"`
	Score               int              `json:"score"`
	Issues              []SkillEvalIssue `json:"issues"`
	Reachable           bool             `json:"reachable"`
	HasCycles           bool             `json:"has_cycles"`
	AllReferencedValid  bool             `json:"all_referenced_valid"`
	Message             string           `json:"message"`
}

type workflowStep struct {
	ID         string                 `yaml:"id"`
	Plugin     string                 `yaml:"plugin"`
	Type       string                 `yaml:"type"`
	Inputs     map[string]interface{} `yaml:"inputs,omitempty"`
	Timeout    int                    `yaml:"timeout,omitempty"`
	Retry      int                    `yaml:"retry,omitempty"`
	RetryDelay int                    `yaml:"retry_delay,omitempty"`
	OnError    string                 `yaml:"on_error,omitempty"`
	Next       yaml.Node              `yaml:"next,omitempty"`
}

func (s *workflowStep) parseNext() []struct {
	If      string
	Goto    string
	Default bool
} {
	var list []struct {
		If      string
		Goto    string
		Default bool
	}
	if s.Next.Kind == 0 {
		return list
	}
	// Try list format first
	if err := s.Next.Decode(&list); err == nil {
		return list
	}
	// Try string format
	var str string
	if err := s.Next.Decode(&str); err == nil && str != "" {
		list = append(list, struct {
			If      string
			Goto    string
			Default bool
		}{Goto: str})
	}
	return list
}

func SkillEvaluate(name string, yamlContent string) *ToolResult {
	var def struct {
		ID    string         `yaml:"id"`
		Steps []workflowStep `yaml:"steps"`
	}
	if yamlContent == "" {
		if name == "" {
			return errorResult("name 或 yaml_content 至少提供一个")
		}
		wfDir := "./workflows"
		if d := os.Getenv("WORKFLOW_DIR"); d != "" {
			wfDir = d
		}
		path := filepath.Join(wfDir, name+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return errorResult(fmt.Sprintf("读取工作流文件失败: %v", err))
		}
		yamlContent = string(data)
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &def); err != nil {
		return errorResult(fmt.Sprintf("YAML 解析失败: %v", err))
	}
	if def.ID == "" {
		def.ID = name
	}

	report := SkillEvalReport{
		WorkflowID: def.ID, StepCount: len(def.Steps),
		Score: 100, Reachable: true, AllReferencedValid: true,
	}

	if len(def.Steps) == 0 {
		report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Message: "工作流没有定义任何步骤"})
		report.Score -= 40
		finalizeReport(&report)
		b, _ := json.MarshalIndent(report, "", "  ")
		return successResult(string(b))
	}

	if len(def.Steps) > 20 {
		report.Issues = append(report.Issues, SkillEvalIssue{Severity: "warning", Message: fmt.Sprintf("步骤数 %d 超过建议上限 20", len(def.Steps))})
		report.Score -= 10
	}

	stepIDs := make(map[string]bool)
	for _, s := range def.Steps {
		if s.ID == "" {
			report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Message: "存在未设置 ID 的步骤"})
			report.Score -= 20
			continue
		}
		if stepIDs[s.ID] {
			report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Step: s.ID, Message: "重复的步骤 ID"})
			report.Score -= 20
		}
		stepIDs[s.ID] = true
	}

	for _, s := range def.Steps {
		if s.Plugin == "" {
			report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Step: s.ID, Message: "缺少 plugin 字段"})
			report.Score -= 15
		}
		if s.Type == "" {
			report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Step: s.ID, Message: "缺少 type 字段"})
			report.Score -= 15
		}
	}

	allRefs := make(map[string]bool)
	for _, s := range def.Steps {
		for _, n := range s.parseNext() {
			if n.Goto != "" {
				allRefs[n.Goto] = true
			}
		}
		if s.OnError != "" {
			allRefs[s.OnError] = true
		}
	}
	for ref := range allRefs {
		if ref == "done" || ref == "" {
			continue
		}
		if !stepIDs[ref] {
			report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Message: fmt.Sprintf("引用了不存在的步骤: %s", ref)})
			report.AllReferencedValid = false
			report.Score -= 20
		}
	}

	firstStep := def.Steps[0].ID
	reachable := make(map[string]bool)
	queue := []string{firstStep}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if reachable[current] {
			continue
		}
		reachable[current] = true
		for _, s := range def.Steps {
			if s.ID == current {
				for _, n := range s.parseNext() {
					if n.Goto != "" && n.Goto != "done" {
						queue = append(queue, n.Goto)
					}
				}
				if s.OnError != "" && s.OnError != "done" {
					queue = append(queue, s.OnError)
				}
				break
			}
		}
	}
	var unreachable []string
	for _, s := range def.Steps {
		if !reachable[s.ID] {
			unreachable = append(unreachable, s.ID)
		}
	}
	if len(unreachable) > 0 {
		report.Reachable = false
		sort.Strings(unreachable)
		report.Issues = append(report.Issues, SkillEvalIssue{Severity: "warning", Message: fmt.Sprintf("无法到达的步骤: %s", strings.Join(unreachable, ", "))})
		report.Score -= 10
	}

	visited := make(map[string]int)
	var hasCycle bool
	var dfs func(id string)
	dfs = func(id string) {
		if hasCycle || id == "done" || id == "" {
			return
		}
		if visited[id] == 1 {
			hasCycle = true
			return
		}
		if visited[id] == 2 {
			return
		}
		visited[id] = 1
		for _, s := range def.Steps {
			if s.ID == id {
				for _, n := range s.parseNext() {
					if n.Goto != "" && n.Goto != "done" {
						dfs(n.Goto)
					}
				}
				break
			}
		}
		visited[id] = 2
	}
	dfs(firstStep)
	report.HasCycles = hasCycle
	if hasCycle {
		report.Issues = append(report.Issues, SkillEvalIssue{Severity: "error", Message: "工作流存在循环依赖"})
		report.Score -= 30
	}

	finalizeReport(&report)
	b, _ := json.MarshalIndent(report, "", "  ")
	return successResult(string(b))
}

func finalizeReport(r *SkillEvalReport) {
	if r.Score < 0 {
		r.Score = 0
	}
	if r.Score >= 90 {
		r.Message = "工作流质量良好"
	} else if r.Score >= 70 {
		r.Message = "工作流基本可用，建议修复 warning"
	} else {
		r.Message = "工作流存在问题，建议修复 error 后使用"
	}
}

func registerSkillEvalTools() {
	Register("skill_evaluate", "评估工作流质量：检查结构、连通性、循环依赖等。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":         stringParam("工作流名称（从 workflows/ 目录读取）"),
				"yaml_content": stringParam("YAML 内容（优先于 name）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return SkillEvaluate(strArg(args, "name"), strArg(args, "yaml_content"))
		},
	)
}
