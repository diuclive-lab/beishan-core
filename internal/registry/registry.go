package registry

import (
	"fmt"
	"sort"
)

// ── Lifecycle ────────────────────────────────────────────────────────────────

// Phase controls whether registration is allowed.
type Phase int

const (
	PhaseInit    Phase = iota // tools can be registered
	PhaseRunning              // registry locked, no more registration
)

// ── Registry ─────────────────────────────────────────────────────────────────

// Registry is a lifecycle-gated tool repository. Registration only during init.
type Registry struct {
	tools    map[string]any           // name -> tool instance
	metadata map[string]Metadata       // name -> metadata
	phase    Phase
}

// New creates an empty registry in init phase.
func New() *Registry {
	return &Registry{
		tools:    make(map[string]any),
		metadata: make(map[string]Metadata),
		phase:    PhaseInit,
	}
}

// ── Registration ─────────────────────────────────────────────────────────────

// Register adds a tool with default metadata.
func (r *Registry) Register(name string, tool any, desc string) error {
	if r.phase != PhaseInit {
		return fmt.Errorf("registry: cannot register %q after lock", name)
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("registry: tool %q already registered", name)
	}
	r.tools[name] = tool
	r.metadata[name] = Metadata{
		Name: name, Description: desc,
		Toolset: "default", Category: "utility",
	}
	return nil
}

// RegisterWithMeta adds a tool with explicit metadata.
func (r *Registry) RegisterWithMeta(name string, tool any, meta Metadata) error {
	if r.phase != PhaseInit {
		return fmt.Errorf("registry: cannot register %q after lock", name)
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("registry: tool %q already registered", name)
	}
	meta.Name = name
	if meta.Toolset == "" {
		meta.Toolset = "default"
	}
	if meta.Category == "" {
		meta.Category = "utility"
	}
	r.tools[name] = tool
	r.metadata[name] = meta
	return nil
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

// Lock transitions to running phase. After lock, registration is rejected.
func (r *Registry) Lock() { r.phase = PhaseRunning }

// ── Lookup ───────────────────────────────────────────────────────────────────

// Get returns a tool by name.
func (r *Registry) Get(name string) (any, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Meta returns metadata for a tool.
func (r *Registry) Meta(name string) (Metadata, bool) {
	m, ok := r.metadata[name]
	return m, ok
}

// Names returns all registered tool names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// GetByToolset returns names of tools in a given toolset.
func (r *Registry) GetByToolset(toolset string) []string {
	var names []string
	for n, m := range r.metadata {
		if m.Toolset == toolset {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// GetByCategory returns names of tools in a given category.
func (r *Registry) GetByCategory(category string) []string {
	var names []string
	for n, m := range r.metadata {
		if m.Category == category {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered tools.
func (r *Registry) Count() int { return len(r.tools) }

// List returns all capabilities (for display/discovery).
func (r *Registry) List() []Capability {
	caps := make([]Capability, 0, len(r.metadata))
	for _, m := range r.metadata {
		caps = append(caps, Classify(m))
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i].Name < caps[j].Name })
	return caps
}


// DefaultInstance is the global registry used by tools.Init().
var DefaultInstance = New()
