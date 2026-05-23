package workflow

import (
	"fmt"
	"strings"
)

// ErrorKind classifies a step failure for retry/fallback decisions.
type ErrorKind string

const (
	KindInputMismatch     ErrorKind = "input_mismatch"
	KindDependencyMissing ErrorKind = "dependency_missing"
	KindPermissionDenied  ErrorKind = "permission_denied"
	KindTransientBackend  ErrorKind = "transient_backend"
	KindTimeout           ErrorKind = "timeout"
	KindInternal          ErrorKind = "internal"
)

// ToolError is a structured step failure.
type ToolError struct {
	Kind      ErrorKind `json:"kind"`
	ToolName  string    `json:"tool_name"`
	Detail    string    `json:"detail,omitempty"`
	Retryable bool      `json:"retryable"`
	Recovered bool      `json:"recovered,omitempty"`
}

func (e *ToolError) Error() string {
	base := fmt.Sprintf("[%s] tool %q", e.Kind, e.ToolName)
	if e.Detail != "" {
		base += ": " + e.Detail
	}
	return base
}

// ClassifyError wraps an error into a ToolError with best-guess classification.
func ClassifyError(toolName string, err error) *ToolError {
	if err == nil {
		return nil
	}
	msg := err.Error()

	switch {
	case containsAny(msg, "timeout", "deadline exceeded"):
		return &ToolError{Kind: KindTimeout, ToolName: toolName, Detail: msg, Retryable: true}
	case containsAny(msg, "connection refused", "connection reset", "no such host",
		"broken pipe", "temporary", "too many requests", "rate limit"):
		return &ToolError{Kind: KindTransientBackend, ToolName: toolName, Detail: msg, Retryable: true}
	case containsAny(msg, "permission", "forbidden", "unauthorized", "not allowed"):
		return &ToolError{Kind: KindPermissionDenied, ToolName: toolName, Detail: msg, Retryable: false}
	case containsAny(msg, "not found", "no such file", "no results", "missing"):
		return &ToolError{Kind: KindDependencyMissing, ToolName: toolName, Detail: msg, Retryable: false}
	case containsAny(msg, "required", "invalid", "bad", "解析失败"):
		return &ToolError{Kind: KindInputMismatch, ToolName: toolName, Detail: msg, Retryable: false}
	default:
		return &ToolError{Kind: KindInternal, ToolName: toolName, Detail: msg, Retryable: false}
	}
}

// IsRetryable checks if an error qualifies for retry.
func IsRetryable(err error) bool {
	if te, ok := err.(*ToolError); ok {
		return te.Retryable
	}
	return true
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
