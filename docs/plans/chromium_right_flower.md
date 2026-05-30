# Chromium 右花接入方案

> 参考源：`/Users/dc/Desktop/cankaocangku/chromium/`
> OWL 架构参考：OWL 安全架构 + Input Architecture + Runtime Internals
> 现状：已有 `cdp.go`（CDP-over-pipe）+ `deepseek_web.go`（DeepSeek 搜索工作流）
> 工期：3-4 天

---

## 总体思路

**不是"把 Chromium 整个变成右花"，而是把"beishan 自有 Chrome 进程 + CDP 控制"抽象成右花。** Chromium 源码作为参考（学 OWL 的设计取舍），不直接依赖。

## 架构

```
beishan
  │
  ├── internal/browser/          ← 新包：引擎无关的浏览器接口
  │   ├── engine.go              ← Engine / Page 接口定义
  │   ├── chrome_cdp.go          ← Chrome CDP-over-pipe 实现（迁入现有 cdp.go）
  │   └── factory.go             ← 引擎工厂（配置/env → 选择实现）
  │
  ├── right_flowers/
  │   └── chromium.yaml          ← 右花 manifest
  │
  ├── internal/tools/cdp.go      ← 重构：CDP wire 协议保留，上层逻辑迁入 browser/
  ├── internal/tools/deepseek_web.go ← 改调 browser.Engine 接口
  └── internal/tools/browser.go  ← 现有 browser 工具：补全安全链
```

---

## 第 1 天：Engine/Page 接口抽象 + chromeCDP 实现

### 目标

创建 `internal/browser/` 包，把现有 `cdp.go` 中的 CDP 控制逻辑重构为接口实现。

### 接口定义

```go
// internal/browser/engine.go

package browser

// Engine 浏览器引擎。抽象 Chrome/Servo 的共性。
type Engine interface {
    // NewPage 创建新页面，返回 Page 实例。
    NewPage(url string) (Page, error)
    // Close 关闭引擎（释放 Chrome 进程等）。
    Close()
}

// Page 单个页面控制句柄。
type Page interface {
    // Eval 执行 JS，返回字符串结果。
    Eval(js string) (string, error)
    // InnerText 读取 document.body.innerText。
    InnerText() (string, error)
    // InsertText 模拟真实输入（React 受控组件友好）。
    InsertText(text string) error
    // PressKey 模拟按键。
    PressKey(key string) error
    // Navigate 导航到 URL。
    Navigate(url string) error
    // Close 关闭页面。
    Close()
}
```

### chromeCDP 实现

`internal/browser/chrome_cdp.go`：

- 迁入现有 `cdp.go` 的 `newCDPConn`、`send`、`attachPage`、`evalString`、`insertText`、`pressEnter` 等
- 实现 `Engine` 接口（`NewPage` = attachPage + createTarget）
- 实现 `Page` 接口（Eval/InnerText/InsertText/PressKey/Navigate）
- `GetDOM` / `Screenshot` 等作为可选的扩展接口（`PageExt`，类型断言检测）

### 迁移现有代码

| 现有函数 | 去处 | 说明 |
|---------|------|------|
| `newCDPConn` | `chrome_cdp.go` | 保留，作为 chromeCDP 的启动逻辑 |
| `send` (CDP 消息) | `chrome_cdp.go` | 保留作为内部方法 |
| `attachPage` | `chrome_cdp.go` → `NewPage` | 实现 Engine 接口 |
| `evalString` | `chrome_cdp.go` → `Page.Eval` | |
| `insertText` | `chrome_cdp.go` → `Page.InsertText` | |
| `pressEnter` | `chrome_cdp.go` → `Page.PressKey` | |
| `ensureDeepseekToggleOn` | `deepseek_web.go` | 保留，业务逻辑不迁 |
| `extractMarkedJSON` | `deepseek_web.go` | 保留 |

### 验证标准

- [ ] `go build ./internal/browser/...` ✅
- [ ] `chromeCDP` 实现 Engine/Page 全部方法
- [ ] 原有 `deepseek_web_search` 端到端测试（需 `BEISHAN_DEEPSEEK_TEST=1`）仍通过，行为零变化
- [ ] 所有现有 `cdp.go` 的测试仍通过

---

## 第 2 天：右花 manifest + 硬化层集成

### 目标

把 Chromium 浏览器引擎注册为右花，所有浏览器操作走硬化层通道。

### 右花 manifest

```yaml
# right_flowers/chromium.yaml
id: chromium
type: browser_engine
version: "1.0"
description: "Chromium 无头浏览器引擎，通过 CDP-over-pipe 控制"
endpoint: internal  # 内部引擎，非 HTTP
capabilities:
  - browser.navigate
  - browser.eval
  - browser.input
  - browser.screenshot
route_exposed: false
```

### 注册到 main.go

```go
// 启动 Chrome 引擎（由 glue 管理进程生命周期）
chromeEngine := browser.NewChromeEngine()
gl.RegisterSidecar("chrome", chromeEngine.Cmd())  // 进程管理
// 注册为工具
RegisterBrowserTools(chromeEngine)
```

`RegisterBrowserTools` 注册的工具：
- `browser_navigate` — 导航 + SSRF 检查（已实现，补全安全链）
- `browser_eval` — 执行 JS（新）
- `browser_input` — 输入文本/按键（已实现）
- `browser_screenshot` — 截图（新，复用以有 CDP Page.captureScreenshot）
- `browser_search_deepseek` — DeepSeek 网页搜索（已有，迁入工具注册）

### 硬化层加固

当前 `browser.go` 的安全检查和缺失补齐：

| 检查 | 状态 | 说明 |
|------|:----:|------|
| `isSafeURL`（SSRF 保护） | ✅ 已有 | 拒绝私有 IP / localhost |
| `containsSecret`（密钥泄漏） | ✅ 已有 | 拒绝含 API key 的 URL |
| `isSearchEngineURL` | ✅ 已有 | 搜索 URL → 指向 web_search |
| 工具注册走 `ValidateAndExecute` | ✅ 已有 | L3 硬化层 |
| Agent 输入特权控制 | ❌ 缺 | 需要：区分"用户请求的操作"vs"Agent 自主操作" |

Agent 输入特权控制的实现方式：在 `browser_eval` 等工具内，识别调用来源（`source: "agent"` vs `source: "user"`），Agent 发起的 eval 默认不加盖 `window` 对象——与 OWL 的 Direct-to-Renderer 原则一致，不让 Agent 获得浏览器特权。

### 验证标准

- [ ] Chromium manifest 注册不报错
- [ ] `browser_navigate` 拒绝私有 IP 的 SSRF 测试通过
- [ ] `browser_eval` 返回 JS 执行结果
- [ ] `go build ./...` ✅ | `go test ./...` ✅

---

## 第 3 天：Agent 安全隔离 + StoragePartition 模式

### 目标

参考 OWL 的 StoragePartition 设计，实现 Agent 会话隔离。

### 设计

OWL 的 `ephemeral logged-out context` 在 Chromium 里对应 `--incognito` + 独立 user data dir。我们用相同的机制：

```go
// internal/browser/session.go

// SessionConfig 控制浏览器会话的隔离级别。
type SessionConfig struct {
    Incognito    bool   // 无痕模式（默认 true for agent）
    UserDataDir  string // 空 = 临时目录
    ProfileDir   string // 持久化 profile 名，空 = 临时
}

// NewSession 创建独立浏览器会话。
// Agent 任务 → Incognito=true, UserDataDir=tmp → 结束后销毁。
// 用户操作 → Incognito=false, UserDataDir=persistent → 持久化。
func NewSession(cfg SessionConfig) (*chromeCDP, error)
```

### 应用场景

| 场景 | Incognito | UserDataDir | 生命周期 |
|------|:---------:|-------------|----------|
| 用户日常浏览 | false | `~/.hermes/chrome_profile` | 持久 |
| Agent 自主搜索 | true | 临时目录 | 任务结束销毁 |
| DeepSeek 网页搜索 | false | `~/.hermes/deepseek_web_profile` | 持久（复用登录态） |

### 验证标准

- [ ] Agent 会话结束后临时目录被清理
- [ ] 两个 Agent 并发任务互不干扰（各自的 cookie/storage 隔离）
- [ ] 用户持久会话与 Agent 临时会话不共享身份

---

## 第 4 天：验证 + 文档 + 清理

### 验证清单

| 检查项 | 命令/方法 | 预期 |
|--------|-----------|------|
| 编译 | `go build ./...` | ✅ |
| 测试 | `go test ./...` | 22 packages ✅ |
| 集成 | `bash scripts/integration_check.sh` | ✅ |
| CDP 传输 | `TestCDPPipeTransport` | HeadlessChrome UA |
| DeepSeek 搜索 | `TestDeepseekWebSearchLive`（env gated） | success=true + 已思考 |
| 会话隔离 | 两个 Agent 任务独立 cookie | 不共享 |
| SSRF 保护 | navigate 私有 IP | 拒绝 |
| INTEGRATION_PROOF | 按章程输出 | 含 build/test 行 |

### 需更新文档

- `CLAUDE.md` — Key Files 添加 `internal/browser/`
- `docs/DATA_FLOW.md` — 新增路径 U（浏览器引擎）
- `docs/DIRECTORY.md` — 新增 `internal/browser/` 条目
- `docs/RIGHT_FLOWER_PROTOCOL.md` — 新增 browser_engine 类型（如果需要）

### 工程章程检查

对照 CLAUDE.md 10 项零容忍规则逐一确认：
- [x] 所有浏览器操作工具注册后 grep 确认非测试调用点
- [x] 每次改动后 `go build` 确认
- [x] 每完成一个阶段输出 INTEGRATION_PROOF
- [x] Engine/Page 接口不写空占位（要么实现完整，要么 UNIMPLEMENTED）

---

## 参考映射

| OWL 概念 | 我们的实现 |
|----------|-----------|
| OWL Client → Host (Mojo IPC) | beishan → Chrome (CDP-over-pipe, fd 3/4) |
| WebContents / RenderFrameHost | Chrome 内部，不直接接触 |
| StoragePartition ephemeral session | `chrome --incognito --user-data-dir=$(mktemp -d)` |
| Synthetic WebInputEvent → Renderer | CDP Input.insertText / dispatchKeyEvent |
| Capability Router | `ValidateAndExecute` + `source: "agent"` 检查 |
| Remote CALayer Embedding | 不需要（headless） |
| Bidirectional Input Pipeline | 不需要（headless，无 UI） |
