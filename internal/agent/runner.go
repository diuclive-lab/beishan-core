package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/observatory"
	"beishan/internal/tools"
)

// RunOptions controls a single sub-agent execution.
type RunOptions struct {
	TaskPrompt  string
	Definition  Definition
	TaskTimeout time.Duration
}

// SubagentErrorCode classifies sub-agent failures for structured error handling.
type SubagentErrorCode string

const (
	ErrDepthExceeded SubagentErrorCode = "depth_exceeded"
	ErrTimeout       SubagentErrorCode = "timeout"
	ErrTool          SubagentErrorCode = "tool_error"
	ErrLLM           SubagentErrorCode = "llm_error"
	ErrNotFound      SubagentErrorCode = "agent_not_found"
	ErrEmptyPrompt   SubagentErrorCode = "empty_prompt"
	ErrUnknown       SubagentErrorCode = "unknown"
)

// SubagentResult is the outcome of a sub-agent run.
type SubagentResult struct {
	TaskID     string            `json:"task_id"`
	AgentID    string            `json:"agent_id"`
	Output     string            `json:"output"`
	Iterations int               `json:"iterations"`
	ElapsedMs  int64             `json:"elapsed_ms"`
	Error      string            `json:"error,omitempty"`
	ErrorCode  SubagentErrorCode `json:"error_code,omitempty"`
}

// ParallelTask is one unit of work in a parallel spawn.
type ParallelTask struct {
	AgentID string `json:"agent_id"`
	Prompt  string `json:"prompt"`
}

// RunSubagent executes a sub-agent with the given definition and task prompt.
func RunSubagent(ctx context.Context, taskID, taskPrompt string, def Definition, timeout time.Duration) SubagentResult {
	start := time.Now()

	// Check spawn depth via context (goroutine-safe — each goroutine has its own context)
	_, err := CtxWithSpawnDepth(ctx)
	if err != nil {
		return SubagentResult{
			TaskID: taskID, AgentID: def.ID,
			Error:     fmt.Sprintf("spawn depth exceeded: possible infinite recursion"),
			ErrorCode: ErrDepthExceeded,
			ElapsedMs: time.Since(start).Milliseconds(),
		}
	}

	// Reject empty prompts before any LLM call
	if strings.TrimSpace(taskPrompt) == "" {
		return SubagentResult{
			TaskID: taskID, AgentID: def.ID,
			Error:     "empty task prompt — sub-agent requires a non-empty prompt",
			ErrorCode: ErrEmptyPrompt,
			ElapsedMs: time.Since(start).Milliseconds(),
		}
	}

	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	maxIter := def.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	// Build system prompt with tool descriptions
	sysPrompt := buildSubagentPrompt(def)
	toolDesc := buildToolDescriptions(def.Tools)

	messages := []llm.ChatMessage{
		{Role: "system", Content: sysPrompt + "\n\n" + toolDesc},
		{Role: "user", Content: taskPrompt},
	}

	// Use model/provider override if specified
	useProvider := ""
	if def.Provider != "" {
		useProvider = def.Provider
	}

	// Publish spawn event
	observatory.PublishEvent(observatory.Event{
		Type: observatory.EventAgentSpawn,
		Data: observatory.AgentSpawnData{
			AgentID:    def.ID,
			TaskPrompt: taskPrompt,
			ParentID:   taskID,
		},
	})

	iterations := 0
	var lastOutput string
	var allMessages []json.RawMessage

	for iterations < maxIter {
		iterations++

		var reply string
		var llmErr error
		for attempt := 0; attempt < 2; attempt++ {
			if useProvider != "" {
				reply, _, llmErr = llm.ChatCompletionWithProvider(useProvider, messages, timeout)
			} else {
				reply, _, llmErr = llm.ChatCompletionWithUsage(messages, timeout)
			}
			if llmErr == nil {
				break
			}
			// Retry once on transient errors, bail on auth/permanent failures
			errMsg := llmErr.Error()
			if attempt == 0 && !strings.Contains(errMsg, "auth") && !strings.Contains(errMsg, "invalid") && !strings.Contains(errMsg, "not found") {
				log.Printf("[subagent %s] LLM attempt 1 failed, retrying: %v", def.ID, llmErr)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			break
		}
		if llmErr != nil {
			errCode := ErrLLM
			errMsg := llmErr.Error()
			if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline") {
				errCode = ErrTimeout
			}
			return SubagentResult{
				TaskID: taskID, AgentID: def.ID,
				Error:     "LLM call: " + errMsg,
				ErrorCode: errCode,
				ElapsedMs: time.Since(start).Milliseconds(),
			}
		}

		tc := parseToolCall(reply)
		if tc == nil {
			lastOutput = reply
			if msg, err := json.Marshal(llm.ChatMessage{Role: "assistant", Content: reply}); err == nil {
				allMessages = append(allMessages, json.RawMessage(msg))
			}
			break
		}

		log.Printf("[subagent %s] iter %d: calling %s", def.ID, iterations, tc.Tool)
		toolResult := executeTool(tc)

		msgBytes, _ := json.Marshal(llm.ChatMessage{Role: "assistant", Content: reply})
		allMessages = append(allMessages, json.RawMessage(msgBytes))
		messages = append(messages, llm.ChatMessage{Role: "assistant", Content: reply})
		msgBytes2, _ := json.Marshal(llm.ChatMessage{Role: "tool", Content: toolResult})
		allMessages = append(allMessages, json.RawMessage(msgBytes2))
		messages = append(messages, llm.ChatMessage{Role: "tool", Content: toolResult})

		lastOutput = reply
	}

	// Validate output quality — detect empty/meaningless completions
	outputValid, outputWarning := validateOutput(lastOutput)
	if !outputValid {
		log.Printf("[subagent %s] output validation: %s", def.ID, outputWarning)
	}

	observatory.PublishEvent(observatory.Event{
		Type: observatory.EventAgentComplete,
		Data: observatory.AgentCompleteData{
			AgentID:    def.ID,
			Iterations: iterations,
			ElapsedMs:  time.Since(start).Milliseconds(),
			Output:     lastOutput,
			Messages:   allMessages,
		},
	})

	return SubagentResult{
		TaskID: taskID, AgentID: def.ID,
		Output:     lastOutput,
		Iterations: iterations,
		ElapsedMs:  time.Since(start).Milliseconds(),
	}
}

// RunParallel executes multiple sub-agent tasks concurrently and returns combined results.
func RunParallel(ctx context.Context, tasks []ParallelTask, timeout time.Duration) []SubagentResult {
	results := make([]SubagentResult, len(tasks))
	done := make(chan struct{}, len(tasks))

	for i, t := range tasks {
		go func(idx int, task ParallelTask) {
			def, ok := Get(task.AgentID)
			if !ok {
				results[idx] = SubagentResult{
					TaskID: task.AgentID, AgentID: task.AgentID,
					Error:     fmt.Sprintf("agent %q not found", task.AgentID),
					ErrorCode: ErrNotFound,
				}
				done <- struct{}{}
				return
			}
			// Each goroutine runs with its own context depth — no race on the counter
			results[idx] = RunSubagent(ctx, fmt.Sprintf("parallel-%d", idx), task.Prompt, def, timeout)
			done <- struct{}{}
		}(i, t)
	}

	for range tasks {
		<-done
	}
	return results
}

type parsedToolCall struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

func parseToolCall(output string) *parsedToolCall {
	raw := strings.TrimSpace(output)
	braceStart := strings.Index(raw, "{")
	braceEnd := strings.LastIndex(raw, "}")
	if braceStart < 0 || braceEnd <= braceStart {
		return nil
	}

	var tc parsedToolCall
	if err := json.Unmarshal([]byte(raw[braceStart:braceEnd+1]), &tc); err != nil {
		return nil
	}
	if tc.Tool == "" {
		return nil
	}
	return &tc
}

func executeTool(tc *parsedToolCall) string {
	if !tools.HasAgentTool(tc.Tool) {
		return fmt.Sprintf("Error: tool %q not found", tc.Tool)
	}
	argsJSON, _ := json.Marshal(tc.Arguments)
	result := tools.ExecuteAgentTool(tc.Tool, json.RawMessage(argsJSON))
	if result.Error != "" {
		return fmt.Sprintf("Tool %s error: %s", tc.Tool, result.Error)
	}
	output, truncated := truncateStr(result.Output, 2000)
	if truncated {
		return fmt.Sprintf("Tool %s result (%d total chars, truncated to 2000):\n%s", tc.Tool, len(result.Output), output)
	}
	return fmt.Sprintf("Tool %s result:\n%s", tc.Tool, output)
}

func buildSubagentPrompt(def Definition) string {
	p := def.SystemPrompt
	if p == "" {
		p = "You are a helpful sub-agent. Complete the delegated task concisely."
	}
	p += "\n\nTo use a tool, output a JSON object on its own line:\n"
	p += `{"tool": "name", "arguments": {"key": "value"}}` + "\n"
	p += "When done, respond with your final answer in plain text (no JSON)."
	return p
}

func buildToolDescriptions(allowedTools []string) string {
	defs := tools.GetAgentDefinitions(allowedTools)
	if len(defs) == 0 {
		return "No tools available."
	}
	var b strings.Builder
	b.WriteString("Available tools:\n")
	for _, d := range defs {
		b.WriteString(fmt.Sprintf("  - %s: %s\n", d.Function.Name, d.Function.Description))
	}
	return b.String()
}

// validateOutput checks agent output for empty/meaningless results.
// Returns (valid, warning_message). Callers should log the warning.
func validateOutput(output string) (bool, string) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return false, "sub-agent completed but produced no output"
	}
	lower := strings.ToLower(trimmed)
	giveupPhrases := []string{"i cannot complete", "i'm unable", "i am unable", "i cannot fulfill",
		"unable to complete", "sorry, i cannot", "i don't know how to"}
	for _, phrase := range giveupPhrases {
		if strings.Contains(lower, phrase) {
			return false, "sub-agent returned a give-up response (matched: " + phrase + ")"
		}
	}
	return true, ""
}

func truncateStr(s string, n int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= n {
		return s, false
	}
	return string(runes[:n]) + "...[truncated]", true
}
