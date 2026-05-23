// Package observatory records why the system made each decision.
package observatory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Trace records a single system decision (route, tool selection, clarify).
type Trace struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Input     string    `json:"input"`

	Mode       string `json:"mode,omitempty"`       // execution mode
	Complexity string `json:"complexity,omitempty"` // task complexity

	Route      string  `json:"route"`
	Plugin     string  `json:"plugin,omitempty"`    // renamed from Skill
	Tool       string  `json:"tool,omitempty"`
	Confidence float64 `json:"confidence"`

	RouteReason  string `json:"route_reason"`           // renamed from WhyRouted
	WhyClarified string `json:"why_clarified,omitempty"` // kept separate for clarify tracking
	WhyRejected  string `json:"why_rejected,omitempty"`

	Status     string `json:"status"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// Recorder holds and persists decision traces.
type Recorder struct {
	traces   []Trace
	filePath string
}

func NewRecorder() *Recorder {
	return &Recorder{traces: make([]Trace, 0)}
}

func NewPersistentRecorder(path string) *Recorder {
	r := &Recorder{traces: make([]Trace, 0), filePath: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var t Trace
		if json.Unmarshal([]byte(line), &t) == nil {
			r.traces = append(r.traces, t)
		}
	}
	return r
}

func (r *Recorder) Record(t Trace) {
	t.Timestamp = time.Now().UTC()
	r.traces = append(r.traces, t)
	if r.filePath != "" {
		data, _ := json.Marshal(t)
		os.MkdirAll(filepath.Dir(r.filePath), 0o755)
		f, _ := os.OpenFile(r.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if f != nil {
			f.Write(data)
			f.Write([]byte{'\n'})
			f.Close()
		}
	}
}

func (r *Recorder) All() []Trace  { return r.traces }
func (r *Recorder) Clear()        { r.traces = nil }

func (r *Recorder) Recent(n int) []Trace {
	if n <= 0 || len(r.traces) == 0 {
		return nil
	}
	if n >= len(r.traces) {
		return r.traces
	}
	return r.traces[len(r.traces)-n:]
}

// Summary aggregates trace statistics.
type Summary struct {
	TotalTurns   int            `json:"total_turns"`
	ByRoute      map[string]int `json:"by_route"`
	ByPlugin     map[string]int `json:"by_plugin"`      // renamed from BySkill
	ByTool       map[string]int `json:"by_tool"`
	ByMode       map[string]int `json:"by_mode"`
	ByComplexity map[string]int `json:"by_complexity"`
	ByStatus     map[string]int `json:"by_status"`
	ClarifyCount int            `json:"clarify_count"`
	ErrorCount   int            `json:"error_count"`
}

func Summarize(traces []Trace) Summary {
	s := Summary{
		ByRoute:      map[string]int{},
		ByPlugin:     map[string]int{},
		ByTool:       map[string]int{},
		ByStatus:     map[string]int{},
		ByMode:       map[string]int{},
		ByComplexity: map[string]int{},
	}
	for _, t := range traces {
		s.TotalTurns++
		s.ByRoute[t.Route]++
		s.ByPlugin[t.Plugin]++
		s.ByTool[t.Tool]++
		s.ByStatus[t.Status]++
		if t.Mode != "" {
			s.ByMode[t.Mode]++
		}
		if t.Complexity != "" {
			s.ByComplexity[t.Complexity]++
		}
		if t.WhyClarified != "" {
			s.ClarifyCount++
		}
		if t.Error != "" {
			s.ErrorCount++
		}
	}
	return s
}

func (s Summary) Markdown() string {
	var b strings.Builder
	b.WriteString("# Decision Trace Summary\n\n")
	b.WriteString(fmt.Sprintf("- Total turns: **%d**\n", s.TotalTurns))
	b.WriteString(fmt.Sprintf("- Clarify rate: **%.1f%%**\n", pct(s.ClarifyCount, s.TotalTurns)))
	b.WriteString(fmt.Sprintf("- Error rate: **%.1f%%**\n\n", pct(s.ErrorCount, s.TotalTurns)))

	if len(s.ByMode) > 0 {
		b.WriteString("## Mode Distribution\n\n")
		for k, v := range s.ByMode {
			b.WriteString(fmt.Sprintf("- %s: **%d** (%.0f%%)\n", k, v, pct(v, s.TotalTurns)))
		}
		b.WriteString("\n")
	}
	if len(s.ByComplexity) > 0 {
		b.WriteString("## Complexity Distribution\n\n")
		for k, v := range s.ByComplexity {
			b.WriteString(fmt.Sprintf("- %s: **%d** (%.0f%%)\n", k, v, pct(v, s.TotalTurns)))
		}
		b.WriteString("\n")
	}
	if len(s.ByRoute) > 0 {
		b.WriteString("## By Route\n\n")
		for k, v := range s.ByRoute {
			b.WriteString(fmt.Sprintf("- %s: %d (%.0f%%)\n", k, v, pct(v, s.TotalTurns)))
		}
		b.WriteString("\n")
	}
	if len(s.ByTool) > 0 {
		b.WriteString("## By Tool\n\n")
		for k, v := range s.ByTool {
			b.WriteString(fmt.Sprintf("- %s: %d\n", k, v))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}
