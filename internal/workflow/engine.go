package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	tStart := time.Now()
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
			return buildResult(workflowID, results, currentStep, false, fmt.Sprintf("步骤 %s 未定义", currentStep), time.Since(tStart).Milliseconds()), nil
		}

		// skip_if 条件跳过
		if step.SkipIf != "" {
			shouldSkip := evaluateCondition(step.SkipIf, ctx)
			if shouldSkip {
				result := StepResult{ID: step.ID, Output: "skipped: " + step.SkipIf, ElapsedMs: 0}
				results = append(results, result)
				currentStep = resolveNext(step, ctx)
				continue
			}
		}

		payload := buildPayload(step.Inputs, ctx)

		// 自动注入 no_retrieval 模式（think_plugin 步骤默认跳过检索，避免干扰 JSON 输出）
		if step.Plugin == "think_plugin" {
			var pm map[string]interface{}
			if json.Unmarshal(payload, &pm) == nil {
				if _, exists := pm["mode"]; !exists {
					pm["mode"] = "no_retrieval"
				}
				payload, _ = json.Marshal(pm)
			}
		}

		// 并行步骤：goroutine + WaitGroup 并发执行子步骤
		if len(step.ParallelSteps) > 0 {
			t0 := time.Now()
			result := e.runParallel(step, ctx)
			result.ElapsedMs = time.Since(t0).Milliseconds()
			results = append(results, result)
			currentStep = resolveNext(step, ctx)
			continue
		}

		timeout := step.Timeout
		if timeout <= 0 {
			timeout = 120
		}

		// 批量循环步骤：对 foreach 数组中的每个元素执行当前步骤
		if step.Batch != nil {
			t0 := time.Now()
			result := e.runBatch(step, ctx, timeout)
			result.ElapsedMs = time.Since(t0).Milliseconds()
			results = append(results, result)
			currentStep = resolveNext(step, ctx)
			continue
		}

		maxRetry := step.Retry
		if maxRetry < 0 {
			maxRetry = 0
		}
		retryDelay := step.RetryDelay
		if retryDelay <= 0 {
			retryDelay = 1
		}

		t0 := time.Now()
		var resp kernel.Message
		var callErr error
		for attempt := 0; attempt <= maxRetry; attempt++ {
			resp, callErr = e.Kernel.Call(kernel.Message{
				Recipient: step.Plugin,
				Type:      step.Type,
				Payload:   payload,
				Provider:  step.Provider,
			}, time.Duration(timeout)*time.Second)

			if callErr == nil {
				// 检查插件返回的响应类型是否包含 ".error"（如 notify_send.error）
				if resp.Type != "" && strings.HasSuffix(resp.Type, ".error") {
					callErr = fmt.Errorf("%s", string(resp.Payload))
				} else {
					break
				}
			}
			if attempt < maxRetry {
				wait := time.Duration(retryDelay*(1<<uint(attempt))) * time.Second
				fmt.Printf("[工作流] 步骤 %s 失败(第%d次)，等待 %v 后重试...\n", step.ID, attempt+1, wait)
				time.Sleep(wait)
			}
		}

		result := StepResult{ID: step.ID, ElapsedMs: time.Since(t0).Milliseconds()}
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
			return buildResult(workflowID, results, step.ID, false, result.Error, time.Since(tStart).Milliseconds()), nil
		}
		result.Output = string(resp.Payload)
		results = append(results, result)

		ctx["steps."+step.ID+".output"] = result.Output

		currentStep = resolveNext(step, ctx)
		stepVisits[step.ID]++
		if stepVisits[step.ID] > maxIter {
			return buildResult(workflowID, results, step.ID, false, fmt.Sprintf("步骤 %s 已达循环上限 %d 次，疑似死循环", step.ID, maxIter), time.Since(tStart).Milliseconds()), nil
		}
	}

	finalOutput := ""
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Error == "" && results[i].Output != "" {
			finalOutput = results[i].Output
			break
		}
	}
	return buildResult(workflowID, results, currentStep, true, "", time.Since(tStart).Milliseconds(), finalOutput), nil
}

func buildResult(workflowID string, steps []StepResult, finalStep string, success bool, err string, totalMs int64, finalOutput ...string) *WorkflowResult {
	r := &WorkflowResult{
		WorkflowID: workflowID, Steps: steps,
		FinalStep: finalStep, Success: success,
		Error: err, TotalMs: totalMs,
	}
	if len(finalOutput) > 0 {
		r.FinalOutput = finalOutput[0]
	}
	return r
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
				Provider:  s.Provider,
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
		ctxKey := "steps." + step.ID + "." + r.id + ".output"
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

// runBatch 批量循环执行：对 foreach 数组中的每个元素调用 step 的 plugin:type。
// 当前元素存入 ctx["item"]，索引存入 ctx["item_index"]。
// 当 step.Batch.Parallel=true 时并发执行（默认串行）。
func (e *Engine) runBatch(step *StepDef, ctx map[string]interface{}, timeout int) StepResult {
	result := StepResult{ID: step.ID}

	// 求值 foreach 表达式，得到数组
	foreachRef := strings.TrimPrefix(strings.TrimSuffix(step.Batch.Foreach, "}"), "${")
	var items []interface{}
	if v, ok := resolveRef(ctx, foreachRef); ok {
		switch arr := v.(type) {
		case []interface{}:
			items = arr
		case string:
			json.Unmarshal([]byte(arr), &items)
		}
	}
	if len(items) == 0 {
		result.Output = "[]"
		ctx["steps."+step.ID+".output"] = "[]"
		return result
	}

	// 确定并发数
	concurrency := step.Batch.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	var (
		outputs   = make([]string, len(items))
		errs      []string
		outputsMu sync.Mutex
		errsMu    sync.Mutex
		wg        sync.WaitGroup
		sem       chan struct{} // nil = 串行
		parallel  = step.Batch.Parallel
	)

	if parallel {
		sem = make(chan struct{}, concurrency)
	}

	for i, item := range items {
		if parallel {
			sem <- struct{}{}
			wg.Add(1)
		}

		idx := i
		it := item

		do := func() {
			if parallel {
				defer wg.Done()
				defer func() { <-sem }()
			}

			// 并发安全：为每个 goroutine 创建独立的 ctx 副本
			localCtx := make(map[string]interface{}, len(ctx)+2)
			for k, v := range ctx {
				localCtx[k] = v
			}
			localCtx["item"] = it
			localCtx["item_index"] = idx

			payload := buildPayload(step.Inputs, localCtx)
			resp, callErr := e.Kernel.Call(kernel.Message{
				Recipient: step.Plugin,
				Type:      step.Type,
				Payload:   payload,
				Provider:  step.Provider,
			}, time.Duration(timeout)*time.Second)

			if callErr != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("[%d]: %v", idx, callErr))
				errsMu.Unlock()
				return
			}
			if resp.Type != "" && strings.HasSuffix(resp.Type, ".error") {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("[%d]: %s", idx, string(resp.Payload)))
				errsMu.Unlock()
				return
			}
			outputsMu.Lock()
			outputs[idx] = string(resp.Payload)
			outputsMu.Unlock()
			fmt.Printf("[工作流] batch %s [%d/%d] 完成\n", step.ID, idx+1, len(items))
		}

		if parallel {
			go do()
		} else {
			do()
		}
	}

	if parallel {
		wg.Wait()
	}

	// 清理临时上下文
	delete(ctx, "item")
	delete(ctx, "item_index")

	// 过滤空 output（并发场景下失败的位置为空字符串）
	var filled []string
	for _, o := range outputs {
		if o != "" {
			filled = append(filled, o)
		}
	}

	if len(errs) > 0 {
		result.Error = fmt.Sprintf("批量执行 %d/%d 失败: %s", len(errs), len(items), strings.Join(errs, "; "))
	}
	outputJSON, _ := json.Marshal(filled)
	result.Output = string(outputJSON)
	ctx["steps."+step.ID+".output"] = result.Output
	ctx["steps."+step.ID+".count"] = len(filled)
	ctx["steps."+step.ID+".errors"] = len(errs)
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
		if input, ok := ctx["input"]; ok && input != "" {
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
	// 支持 != 运算符
	if idx := strings.Index(expr, "!="); idx > 0 {
		ref := strings.TrimSpace(expr[:idx])
		expected := strings.TrimSpace(expr[idx+2:])
		expected = strings.Trim(expected, "'\"")
		var actual string
		if v, ok := ctx[ref]; ok {
			actual = fmt.Sprintf("%v", v)
		} else {
			actual = extractJSONField(ctx, ref)
		}
		return actual != expected
	}

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
	// 尝试从 ctx 中找到 ref 的最长前缀 key，剩余部分作为字段路径
	// 例如 ref="item.path" → ctx["item"] 是 map → 取 path 字段
	// 例如 ref="steps.xxx.output.field" → ctx["steps.xxx.output"] 是 string → JSON 解析后取 field
	parts := strings.Split(ref, ".")
	for i := len(parts) - 1; i >= 1; i-- {
		ctxKey := strings.Join(parts[:i], ".")
		fieldPath := strings.Join(parts[i:], ".")
		if v, ok := ctx[ctxKey]; ok {
			return extractFieldFromValue(v, fieldPath)
		}
	}
	return nil, false
}

// extractFieldFromValue 从值中提取嵌套字段，支持 map 和 JSON 字符串
func extractFieldFromValue(v interface{}, fieldPath string) (interface{}, bool) {
	var current interface{}
	switch val := v.(type) {
	case map[string]interface{}:
		current = val
	case string:
		parsed := resolveJSONValue([]byte(val))
		if parsed == nil {
			return nil, false
		}
		current = parsed
	default:
		return nil, false
	}

	for _, part := range strings.Split(fieldPath, ".") {
		fieldName := part
		arrIdx := -1
		if braceStart := strings.Index(part, "["); braceStart > 0 && strings.HasSuffix(part, "]") {
			fieldName = part[:braceStart]
			if n, err := fmt.Sscanf(part[braceStart+1:len(part)-1], "%d", &arrIdx); err != nil || n != 1 || arrIdx < 0 {
				return nil, false // reject negative or non-integer index
			}
		}
		if m, ok := current.(map[string]interface{}); ok {
			current = m[fieldName]
			if arrIdx >= 0 {
				if arr, ok := current.([]interface{}); ok && arrIdx < len(arr) {
					current = arr[arrIdx]
				} else if arr, ok := current.([]interface{}); ok && arrIdx >= len(arr) {
					return nil, true
				}
			}
		} else if arr, ok := current.([]interface{}); ok && arrIdx >= 0 && arrIdx < len(arr) {
			current = arr[arrIdx]
		} else {
			return nil, false
		}
		if current == nil {
			return nil, true
		}
	}
	return current, true
}

// resolveJSONValue 递归解析 JSON，自动解包嵌套编码的字符串
func resolveJSONValue(raw []byte) interface{} {
	return resolveJSONValueDepth(raw, 0)
}

func resolveJSONValueDepth(raw []byte, depth int) interface{} {
	if depth > 10 {
		return string(raw)
	}
	// 先尝试去除 markdown 代码块包裹
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		// 去掉开头的 ```json 或 ```
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
		// 去掉结尾的 ```
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
		return resolveJSONValue([]byte(s))
	}
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
		if len(str) > 0 && len(str) < len(raw) {
			return resolveJSONValueDepth([]byte(str), depth+1)
		}
		return raw
	}
	// 尝试从文本中提取 JSON（LLM 可能在 JSON 前后添加说明文字）
	if idx := strings.IndexAny(s, "{["); idx >= 0 {
		closeChar := byte('}')
		if s[idx] == '[' {
			closeChar = ']'
		}
		if lastIdx := strings.LastIndex(s, string(closeChar)); lastIdx > idx {
			candidate := s[idx : lastIdx+1]
			if parsed := resolveJSONValueDepth([]byte(candidate), depth+1); parsed != nil {
				return parsed
			}
		}
	}
	return nil
}
