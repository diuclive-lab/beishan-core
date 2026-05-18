package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type procInst struct {
	ID      string
	Command string
	Cmd     *exec.Cmd
	Start   time.Time
	Stdout  bytes.Buffer
	Stderr  bytes.Buffer
	Done    bool
	mu      sync.Mutex
}

var (
	procsMu sync.Mutex
	procs   = map[string]*procInst{}
)

func registerTerminalTools() {
	Register("terminal_exec", "Execute a shell command. Returns stdout, stderr, exit code.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"command"},
			"properties": map[string]interface{}{
				"command":  stringParam("Shell command to execute"),
				"timeout":  intParam("Timeout in seconds (default 180)"),
				"workdir":  stringParam("Working directory (optional)"),
				"task_id":  stringParam("Unique ID to run in background (optional)"),
			},
		},
		terminalExecHandler,
	)

	Register("terminal_list", "List running background processes.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		terminalListHandler,
	)

	Register("terminal_poll", "Poll a background process by task_id.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": stringParam("Task ID from terminal_exec"),
			},
		},
		terminalPollHandler,
	)

	Register("terminal_kill", "Kill a background process by task_id.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": stringParam("Task ID to kill"),
			},
		},
		terminalKillHandler,
	)
}

func terminalExecHandler(args map[string]interface{}) *ToolResult {
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return errorResult("command is required")
	}
	timeout := 180
	if t, ok := args["timeout"].(float64); ok {
		timeout = int(t)
	}
	workdir, _ := args["workdir"].(string)
	taskID, _ := args["task_id"].(string)

	if taskID != "" {
		return startBg(taskID, cmdStr, workdir, timeout)
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		out := stdout.String()
		if e := stderr.String(); e != "" {
			out += "\n" + e
		}
		if err != nil {
			return successResult(fmt.Sprintf("Exit: %v\n%s", err, out))
		}
		return successResult(out)
	case <-time.After(time.Duration(timeout) * time.Second):
		cmd.Process.Kill()
		return errorResult(fmt.Sprintf("timeout after %ds", timeout))
	}
}

func startBg(taskID, cmdStr, workdir string, timeout int) *ToolResult {
	cmd := exec.Command("sh", "-c", cmdStr)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	p := &procInst{
		ID: taskID, Command: cmdStr, Cmd: cmd, Start: time.Now(),
	}
	procsMu.Lock()
	procs[taskID] = p
	procsMu.Unlock()

	go func() {
		err := cmd.Run()
		p.mu.Lock()
		p.Done = true
		p.Stdout, p.Stderr = stdout, stderr
		p.mu.Unlock()
		if err != nil {
			p.mu.Lock()
			p.Stderr.WriteString(fmt.Sprintf("\n[exit: %v]", err))
			p.mu.Unlock()
		}
		time.Sleep(time.Duration(timeout) * time.Second)
		procsMu.Lock()
		delete(procs, taskID)
		procsMu.Unlock()
	}()

	return successResult(fmt.Sprintf("bg started [%s]: %s", taskID, cmdStr))
}

func terminalListHandler(args map[string]interface{}) *ToolResult {
	procsMu.Lock()
	defer procsMu.Unlock()
	if len(procs) == 0 {
		return successResult("no bg processes")
	}
	var sb strings.Builder
	for id, p := range procs {
		p.mu.Lock()
		status := "running"
		if p.Done {
			status = "done"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s — %s (%s)\n", status, id, p.Command, time.Since(p.Start).Round(time.Second)))
		p.mu.Unlock()
	}
	return successResult(sb.String())
}

func terminalPollHandler(args map[string]interface{}) *ToolResult {
	taskID, _ := args["task_id"].(string)
	procsMu.Lock()
	p, ok := procs[taskID]
	procsMu.Unlock()
	if !ok {
		return errorResult("task not found: " + taskID)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	status := "running"
	if p.Done {
		status = "done"
	}
	out := fmt.Sprintf("[%s] %s (%s)\n%s", status, p.Command, time.Since(p.Start).Round(time.Second), p.Stdout.String()+p.Stderr.String())
	return successResult(out)
}

func terminalKillHandler(args map[string]interface{}) *ToolResult {
	taskID, _ := args["task_id"].(string)
	procsMu.Lock()
	p, ok := procs[taskID]
	procsMu.Unlock()
	if !ok {
		return errorResult("task not found: " + taskID)
	}
	p.Cmd.Process.Kill()
	procsMu.Lock()
	delete(procs, taskID)
	procsMu.Unlock()
	return successResult("killed: " + taskID)
}
