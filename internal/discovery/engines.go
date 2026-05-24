// Package discovery scans for local inference engines.
package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Engine describes a detected local inference engine.
type Engine struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Type     string `json:"type"`     // "openai", "ollama", "llamacpp", "kobold"
	Endpoint string `json:"endpoint"` // base URL
	Model    string `json:"model,omitempty"`
}

// ProbeSpec defines how to check for a specific engine.
type probeSpec struct {
	name  string
	port  int
	kind  string
	path  string
	check func([]byte) bool // response validator
}

var probes = []probeSpec{
	// OpenAI-compatible (LM Studio, LocalAI, vLLM, TabbyAPI, GPT4All, oobabooga, TextSynth)
	{name: "LM Studio", port: 1234, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "vLLM", port: 8000, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "TabbyAPI", port: 8000, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "LocalAI", port: 8080, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "TextSynth", port: 8080, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "GPT4All", port: 4891, kind: "openai", path: "/v1/models", check: hasModels},
	{name: "oobabooga", port: 5000, kind: "openai", path: "/v1/models", check: hasModels},

	// Ollama
	{name: "Ollama", port: 11434, kind: "ollama", path: "/api/tags", check: hasModels},

	// llama.cpp / llamafile
	{name: "llama.cpp", port: 8080, kind: "llamacpp", path: "/completion", check: hasContentType},

	// koboldcpp
	{name: "koboldcpp", port: 5001, kind: "kobold", path: "/api/v1/model", check: hasModel},
}

func hasModels(body []byte) bool {
	var resp struct {
		Data []interface{} `json:"data"`
		Models []interface{} `json:"models"`
	}
	if json.Unmarshal(body, &resp) == nil && (len(resp.Data) > 0 || len(resp.Models) > 0) {
		return true
	}
	return false
}

func hasContentType(body []byte) bool {
	return len(body) > 0
}

func hasModel(body []byte) bool {
	var resp struct {
		Result string `json:"result"`
		Model  string `json:"model"`
		Type   string `json:"type"`
	}
	if json.Unmarshal(body, &resp) == nil && (resp.Model != "" || resp.Result != "") {
		return true
	}
	return false
}

// Scan attempts to detect all running inference engines.
func Scan(timeout time.Duration) []Engine {
	var engines []Engine
	seen := map[int]bool{} // ports already detected

	for _, p := range probes {
		if seen[p.port] {
			continue // already found an engine on this port
		}
		url := fmt.Sprintf("http://127.0.0.1:%d%s", p.port, p.path)
		client := &http.Client{Timeout: timeout}
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if p.check(body) {
			engines = append(engines, Engine{
				Name: p.name, Port: p.port,
				Type: p.kind, Endpoint: fmt.Sprintf("http://127.0.0.1:%d", p.port),
			})
			seen[p.port] = true
		}
	}
	return engines
}

// ScanWithModel detects engines and attempts to identify loaded model.
func ScanWithModel(timeout time.Duration) []Engine {
	engines := Scan(timeout)
	for i, e := range engines {
		model, _ := detectModel(e)
		engines[i].Model = model
	}
	return engines
}

func detectModel(e Engine) (string, error) {
	switch e.Type {
	case "ollama":
		return ollamaModel(e.Endpoint)
	case "openai":
		return openaiModel(e.Endpoint)
	}
	return "", nil
}

func ollamaModel(base string) (string, error) {
	resp, err := http.Get(base + "/api/tags")
	if err != nil { return "", err }
	defer resp.Body.Close()
	var result struct {
		Models []struct{ Name string `json:"name"` } `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Models) > 0 {
		return result.Models[0].Name, nil
	}
	return "", nil
}

func openaiModel(base string) (string, error) {
	resp, err := http.Get(base + "/v1/models")
	if err != nil { return "", err }
	defer resp.Body.Close()
	var result struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Data) > 0 {
		return result.Data[0].ID, nil
	}
	return "", nil
}

// Summary returns a human-readable string of detected engines.
func Summary(engines []Engine) string {
	if len(engines) == 0 { return "未发现本地推理引擎" }
	var b strings.Builder
	for _, e := range engines {
		b.WriteString(fmt.Sprintf("  %s (: %d, %s)", e.Name, e.Port, e.Type))
		if e.Model != "" { b.WriteString(fmt.Sprintf(" [%s]", e.Model)) }
		b.WriteString("\n")
	}
	return b.String()
}
