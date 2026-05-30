package tools

// cdp.go — 极小的 CDP-over-pipe 客户端：beishan 自己启动并「拥有」一个 Chrome 进程，
// CDP 协议走 Chrome 的 --remote-debugging-pipe（fd 3 收命令 / fd 4 发响应，null 分隔 JSON），
// 不经 websocket、不用 playwright 库、不起外部 driver/daemon。零新增依赖。
//
// 这是「让浏览器成为智能体一部分」的第一步：进程生命周期由 beishan 持有，私有管道通信。
// 远期方向（Servo 内嵌引擎）见 docs。
//
// 设计取舍：CDP 面刻意收到最小——只用 Target.createTarget/attachToTarget + Runtime.evaluate
// + Input.* + Target.closeTarget。页面交互（找输入框/读正文）几乎全走 Runtime.evaluate 跑 JS。

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

// findChrome 定位本机 Chrome 可执行文件（BEISHAN_CHROME 覆盖 + 常见默认路径）。
func findChrome() string {
	if p := os.Getenv("BEISHAN_CHROME"); p != "" {
		return p
	}
	for _, c := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// cdpConn 是一个 beishan 拥有的 Chrome 进程 + 其 CDP 管道。
type cdpConn struct {
	cmd *exec.Cmd
	w   *os.File // 我们写命令 → child fd 3
	r   *os.File // child fd 4 → 我们读响应

	wmu    sync.Mutex // 串行化写
	idmu   sync.Mutex
	nextID int64

	pmu     sync.Mutex
	pending map[int64]chan cdpMessage
	closed  bool
}

// newCDPConn 启动一个 headless Chrome，建立 CDP-over-pipe 连接。
// userDataDir 为持久化 profile（复用一次性登录）；headless=false 用于一次性人工登录。
func newCDPConn(chromePath, userDataDir string, headless bool, extraArgs ...string) (*cdpConn, error) {
	if chromePath == "" {
		return nil, fmt.Errorf("未找到 Chrome 可执行文件（设 BEISHAN_CHROME 或安装 Google Chrome）")
	}
	// fd 3：child 读命令 ← 我们写 w3
	r3, w3, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// fd 4：child 写响应 → 我们读 r4
	r4, w4, err := os.Pipe()
	if err != nil {
		r3.Close()
		w3.Close()
		return nil, err
	}

	args := []string{
		"--remote-debugging-pipe",
		"--user-data-dir=" + userDataDir,
		"--no-first-run", "--no-default-browser-check",
		"--disable-gpu", "--disable-extensions",
		"--disable-background-networking", "--disable-sync",
		"--disable-features=Translate,MediaRouter",
		"--mute-audio", "--no-default-browser-check",
	}
	if headless {
		args = append(args, "--headless=new")
	}
	args = append(args, extraArgs...)
	args = append(args, "about:blank")

	cmd := exec.Command(chromePath, args...)
	cmd.ExtraFiles = []*os.File{r3, w4} // → child fd 3 = r3, fd 4 = w4
	cmd.Stderr = io.Discard             // Chrome stderr 很吵，丢弃

	if err := cmd.Start(); err != nil {
		r3.Close()
		w3.Close()
		r4.Close()
		w4.Close()
		return nil, fmt.Errorf("启动 Chrome 失败: %w", err)
	}
	// parent 关掉 child 的那两端
	r3.Close()
	w4.Close()

	c := &cdpConn{
		cmd:     cmd,
		w:       w3,
		r:       r4,
		pending: make(map[int64]chan cdpMessage),
	}
	go c.readLoop()
	return c, nil
}

func (c *cdpConn) readLoop() {
	reader := bufio.NewReaderSize(c.r, 1<<20)
	for {
		data, err := reader.ReadBytes(0) // CDP-over-pipe 以 \0 分隔
		if err != nil {
			c.failAllPending(fmt.Errorf("cdp 管道关闭: %w", err))
			return
		}
		if len(data) > 0 && data[len(data)-1] == 0 {
			data = data[:len(data)-1]
		}
		if len(data) == 0 {
			continue
		}
		var msg cdpMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			c.pmu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.pmu.Unlock()
			if ch != nil {
				ch <- msg
			}
		}
		// 无 ID 的事件目前忽略（本用例只靠 Runtime.evaluate 轮询，不需要事件流）
	}
}

func (c *cdpConn) failAllPending(err error) {
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

// send 发一条 CDP 命令并等响应。sessionID 非空时附在顶层（flatten 模式）。
func (c *cdpConn) send(method string, params map[string]interface{}, sessionID string, timeout time.Duration) (json.RawMessage, error) {
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
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("写 cdp 命令失败: %w", werr)
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

// attachPage 新建一个页面 target 并 attach（flatten），返回 sessionID。
func (c *cdpConn) attachPage(url string, timeout time.Duration) (sessionID string, err error) {
	res, err := c.send("Target.createTarget", map[string]interface{}{"url": url}, "", timeout)
	if err != nil {
		return "", err
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &ct); err != nil || ct.TargetID == "" {
		return "", fmt.Errorf("createTarget 无 targetId: %v", err)
	}
	res2, err := c.send("Target.attachToTarget", map[string]interface{}{"targetId": ct.TargetID, "flatten": true}, "", timeout)
	if err != nil {
		return "", err
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res2, &at); err != nil || at.SessionID == "" {
		return "", fmt.Errorf("attachToTarget 无 sessionId: %v", err)
	}
	return at.SessionID, nil
}

// evalString 在页面里跑 JS，要求结果是字符串，返回该字符串。
func (c *cdpConn) evalString(sessionID, expr string, timeout time.Duration) (string, error) {
	res, err := c.send("Runtime.evaluate", map[string]interface{}{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	}, sessionID, timeout)
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
	if err := json.Unmarshal(res, &ev); err != nil {
		return "", err
	}
	if len(ev.ExceptionDetails) > 0 {
		return "", fmt.Errorf("JS 异常: %s", string(ev.ExceptionDetails))
	}
	if len(ev.Result.Value) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(ev.Result.Value, &s); err != nil {
		// 非字符串值，原样返回 JSON
		return string(ev.Result.Value), nil
	}
	return s, nil
}

// insertText 把文本「敲」进当前聚焦元素（CDP Input 域，等价真实输入，对 React 输入框友好）。
func (c *cdpConn) insertText(sessionID, text string, timeout time.Duration) error {
	_, err := c.send("Input.insertText", map[string]interface{}{"text": text}, sessionID, timeout)
	return err
}

// pressEnter 发一次 Enter 键（keyDown+keyUp），用于提交聊天输入。
func (c *cdpConn) pressEnter(sessionID string, timeout time.Duration) error {
	base := func(typ string) map[string]interface{} {
		return map[string]interface{}{
			"type": typ, "key": "Enter", "code": "Enter",
			"windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
		}
	}
	if _, err := c.send("Input.dispatchKeyEvent", base("keyDown"), sessionID, timeout); err != nil {
		return err
	}
	_, err := c.send("Input.dispatchKeyEvent", base("keyUp"), sessionID, timeout)
	return err
}

// Close 杀掉 beishan 拥有的 Chrome 进程并回收管道。
func (c *cdpConn) Close() {
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
