package registry

import "strings"

// ── Profile ──────────────────────────────────────────────────────────────────

// Profile is a named set of tool access rules using glob-style patterns.
// A tool is allowed if any pattern matches its name.
type Profile struct {
	Name        string   `json:"name"`
	Patterns    []string `json:"patterns"`     // glob patterns: "weather", "filesystem_*"
	Includes    []string `json:"includes"`     // other profile names to inherit
}

// ── Built-in Profiles ────────────────────────────────────────────────────────

// DefaultProfiles returns standard TwinFlower profiles.
// nil patterns = all tools allowed (full_local).
func DefaultProfiles() map[string]Profile {
	return map[string]Profile{
		"full_local": {
			Name: "full_local",
		},
		"safe": {
			Name: "safe",
			Patterns: []string{
				"weather", "search", "translate", "stock", "currency",
				"filesystem_list", "filesystem_read", "filesystem_search",
			},
		},
		"research": {
			Name:     "research",
			Includes: []string{"safe"},
			Patterns: []string{"search", "web_*"},
		},
	}
}

// Description is a human-readable summary for display.
func (p Profile) Description() string {
	if p.Patterns == nil {
		return "All tools allowed"
	}
	return strings.Join(p.Patterns, ", ")
}

// ── Policy ───────────────────────────────────────────────────────────────────

// Policy evaluates tool access against profiles.
type Policy struct {
	profiles map[string]Profile
}

// NewPolicy creates a policy from profile definitions.
func NewPolicy(profiles map[string]Profile) *Policy {
	return &Policy{profiles: profiles}
}

// Allowed checks if a tool name is permitted by the given profile.
// Returns true if any pattern or inherited pattern matches.
func (p *Policy) Allowed(toolName string, profileName string) bool {
	profile, ok := p.profiles[profileName]
	if !ok {
		return false
	}

	// nil patterns = all tools allowed
	if profile.Patterns == nil {
		return true
	}

	// Check own patterns
	for _, pattern := range profile.Patterns {
		if matchGlob(toolName, pattern) {
			return true
		}
	}

	// Check inherited profiles
	for _, inc := range profile.Includes {
		if p.Allowed(toolName, inc) {
			return true
		}
	}

	return false
}

// Filter filters a list of tool names through the profile.
func (p *Policy) Filter(toolNames []string, profileName string) []string {
	var result []string
	for _, name := range toolNames {
		if p.Allowed(name, profileName) {
			result = append(result, name)
		}
	}
	return result
}

// ── Glob matching ────────────────────────────────────────────────────────────

// matchGlob checks if a tool name matches a pattern.
// Supports: exact match, "*" suffix wildcard.
func matchGlob(name, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(name, prefix)
	}
	return name == pattern
}
