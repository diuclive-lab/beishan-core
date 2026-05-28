package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"beishan/internal/workflow"
	"beishan/kernel"
)

/* WorkflowPlugin （工作流插件）

   接收 workflow_run 消息，交给 workflow.Engine 执行。
   接收 workflow_list 消息，扫描 workflows/ 目录返回可用工作流列表。
   scheduler 也可以定时发消息到本插件触发工作流。
*/
type WorkflowPlugin struct {
	Engine *workflow.Engine
}

func (p *WorkflowPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "workflow_list":
		return p.handleList()
	case "workflow_run":
		return p.handleRun(msg)
	default:
		return kernel.Message{}, fmt.Errorf("workflow_plugin: 未知类型 %s", msg.Type)
	}
}

func (p *WorkflowPlugin) handleList() (kernel.Message, error) {
	entries, err := os.ReadDir(p.Engine.Dir)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("workflow_plugin: 扫描目录失败: %w", err)
	}

	type item struct {
		ID          string `json:"id"`
		Description string `json:"description,omitempty"`
	}
	var list []item
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") || strings.HasPrefix(name, "_") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		desc := extractWorkflowDescription(filepath.Join(p.Engine.Dir, name))
		list = append(list, item{ID: id, Description: desc})
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"count":     len(list),
		"workflows": list,
	})
	return kernel.Message{Type: "workflow.list", Payload: payload}, nil
}

// extractWorkflowDescription 读取 YAML 文件的顶层 description 字段。
// 仅做简单文本扫描，不全量解析，失败时返回空字符串。
func extractWorkflowDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 20) {
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, `"'`)
			return desc
		}
	}
	return ""
}

func (p *WorkflowPlugin) handleRun(msg kernel.Message) (kernel.Message, error) {
	var req struct {
		Workflow string          `json:"workflow"`
		Input    json.RawMessage `json:"input"`
	}

	// 先试结构化 JSON，如果失败则尝试把 payload 当作纯文本 workflow 名
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		workflowName := strings.Trim(string(msg.Payload), `"`)
		if workflowName == "" {
			return kernel.Message{}, fmt.Errorf("workflow_plugin: 参数解析失败: %w", err)
		}
		req.Workflow = workflowName
	}
	if req.Workflow == "" {
		return kernel.Message{}, fmt.Errorf("workflow_plugin: workflow 参数不能为空")
	}

	result, err := p.Engine.Run(req.Workflow, req.Input)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("workflow_plugin: %w", err)
	}

	fmt.Printf("[工作流] %s: %d 步完成\n", result.WorkflowID, len(result.Steps))

	payload, _ := json.Marshal(result)
	return kernel.Message{
		Type:    "workflow.result",
		Payload: payload,
	}, nil
}
