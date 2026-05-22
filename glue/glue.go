package glue

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"beishan/kernel"
)

const (
	maxMsgSize      = 10 * 1024 * 1024  // 10MB — 单条消息上限
	healthCheckIntv = 30 * time.Second  // 健康检查间隔
	maxRestarts     = 3                  // 5 分钟内最大重启次数
	restartWindow   = 5 * time.Minute   // 重启计数窗口
)

/* GlueLayer 是内核与 Python 插件之间的胶水层。

   它作为内核的一个 Plugin 注册，管理所有 Python 子进程的生命周期。
   内核不直接感知 Python 插件的存在，只看到 GlueLayer 实现了 Plugin 接口。
*/
type GlueLayer struct {
	kernel   *kernel.Kernel
	dir      string          // 插件目录路径
	procs    map[string]*proc // 插件名 → 子进程
	mu       sync.RWMutex
	manifests []Manifest     // 存 manifest 用于重启
	stopHealth chan struct{}
}

/* proc 代表一个 Python 插件子进程。 */
type proc struct {
	name      string
	cmd       *exec.Cmd
	stdin     *bufio.Writer
	stdout    *bufio.Scanner
	alive     bool          // 子进程是否存活
	restarts  int           // 重启次数
	lastRest  time.Time     // 上次重启时间
	manifest  Manifest      // 用于重启
}

/* New 创建胶水层实例，还不启动子进程。 */
func New(k *kernel.Kernel, pluginDir string) *GlueLayer {
	return &GlueLayer{
		kernel: k,
		dir:    pluginDir,
		procs:  make(map[string]*proc),
	}
}

/* Start 扫描插件目录，启动所有子进程，注册到内核。

   启动流程：
   1. 扫描 plugins/ 目录下的所有 manifest.json
   2. 对每个合法插件 spawn 子进程
   3. 等待子进程发 register 确认
   4. 将该插件名注册到内核
   5. 注册完成后，该插件名的消息会路由到 GlueLayer.OnMessage
*/
func (g *GlueLayer) Start() error {
	var err error
	g.manifests, err = ScanDir(g.dir)
	if err != nil {
		return err
	}

	for _, m := range g.manifests {
		if err := g.spawn(m); err != nil {
			log.Printf("[Glue] 插件 %s 启动失败: %v", m.Name, err)
			continue
		}
	}

	// 启动健康检查
	g.stopHealth = make(chan struct{})
	go g.healthCheckLoop()

	log.Printf("[Glue] 胶水层就绪，已启动 %d 个插件", len(g.procs))
	return nil
}

func (g *GlueLayer) spawn(m Manifest) error {
	// 路线 A：检测 requirements.txt，自动 pip install
	reqFile := filepath.Join(g.dir, m.Name, "requirements.txt")
	if _, err := os.Stat(reqFile); err == nil {
		install := exec.Command("pip3", "install", "-r", reqFile)
		install.Stderr = os.Stderr
		install.Stdout = os.Stderr
		if err := install.Run(); err != nil {
			return fmt.Errorf("pip install 失败: %w", err)
		}
		log.Printf("[Glue] 插件 %s 依赖安装完成", m.Name)
	}

	entryPath := filepath.Join(g.dir, m.Name, m.Entry)

	var cmd *exec.Cmd
	switch m.Type {
	case "go":
		cmd = exec.Command(entryPath) // 已编译好的二进制
	case "python", "":
		cmd = exec.Command("python3", entryPath) // Python 脚本
	default:
		return fmt.Errorf("不支持的插件类型: %s", m.Type)
	}
	cmd.Stderr = os.Stderr // 子进程的 stderr 直接输出到终端，便于调试

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 stdin pipe 失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 stdout pipe 失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动子进程失败: %w", err)
	}

	p := &proc{
		name:     m.Name,
		cmd:      cmd,
		stdin:    bufio.NewWriter(stdin),
		stdout:   bufio.NewScanner(stdout),
		alive:    false,
		manifest: m,
		lastRest: time.Now(),
	}

	// 等待子进程发 register 确认（最长 5 秒）
	if err := g.waitRegister(p, 5*time.Second); err != nil {
		cmd.Process.Kill()
		return err
	}

	p.alive = true

	// 注册到内核（首次启动时注册，重启时已存在则跳过）
	g.mu.Lock()
	_, alreadyRegistered := g.procs[m.Name]
	g.procs[m.Name] = p
	g.mu.Unlock()

	if !alreadyRegistered {
		g.kernel.Register(m.Name, g)
	}
	log.Printf("[Glue] 插件 %s 已就绪 (PID %d)", m.Name, cmd.Process.Pid)
	return nil
}

func (g *GlueLayer) waitRegister(p *proc, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		if p.stdout.Scan() {
			var msg ProtocolMessage
			if err := json.Unmarshal(p.stdout.Bytes(), &msg); err != nil {
				done <- fmt.Errorf("解析 register 消息失败: %w", err)
				return
			}
			if msg.Type != "register" {
				done <- fmt.Errorf("期望 register，收到: %s", msg.Type)
				return
			}
			if msg.Name != p.name {
				done <- fmt.Errorf("插件名不匹配: 期望 %s, 收到 %s", p.name, msg.Name)
				return
			}
			done <- nil
		} else {
			done <- fmt.Errorf("子进程过早退出")
		}
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("等待 register 超时")
	}
}

/* OnMessage 实现 kernel.Plugin 接口。

   内核路由消息到此插件时，胶水层负责转发到对应的子进程。
   msg.Recipient 指定了要发给哪个子进程。
*/
func (g *GlueLayer) OnMessage(msg kernel.Message) (kernel.Message, error) {
	g.mu.RLock()
	p, ok := g.procs[msg.Recipient]
	g.mu.RUnlock()

	if !ok {
		return kernel.Message{}, fmt.Errorf("[Glue] 未知插件: %s", msg.Recipient)
	}

	// 消息大小保护
	if len(msg.Payload) > maxMsgSize {
		return kernel.Message{}, fmt.Errorf("[Glue] 消息过大: %d bytes（上限 %d）", len(msg.Payload), maxMsgSize)
	}

	// 子进程失效时尝试重启一次
	if !p.alive {
		log.Printf("[Glue] 插件 %s 已失效，自动重启", msg.Recipient)
		if err := g.respawn(msg.Recipient); err != nil {
			return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s 重启失败: %w", msg.Recipient, err)
		}
		// 重新获取 proc
		g.mu.RLock()
		p = g.procs[msg.Recipient]
		g.mu.RUnlock()
		if p == nil {
			return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s 重启后仍不可用", msg.Recipient)
		}
	}

	// 构造 dispatch 消息，注入链路元数据
	traceID := newTraceID()
	dispatch := ProtocolMessage{
		Type:       "dispatch",
		ID:         msg.CorrelationID,
		TraceID:    traceID,
		Timestamp:  time.Now().Unix(),
		RetryCount: 0,
		Sender:     msg.Sender,
		MsgType:    msg.Type,
		Payload:    msg.Payload,
	}

	data, err := json.Marshal(dispatch)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("序列化 dispatch 失败: %w", err)
	}

	// 写入子进程 stdin
	if _, err := p.stdin.Write(data); err != nil {
		p.alive = false // 写入失败 → 标记为失效，由健康检查重启
		return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s stdin 写入失败，已标记失效: %w", msg.Recipient, err)
	}
	if err := p.stdin.WriteByte('\n'); err != nil {
		p.alive = false
		return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s 换行写入失败，已标记失效: %w", msg.Recipient, err)
	}
	if err := p.stdin.Flush(); err != nil {
		p.alive = false
		return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s 刷新失败，已标记失效: %w", msg.Recipient, err)
	}

	// 从子进程 stdout 读取响应（带 30 秒超时）
	response, err := g.readResponse(p, 30*time.Second)
	if err != nil {
		p.alive = false // 读取失败 → 标记为失效
		return kernel.Message{}, fmt.Errorf("[Glue] 插件 %s 响应失败，已标记失效: %w", msg.Recipient, err)
	}

	// 如果有关联 ID，把子进程响应包装为 Message 返回给内核
	if msg.CorrelationID != "" && response != nil {
		payload, _ := json.Marshal(response)
		return kernel.Message{
			Sender:        msg.Recipient,
			Recipient:     msg.Sender,
			Type:          msg.Type + ".response",
			Payload:       payload,
			CorrelationID: msg.CorrelationID,
		}, nil
	}

	log.Printf("[Glue] 插件 %s 处理完成", msg.Recipient)
	return kernel.Message{}, nil
}

// healthCheckLoop 定期检查子进程存活状态，异常时自动重启。
func (g *GlueLayer) healthCheckLoop() {
	ticker := time.NewTicker(healthCheckIntv)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.mu.RLock()
			names := make([]string, 0, len(g.procs))
			for name, p := range g.procs {
				if !p.alive {
					names = append(names, name)
				}
			}
			g.mu.RUnlock()

			for _, name := range names {
				log.Printf("[Glue] 检测到插件 %s 已失效，尝试重启", name)
				if err := g.respawn(name); err != nil {
					log.Printf("[Glue] 插件 %s 重启失败: %v", name, err)
				}
			}

		case <-g.stopHealth:
			return
		}
	}
}

// respawn 重启指定名称的子进程。
func (g *GlueLayer) respawn(name string) error {
	g.mu.Lock()
	p, ok := g.procs[name]
	if !ok {
		g.mu.Unlock()
		return fmt.Errorf("未知插件: %s", name)
	}

	// 重启计数保护：5分钟内超过 maxRestarts 次则熔断
	now := time.Now()
	if now.Sub(p.lastRest) < restartWindow {
		p.restarts++
		if p.restarts > maxRestarts {
			g.mu.Unlock()
			return fmt.Errorf("熔断保护：插件 %s 5分钟内重启 %d 次", name, maxRestarts)
		}
	} else {
		p.restarts = 1
	}
	p.lastRest = now
	g.mu.Unlock()

	// 找到 manifest
	var m Manifest
	for _, mm := range g.manifests {
		if mm.Name == name {
			m = mm
			break
		}
	}
	if m.Name == "" {
		return fmt.Errorf("找不到插件 %s 的 manifest", name)
	}

	// 清理旧进程（发送 SIGKILL 后等待退出）
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		waitDone := make(chan struct{}, 1)
		go func() { p.cmd.Wait(); waitDone <- struct{}{} }()
		select {
		case <-waitDone:
		case <-time.After(3 * time.Second):
			log.Printf("[Glue] 插件 %s 旧进程等待超时，强制继续", name)
		}
	}

	// 重新 spawn
	return g.spawn(m)
}

func (g *GlueLayer) readResponse(p *proc, timeout time.Duration) (*ProtocolMessage, error) {
	done := make(chan *ProtocolMessage, 1)
	errCh := make(chan error, 1)

	go func() {
		if p.stdout.Scan() {
			var msg ProtocolMessage
			if err := json.Unmarshal(p.stdout.Bytes(), &msg); err != nil {
				errCh <- fmt.Errorf("解析 response 失败: %w", err)
				return
			}
			done <- &msg
		} else {
			errCh <- fmt.Errorf("子进程 stdout 关闭")
		}
	}()

	select {
	case msg := <-done:
		return msg, nil
	case err := <-errCh:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("读取响应超时")
	}
}

/* Shutdown 关闭所有子进程。

   向每个子进程发送 shutdown 消息，等待它们退出。
*/
func (g *GlueLayer) Shutdown() {
	// 停止健康检查
	if g.stopHealth != nil {
		close(g.stopHealth)
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	for name, p := range g.procs {
		msg := ProtocolMessage{Type: "shutdown"}
		data, _ := json.Marshal(msg)
		p.stdin.Write(data)
		p.stdin.WriteByte('\n')
		p.stdin.Flush()

		done := make(chan struct{}, 1)
		go func() {
			p.cmd.Wait()
			done <- struct{}{}
		}()

		select {
		case <-done:
			log.Printf("[Glue] 插件 %s 已退出", name)
		case <-time.After(5 * time.Second):
			p.cmd.Process.Kill()
			log.Printf("[Glue] 插件 %s 强制终止", name)
		}
	}
}

/* newTraceID 生成 8 字节随机全链路追踪 ID。

   在每次 dispatch 时由胶水层自动注入。
   L3 校验失败时可通过 TraceID 关联回原始请求和 LLM 输出。
*/
func newTraceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
