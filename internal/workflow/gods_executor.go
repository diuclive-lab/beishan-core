package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"beishan/kernel"
)

/* ═══════════════════════════════════════════════════════════
   StateStore — 步骤结果的状态存储
   ═══════════════════════════════════════════════════════════ */

// StateStore 以 OutputVar 为键存储步骤结果
type StateStore struct {
	results   map[string]StepResult
	rawInput  map[string]interface{}
}

func NewStateStore(rawInput map[string]interface{}) *StateStore {
	return &StateStore{
		results:  make(map[string]StepResult),
		rawInput: rawInput,
	}
}

// Set 存储步骤结果
func (s *StateStore) Set(outputVar string, result StepResult) {
	s.results[outputVar] = result
}

// Get 按点号路径取值：支持 "step_id"、"step_id.field"、"step_id.field.nested"
func (s *StateStore) Get(path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}

	// 单字段：先查 results，再查 rawInput
	if len(parts) == 1 {
		if r, ok := s.results[path]; ok {
			return r, true
		}
		if v, ok := s.rawInput[path]; ok {
			return v, true
		}
		return nil, false
	}

	// 多字段：按最长前缀匹配 results
	for i := len(parts) - 1; i >= 1; i-- {
		key := strings.Join(parts[:i], ".")
		fieldPath := strings.Join(parts[i:], ".")
		if r, ok := s.results[key]; ok {
			v, found := extractFieldFromValue(r.Output, fieldPath)
			if found {
				return v, true
			}
			if r.Data != nil {
				if fv, found := extractFieldFromValue(r.Data, fieldPath); found {
					return fv, true
				}
			}
		}
	}

	// fallback 到 rawInput
	if len(parts) >= 2 {
		key := parts[0]
		fieldPath := strings.Join(parts[1:], ".")
		if v, ok := s.rawInput[key]; ok {
			if fv, found := extractFieldFromValue(v, fieldPath); found {
				return fv, true
			}
		}
	}

	return nil, false
}

/* ═══════════════════════════════════════════════════════════
   GoExecutor — 执行 GoWorkflow
   ═══════════════════════════════════════════════════════════ */

// GoExecutor 执行编译时安全的 Go-DSL 工作流
type GoExecutor struct {
	kernel   *kernel.Kernel
	toolHost map[string]string // tool 名 → 宿主插件名
}

func NewGoExecutor(k *kernel.Kernel, toolHost map[string]string) *GoExecutor {
	return &GoExecutor{kernel: k, toolHost: toolHost}
}

// Run 执行 GoWorkflow，返回与 YAML 引擎兼容的 WorkflowResult
func (ex *GoExecutor) Run(wf GoWorkflow, rawInput map[string]interface{}) *WorkflowResult {
	tStart := time.Now()
	state := NewStateStore(rawInput)
	ctx := GoContext{
		WorkflowName: wf.Name,
		Kernel:       ex.kernel,
	}

	var results []StepResult

	// 全局超时
	timeout := wf.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	ctxTimeout, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, step := range wf.Steps {
		select {
		case <-ctxTimeout.Done():
			return buildGoResult(wf.Name, results, "", false, "工作流超时", time.Since(tStart).Milliseconds())
		default:
		}

		ctx.StepID = step.ID

		// 1. 解析输入
		input := ex.resolveGoStepInput(step.Input, state)

		// 2. BeforeExecute（约定：纯数据变换，不做 I/O）
		if step.BeforeExecute != nil {
			if modified, err := step.BeforeExecute(ctx, input); err == nil {
				input = modified
			}
		}

		// 3. 执行步骤 + 重试
		result := ex.runGoStepWithRetry(step, input)

		// 4. AfterExecute（约定：只检查/过滤，不做 I/O）
		if step.AfterExecute != nil {
			if modified, err := step.AfterExecute(ctx, input, &result); err == nil {
				result = *modified
			}
		}

		// 5. 存储状态
		if step.OutputVar != "" {
			state.Set(step.OutputVar, result)
		}
		results = append(results, result)

		// 6. 错误决策
		if result.Error != "" {
			switch step.OnError {
			case ErrorContinue:
				continue
			case ErrorFailStep:
				// 记录错误继续执行
			default: // ErrorFailWorkflow 或未设置
				return buildGoResult(wf.Name, results, step.ID, false,
					fmt.Sprintf("步骤 %s 失败: %s", step.ID, result.Error),
					time.Since(tStart).Milliseconds())
			}
		}
	}

	// 取最终输出
	finalStep := wf.ResultStep
	if finalStep == "" && len(results) > 0 {
		finalStep = results[len(results)-1].ID
	}
	var finalOutput string
	for i := len(results) - 1; i >= 0; i-- {
		if finalStep != "" && results[i].ID != finalStep {
			continue
		}
		if results[i].Error == "" && results[i].Output != "" {
			finalOutput = results[i].Output
			break
		}
	}

	return buildGoResult(wf.Name, results, finalStep, true, "", time.Since(tStart).Milliseconds(), finalOutput)
}

// resolveGoStepInput 构建步骤输入参数（map 级拼接，非字符串替换）
func (ex *GoExecutor) resolveGoStepInput(input *GoStepInput, state *StateStore) map[string]interface{} {
	result := make(map[string]interface{})

	if input == nil {
		return result
	}

	// From：引用另一步骤的全部输出
	if input.From != "" {
		if v, ok := state.Get(input.From); ok {
			if m, ok := v.(StepResult); ok && m.Data != nil {
				return m.Data
			}
			if m, ok := v.(map[string]interface{}); ok {
				return m
			}
			if s, ok := v.(string); ok && s != "" {
				var parsed map[string]interface{}
				if json.Unmarshal([]byte(s), &parsed) == nil {
					return parsed
				}
				return map[string]interface{}{"output": s}
			}
		}
		return result
	}

	// RawInputKeys：从原始输入映射字段
	for key, ref := range input.RawInputKeys {
		ref = strings.TrimPrefix(ref, "${")
		ref = strings.TrimSuffix(ref, "}")
		if ref == "input" {
			if raw, ok := state.rawInput["input"]; ok {
				result[key] = raw
			}
			continue
		}
		fieldRef := strings.TrimPrefix(ref, "input.")
		if v, ok := state.rawInput[fieldRef]; ok {
			result[key] = v
		}
	}

	// Merge：组合多个源
	for _, src := range input.Merge {
		val := ex.resolveSource(src, state)
		if val != nil {
			result[src.Key] = val
		}
	}

	// Static：静态键值对
	for k, v := range input.Static {
		if s, ok := v.(string); ok {
			result[k] = ex.resolveTemplate(s, state)
		} else {
			result[k] = v
		}
	}

	return result
}

func (ex *GoExecutor) resolveSource(src GoInputSource, state *StateStore) interface{} {
	if src.Step != "" {
		if src.Field == "" || src.Field == "output" {
			if r, ok := state.results[src.Step]; ok {
				if r.Data != nil {
					return r.Data
				}
				return r.Output
			}
			return nil
		}
		path := src.Step + "." + src.Field
		if v, ok := state.Get(path); ok {
			return v
		}
		return nil
	}
	if src.Value != "" {
		return ex.resolveTemplate(src.Value, state)
	}
	return nil
}

// resolveTemplate 解析 ${input} / ${steps.step_id.output} 模板
func (ex *GoExecutor) resolveTemplate(tmpl string, state *StateStore) interface{} {
	if ref, ok := singleTemplateRef(tmpl); ok {
		if v, found := state.Get(ref); found {
			return v
		}
		return tmpl
	}

	// String 中的内联模板
	result := tmpl
	for _, match := range tmplRe.FindAllString(tmpl, -1) {
		ref := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")
		if v, ok := state.Get(ref); ok {
			replacement := fmt.Sprintf("%v", v)
			result = strings.Replace(result, match, replacement, 1)
		}
	}
	return result
}

// runGoStepWithRetry 执行步骤（带重试）
func (ex *GoExecutor) runGoStepWithRetry(step GoStep, input map[string]interface{}) StepResult {
	// Transform/Parallel/Chain 不走重试逻辑，直接执行
	switch step.Type {
	case GoStepTransform:
		trCtx := GoContext{WorkflowName: "", StepID: step.ID, Kernel: ex.kernel}
		data, err := step.TransformFn(trCtx, input)
		if err != nil {
			return StepResult{ID: step.ID, Error: err.Error()}
		}
		return stepResultFromData(step.ID, data)
	case GoStepParallel:
		return ex.runGoStepParallel(step, input)
	case GoStepChain:
		return ex.runGoStepChain(step, input)
	}

	maxRetry := step.MaxRetries
	if maxRetry < 0 {
		maxRetry = 0
	}
	retryDelay := step.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	var lastErr error
	var resp kernel.Message
	fallbackTried := false

	for attempt := 0; attempt <= maxRetry; attempt++ {
		resp, lastErr = ex.callStep(step, input)

		if lastErr == nil {
			if resp.Type != "" && strings.HasSuffix(resp.Type, ".error") {
				lastErr = fmt.Errorf("%s", string(resp.Payload))
			} else {
				break
			}
		}

		// Fallback: 主工具失败后尝试降级工具（仅对不可重试错误）
		if step.Fallback != "" && !fallbackTried {
			if te := ClassifyError(step.Tool, lastErr); !te.Retryable {
				fbStep := step
				fbStep.Tool = step.Fallback
				fbStep.Fallback = ""
				var fbResp kernel.Message
				fbResp, lastErr = ex.callStep(fbStep, input)
				if lastErr == nil {
					if fbResp.Type != "" && strings.HasSuffix(fbResp.Type, ".error") {
						lastErr = fmt.Errorf("%s", string(fbResp.Payload))
					} else {
						resp = fbResp
						lastErr = nil
						break
					}
				}
				fallbackTried = true
			}
		}

		if attempt < maxRetry {
			wait := retryDelay * time.Duration(1<<uint(attempt))
			time.Sleep(wait)
		}
	}

	if lastErr != nil {
		return StepResult{
			ID:    step.ID,
			Error: fmt.Sprintf("步骤 %s 失败(%d次重试后): %v", step.ID, maxRetry, lastErr),
		}
	}

	return StepResult{
		ID:     step.ID,
		Output: string(resp.Payload),
	}
}

// callStep executes a Tool or Plugin step once, without retry.
func (ex *GoExecutor) callStep(step GoStep, input map[string]interface{}) (kernel.Message, error) {
	switch step.Type {
	case GoStepTool:
		host, ok := ex.toolHost[step.Tool]
		if !ok {
			return kernel.Message{}, fmt.Errorf("Go-DSL: 工具 %q 未配置宿主插件", step.Tool)
		}
		return ex.kernel.Call(kernel.Message{
			Recipient: host,
			Type:      step.Tool,
			Payload:   toRawMessage(input),
		}, step.PluginTimeout)
	case GoStepPlugin:
		return ex.kernel.Call(kernel.Message{
			Recipient: step.Recipient,
			Type:      step.MsgType,
			Payload:   toRawMessage(input),
		}, step.PluginTimeout)
	default:
		return kernel.Message{}, fmt.Errorf("未知步骤类型 %s", step.Type)
	}
}

// runGoStepChain 顺序执行子步骤
func (ex *GoExecutor) runGoStepChain(step GoStep, input map[string]interface{}) StepResult {
	result := StepResult{ID: step.ID}
	subState := NewStateStore(nil)
	var subResults []StepResult

	current := input
	for _, sub := range step.SubSteps {
		subInput := ex.resolveGoStepInput(sub.Input, subState)
		if subInput == nil || len(subInput) == 0 {
			subInput = current
		}

		subResult := ex.runGoStepWithRetry(sub, subInput)
		if sub.OutputVar != "" {
			subState.Set(sub.OutputVar, subResult)
		}
		subResults = append(subResults, subResult)

		if subResult.Error != "" && sub.OnError == ErrorFailWorkflow {
			result.Error = fmt.Sprintf("子步骤 %s 失败: %s", sub.ID, subResult.Error)
			result.SubResults = subResults
			return result
		}

		if subResult.Output != "" {
			current = map[string]interface{}{"output": subResult.Output}
		}
	}

	if len(subResults) > 0 {
		last := subResults[len(subResults)-1]
		result.Output = last.Output
	}
	result.SubResults = subResults
	return result
}

// runGoStepParallel 并发执行子步骤
func (ex *GoExecutor) runGoStepParallel(step GoStep, input map[string]interface{}) StepResult {
	result := StepResult{ID: step.ID}
	var mu sync.Mutex
	var subResults []StepResult
	var wg sync.WaitGroup

	for _, sub := range step.SubSteps {
		wg.Add(1)
		go func(s GoStep) {
			defer wg.Done()
			sr := ex.runGoStepWithRetry(s, input)
			mu.Lock()
			subResults = append(subResults, sr)
			mu.Unlock()
		}(sub)
	}
	wg.Wait()

	result.SubResults = subResults

	var outputs []string
	var errs []string
	for _, sr := range subResults {
		if sr.Error != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", sr.ID, sr.Error))
		} else if sr.Output != "" {
			outputs = append(outputs, sr.Output)
		}
	}

	if len(errs) > 0 {
		result.Error = strings.Join(errs, "; ")
	}
	if len(outputs) > 0 {
		result.Output = strings.Join(outputs, "\n")
	}
	return result
}

/* ═══════════════════════════════════════════════════════════
   GoWorkflowPlugin — kernel.Plugin 适配器
   ═══════════════════════════════════════════════════════════ */

// GoWorkflowPlugin 实现 kernel.Plugin 接口
type GoWorkflowPlugin struct {
	kernel    *kernel.Kernel
	router    map[string]GoWorkflow
	exec      *GoExecutor
}

// NewGoWorkflowPlugin 构造 GoWorkflowPlugin
//
//	ensureTool: 校验函数，如 func(name string) bool { return tools.GetToolSchema(name) != nil }
//	            传 nil 则跳过校验
func NewGoWorkflowPlugin(k *kernel.Kernel, toolHost map[string]string, ensureTool func(string) bool, router map[string]GoWorkflow) *GoWorkflowPlugin {
	if ensureTool != nil {
		for wfName, wf := range router {
			for _, step := range wf.Steps {
				validateGoStep(step, wfName, toolHost, ensureTool)
			}
		}
	}
	return &GoWorkflowPlugin{
		kernel: k,
		router: router,
		exec:   NewGoExecutor(k, toolHost),
	}
}

// NewGoToolPlugin 简化版 — 直接将 msg.Type 映射到同名 Tool
//
//	routing: {"web_search": "web_search", "write_file": "write_file"}
//	toolHost: {"web_search": "search_plugin", "write_file": "write_plugin"}
func NewGoToolPlugin(k *kernel.Kernel, toolHost map[string]string, ensureTool func(string) bool, routing map[string]string) *GoWorkflowPlugin {
	router := make(map[string]GoWorkflow, len(routing))
	for typeStr, toolName := range routing {
		if ensureTool != nil && !ensureTool(toolName) {
			panic(fmt.Sprintf("Go-DSL: 工具 %q 未注册(路由类型 %q)", toolName, typeStr))
		}
		if _, ok := toolHost[toolName]; !ok {
			panic(fmt.Sprintf("Go-DSL: 工具 %q 未配置 toolHost(路由类型 %q)", toolName, typeStr))
		}
		router[typeStr] = GoWorkflow{
			Name:       typeStr,
			ResultStep: "step0",
			Steps: []GoStep{{
				ID:            "step0",
				Type:          GoStepTool,
				Tool:          toolName,
				PluginTimeout: 120 * time.Second,
				Input: &GoStepInput{
					RawInputKeys: map[string]string{"input": "input"},
				},
			}},
		}
	}
	return NewGoWorkflowPlugin(k, toolHost, ensureTool, router)
}

// OnMessage 实现 kernel.Plugin 接口
func (p *GoWorkflowPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	wf, ok := p.router[msg.Type]
	if !ok {
		return kernel.Message{}, fmt.Errorf("Go-DSL: 未知消息类型 %s", msg.Type)
	}

	// 解析 Payload 为 map
	rawInput := make(map[string]interface{})
	rawInput["input"] = string(msg.Payload)

	var payloadMap map[string]interface{}
	if json.Unmarshal(msg.Payload, &payloadMap) == nil {
		for k, v := range payloadMap {
			rawInput[k] = v
		}
	}

	result := p.exec.Run(wf, rawInput)
	if !result.Success {
		return kernel.Message{}, fmt.Errorf("Go-DSL 工作流 %s 失败: %s", wf.Name, result.Error)
	}

	payload, _ := json.Marshal(result)
	return kernel.Message{
		Type:    wf.Name + ".result",
		Payload: payload,
	}, nil
}

func validateGoStep(step GoStep, wfName string, toolHost map[string]string, ensureTool func(string) bool) {
	if step.Type == GoStepTool {
		if step.Tool == "" {
			panic(fmt.Sprintf("Go-DSL 工作流 %q: GoStepTool 步骤 %q 缺少 Tool 字段", wfName, step.ID))
		}
		if !ensureTool(step.Tool) {
			panic(fmt.Sprintf("Go-DSL 工作流 %q: 工具 %q 未注册(步骤 %q)", wfName, step.Tool, step.ID))
		}
		if _, ok := toolHost[step.Tool]; !ok {
			panic(fmt.Sprintf("Go-DSL 工作流 %q: 工具 %q 未配置宿主插件(步骤 %q)", wfName, step.Tool, step.ID))
		}
	}
	for _, sub := range step.SubSteps {
		validateGoStep(sub, wfName, toolHost, ensureTool)
	}
}

/* ═══════════════════════════════════════════════════════════
   工具函数
   ═══════════════════════════════════════════════════════════ */

func toRawMessage(input map[string]interface{}) json.RawMessage {
	data, err := json.Marshal(input)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func stepResultFromData(id string, data map[string]interface{}) StepResult {
	output, _ := json.Marshal(data)
	return StepResult{
		ID:     id,
		Output: string(output),
		Data:   data,
	}
}

func buildGoResult(workflowID string, steps []StepResult, finalStep string, success bool, err string, totalMs int64, finalOutput ...string) *WorkflowResult {
	r := &WorkflowResult{
		WorkflowID: workflowID,
		Steps:      steps,
		FinalStep:  finalStep,
		Success:    success,
		Error:      err,
		TotalMs:    totalMs,
	}
	if len(finalOutput) > 0 {
		r.FinalOutput = finalOutput[0]
	}
	return r
}
