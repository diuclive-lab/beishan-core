package observatory

import (
	"fmt"
	"strings"
	"time"
)

// Pulse is a lightweight system health snapshot.
type Pulse struct {
	Timestamp     time.Time `json:"timestamp"`
	Provider      string    `json:"provider"`       // "healthy", "degraded", "down"
	ToolCount     int       `json:"tool_count"`
	KnowledgeSize int       `json:"knowledge_size"` // stored entries
	TraceCount    int              `json:"trace_count"`
	Failure       FailureTaxonomy  `json:"failure,omitempty"`    // decision traces recorded
	LastError     string    `json:"last_error,omitempty"`
	UptimeHours   float64   `json:"uptime_hours"`
}

func Check(providerOK bool, toolCount, knowledgeSize, traceCount int, lastErr string, uptime time.Duration, metrics ...map[string]float64) Pulse {
	var ft FailureTaxonomy
	if len(metrics) > 0 { ft = Classify(metrics[0]) }
	provider := "healthy"
	if !providerOK {
		provider = "down"
	}
	return Pulse{
		Timestamp:     time.Now().UTC(),
		Provider:      provider,
		ToolCount:     toolCount,
		KnowledgeSize: knowledgeSize,
		TraceCount:    traceCount,
		LastError:     lastErr,
		UptimeHours:   uptime.Hours(),
		Failure:       ft,
	}
}

func (p Pulse) Markdown() string {
	var b strings.Builder
	b.WriteString("# System Pulse\n\n")
	b.WriteString(fmt.Sprintf("- **Provider:** %s\n", p.Provider))
	b.WriteString(fmt.Sprintf("- **Tools:** %d\n", p.ToolCount))
	b.WriteString(fmt.Sprintf("- **Knowledge base:** %d entries\n", p.KnowledgeSize))
	b.WriteString(fmt.Sprintf("- **Traces recorded:** %d\n", p.TraceCount))
	b.WriteString(fmt.Sprintf("- **Uptime:** %.1f hours\n", p.UptimeHours))
	if p.LastError != "" {
		b.WriteString(fmt.Sprintf("- **Last error:** %s\n", p.LastError))
	}
	return b.String()
}
