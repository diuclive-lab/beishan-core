package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"beishan/kernel"
	"gopkg.in/yaml.v3"
)

/* Engine 工作流引擎。

   职责：读取 workflows/*.yaml，按步骤调用 kernel.Call，
   步骤间通过 ${steps.<id>.output} 传递数据，支持条件分支。
*/
type Engine struct {
	Kernel *kernel.Kernel
	Dir    string // workflows/ 目录路径
}

/* New 创建工作流引擎实例。 */
func New(k *kernel.Kernel, dir string) *Engine {
	return &Engine{Kernel: k, Dir: dir}
}

/* Run 执行指定工作流。

   workflowID: 工作流 ID，对应 workflows/<id>.yaml
   input:      用户输入的原始数据（步骤中可通过 ${input} 引用）
*/
func (e *Engine) Run(workflowID string, input json.RawMessage) (*WorkflowResult, error) {
	def, err := e.load(workflowID)
	if err != nil {
		return nil, err
	}

	ctx := map[string]interface{}{"input": string(input)}
	var results []StepResult
	currentStep := def.Steps[0].ID

	for currentStep != "done" && currentStep != "" {
		step := findStep(def, currentStep)
		if step == nil {
			return &WorkflowResult{
				WorkflowID: workflowID, Steps: results,
				FinalStep: currentStep, Success: false,
				Error: fmt.Sprintf("步骤 %s 未定义", currentStep),
			}, nil
		}

		// 构建 payload：插值 ${steps.xxx.output} 引用
		payload := buildPayload(step.Inputs, ctx)

		// 调用目标插件
		resp, err := e.Kernel.Call(kernel.Message{
			Recipient: step.Plugin,
			Type:      step.Type,
			Payload:   payload,
		}, 120*time.Second) // functions imported

		result := StepResult{ID: step.ID}
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			return &WorkflowResult{
				WorkflowID: workflowID, Steps: results,
				FinalStep: step.ID, Success: false,
				Error: fmt.Sprintf("步骤 %s 失败: %v", step.ID, err),
			}, nil
		}
		result.Output = string(resp.Payload)
		results = append(results, result)

		// 存入上下文
		ctx["steps."+step.ID+".output"] = result.Output

		// 路由到下一步
		currentStep = resolveNext(step, ctx)
	}

	return &WorkflowResult{
		WorkflowID: workflowID, Steps: results,
		FinalStep: currentStep, Success: true,
	}, nil
}

/* load 从 workflows/<id>.yaml 加载工作流定义。 */
func (e *Engine) load(id string) (*WorkflowDef, error) {
	path := filepath.Join(e.Dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("工作流 %s 未找到: %w", id, err)
	}
	var def WorkflowDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("工作流 %s 解析失败: %w", id, err)
	}
	if def.ID == "" {
		return nil, fmt.Errorf("工作流 %s 缺少 id 字段", id)
	}
	if len(def.Steps) == 0 {
		return nil, fmt.Errorf("工作流 %s 没有步骤", id)
	}
	return &def, nil
}

func findStep(def *WorkflowDef, id string) *StepDef {
	for i := range def.Steps {
		if def.Steps[i].ID == id {
			return &def.Steps[i]
		}
	}
	return nil
}

var tmplRe = regexp.MustCompile(`\$\{([^}]+)\}`)

/* buildPayload 构建步骤的 payload。

   如果 inputs 为空，透传原始 input。
   如果有 inputs，对其中的 ${steps.x.output} 做插值。
*/
func buildPayload(inputs map[string]string, ctx map[string]interface{}) json.RawMessage {
	if len(inputs) == 0 {
		if input, ok := ctx["input"]; ok {
			return json.RawMessage(fmt.Sprintf(`"%v"`, input))
		}
		return json.RawMessage(`{}`)
	}

	result := make(map[string]interface{})
	for key, tmpl := range inputs {
		value := tmplRe.ReplaceAllStringFunc(tmpl, func(match string) string {
			ref := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")
			if v, ok := ctx[ref]; ok {
				return fmt.Sprintf("%v", v)
			}
			return match
		})
		result[key] = value
	}
	data, _ := json.Marshal(result)
	return data
}

/* resolveNext 根据步骤定义和上下文决定下一步。

   规则：
   1. next 为空 → done
   2. next 只有一项且无 if → 无条件跳转到 goto
   3. next 有多项 → 逐条评估条件，命中的跳转；default 作为兜底
*/
func resolveNext(step *StepDef, ctx map[string]interface{}) string {
	if len(step.Next) == 0 {
		return "done"
	}
	if len(step.Next) == 1 && step.Next[0].If == "" {
		return step.Next[0].Goto
	}

	for _, n := range step.Next {
		if n.If != "" && evaluateCondition(n.If, ctx) {
			return n.Goto
		}
		if n.Default {
			return n.Goto
		}
	}

	return "done"
}

/* evaluateCondition 评估条件表达式。

   格式: "steps.<id>.output.<field> == <value>"
   示例: "steps.cold_start.output.contract_type == 'labor'"
*/
func evaluateCondition(expr string, ctx map[string]interface{}) bool {
	parts := strings.SplitN(expr, "==", 2)
	if len(parts) != 2 {
		return false
	}
	ref := strings.TrimSpace(parts[0])   // steps.cold_start.output.contract_type
	expected := strings.TrimSpace(parts[1]) // 'labor'
	expected = strings.Trim(expected, "'\"")

	if v, ok := ctx[ref]; ok {
		return fmt.Sprintf("%v", v) == expected
	}

	// 尝试从 JSON output 中提取字段
	return extractJSONField(ctx, ref) == expected
}

/* extractJSONField 从上下文的 JSON 值中提取指定字段。

   ref 格式: "steps.cold_start.output.contract_type"
   提取流程: ctx["steps.cold_start.output"] → JSON 解析 → .contract_type
*/
func extractJSONField(ctx map[string]interface{}, ref string) string {
	// 拆出 steps.<id>.output 和剩下的 field 路径
	idx := strings.Index(ref, ".output")
	if idx < 0 {
		return ""
	}
	ctxKey := ref[:idx+7] // steps.<id>.output
	fieldPath := strings.TrimPrefix(ref[idx+8:], ".") // contract_type

	raw, ok := ctx[ctxKey]
	if !ok {
		return ""
	}
	rawStr, ok := raw.(string)
	if !ok {
		return ""
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(rawStr), &data); err != nil {
		return ""
	}

	// 沿 field 路径逐层查找
	current := interface{}(data)
	for _, part := range strings.Split(fieldPath, ".") {
		m, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = m[part]
		if current == nil {
			return ""
		}
	}
	return fmt.Sprintf("%v", current)
}

