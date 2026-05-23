package rightflower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
		return nil, fmt.Errorf("yaml 解析失败: %w", err)
	}
	if err := ValidateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ValidateManifest checks manifest fields against allowed values.
func ValidateManifest(m *Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	if m.Protocol != "http" {
		return fmt.Errorf("v0 仅支持 http 协议（当前: %s）", m.Protocol)
		return fmt.Errorf("protocol 必须是 http 或 ipc（当前: %s）", m.Protocol)
	}
	if m.Endpoint == "" {
		return fmt.Errorf("endpoint 不能为空")
	}
	// v0 安全约束：HTTP 仅允许 localhost/127.0.0.1
	if m.Protocol == "http" {
		u, err := url.Parse(m.Endpoint)
		if err != nil {
			return fmt.Errorf("endpoint 不是合法 URL: %w", err)
		}
		host := strings.ToLower(u.Hostname())
		if host != "localhost" && host != "127.0.0.1" {
			return fmt.Errorf("v0 仅允许 localhost/127.0.0.1（当前: %s）", host)
		}
	}
	if m.OutputFormat == "" {
		m.OutputFormat = "diff"
	}
	allowedFormats := map[string]bool{"diff": true, "json": true, "markdown": true}
	if !allowedFormats[m.OutputFormat] {
		return fmt.Errorf("output_format 必须是 diff/json/markdown（当前: %s）", m.OutputFormat)
	}
	allowedSafety := map[string]bool{"sandbox": true, "restricted": true, "trusted": true}
	if !allowedSafety[m.SafetyLevel] {
		return fmt.Errorf("safety_level 必须是 sandbox/restricted/trusted（当前: %s）", m.SafetyLevel)
	}
	if len(m.Capabilities) == 0 {
		return fmt.Errorf("capabilities 不能为空")
	}
	return nil
}

// HTTPError wraps a non-2xx response from a right flower.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("右花返回 HTTP %d: %s", e.StatusCode, e.Body)
}

// Client sends requests to right flowers via HTTP.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) Dispatch(endpoint string, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	httpResp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, &HTTPError{StatusCode: httpResp.StatusCode, Body: snippet}
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse: %w (body: %s)", err, string(respBody[:min(len(respBody), 100)]))
	}
	return &resp, nil
}

// SecurityWrapper marks all findings as unverified.
func SecurityWrapper(result *Result) error {
	if result == nil {
		return nil
	}
	for i := range result.Findings {
		result.Findings[i].Verified = false
	}
	return nil
}
