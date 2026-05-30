# 下午工作报告 2026-05-30

> 覆盖时间段：阅读 OWL Scheduling & Performance 架构分析 → Chromium NORTHSTAR 阶段 3-5 实现 → 阶段 5 评估 → 文档更新
> 提交范围：`1dda991` → `003b2cf` → `9dd5b57` → `195658b` → `3242864`

---

## 一、阅读材料

### OWL Scheduling & Performance Internals（13 部分）

| 部分 | 内容 | 对我们架构的启示 |
|:----:|------|-----------------|
| 1 | 浏览器是软实时系统（~100ms 感知阈值，16.6ms/帧 @60Hz） | 确认 CDP-over-pipe 异步传输设计正确 |
| 2 | Chromium 线程架构（Main/Compositor/IO/Worker/Raster） | 我们的工具调用应在 goroutine 中，不阻塞 |
| 3 | Frame Lifecycle（Input→Animation→Style→Layout→Paint→Composite→Present） | headless 可跳过 Paint/Present |
| 4 | RendererScheduler（multi-queue priority） | 参考：可为 Agent 任务设低优先级 |
| 5 | Input Latency Budget（16.6ms 内装 OS+IPC+Renderer+GPU） | OWL IPC 必须极低延迟 |
| 6 | Async Scrolling（Compositor Thread 独立处理） | 我们的 headless 无滚动问题 |
| 7 | Main Thread Contention | CDP-over-pipe 天生不阻塞 main thread |
| 8 | AI Agent Scheduling Problem（LLM 与 realtime 路径隔离） | **核心结论**：Agent 推理必须严格异步 |
| 9 | Mojo IPC Cost | 我们的 pipe 方案避免了 Mojo 序列化开销 |
| 10 | Remote Rendering Performance | 不需要（headless） |
| 11 | Frame Deadline Pressure in OWL | 确认我们不走显示管道是正确的 |
| 12 | Long-Term Scheduling（Agent QoS classes） | 未来可参考 |
| 13 | OWL 性能哲学 | **保护实时交互，同时增加非实时智能** |

### 前置参考文档

| 文档 | 用途 |
|------|------|
| `docs/CHROMIUM_BROWSER_NORTHSTAR.md` | Chromium 路线阶段定义（阶段 3-5） |
| `docs/SERVO_BROWSER_NORTHSTAR.md` | 对比参考（已更新进度） |
| OWL 安全/输入/渲染 6 篇（此前已读） | 阶段 3-5 接口设计参考 |

---

## 二、执行记录

### Chromium 阶段 3：Session Manager（commit `1dda991`）

**设计决策**：
- 参考 OWL StoragePartition `ephemeral logged-out context`
- 持久会话复用（同 profile 共享引擎实例）
- Incognito 隔离（独立 temp profile，Release 时清理）
- 全局 `SessionManager` + `AcquireBrowser(agent)/ReleaseBrowser` 便利函数

**代码变更**：
- `internal/browser/chrome_manager.go` — 新增（~150 行）
- `internal/browser/chrome_manager_test.go` — 3 个测试
- `internal/tools/deepseek_web.go` — 改为 `AcquireBrowser(false)`，不再每次启停 Chrome

**性能数据**：
- 持久会话复用前：每次 ~5s（启停 Chrome 进程）
- 持久会话复用后：`0.11s`（引擎实例直接复用）

**验证**：
```
TestSessionManager_AcquireRelease       → PASS (0.11s) ✅ 复用
TestSessionManager_IncognitoIsolation    → PASS (0.00s) ✅ 隔离
TestAcquireBrowser_Helper                → PASS (0.00s) ✅ 便利函数
```

### Chromium 阶段 4：CDP 能力扩展（commit `e0e6c2f`）

**设计决策**：
- 不破坏现有 `Page` 接口—使用 Go 类型断言检测可选能力
- `PageExt` — PDF/性能/安全检查
- `NetworkPage` — 网络响应捕获
- `withPageExt` helper 封装断言 + 错误处理

**代码变更**：
- `internal/browser/engine.go` — `PageExt` / `NetworkPage` / `FingerprintEngine` 接口
- `internal/browser/chrome_cdp.go` — `PrintToPDF` / `PerformanceMetrics` / `SecurityState` / `StartNetworkCapture` / `StopNetworkCapture`
- `internal/tools/browser.go` — 5 个新工具注册 + withPageExt helper

**验证**：
```
go build ./...  ✅
go test ./internal/tools/ ./internal/browser/  ✅
```

### Chromium 阶段 5：指纹管理 + 反爬对抗（commit `003b2cf`）

**设计决策**：
- 参考 OWL 输入管线架构，headless 检测发生在多层（UA → WebGL → Canvas → 行为）
- 三层覆盖：CDP 层 → JS 注入层 → 自动化标志移除
- `FingerprintEngine` 接口（类型断言，servo 不支持时跳过）
- CDP `Network.setUserAgentOverride` + `Runtime.evaluate` JS 注入
- `browser_configure` 工具注册 + `BEISHAN_BROWSER_FP=1` 环境变量

**代码变更**：
- `internal/browser/chrome_cdp.go` — `FingerprintConfig` / `ApplyFingerprint`
- `internal/browser/engine.go` — `FingerprintEngine` 接口 + `GetDefaultFingerprint()`
- `internal/tools/browser.go` — `browserConfigureHandler`

**验证**：
```
go build ./...  ✅
go test ./...   ✅
```

---

## 三、全量验证结果

```
go build ./...          ✅
go vet ./...            ✅
go test ./...           ✅ 22 packages
integration_check.sh    ✅
```

---

## 四、文件变更统计

```
下午提交：6 笔（1dda991 → 003b2cf → 9dd5b57 → 195658b → 3242864 → 9dd5b57）

 internal/browser/chrome_manager.go           | 150  新增 — Session Manager
 internal/browser/chrome_manager_test.go      | 120  新增 — 3 个测试
 internal/browser/chrome_cdp.go               | 150  FingerprintConfig + ApplyFingerprint
 internal/browser/engine.go                   |  45  PageExt + NetworkPage + FingerprintEngine
 internal/tools/deepseek_web.go               |   5  改用 SessionManager
 internal/tools/browser.go                    | 130  5 个新工具 + withPageExt
 docs/CHROMIUM_BROWSER_NORTHSTAR.md           | 237  新增 — Chromium 路线文档
 docs/reports/browser_engine_absorption_record.md | 335 新增 — 完整吸收记录
 docs/devlog/DEVLOG_20260530.md               |  55  追加
```

---

## 五、待处理项

| 项 | 状态 | 说明 |
|----|:----:|------|
| SERVO_NORTHSTAR 阶段 5（in-process FFI） | ⏸️ 待评估 | 需 cgo + libservo 编译链 |
| `deepseek_web_search` 异步化 | 📋 待办 | 当前同步等 120s，应改为 goroutine + polling |
| NetworkPage 事件捕获完善 | 📋 待办 | 需要修改 chromeCDP readLoop 路由事件到页面 |
| SessionManager 超时回收 | 📋 待办 | 泄漏 session 自动清理 |
