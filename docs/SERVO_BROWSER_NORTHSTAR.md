# North-Star：把浏览器引擎内嵌进智能体根茎（Servo）

> ⛔ **状态(2026-05-30 决策)：暂停。** 以 `docs/BROWSER_RUNTIME_DECISION.md` 为准。
> 深度 B 已证 Servo in-process FFI **技术可行**,但其 `rustls`/`hyper` 网络栈给出**非 Chrome 的
> JA3/HTTP2 指纹**,结构性输掉反爬护城河 → **主力定为 Chromium 深绑,Servo 暂停**(复活条件见决策文档 §4)。
> **不要再按本文档推进 Servo FFI。** 本文保留为可行性记录 + 远期参考。
>
> 这是一份**交接文档**，给另一名开发者用 AI 辅助快速推进。读完你应能：理解愿景、看清现状轨迹、
> 按分阶段路线图动手、且每一步都有明确的「怎么验证它真的work」的判据。
>
> 作者注：本文对 Servo 的能力**区分「已确认」与「待验证」**——凡标 `【待验证】` 的，接手时请先实测/查最新源码，
> 不要当成既成事实。这本身就是本项目的纪律。

---

## 1. 愿景：浏览器成为智能体的一部分

今天的智能体「用」浏览器：起一个外部 Chrome、用 CDP 远程驱动它。浏览器是**外挂**——它的进程、版本、
反爬对抗、登录态，都在智能体之外，随时漂移、随时被网站针对。

愿景是反过来：**让浏览器引擎长在智能体身体里**（根茎里）。智能体不是「调用一个浏览器」，而是
**自己就是一个会浏览网页的东西**。好处：

- **没有外部依赖漂移**：不依赖系统装了哪个 Chrome、playwright 驱动版本、CDP 协议变更。
- **完全可控**：渲染、JS 执行、网络栈、指纹，都在我们手里——反爬对抗、隐私、可定制都可能。
- **单一身体**：理想终态是一个二进制，引擎在进程内，没有「另一个浏览器进程」。

候选引擎是 **Servo**（Rust 写的浏览器引擎，Mozilla 起源、现 Linux Foundation 下，实验性但活跃，
GPU 渲染 WebRender + JS 引擎 SpiderMonkey，**设计上就是可嵌入的** `libservo`）。

---

## 2. 现状与轨迹（我们已经在这条路上了）

2026-05-30 落地的 `internal/tools/cdp.go`（CDP-over-pipe）**就是这条路的第一步**：

- beishan 用 `exec` **自己启动并拥有** Chrome（`--remote-debugging-pipe`），CDP 走 **fd 3/4 的 null 分隔
  JSON**，私有管道、不经 websocket、不用 playwright 库、不起外部 driver/daemon。
- 「进程由智能体拥有、私有通道通信」——这正是「浏览器是身体一部分」的雏形，只是引擎暂时还是 Chrome。

轨迹：**外部驱动 Chrome（playwright，旧）→ 自有进程 + 私有管道驱动 Chrome（CDP-over-pipe，现在）→
自有进程 + 私有通道驱动 Servo（下一步）→ 引擎在进程内（in-process FFI，终态）**。

抽象出一个**引擎无关的浏览器接口**是关键过渡设计（见 §4），这样从 Chrome 换到 Servo 时上层不动。

---

## 3. 为什么是 Servo（以及它的成熟度边界）

- **可嵌入**【已确认（设计意图）】：Servo 提供 `libservo` crate + `servoshell` 参考嵌入器，嵌入是一等用例。
- **Rust，单库可链**【已确认】：可编译成静态/动态库，理论上能被别的进程/语言链接。
- **自动化/控制面成熟度**【待验证】：Chrome 有完整的 CDP；Servo 的**自动化协议（WebDriver）支持是部分的**，
  接手时务必先查 Servo 当前对 WebDriver 的支持范围（哪些命令可用：navigate / execute_script / get_dom /
  input）。这决定了「Servo 子进程 + 控制协议」路线的可行度。
- **JS 执行 + DOM 读取**【待验证】：我们的核心只需要四件事——navigate、`execute_script`(跑 JS)、读
  `document.body.innerText`、输入文本/回车。确认 Servo 能稳定做这四件事，就够支撑现在的
  `deepseek_web` 用例。

**结论**：Servo 是正确的 north-star，但它的自动化面不如 CDP 成熟。所以路线要**分阶段、可回退**——
任何一阶段卡住，都能退回「CDP-over-pipe + Chrome」继续用（现状已可用）。

---

## 4. 架构设计：引擎无关的浏览器接口

先把上层与「具体引擎」解耦。定义一个 Go 接口（`internal/browser` 新包，建议）：

```go
type Engine interface {
    NewPage(url string) (Page, error)
    Close()
}
type Page interface {
    Eval(js string) (string, error)   // 跑 JS，返回字符串结果
    InsertText(text string) error     // 模拟真实输入（受控组件友好）
    PressKey(key string) error        // Enter 等
    InnerText() (string, error)       // document.body.innerText
    Screenshot() ([]byte, error)      // 可选
    Close() error
}
```

- 现状 `cdp.go` 重构成 `Engine` 的一个实现 `chromeCDP`（几乎现成：`evalString`→`Eval`，
  `insertText`/`pressEnter` 已有，`attachPage`→`NewPage`）。
- Servo 实现 `servoEngine` 后端，上层（`deepseek_web.go` 等）**完全不改**。

三种 Servo 接入方式，按「智能体拥有引擎」的程度递进：

| 方式 | 描述 | 「内嵌」程度 | 难度 | 风险 |
|------|------|------------|------|------|
| **B. Servo 子进程 + 控制协议** | 像现在驱动 Chrome 一样，但进程是 Servo，协议用 Servo 的 WebDriver/自定义 IPC | 中（仍是独立进程） | 中 | Servo 自动化面是否够用【待验证】 |
| **A. in-process FFI（cgo + libservo）** | 把 `libservo` 编译成 C-ABI 库，Go 用 cgo 在**进程内**驱动 | 高（引擎在身体里=终态） | 高 | cgo + Rust 构建链、巨型依赖、线程模型 |
| C. 自定义嵌入器 | 用 Rust 写一个薄嵌入器（基于 `libservo`），暴露我们要的 4 个原语，Go 经 IPC/FFI 调 | 中-高 | 中-高 | 要维护一段 Rust |

**建议落地顺序：先 B（验证 Servo 能跑我们的用例）→ 再 C（薄嵌入器收敛控制面）→ 终态 A（in-process）。**
A 一上来就做风险最高、收益最晚。

---

## 5. 分阶段路线图（每阶段独立可验证）

> 设计原则：**每阶段都能独立 demo + 有明确验证判据 + 失败可回退到上一阶段**。这正好适合 AI 辅助
> 逐阶段推进（每阶段是一个清晰的、可测的小目标）。

### 阶段 0（✅ 已完成）：CDP-over-pipe + Chrome
- 产物：`internal/tools/cdp.go` + `deepseek_web.go`。
- **验证判据**（已达成）：CDP-over-pipe 传输验证通过 → internal/browser/chrome_cdp.go；
  Engine/Page 接口已实现；deepseek_web_search 通过接口调用。
  额外交付：硬化层集成 + agent_source 权限检查 + StoragePartition 会话隔离。

### 阶段 1（✅ 已完成）：抽象 `Engine`/`Page` 接口，Chrome 后端重构进去
- 产物：`internal/browser` 包 + `chromeCDP` 实现；`deepseek_web.go` 改用接口。
- **验证判据**：`deepseek_web_search` 行为**零变化**（同一个 live 测试仍 `success=true`）。纯重构，定义计数守恒。

### 阶段 2（✅ 已完成）：Servo 4 原语 live 验证通过（spike → GO）
- Servo 源码确认 WebDriver 实现完整（lib.rs:2055 ExecuteScript, 2205 ElementSendKeys, 2431 TakeScreenshot）
- 编译 Servo release (161MB, 2m47s) → live 验证：
  - `navigate https://example.com` ✅
  - `eval document.title` → "Example Domain" ✅
  - `innerText document.body.innerText` ✅
  - `screenshot` → 39KB PNG ✅
- go/no-go = **GO** → servo_webdriver.go 已实现并端到端验证通过
- 失败回退：阶段 3 已实现，无回退必要。如 WebDriver 不完整可退方式 C（薄 Rust 嵌入器）

### 阶段 3（✅ 已完成）：`servoEngine` 后端（方式 B，WebDriver 子进程）
- 产物：`internal/browser/servo.go` 实现 `Engine`/`Page`，驱动 Servo 子进程。
- **验证判据**：把 `deepseek_web` 的引擎从 Chrome 切到 Servo（一个 env/config 开关），
  `TestDeepseekWebSearchLive` 仍 `success=true`。**同一个端到端测试是两套引擎的共同标尺**。

### 阶段 4：薄 Rust 嵌入器（方式 C）收敛控制面
- 动作：基于 `libservo` 写一个小 Rust 嵌入器，暴露稳定的 4 原语（navigate/eval/input/innerText）+ 持久 profile。
- **验证判据**：同上端到端测试通过，且控制面比阶段 3 更稳（不再依赖 Servo WebDriver 的不完整性）。

### 阶段 5（终态）：in-process FFI（方式 A）
- 动作：cgo 链接嵌入器为 C-ABI 库，引擎进入 beishan 进程内。
- **验证判据**：单一二进制内完成 `TestDeepseekWebSearchLive`，无任何外部浏览器进程（`ps` 验证无独立 Chrome/Servo 子进程）。

---

## 6. 通用验证思路（这类「驱动真实页面」的活怎么测）

本次 DeepSeek-web 落地踩的坑总结成可复用的验证姿势（接手必读）：

1. **分步取证，别信参考源码**：scrape 聊天 UI 的脆弱点（输入时序、开关命名/状态、富文本注入）只有
   **对着真实页面逐步 dump** 才暴露。每个阶段先写一个「dump 诊断」再写「正式逻辑」。
2. **时序**：受控组件（React）是异步的——`insertText` 后要**等一下再回车**，否则空发（本次首跑 90s 超时根因）。
3. **状态而非动作**：开关用 `aria-pressed` 判**当前状态**、只在需要时点，别盲翻（盲 toggle 会随机开/关）。
4. **富文本注入**：网页把引用角标/换行注入到「看起来是 JSON」的文本里→非法 JSON。解析要带**修复重试**。
5. **靠文本锚点、不靠哈希类名**（`_6dbc175` 这种 build 一次变一次）。
6. **hermetic 门禁**：所有「起浏览器/联网」的测试 **gate 在 env**（`BEISHAN_DEEPSEEK_TEST=1`），
   `verify.sh` 必须跳过它们（离线门禁不依赖浏览器/网络，Task L 教训）。纯函数（JSON 抽取/状态判定）才常驻单测。

---

## 7. 风险与备选

- **Servo 自动化面不成熟**【待验证】：最大未知。先做阶段 2 的 go/no-go spike，别先写一堆 Go。
- **cgo + Rust 构建链**：阶段 5 会显著复杂化 beishan 的构建（目前 `go build` 一把过）。在线索清晰前不碰。
- **巨型依赖**：Servo 编译产物大、构建慢。权衡「身体内嵌」收益 vs 构建/分发成本。
- **备选引擎**：若 Servo 自动化面长期不就绪——CEF（Chromium Embedded）、WebKitGTK、或干脆**长期停在
  阶段 1**（CDP-over-pipe + 系统 Chrome，已可用）也是诚实选项。north-star 是方向，不是必须今天到达。

---

## 8. 给接手开发者（用 AI 辅助）

- **先读**：`internal/tools/cdp.go`（看我们怎么自有进程 + 管道驱动浏览器）+ `internal/tools/deepseek_web.go`
  （看一个真实页面自动化用例长什么样）+ 本会话 devlog（`docs/devlog/DEVLOG_20260530.md` 的 DeepSeek 段，
  有全部踩坑）。
- **第一刀**：做**阶段 1**（抽 `Engine`/`Page` 接口、Chrome 后端重构进去）。低风险、纯收益、为换引擎铺路，
  且验证判据现成（live 测试零变化）。
- **第二刀**：做**阶段 2 的 spike**（Servo 能否跑 4 原语）——这是整条路的 go/no-go 闸，**别跳过**。
- **用 AI 的方式**：每阶段都是「清晰小目标 + 明确验证判据」，适合让 AI 写实现 + 你跑验证判据卡关。
  坚持本项目纪律：**先确认非测试调用点、改完 `go build`、声明完成前填 INTEGRATION_PROOF、起浏览器的测试 gate 在 env**。
- **纪律红线**：别把「提示词/参考源码说能行」当「代码验证过」。每阶段的验证判据是实测，不是阅读。
