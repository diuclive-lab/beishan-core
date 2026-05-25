package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Client connects to one MCP server via stdio.
type Client struct {
	Def    ServerDef
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
}

// Connect starts the MCP server process and does the initialize handshake.
func (c *Client) Connect() error {
	c.cmd = exec.Command(c.Def.Command, c.Def.Args...)
	stdin, _ := c.cmd.StdinPipe()
	stdout, _ := c.cmd.StdoutPipe()
	c.stdin = bufio.NewWriter(stdin)
	c.stdout = bufio.NewScanner(stdout)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp start failed: %w", err)
	}

	// Send initialize request
	_, err := c.sendRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "beishan-core", "version": "0.2.0"},
	})
	return err
}

// ListTools returns the tools exposed by this MCP server.
func (c *Client) ListTools() ([]ToolDefinition, error) {
	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server and returns the result.
func (c *Client) CallTool(name string, args map[string]interface{}) (*ToolResult, error) {
	resp, err := c.sendRequest("tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}

	var result ToolResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools/call: %w", err)
	}
	return &result, nil
}

// Close terminates the MCP server.
func (c *Client) Close() error {
	c.sendRequest("shutdown", nil)
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      strings.ReplaceAll(time.Now().Format("150405.000"), ".", ""),
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	data, _ := json.Marshal(req)
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	if _, err := c.stdin.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("write newline: %w", err)
	}
	if err := c.stdin.Flush(); err != nil {
		return nil, fmt.Errorf("flush: %w", err)
	}

	// Read response (one line)
	if !c.stdout.Scan() {
		return nil, fmt.Errorf("mcp server closed: %v", c.stdout.Err())
	}

	var resp struct {
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(c.stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp error: %s", resp.Error.Message)
	}
	return resp.Result, nil
}
