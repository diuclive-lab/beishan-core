// Package evidence provides causal tracing — tracking how parameters, skills,
// and outcomes relate. Adapted from 66's Evidence Graph, repositioned as
// exploratory analysis, not a verification gate.
package observatory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

// NodeType is what kind of thing a node represents.
type NodeType string

const (
	NodeParameter NodeType = "parameter" // config value: threshold, decay, bias
	NodeSkill     NodeType = "skill"     // skill: search_skill, filesystem_skill
	NodeOutcome   NodeType = "outcome"   // result: clarify_rate, pass_rate, latency
)

// EdgeType is how two nodes relate.
type EdgeType string

const (
	EdgeAffects     EdgeType = "affects"      // A changes B
	EdgeImproves    EdgeType = "improves"     // A makes B better
	EdgeDegrades    EdgeType = "degrades"     // A makes B worse
	EdgeCorrelates  EdgeType = "correlates"   // A and B change together
)

// Node is one thing in the evidence graph.
type Node struct {
	ID    string   `json:"id"`
	Type  NodeType `json:"type"`
	Label string   `json:"label"`
	Value float64  `json:"value,omitempty"`
}

// Edge is a relationship between two nodes.
type Edge struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Type   EdgeType `json:"type"`
	Weight float64  `json:"weight,omitempty"` // strength of relationship (0-1)
}

// Graph is a collection of nodes and edges representing known relationships.
type Graph struct {
	Name       string    `json:"name"`
	Nodes      []Node    `json:"nodes"`
	Edges      []Edge    `json:"edges"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Observation is a recorded data point in the evidence system.
type Observation struct {
	Source string    `json:"source"` // which edge this supports
	Value  float64   `json:"value"`
	Note   string    `json:"note,omitempty"`
	Time   time.Time `json:"time"`
}

// Report aggregates evidence observations over a time window.
type Report struct {
	Graph        Graph         `json:"graph"`
	Observations []Observation `json:"observations"`
	Period       string        `json:"period"` // e.g. "last_30d"
}

// ── Building ─────────────────────────────────────────────────────────────────

// NewGraph creates an empty graph.
func NewGraph(name string) Graph {
	return Graph{
		Name:       name,
		Nodes:      make([]Node, 0),
		Edges:      make([]Edge, 0),
		GeneratedAt: time.Now().UTC(),
	}
}

// AddNode adds a node if not already present.
func (g *Graph) AddNode(id string, typ NodeType, label string, value float64) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return
		}
	}
	g.Nodes = append(g.Nodes, Node{ID: id, Type: typ, Label: label, Value: value})
}

// AddEdge adds a directed relationship.
func (g *Graph) AddEdge(from, to string, typ EdgeType, weight float64) {
	g.Edges = append(g.Edges, Edge{From: from, To: to, Type: typ, Weight: weight})
}

// ── Known Relationships ──────────────────────────────────────────────────────

// BuildDefaultGraph returns the default evidence graph for TwinFlower.
// These relationships are known from 66's experience and TwinFlower's design.
func BuildDefaultGraph() Graph {
	g := NewGraph("twinflower_cognition")

	// Parameters
	g.AddNode("clarify_threshold", NodeParameter, "Clarify Threshold", 0.45)
	g.AddNode("decay_lambda", NodeParameter, "Recency Decay Rate", 0.05)
	g.AddNode("tool_bias", NodeParameter, "Tool Selection Bias", 0)

	// Skills
	g.AddNode("search_skill", NodeSkill, "Search Skill", 0)
	g.AddNode("filesystem_skill", NodeSkill, "Filesystem Skill", 0)
	g.AddNode("clarify_skill", NodeSkill, "Clarify Skill", 0)

	// Outcomes
	g.AddNode("clarify_rate", NodeOutcome, "Clarify Rate", 0)
	g.AddNode("soft_execute_rate", NodeOutcome, "Soft Execute Rate", 0)
	g.AddNode("pass_rate", NodeOutcome, "Eval Pass Rate", 0)
	g.AddNode("latency_ms", NodeOutcome, "Response Latency", 0)

	// Edges: parameter → outcome
	g.AddEdge("clarify_threshold", "clarify_rate", EdgeAffects, 0.9)
	g.AddEdge("clarify_threshold", "soft_execute_rate", EdgeAffects, 0.6)
	g.AddEdge("decay_lambda", "clarify_rate", EdgeAffects, 0.3)

	// Edges: skill → outcome
	g.AddEdge("search_skill", "pass_rate", EdgeImproves, 0.7)
	g.AddEdge("filesystem_skill", "pass_rate", EdgeImproves, 0.6)
	g.AddEdge("clarify_skill", "clarify_rate", EdgeAffects, 0.8)

	return g
}

// ── Reporting ─────────────────────────────────────────────────────────────────

// NewReport creates a report from a graph and observations.
func NewReport(graph Graph, observations []Observation, period string) Report {
	return Report{
		Graph:        graph,
		Observations: observations,
		Period:       period,
	}
}

// Markdown renders the evidence report.
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Evidence Graph: %s\n\n", r.Graph.Name))
	b.WriteString(fmt.Sprintf("- Period: %s\n", r.Period))
	b.WriteString(fmt.Sprintf("- Observations: %d\n", len(r.Observations)))
	b.WriteString(fmt.Sprintf("- Known relationships: %d\n\n", len(r.Graph.Edges)))

	// Group edges by type
	byType := map[EdgeType][]Edge{}
	for _, e := range r.Graph.Edges {
		byType[e.Type] = append(byType[e.Type], e)
	}

	for _, typ := range []EdgeType{EdgeAffects, EdgeImproves, EdgeDegrades, EdgeCorrelates} {
		edges := byType[typ]
		if len(edges) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", string(typ)))
		for _, e := range edges {
			from := nodeLabel(r.Graph.Nodes, e.From)
			to := nodeLabel(r.Graph.Nodes, e.To)
			b.WriteString(fmt.Sprintf("- %s → %s", from, to))
			if e.Weight > 0 {
				b.WriteString(fmt.Sprintf(" (strength=%.2f)", e.Weight))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(r.Observations) > 0 {
		b.WriteString("## Recent Observations\n\n")
		sorted := make([]Observation, len(r.Observations))
		copy(sorted, r.Observations)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.After(sorted[j].Time) })

		for _, o := range sorted[:min(len(sorted), 20)] {
			b.WriteString(fmt.Sprintf("- `%s` | %.2f", o.Source, o.Value))
			if o.Note != "" {
				b.WriteString(fmt.Sprintf(" — %s", o.Note))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	b.WriteString("_Evidence graph is an observational tool. It visualizes relationships; it does not enforce them._\n")
	return b.String()
}

func nodeLabel(nodes []Node, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return id
}

func min(a, b int) int { if a < b { return a }; return b }
