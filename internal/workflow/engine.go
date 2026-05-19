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

	// Unquote input JSON string: JSON `"abc"` → Go `abc`
	inputStr := string(input)
	var unquoted string
	if json.Unmarshal(input, &unquoted) == nil {
		inputStr = unquoted
	}
	ctx := map[string]interface{}{"input": inputStr}
	var results []StepResult
	currentStep := def.Steps[0].ID
	stepVisits := make(map[string]int) // 循环保护：记录每步访问次数
	maxIter := def.MaxIterations
	if maxIter <= 0 {
		maxIter = 200
	}

	for currentStep != "done" && currentStep != "" {
		step := findStep(def, currentStep)
		if step == nil {
			return buildResult(workflowID, results, currentStep, false, fmt.Sprintf("步骤 %s 未定义", currentStep)), nil
		}

		payload := buildPayload(step.Inputs, ctx)

		// 并行步骤：goroutine + WaitGroup 并发执行子步骤
		if len(step.ParallelSteps) > 0 {
			result := e.runParallel(step, ctx)
			results = append(results, result)
			currentStep = resolveNext(step, ctx)
			continue
		}

		timeout := step.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		maxRetry := step.Retry
		if maxRetry < 0 {
			maxRetry = 0
		}
		retryDelay := step.RetryDelay
		if retryDelay <= 0 {
			retryDelay = 1
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
				wait := time.Duration(retryDelay*(1<<uint(attempt))) * time.Second
				fmt.Printf("[工作流] 步骤 %s 失败(第%d次)，等待 %v 后重试...\n", step.ID, attempt+1, wait)
				time.Sleep(wait)
			}
		}

		result := StepResult{ID: step.ID}
		if callErr != nil {
			result.Error = fmt.Sprintf("步骤 %s 失败(%d次重试后): %v", step.ID, maxRetry, callErr)
			results = append(results, result)
			ctx["steps."+step.ID+".error"] = result.Error

			// on_error：继续到指定步骤，不终止工作流
			if step.OnError != "" {
				fmt.Printf("[工作流] 步骤 %s 失败，跳过后续重试，继续到 %s\n", step.ID, step.OnError)
				currentStep = step.OnError
				continue
			}
			return buildResult(workflowID, results, step.ID, false, result.Error), nil
		}
		result.Output = string(resp.Payload)
		results = append(results, result)

		ctx["steps."+step.ID+".output"] = result.Output

		currentStep = resolveNext(step, ctx)
		stepVisits[step.ID]++
		if stepVisits[step.ID] > maxIter {
			return buildResult(workflowID, results, step.ID, false, fmt.Sprintf("步骤 %s 已达循环上限 %d 次，疑似死循环", step.ID, maxIter)), nil
		}
	}

	return buildResult(workflowID, results, currentStep, true, ""), nil
}

func buildResult(workflowID string, steps []StepResult, finalStep string, success bool, err string) *WorkflowResult {
	return &WorkflowResult{
		WorkflowID: workflowID, Steps: steps,
		FinalStep: finalStep, Success: success,
		Error: err,
	}
}

// runParallel 并行执行步骤的多个子步骤（goroutine + channel）
func (e *Engine) runParallel(step *StepDef, ctx map[string]interface{}) StepResult {
	result := StepResult{ID: step.ID}
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = 120
	}

	type subResult struct {
		id     string
		output string
		err    error
	}

	ch := make(chan subResult, len(step.ParallelSteps))
	for i := range step.ParallelSteps {
		sub := step.ParallelSteps[i]
		go func(s StepDef) {
			payload := buildPayload(s.Inputs, ctx)
			resp, err := e.Kernel.Call(kernel.Message{
				Recipient: s.Plugin,
				Type:      s.Type,
				Payload:   payload,
			}, time.Duration(timeout)*time.Second)
			if err != nil {
				ch <- subResult{id: s.ID, err: err}
				return
			}
			ch <- subResult{id: s.ID, output: string(resp.Payload)}
		}(sub)
	}

	var outputs []string
	var errs []string
	for range step.ParallelSteps {
		r := <-ch
		ctxKey := "steps." + step.ID + ".output." + r.id
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.id, r.err))
			ctx[ctxKey] = fmt.Sprintf("error: %v", r.err)
		} else {
			outputs = append(outputs, r.output)
			ctx[ctxKey] = r.output
		}
	}

	if len(errs) > 0 {
		result.Error = "并行步骤部分失败: " + strings.Join(errs, "; ")
		ctx["steps."+step.ID+".output"] = "parallel_errors: " + result.Error
	} else {
		result.Output = strings.Join(outputs, "\n")
		ctx["steps."+step.ID+".output"] = result.Output
	}
	return result
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

func buildPayload(inputs map[string]interface{}, ctx map[string]interface{}) json.RawMessage {
	if len(inputs) == 0 {
		if input, ok := ctx["input"]; ok {
			return json.RawMessage(fmt.Sprintf(`"%v"`, input))
		}
		return json.RawMessage(`{}`)
	}

	result := make(map[string]interface{})
	for key, rawTmpl := range inputs {
		tmpl, ok := rawTmpl.(string)
		if !ok {
			result[key] = rawTmpl
			continue
		}
		if ref, ok := singleTemplateRef(tmpl); ok {
			if v, found := resolveRef(ctx, ref); found {
				result[key] = v
				continue
			}
		}

		value := tmplRe.ReplaceAllStringFunc(tmpl, func(match string) string {
			ref := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")
			if v, ok := ctx[ref]; ok {
				return fmt.Sprintf("%v", v)
			}
			// JSON 字段路径提取：${steps.xxx.output.field}
			if fv, ok := extractJSONFieldValue(ctx, ref); ok {
				return valueToString(fv)
			}
			return match
		})

		// 自动检测 JSON 数组/对象，保持类型完整性
		trimmed := strings.TrimSpace(value)
		if len(trimmed) >= 2 && (trimmed[0] == '[' || trimmed[0] == '{') && json.Valid([]byte(trimmed)) {
			var parsed interface{}
			json.Unmarshal([]byte(trimmed), &parsed)
			result[key] = parsed
		} else {
			result[key] = value
		}
	}
	data, _ := json.Marshal(result)
	return data
}

func singleTemplateRef(tmpl string) (string, bool) {
	matches := tmplRe.FindStringSubmatch(strings.TrimSpace(tmpl))
	if len(matches) != 2 || strings.TrimSpace(tmpl) != matches[0] {
		return "", false
	}
	return matches[1], true
}

func resolveRef(ctx map[string]interface{}, ref string) (interface{}, bool) {
	if v, ok := ctx[ref]; ok {
		return v, true
	}
	return extractJSONFieldValue(ctx, ref)
}

func valueToString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []interface{}, map[string]interface{}:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		return fmt.Sprintf("%v", t)
	}
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
	v, ok := extractJSONFieldValue(ctx, ref)
	if !ok {
		return ""
	}
	return valueToString(v)
}

func extractJSONFieldValue(ctx map[string]interface{}, ref string) (interface{}, bool) {
	idx := strings.Index(ref, ".output")
	if idx < 0 {
		return nil, false
	}
	ctxKey := ref[:idx+7]
	fieldPath := strings.TrimPrefix(ref[idx+8:], ".")

	raw, ok := ctx[ctxKey]
	if !ok {
		return nil, false
	}
	rawStr, ok := raw.(string)
	if !ok {
		return nil, false
	}

	// 处理 JSON 嵌套编码：先尝试解包最外层字符串
	parsed := resolveJSONValue([]byte(rawStr))
	if parsed == nil {
		return nil, false
	}

	current := parsed
	for _, part := range strings.Split(fieldPath, ".") {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = m[part]
		if current == nil {
			return nil, true
		}
	}
	return current, true
}

// resolveJSONValue 递归解析 JSON，自动解包嵌套编码的字符串
func resolveJSONValue(raw []byte) interface{} {
	// 先试对象
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	// 再试数组
	var arr []interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// 如果是 JSON 字符串（嵌套编码），解包后递归
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return resolveJSONValue([]byte(str))
	}
	return nil
}
