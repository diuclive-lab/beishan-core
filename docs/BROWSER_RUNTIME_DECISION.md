# 浏览器运行时决策：深绑 Chromium（Servo = 可行但暂停的 north-star）

> 给另一名开发者用 AI 辅助执行交付。读完即可按「执行计划」分阶段推进，每阶段有验证判据。
> 这是一份**决策 + 路线图**，统一两名开发者的方向，避免两条引擎线各跑各的。
> 决策依据：本会话的深度 B(Servo 源码 recon)+ OWL 架构分析 + 反爬当前技术面。

---

## 0. 战略前提（为什么这事值得深投）

两名独立开发者对大厂/社区的**唯一三个优势**：① 个人知识库 ② **AI 浏览器自主化** ③ 可操控电脑的工作流。
所以浏览器**不是一个搜索工具,是护城河**。护城河 = 「**一个能在真实、有反爬防护的网站上,自主、可靠、且别人难以复制或封禁地使用网络的 AI**」。拆成两条硬属性：

- **P1 — 封不掉**(反爬真实性)：被 Cloudflare/Datadome 拦 = 自主化归零。**最高权重。**
- **P2 — 控得深 / 拿不走**(自有引擎、深度控制、独特能力)：差异化来源。

**判据**：任何引擎选择,先问「它能不能让 P1 成立」。一个"深嵌进我们二进制、却被高防站一拦就废、且渲染不了半数真站"的引擎,提供**零**护城河。

---

## 1. 决策（结论先行）

| | 决定 |
|---|---|
| **主力引擎(深绑进智能体根茎)** | **Chromium** —— 以「智能体自有、自带、深度控制的子进程运行时」形态 |
| **Servo in-process FFI** | **可行,但暂停** —— 文档化为 north-star,设明确复活条件(见 §4) |
| **`cmd/servo-embed`** | **删**(冗余:它不嵌入 Servo,只是个 WebDriver-over-HTTP 代理,见 §2) |
| **`servo_webdriver.go`** | 降级为可选实验后端,不投入;或一并删以收surface(执行计划 Stage D 定) |

**一句话**：对 headless 自主浏览器护城河,**"封不掉 + 渲染得了真站" > "引擎在同一个二进制里"**。Chromium 在 P1 上结构性碾压,Servo 在 P1 上结构性致命伤。

---

## 2. 深度 B：Servo 源码 recon 结论（evidence-backed）

源码：`~/Desktop/cankaocangku/servo`（2026-05-30 提交,当前；嵌入 crate `components/servo` v0.2.0）。

### 2.1 in-process 嵌入 API —— 真实、够用、**可行**

`components/servo/webview.rs` 的 `WebView` **直接暴露我们要的全部原语(in-process,非 WebDriver-only)**：
- `load(url)` / `reload()` / `url()` / `load_status()`
- **`evaluate_javascript(script, callback→Result<JSValue,…>)`** —— 进程内 JS 执行
- **`notify_input_event(InputEvent)`** —— 进程内输入注入
- **`take_screenshot(...)`** —— 进程内截图
- headless：`lib.rs` 导出 `OffscreenRenderingContext` / `SoftwareRenderingContext`(无 GPU/窗口可跑)
- 网络控制：`network_manager.rs` + delegate `intercept()`(嵌入方掌控资源加载/cookie)

→ **「Servo 只能 WebDriver 自动化」的担忧是错的**。in-process FFI **技术上可行**。

### 2.2 FFI 的真实成本（非阻断,但非平凡）

- **事件循环 + 线程亲和**：`Servo::spin_event_loop()` 要嵌入方反复 pump；`Servo`/`WebView` 基本非 `Send/Sync` → cgo 绑定须把 Servo 跑在**专属 OS 线程**,所有调用 + 异步回调跨线程 marshal 给 Go。
- **异步回调**：`evaluate_javascript`/`take_screenshot` 是回调式(在后续 `spin_event_loop` 里 resolve)→ FFI 层要做 id 关联 + pump 直到回调触发,转成 Go 的同步 req/resp(和我们 CDP 已写的 id-correlation 同模式)。
- **构建链**：cgo + Rust staticlib；Servo 巨大(release ~161MB / 数分钟),`go build` 一把过的简单性丧失,二进制变大、构建变慢。
- **维护**：嵌入 API 在演进(v0.2.0,频繁提交)→ 须 pin Servo revision,有 API churn 风险。

### 2.3 决定性事实：Servo 网络栈 = 非 Chrome 指纹（P1 致命伤）

`components/net/Cargo.toml` + `Cargo.lock` 确认：Servo 用 **`hyper` + `rustls`(aws-lc-rs) + `tokio`**,**Cargo.lock 里 0 个 boringssl**。
- Chrome 用 **BoringSSL**；Servo 用 **rustls**。两者的 **TLS ClientHello(JA3/JA4)、HTTP/2 SETTINGS/帧序**结构性不同。
- 高防系统(Cloudflare/Datadome/Akamai)在**网络握手层**就能区分,**早于任何 JS**。所以 §2.1 的 in-process JS 指纹伪装**对这层无效**。
- 叠加:Servo web-compat 弱于 Blink(不少真站渲染不全/坏)。

→ **Servo 结构性地削弱 P1**(封不掉)和"能用真站"。这就是暂停它的根本原因——**不是不可行,是不划算**。

---

## 3. 三方对照（按护城河 + headless 加权）

| 维度(权重) | Chromium 自有运行时(选) | Servo in-process FFI | OWL 式自建 Host |
|---|---|---|---|
| **P1 封不掉/反爬**(★★★) | ✅ 真 Chrome 网络栈,JA3/HTTP2 天然真 | ❌ rustls/hyper 一眼假,JS 救不了 | ✅ 同 Chromium |
| **能用真站(web-compat)**(★★★) | ✅ Blink 全兼容 | ⚠️ 实验级,真站常坏 | ✅ Blink |
| **可靠 headless 自动化**(★★★) | ✅ CDP 100+ 域,现成 | ✅ in-process API 够用(但要 FFI) | ✅ 但要写 Host |
| **深度控制 / 自有 / 拿不走 P2**(★★) | ✅ 自带+自控子进程(见 §5) | ✅✅ 引擎在二进制里(终极) | ⚠️ 仍是 Chromium |
| **崩溃隔离**(★★) | ✅ 子进程,引擎崩 agent 活 | ❌ in-process,引擎崩=进程崩 | ✅ Host 崩 App 活 |
| **构建/运维简单**(★★) | ✅ go build 一把过 | ❌ cgo+Rust staticlib,大、慢 | ❌❌ Swift/Mojo,极复杂 |
| **显示/UI/帧/GPU**(对 headless=0) | N/A | N/A | OWL 全部难点在此,对我们零价值 |

**读法**：把 ★★★ 行连起来——**Chromium 在"封不掉 + 真站 + 现成自动化"三条最高权重上全胜**。Servo 唯一独占 P2 的"引擎在二进制里",但它**同时输掉 P1 + web-compat + 崩溃隔离 + 构建简单**。OWL 的全部增量都在我们不需要的显示层。

---

## 4. Servo 状态：可行但暂停的 north-star（不是否决）

deep-B 证明 in-process FFI **可行**,所以它是**真实的远期选项**,不是幻想。文档化保留,**复活条件(满足其一即重启评估)**：
1. Servo 提供 **Chrome-class 网络指纹**(uTLS 风格的可定制 ClientHello,或前置一个伪装代理把 rustls 握手改成 Chrome JA3)——P1 这关能过。
2. Servo web-compat 达到"真站基本不坏"。
3. 出现一个**压倒 P1 的战略需求**:完全主权 / 零外部二进制 / 分发单文件,且我们愿意为此承担反爬劣势(例如目标站点都不设高防)。
→ 在此之前,**不写一行 Servo FFI 代码**(deep-B 已把"该不该现在做"判成"否")。

---

## 5. "深绑进根茎" 对 Chromium 到底是什么形态（诚实回应愿望）

"引擎进根目录"的浪漫版是 Servo-in-binary。但对 **headless agent**,正确的"深绑"**不需要同一个二进制**——OWL 自己也是把 Chromium 当**独立进程/服务**(Client/Host 分离),正是为了崩溃隔离。所以 Chromium 的"深绑" = **智能体自有、自带、深度控制的子进程运行时**：

1. **自带**:agent 打包/锁定一个已知良好的 Chromium 修订(Chrome-for-Testing),**不依赖系统装了什么**。
2. **自控生命周期**:CDP-over-pipe(已有)+ Session Manager(已有)→ agent 拥有进程、私有管道、会话池。
3. **深度能力**:CDP 100+ 域 + 网络拦截 + 指纹 + capability 分级。
4. **崩溃隔离**:引擎崩,agent 活(对 in-process 是**优点**,不是缺点)。

这就是 headless agent 的"引擎在根茎里"。**比 Servo-in-binary 更稳、更能打、更简单。**

---

## 6. 执行计划（交付给另一名开发者,AI 辅助,分阶段）

> 现状基线(另一名开发者已完成):`internal/browser/`(Engine/Page + factory)、`chrome_cdp.go`(CDP-over-pipe)、`chrome_manager.go`(Session Manager,5s→0.11s)、CDP 扩展(PageExt/NetworkPage)、`FingerprintConfig`。
> 纪律红线(每阶段都守):先确认非测试调用点 → 改完 `go build ./...` → 起浏览器的测试 gate 在 `BEISHAN_DEEPSEEK_TEST=1`(verify.sh 须跳过保持 hermetic)→ 声明完成填 INTEGRATION_PROOF。

### Stage D（先做,半天）：清理 + 收 surface
- **删 `cmd/servo-embed/`**(Rust 代理,冗余,见 §2)+ `internal/browser/servo_embed.go` + factory 里 `servo_embed` 分支。
- `servo_webdriver.go`：决定**保留为文档化实验后端**(加注释:仅实验,生产不用)**或一并删**。建议删——少一条会漂移的线。
- 两份 north-star 文档(`SERVO_*`/`CHROMIUM_*`)顶部加指针 → 本决策文档。
- **验证**：`go build ./... && go test ./...` 全绿；`grep -rn servo_embed` 无残留调用点。

### Stage A（1-2 天）：让 Chromium 成为"自有"运行时
- **自带 Chromium**：`findChrome()` 优先用打包/缓存的 Chrome-for-Testing(`~/.hermes/chromium/`),缺失时**首次运行下载**(CfT 有稳定下载 URL + 版本清单)。系统 Chrome 仅作 fallback。
- 锁定一个已知良好版本号(写进 config),避免"系统 Chrome 随机版本"导致 CDP 漂移。
- **验证判据**：**卸载/隐藏系统 Chrome**,agent 仍能跑通 `TestDeepseekWebSearchLive`(用自带 Chromium)。

### Stage B（3-5 天,**护城河核心**）：反爬真实性(P1)
- **先实测(就是 C,见 §7),拿到 baseline 分数**,再改——别凭感觉调。
- 关键认知(本会话反爬技术面)：反爬是 **TLS+HTTP2+JS+渲染+行为+reputation 联合评分**;真浏览器**稳定一致**,stealth 最忌"处处随机噪声"。
- Chrome 的**天然优势**:JA3/HTTP2 真。所以重点在 **JS/runtime 层**：
  - `navigator.webdriver` 移除 + **property graph 自洽**(UA↔platform↔WebGL renderer↔fonts↔languages↔userAgentData 组合一致,别 macOS UA 配 Windows WebGL)。
  - **CDP-instrumentation 检测**缓解(`Runtime.enable` 副作用、console/serialization quirks)——能不开的 CDP 域不开。
  - Canvas/WebGL **coherent spoofing**(device-class 模板,不是 random noise)。
  - 评估 `--headless=new` 是否可被识别 → 备选 `--headless=old` / 虚拟显示 headful / **真持久 profile**(借 DeepSeek 登录那套,带 browser-aging + storage 连续性 reputation)。
- **验证判据**(硬指标,gate 在 env)：
  - `bot.sannysoft.com` 全绿;`CreepJS` trust 分达标;`fingerprint.com` demo 不被判 bot。
  - 一个真实 Cloudflare-protected / Datadome 站点能加载到内容(非 challenge 页)。
  - 记录"改前 vs 改后"分数到报告。

### Stage C（2-3 天）：深度控制 + 异步(P2,落 OWL 原则)
- **agent 浏览器调用异步化**(OWL「agent 必须严格异步」)：`deepseek_web_search` 现同步堵 120s → 改 goroutine + polling,不阻塞 agent 主回路。这是**真卡使用**的项,优先级高。
- **NetworkPage 事件路由完善**:把 `Network.*` 事件从 `chromeCDP.readLoop` 路由到页面(请求拦截/响应捕获 = 反爬 + 观察 = OWL observe 闭环)。
- **Session Manager 超时 + LRU 回收**(OWL memory QoS):泄漏 session 自动清理,上限 + 回收。
- **capability 分级强制**(OWL 安全):L1 navigate(agent 可)/L2 eval+screenshot(需 source=user)/L3 download+network+identity(需用户确认);agent 来源的 L2/L3 请求被拒。
- **验证判据**:并发两个 agent session cookie/storage 互不可见;长跑无泄漏;agent-source 的 eval/screenshot 被 `agentSourceAllowed` 拒。

### Stage E（持续）：观察闭环(可选,向 OWL agent-native 靠)
- DOM snapshot / accessibility tree / screenshot 三选一作为 agent 的"看",喂回推理 → `navigate→observe→reason→act` 闭环。**先不做,等前面稳。**

---

## 7. C —— 反爬实测清单（Stage B 的前置,交另一名开发者跑）

| 测试站 | 看什么 | 判读标准 |
|---|---|---|
| `bot.sannysoft.com` | webdriver/UA/plugins/WebGL/权限 一票项 | **全绿** |
| `CreepJS`(abrahamjuliot.github.io/creepjs) | trust score + lies + headless 标记 | trust 高、lies 少、无 headless |
| `fingerprint.com` demo | visitorId + bot 判定 | 不被判 automation/bot |
| Cloudflare 保护站(任一真实) | 是否过 challenge 到内容 | 拿到真实内容,非 5s challenge |
| Datadome / Akamai 保护站 | 同上 | 同上 |

- 每项记"改前/改后",写进 `docs/reports/antibot_baseline_<date>.md`。
- **重点判读**：多半会发现"真 Chrome 网络层天然过(JA3/HTTP2),但若我们走任何代理就露"——所以**别在 agent 与目标站之间插非 Chrome 网络栈的代理**。

---

## 8. 验证 & 纪律（两名开发者共用）

1. 所有起浏览器/联网的测试 **gate 在 `BEISHAN_DEEPSEEK_TEST=1`**;`bash scripts/verify.sh` 必须 hermetic 全绿(跳过这些)。
2. 纯函数(JSON 抽取/状态判定/指纹配置构造)留常驻单测。
3. 每阶段声明完成 = 填 INTEGRATION_PROOF + 非测试调用点 + `go build`/`go test` 输出。
4. 反爬"理论通过"不算数,**只认 §7 实测分数**。
5. 不重写引擎、不抄 OWL 显示层架构(对 headless 零价值)。

---

## 9. 一页纸 TL;DR（给 AI 辅助执行者）

- **深绑 Chromium**(自有子进程运行时:自带 CfT + 自控 + CDP 深控 + 反爬 + capability)。
- **Servo 暂停**(in-process FFI 已验可行,但 rustls/hyper 网络指纹结构性输掉反爬;复活条件见 §4)。
- **删 servo-embed**(冗余代理)。
- **执行序**：Stage D(清理)→ A(自带 Chromium)→ **B(反爬,护城河核心,先跑 §7 实测)** → C(异步+深控)。
- **每阶段验证判据明确,gate 在 env,守集成纪律。**
