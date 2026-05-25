package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/* ─── code_task / code_execute L3 工具 ──────────

   编码智能体：统一入口 → 规划 → 执行（预留 Claude CLI）→ 安全检查 → 写入

   Phase 1:
   - code_task: 理解需求 → 生成多步计划
   - code_execute: 接收计划 → 【预留 Claude CLI 接口】→ 当前返回提示词供手动使用

   Phase 2（将来）:
   - 调 Claude CLI 子进程
   - 解析 stdout diff
   - 自动走安全检查
*/

type CodePlan struct {
	ID        string     `json:"id"`
	Goal      string     `json:"goal"`
	CreatedAt int64      `json:"created_at"`
	Steps     []CodeStep `json:"steps"`
}

type CodeStep struct {
	Action      string `json:"action"`      // read / write / search / refactor
	File        string `json:"file"`        // 目标文件
	Description string `json:"description"` // 干什么
	Prompt      string `json:"prompt"`      // 给编码工具的 prompt
	Completed   bool   `json:"completed"`
	Output      string `json:"output,omitempty"`
}

var sandboxDir string

func getSandboxDir() string {
	if sandboxDir == "" {
		sandboxDir = filepath.Join(HermesHome, "coding_sandbox")
	}
	return sandboxDir
}

func sandboxPlanPath(id string) string {
	return filepath.Join(getSandboxDir(), id+".json")
}

func planDir(id string) string {
	return filepath.Join(getSandboxDir(), id)
}

func CodeTaskHandler(args map[string]interface{}) *ToolResult {
	goal, _ := args["goal"].(string)
	if goal == "" {
		return errorResult("goal（编码目标）不能为空")
	}

	projectContext := ""
	if pc, ok := args["project_context"].(string); ok {
		projectContext = pc
	}

	all := loadAllKnowledge()
	// 提取相关上下文：项目相关的知识条目
	var contextHints []string
	for _, e := range all {
		if strings.Contains(e.Summary, "beishan-core") || strings.Contains(e.Title, "beishan-core") {
			contextHints = append(contextHints, fmt.Sprintf("- %s: %s", e.Title, truncateStr(e.Summary, 100)))
			if len(contextHints) >= 5 {
				break
			}
		}
	}

	prompt := fmt.Sprintf(`你是一个软件架构师。用户提出以下编码需求：

目标：%s

%s

请拆解为2-5个可执行的步骤。每步指定：
- action: read/write/search/refactor
- file: 涉及的文件路径
- description: 做什么（一句话）
- prompt: 给编码工具的详细指示

输出 JSON 数组，不要其他文字：
[
  {"action":"read","file":"...","description":"...","prompt":"..."},
  {"action":"write","file":"...","description":"...","prompt":"..."}
]`, goal, projectContext)

	// 用 LLM 做规划（走 think_plugin 路径）
	planText, err := callLLMForPlan(prompt)
	if err != nil {
		return errorResult(fmt.Sprintf("规划失败: %v", err))
	}

	var steps []CodeStep
	if err := json.Unmarshal([]byte(planText), &steps); err != nil {
		return errorResult(fmt.Sprintf("规划结果解析失败: %v", err))
	}
	if len(steps) == 0 {
		return errorResult("规划结果为空")
	}

	planID := fmt.Sprintf("plan_%d", time.Now().UnixNano())
	now := time.Now().Unix()
	plan := CodePlan{
		ID:        planID,
		Goal:      goal,
		CreatedAt: now,
		Steps:     steps,
	}

	// 保存到沙箱
	os.MkdirAll(getSandboxDir(), 0755)
	os.MkdirAll(planDir(planID), 0755)
	data, _ := json.MarshalIndent(plan, "", "  ")
	os.WriteFile(sandboxPlanPath(planID), data, 0644)

	result := map[string]interface{}{
		"plan_id":    planID,
		"goal":       goal,
		"steps":      steps,
		"step_count": len(steps),
		"message":    fmt.Sprintf("已生成 %d 步计划。使用 code_execute 执行，或用 code_apply 手动应用某个 step 的输出。", len(steps)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

// callLLMForPlan 调 LLM 生成编码计划。
// 复用 think_plugin 路径而不是直接调 llm，保持一致性。
func callLLMForPlan(prompt string) (string, error) {
	// 走 think_plugin 的 no_retrieval 模式生成规划
	// 用 ChatCompletionWithUsage 记录 usage
	return "", fmt.Errorf("编码规划需要 think_plugin 支持，预留接口")
}

// CodeExecuteHandler 执行计划中的一步。
// 【预留】当前返回 prompt 供手动执行，未来调 Claude CLI。
func CodeExecuteHandler(args map[string]interface{}) *ToolResult {
	planID, _ := args["plan_id"].(string)
	stepIdx := 0
	if si, ok := args["step"].(float64); ok {
		stepIdx = int(si)
	}
	mode, _ := args["mode"].(string)

	if planID == "" {
		// 列出所有沙箱计划
		entries, _ := os.ReadDir(getSandboxDir())
		var plans []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				plans = append(plans, strings.TrimSuffix(e.Name(), ".json"))
			}
		}
		if len(plans) == 0 {
			return successResult(`{"plans":[],"message":"暂无编码计划。使用 code_task 创建。"}`)
		}
		b, _ := json.Marshal(map[string]interface{}{"plans": plans})
		return successResult(string(b))
	}

	data, err := os.ReadFile(sandboxPlanPath(planID))
	if err != nil {
		return errorResult(fmt.Sprintf("计划 %s 未找到", planID))
	}
	var plan CodePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return errorResult("计划解析失败")
	}

	if stepIdx < 0 || stepIdx >= len(plan.Steps) {
		return errorResult(fmt.Sprintf("步骤索引 %d 超出范围（共 %d 步）", stepIdx, len(plan.Steps)))
	}

	step := plan.Steps[stepIdx]

	if mode == "manual" || mode == "" {
		// 手动模式：返回 prompt 给用户
		return successResult(fmt.Sprintf(
			`{"plan_id":"%s","step":%d,"action":"%s","file":"%s","description":"%s","prompt":"%s","mode":"manual","message":"请将以上 prompt 复制到编码工具中执行。完成后将输出保存到沙箱目录。"}`,
			planID, stepIdx, step.Action, step.File, step.Description, step.Prompt,
		))
	}

	// 【预留】自动模式：调 Claude CLI 子进程
	return successResult(fmt.Sprintf(
		`{"plan_id":"%s","step":%d,"error":"Claude CLI 子进程未接入。当前支持 manual 模式。","mode":"placeholder"}`,
		planID, stepIdx,
	))
}

// CodeSaveOutputHandler 将手动执行的结果保存到沙箱。
func CodeSaveOutputHandler(args map[string]interface{}) *ToolResult {
	planID, _ := args["plan_id"].(string)
	stepIdx := 0
	if si, ok := args["step"].(float64); ok {
		stepIdx = int(si)
	}
	output, _ := args["output"].(string)

	if planID == "" || output == "" {
		return errorResult("plan_id 和 output 不能为空")
	}

	data, err := os.ReadFile(sandboxPlanPath(planID))
	if err != nil {
		return errorResult(fmt.Sprintf("计划 %s 未找到", planID))
	}
	var plan CodePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return errorResult("计划解析失败")
	}
	if stepIdx < 0 || stepIdx >= len(plan.Steps) {
		return errorResult("步骤索引超出范围")
	}

	step := &plan.Steps[stepIdx]
	step.Output = output
	step.Completed = true

	// 保存到沙箱 step 文件
	stepPath := filepath.Join(planDir(planID), fmt.Sprintf("step_%d_output.txt", stepIdx))
	os.WriteFile(stepPath, []byte(output), 0644)

	// 更新计划 JSON
	planData, _ := json.MarshalIndent(plan, "", "  ")
	os.WriteFile(sandboxPlanPath(planID), planData, 0644)

	return successResult(fmt.Sprintf(`{"plan_id":"%s","step":%d,"message":"步骤输出已保存"}`, planID, stepIdx))
}

func registerCodeTaskTools() {
	Register("code_task", "编码智能体：理解需求，拆解为多步编码计划，生成给编码工具的 prompt。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"goal"},
			"properties": map[string]interface{}{
				"goal":            stringParam("编码目标描述，如「给 system_info 加磁盘查询」"),
				"project_context": stringParam("可选的额外上下文信息"),
			},
		},
		CodeTaskHandler,
	)

	Register("code_execute", "执行编码计划中的一步。【预留】自动模式调 Claude CLI，当前支持 manual 模式返回 prompt。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"plan_id"},
			"properties": map[string]interface{}{
				"plan_id": stringParam("code_task 返回的 plan_id"),
				"step":    intParam("步骤索引（从 0 开始），默认 0"),
				"mode":    stringParam("manual（手动）/ auto（预留自动），默认 manual"),
			},
		},
		CodeExecuteHandler,
	)

	Register("code_save_output", "保存手动编码的结果到沙箱。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"plan_id", "output"},
			"properties": map[string]interface{}{
				"plan_id": stringParam("code_task 返回的 plan_id"),
				"step":    intParam("步骤索引（从 0 开始），默认 0"),
				"output":  stringParam("编码工具输出的结果（diff 或文件内容）"),
			},
		},
		CodeSaveOutputHandler,
	)
}
