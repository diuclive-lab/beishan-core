# Servo 右花接入方案

> 参考源：`/Users/dc/Desktop/cankaocangku/servo/`
> 北极星文档：`docs/SERVO_BROWSER_NORTHSTAR.md`
> 架构路线：子进程 + 控制协议（方式 B）→ 薄嵌入器（方式 C）→ in-process FFI（方式 A，终态）
> 共享接口：`internal/browser/` 包（与 Chromium 右花同接口）
> 工期：4-5 天（分 3 阶段，每阶段独立可验证、可回退）

---

## 总体思路

**Servo 右花与 Chromium 右花共享同一个 `Engine`/`Page` 接口。** Chromium 是第一实现（今天可用），Servo 是第二实现（北极星方向）。两者可配置切换，上层工具（`deepseek_web_search` 等）不改代码。

```
beishan
  │
  ├── internal/browser/           ← 共享接口层
  │   ├── engine.go               ← Engine / Page 接口（不变）
  │   ├── chrome_cdp.go           ← Chromium 实现（CDP-over-pipe）
  │   ├── servo_webdriver.go      ← Servo 实现（WebDriver / 自定义 IPC）
  │   └── factory.go              ← 配置/env → 选择实现
  │
  ├── right_flowers/
  │   ├── chromium.yaml           ← Chromium manifest
  │   └── servo.yaml              ← Servo manifest
  │
  └── cmd/servo-embed/            ← 薄 Rust 嵌入器（阶段 4，可选）
```

---

## 阶段 1：Engine/Page 接口 + Servo 可行性确认（go/no-go）

### 说明

这是 SERVO_NORTHSTAR.md 的阶段 2（spike），但放在第一优先级做——**先确认 Servo 能不能跑我们的 4 个原语**，再决定是否值得写 Go 后端。

本阶段与 Chromium 第 1 天共享同一个 `internal/browser/` 接口设计。

### 接口定义（已定，与 Chromium 共用）

```go
// internal/browser/engine.go
type Engine interface {
    NewPage(url string) (Page, error)
    Close()
}
type Page interface {
    Eval(js string) (string, error)
    InnerText() (string, error)
    InsertText(text string) error
    PressKey(key string) error
    Navigate(url string) error
    Close()
}
```

### 验证判据（go/no-go 闸）

**手动验证**，用 `servoshell`（Servo 的参考嵌入器）或直接调用 Servo 的 WebDriver：

```
1. servoshell --headless https://example.com
   → 确认能启动、不崩溃

2. 通过 WebDriver 执行：
   - navigate("https://example.com")
   - execute_script("return document.title")
   → 确认返回页面标题

3. 验证输入：
   - 对测试页面执行 click + input
   → 确认事件到达 DOM

4. 验证 InnerText：
   - execute_script("return document.body.innerText")
   → 确认返回文本内容
```

**通过标准**：4 个原语全部可行。任一不可行 → **退回 Chromium 后端**，Servo 标注"自动化面未就绪，等上游"。

### 工期：半天

---

## 阶段 2：servoEngine 后端（方式 B，子进程 + 控制协议）

### 目标

如果阶段 1 通过，实现 `servo_webdriver.go` 作为 Engine/Page 的第二实现。

### 架构

```
beishan → exec → servoshell (子进程)
                    │ WebDriver / 自定义 IPC
servoEngine ← → servoshell ← → Web page
```

### 实现

```go
// internal/browser/servo_webdriver.go
type servoEngine struct {
    cmd     *exec.Cmd
    webdrv  *webdriver.Client  // Servo 的 WebDriver 客户端
}

func (e *servoEngine) NewPage(url string) (Page, error) {
    // 通过 WebDriver 新建 session
    session, err := e.webdrv.NewSession()
    session.Navigate(url)
    return &servoPage{session: session}, nil
}
```

### 风险与回退

Servo 的 WebDriver 实现可能不完整（SERVO_NORTHSTAR.md 已标注【待验证】）。如果：
- WebDriver 命令覆盖不足 → 退回到**自定义 IPC**（Servo 的 embedder API）
- 自定义 IPC 也不可行 → **退回方式 C**（薄 Rust 嵌入器）
- 全都不行 → **退回 Chromium 后端**，Servo 标记"等上游"

### 工期：2 天

---

## 阶段 3：安全与硬化（共享逻辑，与 Chromium 一致）

与 Chromium 第 2-3 天共享相同设计：

| 安全机制 | 实现位置 | 说明 |
|---------|----------|------|
| SSRF 保护 | `browser_engine.go:isSafeURL` | 拒绝私有 IP，与引擎无关 |
| Agent 输入降权 | `browser_eval` source 检查 | Agent 默认不加盖 window |
| 会话隔离 | `browser/session.go` | Servo profile 独立目录 |
| 工具注册 | `RegisterBrowserTools` | 一组建模工具注册两个后端 |

### 工期：1 天（与 Chromium 共享逻辑）

---

## 阶段 4：薄 Rust 嵌入器（可选，方式 C）

### 说明

如果 Servo 的 WebDriver/自定义 IPC 不稳定，基于 `libservo` 写一个薄 Rust 嵌入器。

### 设计

```rust
// cmd/servo-embed/src/main.rs
// 基于 libservo 实现 4 个原语：
// - navigate(url)
// - eval(js) → String
// - innerText() → String
// - input(text/key)
// 通过 stdin/stdout JSON 与 beishan 通信
```

### 工期：3 天（可选，仅当阶段 2 卡住时触发）

---

## 全流程执行顺序

```
第 1 天: Engine/Page 接口 + Chromium chromeCDP 实现
        → 并行：Servo 阶段 1（spike，手动验证 4 原语）
        → 第 1 天结束时出 go/no-go 判断

第 2-3 天: Chromium 右花硬化 + 会话隔离（Chromium 第 2-3 天）
          → 如果 Servo go: 并行写 servoEngine 后端

第 4 天: 验证 + 文档 + INTEGRATION_PROOF
        → 两端验证：Chromium 端到端 + Servo 4 原语
```

---

## 参考映射（补充 SERVO_NORTHSTAR.md）

| 北极星阶段 | 本方案对应 | Servo 组件 |
|-----------|-----------|-----------|
| 阶段 0 (CDP-over-pipe) | 已完成 | Chrome，非 Servo |
| 阶段 1 (接口抽象) | 本方案第 1 天 | `internal/browser/` |
| 阶段 2 (Servo spike) | 本方案阶段 1 | `servoshell` + WebDriver |
| 阶段 3 (Servo 后端) | 本方案阶段 2 | `servo_webdriver.go` |
| 阶段 4 (薄嵌入器) | 本方案阶段 4（可选） | `cmd/servo-embed/` |
| 阶段 5 (in-process FFI) | 远期终态 | cgo + libservo |
