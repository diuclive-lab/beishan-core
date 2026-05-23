package rightflower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Registry holds all registered right flowers.
type Registry struct {
	Flowers map[string]*Manifest
}

func NewRegistry() *Registry {
	return &Registry{Flowers: make(map[string]*Manifest)}
}

// LoadDir scans a directory for .yaml manifest files and loads them.
func (r *Registry) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		m, err := loadManifest(filepath.Join(dir, e.Name()))
		if err != nil {
			fmt.Printf("[rightflower] 跳过 %s: %v\n", e.Name(), err)
			continue
		}
		r.Flowers[m.Name] = m
		fmt.Printf("[rightflower] 注册: %s (%s)\n", m.Name, m.Type)
	}
	return nil
}

func loadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("缺少 name 字段")
	}
	if m.Protocol == "" {
		m.Protocol = "http"
	}
	if m.SafetyLevel == "" {
		m.SafetyLevel = "sandbox"
	}
	return &m, nil
}

// Client sends dispatch requests to a right flower via HTTP.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Dispatch sends a request to a right flower endpoint.
func (c *Client) Dispatch(endpoint string, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpResp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &resp, nil
}

// SecurityWrapper applies hardening checks to a right flower result.
// All file writes must go through code_security_check + code_apply.
func SecurityWrapper(result *Result) error {
	if result == nil {
		return nil
	}
	// Right flower results are always unverified
	for i := range result.Findings {
		result.Findings[i].Verified = false
	}
	// If there's a diff, it must go through hardening layer before apply
	if result.Diff != "" {
		// The caller (workflow) must invoke code_security_check and code_apply
		// before writing. This wrapper only marks the result.
		return nil
	}
	return nil
}
