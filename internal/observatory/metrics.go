package observatory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Metrics collects behavioral measurements as a flat key-value map.
type Metrics struct {
	Values map[string]float64 `json:"values"`
	Events []string           `json:"events"`
	time   time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{Values: map[string]float64{}, time: time.Now().UTC()}
}

func (m *Metrics) Set(key string, value float64) { m.Values[key] = value }
func (m *Metrics) Inc(key string)                { m.Values[key]++ }

func (m *Metrics) Event(format string, args ...any) {
	m.Events = append(m.Events, fmt.Sprintf(format, args...))
}

func (m *Metrics) Get(key string) float64 { return m.Values[key] }

// Standard metric names.
const (
	MetricClarify       = "clarify_rate"
	MetricSoftExecute   = "soft_execute_rate"
	MetricDirectExecute = "direct_execute_rate"
	MetricError         = "error_rate"
	MetricPreferenceHit = "preference_hit_rate"

	MetricToolCalls  = "tool_calls"
	MetricRetries    = "retry_count"
	MetricFallbacks  = "fallback_count"
	MetricRecoveries = "recovery_count"

	MetricPassRate   = "pass_rate"
	MetricAvgLatency = "avg_latency_ms"
)

// AggregatedMetrics rolls up multiple per-turn snapshots.
type AggregatedMetrics struct {
	Period     string             `json:"period"`
	Rates      map[string]float64 `json:"rates"`
	Counts     map[string]int     `json:"counts"`
	TotalTurns int                `json:"total_turns"`
}

func Aggregate(snapshots []*Metrics, period string) AggregatedMetrics {
	a := AggregatedMetrics{
		Period: period,
		Rates:  map[string]float64{},
		Counts: map[string]int{},
	}
	if len(snapshots) == 0 {
		return a
	}

	rateSums := map[string]float64{}
	rateCount := map[string]int{}

	for _, m := range snapshots {
		a.TotalTurns++
		for k, v := range m.Values {
			if v >= 0 && v <= 1.0 {
				rateSums[k] += v
				rateCount[k]++
			} else {
				a.Counts[k] += int(v)
			}
		}
	}

	for k := range rateSums {
		if rateCount[k] > 0 {
			a.Rates[k] = rateSums[k] / float64(rateCount[k])
		}
	}

	return a
}

func (a AggregatedMetrics) Markdown() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Behavioral Metrics — %s\n\n", a.Period))
	b.WriteString(fmt.Sprintf("- Total turns: %d\n", a.TotalTurns))

	if len(a.Rates) > 0 {
		b.WriteString("\n## Rates\n\n")
		keys := make([]string, 0, len(a.Rates))
		for k := range a.Rates {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("- %s: **%.1f%%**\n", k, a.Rates[k]*100))
		}
	}

	if len(a.Counts) > 0 {
		b.WriteString("\n## Counts\n\n")
		keys := make([]string, 0, len(a.Counts))
		for k := range a.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("- %s: **%d**\n", k, a.Counts[k]))
		}
	}

	return b.String()
}
