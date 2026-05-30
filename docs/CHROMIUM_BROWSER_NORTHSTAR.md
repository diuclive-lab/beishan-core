# North-Star：把 Chromium 浏览器引擎深度内化（参考 OWL 架构）

> 这是一份交接文档，给 AI 辅助开发的接手者。
> 读完应能：理解 Chromium 路线 vs Servo 路线的定位差异、看清现状、按分阶段路线图推进。
>
> 与 `docs/SERVO_BROWSER_NORTHSTAR.md` 互补。
> Servo 是远期北极星（in-process 终态），Chromium 是中期现实路线（服务化运行时）。

---

## 1. 愿景：Chromium 成为智能体的浏览器运行时

ChatGPT Atlas 的 OWL 架构展示了一条清晰的路线：**把 Chromium 从"被集成的浏览器"变成"可被 AI 原生调度的服务化运行时"。**

我们的 Chromium 路线与 Servo 路线不冲突，而是互补：

| 维度 | Servo 路线（远期） | Chromium 路线（中期） |
|------|-------------------|---------------------|
| 引擎 | Rust + libservo, in-process FFI | C++ + Chromium, 子进程服务化 |
| 成熟度 | 实验性，WebDriver 部分支持 | 生产级，CDP 完全成熟 |
| 渲染 | headless 够用 | 需要时可全渲染 |
| 控制协议 | WebDriver / 自定义 IPC | CDP（Chrome DevTools Protocol） |
| 构建集成 | go build + cgo + Rust 链 | 依赖系统 Chrome，无构建侵入 |

**共同点**：共享 `internal/browser/` 接口，上层工具无感知。

---

## 2. 现状与轨迹

### 已完成（阶段 0-3 等效）

| 能力 | 位置 | 状态 |
|------|------|:----:|
| CDP-over-pipe 传输 | `chrome_cdp.go` | ✅ 生产可用 |
| Engine/Page 接口 | `engine.go` | ✅ |
| 硬化层集成（`agentSourceAllowed`） | `browser.go` | ✅ |
| 会话隔离（`ChromeConfig.Incognito`） | `chrome_cdp.go` | ✅ |
| 右花 manifest | `chromium.yaml` | ✅ |
| DeepSeek 网页搜索 | `deepseek_web.go` | ✅ 端到端验证 |
| 一键 Agent 会话 | `NewAgentSession()` | ✅ |

### 轨迹

```
HTTP CDP（playwright，旧）
→ CDP-over-pipe（自有进程 + 私有管道，现在）
→ 服务化 Chromium 运行时（多 session 管理 + 能力路由，下一步）
→ Chromium 嵌入 + 定制启动参数（硬件加速、指纹管理、反爬对抗，远期）
          ↑ 这与 Servo 的 in-process 是平行路线，不冲突
```

---

## 3. 为什么 Chromium 路线独立存在

SERVO_NORTHSTAR 已经覆盖了 Engine/Page 抽象、阶段 3-4 的 Servo 后端、阶段 5 的 in-process FFI 规划。为什么还需要一条 Chromium 路线？

**因为 Chromium 有 Servo 无法替代的能力：**

| 能力 | Chromium | Servo | 对我们是否重要 |
|------|----------|-------|:-------------:|
| 完整 CDP 协议 | ✅ 成熟 | ❌ WebDriver 部分支持 | **是** — 截图、网络拦截、性能分析 |
| 反爬对抗（指纹、UA、JS 渲染） | ✅ 完整 | ⚠️ 基础 | **是** — 搜索场景的核心需求 |
| GPU 加速渲染 | ✅ 硬件加速 | ⚠️ WebRender 实验性 | 当前 headless 不需要 |
| 扩展生态（Chrome Extensions） | ✅ 完整 | ❌ 无 | 中期可能重要 |
| 网络栈（QUIC/HTTP3/代理） | ✅ 完整 | ⚠️ 基础 | headless 场景中等 |
| 多进程安全模型 | ✅ Sandbox + Site Isolation | ⚠️ 单进程 | 高 — Agent 安全依赖 |

**结论**：Chromium 路线不是"等 Servo 成熟前的临时方案"，而是在可预见的未来里，某些能力（CDP 完整度、反爬、安全模型）只能由 Chromium 提供。

---

## 4. 架构设计：从 CDP-over-pipe 到服务化运行时

参考 OWL 的 Client/Host 架构，我们把当前扁平化的 CDP 控制升级为分层结构：

```
当前（已实现）：
Agent Tool → chromeCDP (CDP-over-pipe) → Chrome 进程

目标（服务化运行时）：
Agent Tool → Session Manager → Chrome Pool
              ├── Session 1 (incognito, agent task)
              ├── Session 2 (incognito, agent task)  
              └── Session 3 (持久, 用户 profile)
```

### Chrome Session Manager

```go
// internal/browser/chrome_manager.go

type SessionManager struct {
    pool   map[string]*chromeSession
    mu     sync.RWMutex
}

type chromeSession struct {
    ID        string
    Engine    Engine
    CreatedAt time.Time
    Config    ChromeConfig
    // 用于 OWL StoragePartition 风格的隔离
    DataDir   string  // temp dir, 结束后清理
}
```

### 能力路由（参考 OWL Capability Router）

参考 OWL 的三层能力模型，为 Chromium 工具定义能力级别：

| 能力 | 级别 | 说明 |
|------|:----:|------|
| `browser.navigate` | L1 | 导航、阅读页面 — Agent 可用 |
| `browser.eval` | L2 | 执行 JS — 需 `source=user` |
| `browser.screenshot` | L2 | 截图 — 需 `source=user` |
| `browser.download` | L3 | 文件下载 — 需用户确认 |
| `browser.network` | L3 | 网络拦截、修改请求 — 需用户确认 |
| `browser.identity` | L3 | 身份/密码 — 需用户确认 |

---

## 5. 分阶段路线图

### 阶段 0（✅ 已完成）：CDP-over-pipe + Chrome

`chrome_cdp.go` + `deepseek_web.go`。已验证。

### 阶段 1（✅ 已完成）：Engine/Page 接口抽象

`engine.go` + `chrome_cdp.go` 接口化。

### 阶段 2（✅ 已完成）：硬化 + 会话隔离

`agentSourceAllowed` + `ChromeConfig.Incognito`。

### 阶段 3：Session Manager + 多会话复用（新）

**问题**：当前每次 `deepseek_web_search` 都启停一个 Chrome 进程（~5s 启动时间）。同 profile 不能并发。

**方案**：

```go
type SessionManager struct {
    sessions map[string]*chromeSession
}

// Acquire 获取或创建 Chrome 会话。
// Incognito=true 时总是创建新临时会话。
// Incognito=false 时复用持久会话。
func (sm *SessionManager) Acquire(cfg ChromeConfig) (*chromeSession, error)

// Release 归还会话。incognito 会话在此销毁。
func (sm *SessionManager) Release(id string)
```

**验证判据**：
- 两次 `Acquire(Incognito=false)` 返回同一引擎实例（复用）
- 两次 `Acquire(Incognito=true)` 返回隔离实例
- 并发 Agent 任务使用不同 session，互不干扰
- session 结束后 temp dir 被清理

**工期**：1-2 天

### 阶段 4：CDP 能力扩展（截图/网络拦截/性能）

**问题**：当前只用了 4 个 CDP 命令（createTarget, attachToTarget, Runtime.evaluate, Input.*）。CDP 协议有超过 100 个域，很多对 Agent 有用。

**目标能力**：

| CDP 域 | 用途 | 优先级 |
|--------|------|:------:|
| `Page.captureScreenshot` | 截图（已有） | ✅ |
| `Page.printToPDF` | 导出 PDF | 中 |
| `Network.enable` / `Network.getResponseBody` | 捕获网络响应 | 高 |
| `Performance.enable` / `Performance.getMetrics` | 页面性能 | 中 |
| `Security.enable` / `Security.securityStateChanged` | 安全检测 | 中 |
| `Audits.enable` / `Audits.getEncodedResponse` | 无障碍/最佳实践 | 低 |

**验证判据**：新 CDP 命令注册为可选 `Page` 接口扩展（Go 类型断言检测），不破坏现有接口。

**工期**：2-3 天

### 阶段 5：指纹管理 + 反爬对抗

**问题**：当前 Chrome 用 `--headless=new`，HeadlessChrome UA 容易被识别。部分网站会拒绝 headless 浏览器访问。

**方案**：
- `ChromeConfig.Fingerprint` — 自定义 UA、平台、分辨率
- `ChromeConfig.Features` — 启用/禁用 WebGL、Canvas、AudioContext 等
- 基于真实用户 profile 的持久化指纹

**验证判据**：配置指纹后，目标网站将 headless Chrome 识别为正常浏览器。

**工期**：3-5 天（需实验 + 调参）

---

## 6. 验证思路

1. **端到端测试**：`BEISHAN_DEEPSEEK_TEST=1` 验证 DeepSeek 搜索通过
2. **会话隔离**：两个并发 Agent session 互不共享 cookie/storage
3. **CDP 扩展**：新能力注册后 `grep` 确认非测试调用点
4. **硬化检查**：Agent 来源的 eval/screenshot 请求被拒绝
5. **所有测试 gate 在 env**：使用 Chrome 的测试默认跳过，`verify.sh` hermetic

---

## 7. 风险与备选

| 风险 | 缓解 |
|------|------|
| Chrome 版本升级导致 CDP 协议变更 | CDP 协议向后兼容性好，降级为特定版本 CDP |
| headless 检测升级 | 转向 `--headless=old` 或真实 profile |
| 多 Agent 并发资源耗尽 | Session Manager 上限 + LRU 回收 |
| Chromium 路线与 Servo 路线冲突 | 共享 Engine/Page 接口，两条路线平行不冲突 |

---

## 8. 与 SERVO_NORTHSTAR 的关系

```
SERVO_NORTHSTAR（远期理想）        CHROMIUM_NORTHSTAR（中期现实）
                                    ┃
  阶段 0: CDP-over-pipe Chrome      阶段 0-2: 已完成（共享）
  阶段 1: Engine/Page 接口           阶段 3: Session Manager ⬅️ 下一刀
  阶段 2: Servo spike               阶段 4: CDP 能力扩展
  阶段 3-4: Servo 后端 + 嵌入器      阶段 5: 指纹 + 反爬
  阶段 5: in-process FFI            （无终态，持续演进）
```

两条路线共享阶段 0-2 的基础设施。阶段 3 开始分叉：
- Chromium 路线聚焦**服务化**（Session Manager + CDP 扩展 + 反爬）
- Servo 路线聚焦**内嵌**（in-process FFI）

不冲突，可并行。
