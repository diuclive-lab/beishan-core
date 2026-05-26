package observatory

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
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

// ── Last pulse storage ──────────────────────────────────────────────

var (
	lastPulse   Pulse
	pulseMu     sync.RWMutex
)

// RecordPulse stores the most recent system health snapshot.
func RecordPulse(p Pulse) {
	pulseMu.Lock()
	lastPulse = p
	pulseMu.Unlock()
}

// LastPulse returns the most recently recorded health snapshot.
func LastPulse() Pulse {
	pulseMu.RLock()
	defer pulseMu.RUnlock()
	return lastPulse
}

// ── Metrics snapshot ────────────────────────────────────────────────

// Snapshot is a point-in-time dump of system observability data.
type Snapshot struct {
	Timestamp     time.Time       `json:"timestamp"`
	Traces        TracesSnapshot  `json:"traces"`
	Events        EventsSnapshot  `json:"events"`
	Health        Pulse           `json:"health"`
	Plugins       int             `json:"plugins,omitempty"`
	Tools         int             `json:"tools,omitempty"`
	UptimeHours   float64         `json:"uptime_hours"`
}

// TracesSnapshot summarizes decision trace data.
type TracesSnapshot struct {
	Total   int              `json:"total"`
	Summary Summary          `json:"summary"`
	Recent  []Trace          `json:"recent,omitempty"`
}

// EventsSnapshot summarizes event data.
type EventsSnapshot struct {
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type,omitempty"`
}

// CollectSnapshot builds a metrics snapshot from current state.
func CollectSnapshot() Snapshot {
	s := Snapshot{Timestamp: time.Now().UTC()}

	// Traces
	recorderMu.RLock()
	if defaultRecorder != nil {
		all := defaultRecorder.All()
		s.Traces.Total = len(all)
		s.Traces.Summary = Summarize(all)
		if n := len(all); n > 10 {
			s.Traces.Recent = all[n-10:]
		} else {
			s.Traces.Recent = all
		}
	}
	recorderMu.RUnlock()

	// Events: count from JSONL file
	eventsMu.RLock()
	eventsFileMu.Lock()
	if eventsFile != nil {
		// Count lines in the events file for today
		fname := eventsFile.Name()
		eventsFileMu.Unlock()
		if data, err := os.ReadFile(fname); err == nil {
			lines := strings.Split(string(data), "\n")
			byType := map[string]int{}
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var evt struct {
					Type string `json:"type"`
				}
				if json.Unmarshal([]byte(line), &evt) == nil && evt.Type != "" {
					byType[evt.Type]++
				}
			}
			s.Events.Total = len(lines)
			s.Events.ByType = byType
		}
	} else {
		eventsFileMu.Unlock()
	}
	eventsMu.RUnlock()

	// Health pulse
	s.Health = LastPulse()

	return s
}

// CollectSnapshotJSON returns the metrics snapshot as JSON bytes.
func CollectSnapshotJSON() []byte {
	data, _ := json.MarshalIndent(CollectSnapshot(), "", "  ")
	return data
}
