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

	fmt.Printf("[工作流] %s: %d 步完成\n", result.WorkflowID, len(result.Steps))

	// 如果有 FinalOutput，直接返回可读文本（而非嵌套 JSON）
	if result.FinalOutput != "" && result.Success {
		// 尝试解析 FinalOutput（可能是 JSON 字符串）
		var parsed interface{}
		if json.Unmarshal([]byte(result.FinalOutput), &parsed) == nil {
			// 是合法 JSON，直接返回
			return kernel.Message{
				Type:    "workflow.result",
				Payload: []byte(result.FinalOutput),
			}, nil
		}
		// 不是 JSON，返回原始文本
		outputJSON, _ := json.Marshal(result.FinalOutput)
		return kernel.Message{
			Type:    "workflow.result",
			Payload: outputJSON,
		}, nil
	}

	// 降级：返回完整 WorkflowResult
	payload, _ := json.Marshal(result)
	return kernel.Message{
		Type:    "workflow.result",
		Payload: payload,
	}, nil
}
