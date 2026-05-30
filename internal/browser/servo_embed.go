package browser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// servoEmbed 是基于 servo-embed Rust 嵌入器的 Engine 实现。
// 通过 stdin/stdout JSON 管道（\0 分隔）与 Rust 进程通信，
// Rust 进程内嵌了 Servo WebDriver 控制。
// 方式 C（薄 Rust 嵌入器），比直接 HTTP WebDriver（方式 B）更稳定。
type servoEmbed struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Reader

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan embedResponse
	closed  bool
}

type embedRequest struct {
	ID     int64                  `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type embedResponse struct {
	ID     int64                  `json:"id"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

// servoEmbedPage 是 servoEmbed 创建的页面。
type servoEmbedPage struct {
	engine *servoEmbed
}

// NewServoEmbed 创建并启动 servo-embed Rust 嵌入器。
func NewServoEmbed() (Engine, error) {
	binary := findServoEmbedPath()
	if binary == "" {
		return nil, fmt.Errorf("未找到 servo-embed（设 BEISHAN_SERVO_EMBED 或 build cmd/servo-embed）")
	}

	cmd := exec.Command(binary)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 servo-embed 失败: %w", err)
	}

	e := &servoEmbed{
		cmd:     cmd,
		stdin:   bufio.NewWriter(stdin),
		stdout:  bufio.NewReaderSize(stdout, 1<<20),
		pending: make(map[int64]chan embedResponse),
	}
	go e.readLoop()

	// Ping to verify
	if _, err := e.send("ping", nil, 5*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("servo-embed ping 失败: %w", err)
	}

	// Start Servo
	if _, err := e.send("start", nil, 15*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("servo-embed start 失败: %w", err)
	}

	return e, nil
}

func findServoEmbedPath() string {
	if p := os.Getenv("BEISHAN_SERVO_EMBED"); p != "" {
		return p
	}
	candidates := []string{
		"/Users/dc/Desktop/0/cmd/servo-embed/target/release/servo-embed",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func (e *servoEmbed) readLoop() {
	for {
		data, err := e.stdout.ReadBytes(0)
		if err != nil {
			e.failPending(fmt.Errorf("管道关闭: %w", err))
			return
		}
		data = data[:max(0, len(data)-1)]
		if len(data) == 0 {
			continue
		}
		var resp embedResponse
		if json.Unmarshal(data, &resp) != nil {
			continue
		}
		e.mu.Lock()
		ch := e.pending[resp.ID]
		delete(e.pending, resp.ID)
		e.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

func (e *servoEmbed) failPending(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for id, ch := range e.pending {
		select {
		case ch <- embedResponse{ID: id, Error: err.Error()}:
		default:
		}
		delete(e.pending, id)
	}
}

func (e *servoEmbed) send(method string, params map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	if e.closed {
		e.mu.Unlock()
		return nil, fmt.Errorf("连接已关闭")
	}
	ch := make(chan embedResponse, 1)
	e.pending[id] = ch
	e.mu.Unlock()

	req := embedRequest{ID: id, Method: method, Params: params}
	data, _ := json.Marshal(req)
	data = append(data, 0)

	e.mu.Lock()
	_, werr := e.stdin.Write(data)
	e.stdin.Flush()
	e.mu.Unlock()

	if werr != nil {
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		return nil, fmt.Errorf("写命令失败: %w", werr)
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		return nil, fmt.Errorf("%s 超时", method)
	}
}

// ─── Engine 接口实现 ─────────────────────────────

func (e *servoEmbed) NewPage(url string) (Page, error) {
	_, err := e.send("navigate", map[string]interface{}{"url": url}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return &servoEmbedPage{engine: e}, nil
}

func (e *servoEmbed) Close() {
	e.send("close", nil, 5*time.Second)
	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
}

// ─── Page 接口实现 ───────────────────────────────

func (p *servoEmbedPage) Eval(script string) (string, error) {
	res, err := p.engine.send("eval", map[string]interface{}{"script": script}, 30*time.Second)
	if err != nil {
		return "", err
	}
	// res["result"] 是来自 Rust embedder 的 WebDriver 返回值
	if r, ok := res["result"]; ok {
		if s, ok := r.(string); ok {
			return s, nil
		}
		return fmt.Sprintf("%v", r), nil
	}
	return "", nil
}

func (p *servoEmbedPage) InnerText() (string, error) {
	res, err := p.engine.send("inner_text", nil, 30*time.Second)
	if err != nil {
		return "", err
	}
	if t, ok := res["text"]; ok {
		if s, ok := t.(string); ok {
			return s, nil
		}
	}
	return "", nil
}

func (p *servoEmbedPage) InsertText(text string) error {
	_, err := p.engine.send("eval", map[string]interface{}{
		"script": fmt.Sprintf(`document.body.innerText = %q`, text),
	}, 10*time.Second)
	return err
}

func (p *servoEmbedPage) PressKey(key string) error {
	_, err := p.engine.send("eval", map[string]interface{}{
		"script": fmt.Sprintf(`document.dispatchEvent(new KeyboardEvent('keydown',{'key':%q}));document.dispatchEvent(new KeyboardEvent('keyup',{'key':%q}))`, key, key),
	}, 10*time.Second)
	return err
}

func (p *servoEmbedPage) Navigate(url string) error {
	_, err := p.engine.send("navigate", map[string]interface{}{"url": url}, 30*time.Second)
	return err
}

func (p *servoEmbedPage) URL() (string, error) {
	res, err := p.engine.send("eval", map[string]interface{}{"script": "return window.location.href"}, 10*time.Second)
	if err != nil {
		return "", err
	}
	r, _ := res["result"].(string)
	return r, nil
}

func (p *servoEmbedPage) Screenshot() ([]byte, error) {
	res, err := p.engine.send("screenshot", nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	data, _ := res["data"].(string)
	return []byte(data), nil
}

func (p *servoEmbedPage) Close() {}
