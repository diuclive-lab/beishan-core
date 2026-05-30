package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// servoEngine 是基于 Servo WebDriver 的 Engine 实现。
// Servo 作为子进程启动（--headless --webdriver=port），通过 WebDriver HTTP API 控制。
// 方式 B（子进程 + 控制协议），详见 docs/SERVO_BROWSER_NORTHSTAR.md。
type servoEngine struct {
	cmd       *exec.Cmd
	baseURL   string
	sessionID string
	client    *http.Client
}

// servoPage 是 servoEngine 创建的页面（当前 session 即单页面）。
type servoPage struct {
	engine *servoEngine
}

// findServoPath 定位 Servo 二进制文件。
func findServoPath() string {
	if p := os.Getenv("BEISHAN_SERVO"); p != "" {
		return p
	}
	candidates := []string{
		"/Users/dc/Desktop/cankaocangku/servo/target/release/servoshell",
		"/usr/local/bin/servoshell",
		"/opt/homebrew/bin/servoshell",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// NewServo 创建并启动一个 Servo 浏览器引擎实例。
func NewServo() (Engine, error) {
	svPath := findServoPath()
	if svPath == "" {
		return nil, fmt.Errorf("未找到 Servo（设 BEISHAN_SERVO 或 build Servo）")
	}

	// 找可用端口
	port := findFreePort()
	args := []string{
		"--headless",
		"--webdriver=" + strconv.Itoa(port),
	}

	cmd := exec.Command(svPath, args...)
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 Servo 失败: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 30 * time.Second}

	// 等 WebDriver 就绪
	eng := &servoEngine{
		cmd:     cmd,
		baseURL: baseURL,
		client:  client,
	}
	for i := 0; i < 20; i++ {
		if err := eng.wdGet("/status"); err == nil {
			goto ready
		}
		time.Sleep(500 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	return nil, fmt.Errorf("Servo WebDriver 未在 %ds 内就绪", 10)

ready:
	// 创建 WebDriver session
	session, err := eng.wdPost("/session", map[string]interface{}{
		"capabilities": map[string]interface{}{
			"alwaysMatch": map[string]interface{}{
				"browserName": "servo",
			},
		},
	})
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("Servo 创建 session 失败: %w", err)
	}
	sid, _ := session["sessionId"].(string)
	if sid == "" {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("Servo sessionId 为空")
	}
	eng.sessionID = sid
	return eng, nil
}

// ─── WebDriver 低层 ──────────────────────────────

func (e *servoEngine) wdGet(path string) error {
	resp, err := e.client.Get(e.baseURL + path)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (e *servoEngine) wdPost(path string, body interface{}) (map[string]interface{}, error) {
	data, _ := json.Marshal(body)
	resp, err := e.client.Post(e.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("wd post %s: %w", path, err)
	}
	var val map[string]interface{}
	json.Unmarshal(result.Value, &val)
	return val, nil
}

func (e *servoEngine) wdPostRaw(path string, body interface{}) (json.RawMessage, error) {
	data, _ := json.Marshal(body)
	resp, err := e.client.Post(e.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("wd post %s: %w", path, err)
	}
	return result.Value, nil
}

func (e *servoEngine) wdDelete(path string) error {
	req, err := http.NewRequest("DELETE", e.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ─── Engine 接口实现 ─────────────────────────────

func (e *servoEngine) NewPage(url string) (Page, error) {
	// Servo WebDriver session 内导航
	_, err := e.wdPostRaw(fmt.Sprintf("/session/%s/url", e.sessionID),
		map[string]string{"url": url})
	if err != nil {
		return nil, fmt.Errorf("Servo navigate 失败: %w", err)
	}
	return &servoPage{engine: e}, nil
}

func (e *servoEngine) Close() {
	if e.sessionID != "" {
		e.wdDelete(fmt.Sprintf("/session/%s", e.sessionID))
	}
	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
}

// ─── Page 接口实现 ───────────────────────────────

func (p *servoPage) Eval(script string) (string, error) {
	raw, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/execute/sync", p.engine.sessionID),
		map[string]interface{}{"script": script, "args": []interface{}{}},
	)
	if err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	return string(raw), nil
}

func (p *servoPage) InnerText() (string, error) {
	return p.Eval("return document.body.innerText")
}

func (p *servoPage) InsertText(text string) error {
	// 先聚焦 active element
	_, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/element/active", p.engine.sessionID),
		map[string]interface{}{},
	)
	if err != nil {
		return fmt.Errorf("focus active 元素失败: %w", err)
	}
	// 清空已有内容
	_, err = p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/element/active/clear", p.engine.sessionID),
		map[string]interface{}{},
	)
	if err != nil {
		return fmt.Errorf("clear 失败: %w", err)
	}
	// 输入文本
	_, err = p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/element/active/value", p.engine.sessionID),
		map[string]interface{}{
			"text": text,
			"value": strings.Split(text, ""),
		},
	)
	if err != nil {
		return fmt.Errorf("insert text 失败: %w", err)
	}
	return nil
}

func (p *servoPage) PressKey(key string) error {
	// WebDriver actions 模拟按键
	actions := map[string]interface{}{
		"actions": []interface{}{
			map[string]interface{}{
				"type": "key",
				"id":   "keyboard",
				"actions": []interface{}{
					map[string]interface{}{"type": "keyDown", "value": key},
					map[string]interface{}{"type": "keyUp", "value": key},
				},
			},
		},
	}
	_, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/actions", p.engine.sessionID),
		actions,
	)
	return err
}

func (p *servoPage) Navigate(url string) error {
	_, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/url", p.engine.sessionID),
		map[string]string{"url": url},
	)
	return err
}

func (p *servoPage) URL() (string, error) {
	raw, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/url", p.engine.sessionID),
		map[string]interface{}{},
	)
	if err != nil {
		return "", err
	}
	var url string
	json.Unmarshal(raw, &url)
	return url, nil
}

func (p *servoPage) Screenshot() ([]byte, error) {
	raw, err := p.engine.wdPostRaw(
		fmt.Sprintf("/session/%s/screenshot", p.engine.sessionID),
		map[string]interface{}{},
	)
	if err != nil {
		return nil, err
	}
	var b64 string
	if json.Unmarshal(raw, &b64) != nil || b64 == "" {
		return nil, fmt.Errorf("screenshot 无 data")
	}
	return []byte(b64), nil
}

func (p *servoPage) Close() {
	// Servo session 关闭由 engine.Close 统一处理
}

// findFreePort 找一个可用端口。
func findFreePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 9222
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
