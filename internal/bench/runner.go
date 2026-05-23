package bench

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Runner executes a suite against a handler function.
// The handler is any function that takes a prompt and returns a response.
type Runner struct {
	Handler func(ctx context.Context, prompt string) (string, error)
}

// NewRunner creates a runner with the given handler.
func NewRunner(handler func(ctx context.Context, prompt string) (string, error)) *Runner {
	return &Runner{Handler: handler}
}

// RunSuite executes all cases in a suite and returns a report.
func (r *Runner) RunSuite(ctx context.Context, suite Suite) Report {
	results := make([]Result, 0, len(suite.Cases))

	for _, c := range suite.Cases {
		result := r.runCase(ctx, c)
		results = append(results, result)
	}

	return NewReport(suite.Name, results)
}

func (r *Runner) runCase(ctx context.Context, c Case) Result {
	start := time.Now()
	output, err := r.Handler(ctx, c.Prompt)
	elapsed := time.Since(start).Milliseconds()

	result := Result{
		CaseID:     c.ID,
		CaseName:   c.Name,
		Prompt:     c.Prompt,
		Output:     output,
		DurationMs: elapsed,
	}

	if err != nil {
		result.Error = err.Error()
	}

	// Check each expectation
	for _, exp := range c.Expectations {
		er := ExpectationResult{Text: exp}
		er.Passed, er.Detail = checkExpectation(output, err, exp)
		result.Expectations = append(result.Expectations, er)
	}

	return result
}

// checkExpectation verifies a natural language expectation against the output.
// Uses simple heuristics: substring match, negations, error expectations.
func checkExpectation(output string, err error, exp string) (bool, string) {
	lower := strings.ToLower(output)
	expLower := strings.ToLower(exp)

	// Expectation that an error should occur
	if strings.HasPrefix(expLower, "error:") || strings.HasPrefix(expLower, "should error") {
		return err != nil, fmt.Sprintf("error: %v", err)
	}
	if strings.HasPrefix(expLower, "no error") {
		return err == nil, ""
	}

	// Expectation about output content
	if strings.HasPrefix(expLower, "contains:") {
		substr := strings.TrimSpace(exp[9:])
		pass := strings.Contains(lower, strings.ToLower(substr))
		if pass {
			return true, fmt.Sprintf("found %q", substr)
		}
		return false, fmt.Sprintf("expected %q not found in output", substr)
	}
	if strings.HasPrefix(expLower, "not contains:") {
		substr := strings.TrimSpace(exp[13:])
		pass := !strings.Contains(lower, strings.ToLower(substr))
		if pass {
			return true, ""
		}
		return false, fmt.Sprintf("unexpected %q found in output", substr)
	}

	// Default: check if output contains the expectation text (relaxed)
	// or if it's a positive statement that should be reflected in output
	return strings.Contains(lower, expLower), ""
}

// RunSuiteWithEngine is a convenience wrapper that runs a suite against an engine.
// Takes selectSkill and handle functions from the engine.
func RunSuiteWithEngine(ctx context.Context, engine interface {
	Handle(ctx context.Context, input string) (string, error)
}, suite Suite) Report {
	runner := NewRunner(func(ctx context.Context, prompt string) (string, error) {
		return engine.Handle(ctx, prompt)
	})
	return runner.RunSuite(ctx, suite)
}
