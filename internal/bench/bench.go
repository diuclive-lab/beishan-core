// Package evals provides the Exploration Bench — capability growth tracking,
// not release gating. Adapted from 66's Eval Harness, stripped of gate semantics.
package bench

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

// Case is one test scenario. No tier, no weight, no fault injection.
type Case struct {
	ID           int      `json:"id"`
	Name         string   `json:"name,omitempty"`
	Prompt       string   `json:"prompt"`
	Expectations []string `json:"expectations"` // natural language behaviors to verify
}

// Suite is a collection of cases targeting one capability area.
type Suite struct {
	Name  string `json:"name"`
	Cases []Case `json:"cases"`
}

// Result captures one run of one case.
type Result struct {
	CaseID       int                `json:"case_id"`
	CaseName     string             `json:"case_name,omitempty"`
	Prompt       string             `json:"prompt"`
	Output       string             `json:"output"`
	Expectations []ExpectationResult `json:"expectations"`
	DurationMs   int64              `json:"duration_ms"`
	Error        string             `json:"error,omitempty"`
}

// ExpectationResult records whether one expectation was met.
type ExpectationResult struct {
	Text   string `json:"text"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// Report aggregates results from one suite run.
type Report struct {
	SuiteName  string    `json:"suite_name"`
	RanAt      time.Time `json:"ran_at"`
	TotalCases int       `json:"total_cases"`
	Passed     int       `json:"passed"`
	Failed     int       `json:"failed"`
	PassRate   float64   `json:"pass_rate"`
	DurationMs int64     `json:"total_duration_ms"`
	Results    []Result  `json:"results"`
}

// ── Building ─────────────────────────────────────────────────────────────────

// NewReport builds a report from results, computing pass/fail aggregates.
func NewReport(suiteName string, results []Result) Report {
	passed := 0
	var totalDuration int64
	for _, r := range results {
		allPassed := true
		for _, e := range r.Expectations {
			if !e.Passed {
				allPassed = false
				break
			}
		}
		if allPassed && r.Error == "" {
			passed++
		}
		totalDuration += r.DurationMs
	}
	total := len(results)
	passRate := 0.0
	if total > 0 {
		passRate = math.Round(float64(passed)/float64(total)*10000) / 100
	}
	return Report{
		SuiteName:  suiteName,
		RanAt:      time.Now().UTC(),
		TotalCases: total,
		Passed:     passed,
		Failed:     total - passed,
		PassRate:   passRate,
		DurationMs: totalDuration,
		Results:    results,
	}
}

// ── Display ──────────────────────────────────────────────────────────────────

// Markdown renders the report as a readable summary.
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Exploration Bench: %s\n\n", r.SuiteName))
	b.WriteString(fmt.Sprintf("- Ran at: `%s`\n", r.RanAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Pass rate: **%.1f%%** (%d/%d)\n", r.PassRate, r.Passed, r.TotalCases))
	b.WriteString(fmt.Sprintf("- Total duration: %d ms\n\n", r.DurationMs))

	for _, res := range r.Results {
		status := "✅"
		failed := false
		for _, e := range res.Expectations {
			if !e.Passed {
				failed = true
				break
			}
		}
		if failed || res.Error != "" {
			status = "❌"
		}
		name := res.CaseName
		if name == "" {
			name = fmt.Sprintf("case-%d", res.CaseID)
		}
		b.WriteString(fmt.Sprintf("## %s %s\n\n", status, name))
		b.WriteString(fmt.Sprintf("**Prompt:** %s\n\n", res.Prompt))
		if res.Error != "" {
			b.WriteString(fmt.Sprintf("**Error:** %s\n\n", res.Error))
		}
		for _, e := range res.Expectations {
			icon := "✅"
			if !e.Passed {
				icon = "❌"
			}
			b.WriteString(fmt.Sprintf("- %s %s", icon, e.Text))
			if e.Detail != "" {
				b.WriteString(fmt.Sprintf(" — %s", e.Detail))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Evolution note
	if r.TotalCases > 0 {
		b.WriteString("---\n")
		b.WriteString("_This is an exploration trace, not a release gate. Compare with previous runs to track evolution._\n")
	}
	return b.String()
}

// ── Comparison ───────────────────────────────────────────────────────────────

// Delta describes how a report changed versus a baseline.
type Delta struct {
	SuiteName    string  `json:"suite_name"`
	PassRateDiff float64 `json:"pass_rate_diff"` // positive = improved
	CaseDiffs    []CaseDelta `json:"case_diffs,omitempty"`
}

// CaseDelta describes one case's change.
type CaseDelta struct {
	CaseID   int    `json:"case_id"`
	CaseName string `json:"case_name,omitempty"`
	Status   string `json:"status"` // "improved", "regressed", "stable"
}

// Compare returns the delta between a baseline and current report.
func Compare(baseline, current Report) Delta {
	d := Delta{SuiteName: current.SuiteName}
	if baseline.PassRate != 0 {
		d.PassRateDiff = math.Round((current.PassRate-baseline.PassRate)*100) / 100
	}

	baselineByID := map[int]bool{}
	for _, r := range baseline.Results {
		baselineByID[r.CaseID] = allPassed(r)
	}
	for _, r := range current.Results {
		prev, ok := baselineByID[r.CaseID]
		if !ok {
			continue
		}
		now := allPassed(r)
		var status string
		switch {
		case !prev && now:
			status = "improved"
		case prev && !now:
			status = "regressed"
		default:
			status = "stable"
		}
		if status != "stable" {
			d.CaseDiffs = append(d.CaseDiffs, CaseDelta{
				CaseID: r.CaseID, CaseName: r.CaseName, Status: status,
			})
		}
	}
	sort.Slice(d.CaseDiffs, func(i, j int) bool {
		return d.CaseDiffs[i].CaseID < d.CaseDiffs[j].CaseID
	})
	return d
}

func allPassed(r Result) bool {
	if r.Error != "" {
		return false
	}
	for _, e := range r.Expectations {
		if !e.Passed {
			return false
		}
	}
	return true
}
