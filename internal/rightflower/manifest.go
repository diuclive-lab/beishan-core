// Package rightflower implements the Right Flower Protocol v0.1.
// Right flowers are external tools that connect to beishan-core through
// a defined protocol (HTTP or IPC) and are managed by the hardening layer.
package rightflower

// Manifest describes an external right flower's capabilities and connection.
type Manifest struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Protocol     string   `yaml:"protocol"`
	Endpoint     string   `yaml:"endpoint"`
	Enabled      bool     `yaml:"enabled"`
	RouteExposed bool     `yaml:"route_exposed"`
	Capabilities []string `yaml:"capabilities"`
	OutputFormat string   `yaml:"output_format"`
	SafetyLevel  string   `yaml:"safety_level"`
}

// Request is sent from base to a right flower.
type Request struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // "dispatch"
	Sender      string                 `json:"sender"`
	Recipient   string                 `json:"recipient"`
	Method      string                 `json:"method"`
	Params      map[string]interface{} `json:"params"`
}

// Response is returned from a right flower to the base.
type Response struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "response" or "error"
	Result *Result `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Result carries the right flower's output.
type Result struct {
	Diff      string                   `json:"diff,omitempty"`
	Findings  []Finding                `json:"findings,omitempty"`
	Metadata  map[string]interface{}   `json:"metadata,omitempty"`
	Flower    string                   `json:"flower,omitempty"`
	Method    string                   `json:"method,omitempty"`
	Timestamp string                   `json:"timestamp,omitempty"`
	Kind      string                   `json:"kind,omitempty"`  // "rightflower"
}

// Finding is an observation returned by the right flower.
type Finding struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Verified   bool   `json:"verified"` // always false from external
	Source     string `json:"source"`
}
