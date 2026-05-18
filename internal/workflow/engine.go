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

type Engine struct {
	Kernel *kernel.Kernel
	Dir    string
}

func New(k *kernel.Kernel, dir string) *Engine {
	return &Engine{Kernel: k, Dir: dir}
}

/* Run 执行工作流，支持超时、重试、条件分支。 */
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

		payload := buildPayload(step.Inputs, ctx)

		// 超时：YAML 指定或默认 120 秒
		timeout := step.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		// 重试：YAML 指定或默认 0
		maxRetry := step.Retry
		if maxRetry < 0 {
			maxRetry = 0
		}

		var resp kernel.Message
		var callErr error
		for attempt := 0; attempt <= maxRetry; attempt++ {
			resp, callErr = e.Kernel.Call(kernel.Message{
				Recipient: step.Plugin,
				Type:      step.Type,
				Payload:   payload,
			}, time.Duration(timeout)*time.Second)

			if callErr == nil {
				break
			}
			if attempt < maxRetry {
				fmt.Printf("[工作流] 步骤 %s 失败(第%d次)，重试...\n", step.ID, attempt+1)
			}
		}

		result := StepResult{ID: step.ID}
		if callErr != nil {
			result.Error = callErr.Error()
			results = append(results, result)
			return &WorkflowResult{
				WorkflowID: workflowID, Steps: results,
				FinalStep: step.ID, Success: false,
				Error: fmt.Sprintf("步骤 %s 失败(%d次重试后): %v", step.ID, maxRetry, callErr),
			}, nil
		}
		result.Output = string(resp.Payload)
		results = append(results, result)

		ctx["steps."+step.ID+".output"] = result.Output

		currentStep = resolveNext(step, ctx)
	}

	return &WorkflowResult{
		WorkflowID: workflowID, Steps: results,
		FinalStep: currentStep, Success: true,
	}, nil
}

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

func evaluateCondition(expr string, ctx map[string]interface{}) bool {
	parts := strings.SplitN(expr, "==", 2)
	if len(parts) != 2 {
		return false
	}
	ref := strings.TrimSpace(parts[0])
	expected := strings.TrimSpace(parts[1])
	expected = strings.Trim(expected, "'\"")

	if v, ok := ctx[ref]; ok {
		return fmt.Sprintf("%v", v) == expected
	}
	return extractJSONField(ctx, ref) == expected
}

func extractJSONField(ctx map[string]interface{}, ref string) string {
	idx := strings.Index(ref, ".output")
	if idx < 0 {
		return ""
	}
	ctxKey := ref[:idx+7]
	fieldPath := strings.TrimPrefix(ref[idx+8:], ".")

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
