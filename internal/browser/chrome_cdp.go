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

// chromeCDP 是基于 CDP-over-pipe 的 Engine 实现。
// beishan 自己启动并「拥有」一个 Chrome 进程，
// CDP 走 --remote-debugging-pipe（fd 3/4，null 分隔 JSON）。
type chromeCDP struct {
	cmd *exec.Cmd
	w   *os.File
	r   *os.File
	wmu    sync.Mutex
	idmu   sync.Mutex
	nextID int64
	pmu     sync.Mutex
	pending map[int64]chan cdpMessage
	closed  bool
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// chromePage 是 chromeCDP 创建的页面。
type chromePage struct {
	engine    *chromeCDP
	sessionID string
}

// findChromePath 定位本机 Chrome 可执行文件。
func FindChromePath() string {
	if p := os.Getenv("BEISHAN_CHROME"); p != "" {
		return p
	}
	for _, c := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// NewChrome 创建并启动一个 Chrome 引擎实例。
// userDataDir 为浏览器 profile 目录。
// headless=true 无头模式；headless=false 有头（用于一次性登录）。
func NewChrome(userDataDir string, headless bool) (Engine, error) {
	chromePath := FindChromePath()
	if chromePath == "" {
		return nil, fmt.Errorf("未找到 Chrome（设 BEISHAN_CHROME 或安装 Google Chrome）")
	}
	r3, w3, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	r4, w4, err := os.Pipe()
	if err != nil {
		r3.Close(); w3.Close()
		return nil, err
	}
	args := []string{
		"--remote-debugging-pipe",
		"--user-data-dir=" + userDataDir,
		"--no-first-run", "--no-default-browser-check",
		"--disable-gpu", "--disable-extensions",
		"--disable-background-networking", "--disable-sync",
		"--disable-features=Translate,MediaRouter",
		"--mute-audio",
	}
	if headless {
		args = append(args, "--headless=new")
	}
	args = append(args, "about:blank")

	cmd := exec.Command(chromePath, args...)
	cmd.ExtraFiles = []*os.File{r3, w4}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		r3.Close(); w3.Close(); r4.Close(); w4.Close()
		return nil, fmt.Errorf("启动 Chrome 失败: %w", err)
	}
	r3.Close()
	w4.Close()

	c := &chromeCDP{
		cmd: cmd, w: w3, r: r4,
		pending: make(map[int64]chan cdpMessage),
	}
	go c.readLoop()
	return c, nil
}

func (c *chromeCDP) readLoop() {
	reader := bufio.NewReaderSize(c.r, 1<<20)
	for {
		data, err := reader.ReadBytes(0)
		if err != nil {
			c.failPending(fmt.Errorf("cdp 管道关闭: %w", err))
			return
		}
		data = data[:max(0, len(data)-1)]
		if len(data) == 0 {
			continue
		}
		var msg cdpMessage
		if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
			continue
		}
		c.pmu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.pmu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *chromeCDP) failPending(err error) {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		select {
		case ch <- cdpMessage{Error: &cdpError{Message: err.Error()}}:
		default:
		}
		delete(c.pending, id)
	}
}

func (c *chromeCDP) send(method string, params map[string]interface{}, sessionID string, timeout time.Duration) (json.RawMessage, error) {
	c.idmu.Lock()
	c.nextID++
	id := c.nextID
	c.idmu.Unlock()

	req := map[string]interface{}{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if sessionID != "" {
		req["sessionId"] = sessionID
	}
	body, _ := json.Marshal(req)
	body = append(body, 0)

	ch := make(chan cdpMessage, 1)
	c.pmu.Lock()
	if c.closed {
		c.pmu.Unlock()
		return nil, fmt.Errorf("cdp 连接已关闭")
	}
	c.pending[id] = ch
	c.pmu.Unlock()

	c.wmu.Lock()
	_, werr := c.w.Write(body)
	c.wmu.Unlock()
	if werr != nil {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return nil, fmt.Errorf("写 cdp 失败: %w", werr)
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	case <-time.After(timeout):
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return nil, fmt.Errorf("cdp %s 超时", method)
	}
}

// ─── Engine 接口实现 ─────────────────────────────

func (c *chromeCDP) NewPage(url string) (Page, error) {
	res, err := c.send("Target.createTarget", map[string]interface{}{"url": url}, "", pageTimeout)
	if err != nil {
		return nil, err
	}
	var ct struct{ TargetID string `json:"targetId"` }
	if json.Unmarshal(res, &ct) != nil || ct.TargetID == "" {
		return nil, fmt.Errorf("createTarget 无 targetId")
	}
	res2, err := c.send("Target.attachToTarget", map[string]interface{}{"targetId": ct.TargetID, "flatten": true}, "", pageTimeout)
	if err != nil {
		return nil, err
	}
	var at struct{ SessionID string `json:"sessionId"` }
	if json.Unmarshal(res2, &at) != nil || at.SessionID == "" {
		return nil, fmt.Errorf("attachToTarget 无 sessionId")
	}
	return &chromePage{engine: c, sessionID: at.SessionID}, nil
}

func (c *chromeCDP) Close() {
	if c.w != nil {
		c.w.Close()
	}
	if c.r != nil {
		c.r.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
}

// ─── Page 接口实现 ───────────────────────────────

func (p *chromePage) Eval(js string) (string, error) {
	res, err := p.engine.send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
		"awaitPromise":  true,
	}, p.sessionID, pageTimeout)
	if err != nil {
		return "", err
	}
	var ev struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(res, &ev) != nil {
		return "", fmt.Errorf("eval 响应异常")
	}
	if len(ev.ExceptionDetails) > 0 {
		return "", fmt.Errorf("JS 异常: %s", string(ev.ExceptionDetails))
	}
	if len(ev.Result.Value) == 0 {
		return "", nil
	}
	var s string
	if json.Unmarshal(ev.Result.Value, &s) != nil {
		return string(ev.Result.Value), nil
	}
	return s, nil
}

func (p *chromePage) InnerText() (string, error) {
	return p.Eval("document.body.innerText")
}

func (p *chromePage) InsertText(text string) error {
	_, err := p.engine.send("Input.insertText", map[string]interface{}{"text": text}, p.sessionID, pageTimeout)
	return err
}

func (p *chromePage) PressKey(key string) error {
	base := func(typ string) map[string]interface{} {
		return map[string]interface{}{
			"type": typ, "key": key, "code": key,
			"windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
		}
	}
	if _, err := p.engine.send("Input.dispatchKeyEvent", base("keyDown"), p.sessionID, pageTimeout); err != nil {
		return err
	}
	_, err := p.engine.send("Input.dispatchKeyEvent", base("keyUp"), p.sessionID, pageTimeout)
	return err
}

func (p *chromePage) Navigate(url string) error {
	_, err := p.engine.send("Page.navigate", map[string]interface{}{"url": url}, p.sessionID, pageTimeout)
	return err
}

func (p *chromePage) URL() (string, error) {
	res, err := p.engine.send("Runtime.evaluate", map[string]interface{}{
		"expression":    "window.location.href",
		"returnByValue": true,
		"awaitPromise":  true,
	}, p.sessionID, pageTimeout)
	if err != nil {
		return "", err
	}
	var ev struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &ev) != nil {
		return "", fmt.Errorf("URL eval 响应异常")
	}
	var url string
	json.Unmarshal(ev.Result.Value, &url)
	return url, nil
}

func (p *chromePage) Screenshot() ([]byte, error) {
	res, err := p.engine.send("Page.captureScreenshot", map[string]interface{}{"format": "png"}, p.sessionID, pageTimeout)
	if err != nil {
		return nil, err
	}
	var ss struct{ Data string `json:"data"` }
	if json.Unmarshal(res, &ss) != nil || ss.Data == "" {
		return nil, fmt.Errorf("screenshot 无 data")
	}
	return []byte(ss.Data), nil
}

func (p *chromePage) Close() {
	p.engine.send("Target.closeTarget", map[string]interface{}{}, p.sessionID, 5*time.Second)
}
