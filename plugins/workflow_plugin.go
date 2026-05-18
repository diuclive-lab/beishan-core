package plugins

import (
	"encoding/json"
	"fmt"
	"strings"

	"beishan/internal/workflow"
	"beishan/kernel"
)

/* WorkflowPlugin （工作流插件）

   接收 workflow_run 消息，交给 workflow.Engine 执行。
   scheduler 也可以定时发消息到本插件触发工作流。
*/
type WorkflowPlugin struct {
	Engine *workflow.Engine
}

func (p *WorkflowPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "workflow_run" {
		return kernel.Message{}, fmt.Errorf("workflow_plugin: 未知类型 %s", msg.Type)
	}

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

	payload, _ := json.Marshal(result)
	fmt.Printf("[工作流] %s: %d 步完成\n", result.WorkflowID, len(result.Steps))
	return kernel.Message{
		Type:    "workflow.result",
		Payload: payload,
	}, nil
}
