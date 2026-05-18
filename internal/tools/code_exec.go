package tools

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func registerCodeExecTool() {
	Register("code_exec", "Execute Python code. Returns stdout, stderr. 120s timeout.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"code"},
			"properties": map[string]interface{}{
				"code": stringParam("Python code to execute"),
			},
		},
		codeExecHandler,
	)
}

func codeExecHandler(args map[string]interface{}) *ToolResult {
	code, _ := args["code"].(string)
	if code == "" {
		return errorResult("code is required")
	}

	python := "python3"
	if p := os.Getenv("HERMES_PYTHON_PATH"); p != "" {
		python = p
	}

	cmd := exec.Command(python, "-c", code)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		out := stdout.String()
		if e := stderr.String(); e != "" {
			out += "\n[stderr]\n" + e
		}
		if err != nil {
			out += fmt.Sprintf("\n[exit: %v]", err)
		}
		return successResult(out)
	case <-time.After(120 * time.Second):
		cmd.Process.Kill()
		return errorResult("code exec timed out after 120s")
	}
}
