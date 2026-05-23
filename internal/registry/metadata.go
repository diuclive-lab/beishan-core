// Package registry provides structured tool registration with capability
// metadata, toolset composition, and profile-based access policy.
// Ported from 66's tool registry, stripped of MCP/catalog/OOM coupling.
package registry

// ── Metadata ─────────────────────────────────────────────────────────────────

// Metadata describes a registered tool's capabilities and constraints.
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Toolset     string `json:"toolset"`      // "business", "web", "filesystem", "code"
	Category    string `json:"category"`     // "search", "business", "system", "utility"
	HighRisk    bool   `json:"high_risk"`    // write/exec operations
	Cost        string `json:"cost"`         // "low", "medium", "high" (latency/throughput estimate)
	Auth        string `json:"auth"`         // "none", "optional", "required"
}

// ── Capability ───────────────────────────────────────────────────────────────

// Capability is the runtime profile of a tool, derived from its name and metadata.
type Capability struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`      // compact description
	HighRisk    bool   `json:"high_risk"`
	Expensive   bool   `json:"expensive"`    // high compute/cost tool
}

// Classify derives a capability from metadata.
func Classify(m Metadata) Capability {
	expensive := false
	switch m.Cost {
	case "high":
		expensive = true
	}

	return Capability{
		Name:     m.Name,
		Purpose:  truncate(m.Description, 80),
		HighRisk: m.HighRisk,
		Expensive: expensive,
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
