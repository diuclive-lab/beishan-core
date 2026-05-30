# 变更日志

## 2026-05-29 可靠性 + 可用性优化：panic 安全收口 + 资源审计 + 大文件拆分 + 错误响应三端可读

### R1 panic 安全（`internal/observatory/recover.go`）
- 新增 `Recover` / `RecoverWith` / `SafeGo`：6 个裸 goroutine 纳入 panic 兜底（HTTP handler、异步发送、会话摘要、并行 subagent、两处 workflow 并行）
- 契约保护：完成信号 defer 先注册（LIFO 后执行）、RecoverWith 后注册（先执行），panic 不再使等待方死锁

### R2 测试隔离（`internal/tools/storage_test.go`）
- `withTempKnowledge` 助手：恢复 `knowledgeDir` + 重置 `currentStorage` 懒缓存，消除潜在跨测试污染
- 3 个 graph 测试去样板化

### S1 knowledge.go 拆分（3578 → 492 行 core + 6 兄弟文件）
- 按职责拆分：`knowledge_search`（检索管道）/ `_embedding` / `_links` / `_maintenance` / `_analysis` / `_tools`
- 纯包内移动：93 函数零丢失/零重复，`go build`+`vet`+`test` 全绿，`-shuffle=on` 复测通过
- 顺手清理 9 行失效注释（`RetrievalResult 已迁移` 指针 + `Tool 注册` 孤儿标题）
- 文档同步：CLAUDE.md / DIRECTORY.md / DATA_FLOW.md / DESIGN_PRINCIPLES.md
- 死代码清理：删除 `matchesTag`（commit 75e49fd 接入统一检索管道时漏删的孤儿函数）

### R3 goroutine panic 兜底覆盖（R1 的延伸）

R1 建好 `Recover/RecoverWith/SafeGo` 基础设施 + 8 个调用点；R3 把覆盖面扩到其余跑应用逻辑的裸 goroutine：
- **事件总线** `internal/bus/bus.go`：`go h(evt)` → `SafeGo`——任一订阅者 panic 不再掀翻进程
- **调度器** `plugins/scheduler_plugin.go`：`triggerWorkflow` 顶部 `defer Recover`，单次触发 panic 不杀死 runCron/runInterval 循环
- **知识后台** `knowledge.go` 自动建链 + `knowledge_embedding.go` 批量补向量 → `SafeGo`
- **web 搜索** `web.go`：Tavily/DuckDuckGo/Bing 三 goroutine `RecoverWith`，panic 时仍向 channel 发空结果，消费方不等满 10s 超时
- **工作流** `engine.go`：异步 `routeOutput` → `SafeGo`；并行 batch `do()` 加 `Recover`（LIFO 在 wg.Done/`<-sem` 之后注册→先执行）
- **其他** `review_handler` 通知 + `main.go` 会话落盘/故障切换监控
- 新增内部依赖边 `bus/tools/plugins → observatory`（observatory 是零内部依赖的叶子包，无环）
- **明确未覆盖**（见 KNOWN_LIMITATIONS §17）：glue 长循环（低风险 + 需逐迭代兜底）、`kernel.go:246`（冻结区，待批准）、`done <- cmd.Run()` 系列（stdlib 不 panic）

### 代码格式立场：拒绝全库强制 gofmt（决策记录，无代码改动）
- R3 期间发现 `gofmt -l .` 列出约 115 文件"漂移"；根因排查（`gofmt -d kernel/msg.go`）确认是
  Go 1.26.1 gofmt 把项目手写的 `/* */` 散文块注释"规范化"（正文移出 `/*` 行 + 续行 tab 缩进）+ struct tag 重排
- **决策**：把 `/* */` 块注释确认为有意风格；不全库 `gofmt -w`，不把 `gofmt -l` 加进 gate；新代码靠手维持 gofmt-clean
- 理由：重排会降低散文文档注释可读性 + 触碰冻结的 `kernel/` 仅为美观 + 制造淹没历史的巨型 churn
- 文档：`DESIGN_PRINCIPLES.md` 新增"代码格式立场"节；`docs/MERGE_DECISIONS.md` 决策 14。**未改任何 `.go` 文件**

### kernel.go:246 回调 goroutine panic 兜底——在 kernel 外解决（零内核改动）
- R3 留下的唯一冻结区缺口：`kernel/kernel.go:246` 的 `go notify.Callback(...)` 裸 goroutine，panic 会掀翻进程
- 用户**已批准**改 kernel，但调查发现该 goroutine 执行体 100% 是 `notify.Callback`（唯一非测试调用点就是这行 `go`）
- **修复落在 `internal/notify/notify.go`**：`Callback` 顶部加 `defer observatory.Recover("notify.Callback")`，等价兜底、**零 kernel 改动**
- `notify` 不在冻结区，`notify → observatory` 无环（observatory 是零内部依赖叶子包）
- 治理记录：`DESIGN_PRINCIPLES.md`「内核冻结治理（特许令机制）」+ `docs/MERGE_DECISIONS.md` 决策 15 + KNOWN_LIMITATIONS §17 第 2 类标记为已修复
- **不扩散原则**：这是经批准的防御性修复，不构成未来向 `kernel/` 加功能的先例——反证"想改内核先找内核外解法"通常成立

### glue 长循环逐迭代兜底——收口 panic 安全弧线（§17 第 1 类）
- `glue/glue.go` 的 `demuxLoop` / `healthCheckLoop` 是裸 goroutine（`go g.demuxLoop(p)` / `go g.healthCheckLoop()`），循环体内 panic 会掀翻进程
- 把循环体抽成单次处理函数 `demuxOne` / `runHealthCheck`，函数顶部 `defer observatory.Recover`——单次迭代 panic 被吞、**循环存活**
- 关键：**不在循环顶部** `defer Recover`（那样 panic 会杀死整个循环 → IPC demux / 健康检查静默停摆，比崩溃更隐蔽）；循环控制（`Scan`/`<-ticker.C`/`<-stopHealth`）不 panic，无需覆盖
- 至此 panic 安全弧线 **R1→R3→kernel.go:246→glue 循环** 基本收口，KNOWN_LIMITATIONS §17 仅剩第 3 类（`cmd.Run`/`Wait`，stdlib 不 panic，风险可忽略）
- 纯重构 + 兜底，无行为变化；`go build`+`vet`+`test`（glue 0.801s）全绿，新增代码 gofmt-clean

### 资源/错误审计 + notify 死脚手架清理（可靠性第 2 项）
- 全量审计三类隐患，结论**全部健康**：① HTTP `resp.Body` 泄漏=零（20+ 产生点逐文件 producer/close 配平）；② goroutine 泄漏=零（~20 个 `go` 启动点逐一归类：buffered `done`(cap 1) / buffered-to-N 扇出+`RecoverWith` 兜底发送 / 带 `stop` 路径的 daemon / 进程生命周期级 / fire-and-forget+Recover）；③ 被吞 err=全良性（`json.Marshal` 惯用法、best-effort 备份写、带注释的 unlock）
- "零泄漏"是前面 R1→R3 panic 安全工作的副产品——扇出 goroutine 全带 `RecoverWith` 兜底发送，顺带也就 leak-safe
- 扫描踩坑：`\.Do(` pattern 误匹配 `sync.Once.Do()`（`browserOnce`/`usageOnce`）→ 假阳性 HTTP 泄漏；改精确 pattern 重扫
- 唯一动手项：删 `notify/{slack,email,notify}.go` 的遗留 suppress-unused 脚手架（`logPrefix` 死变量 + `_ = time.Second`/`_ = fmt.Sprintf`/`_ = json.Marshal` + 包级 `var _ = fmt.Sprintf`），共 8 行 + 1 死 import（`slack.go` 的 `time`）；三文件向本就干净的 `wechat.go` 看齐
- `go build`+`vet`+`test`（21 包）全绿；清理 gofmt-中性（gofmt diff 全是既存决策 14 块注释漂移，未触及删改行）

### `/api/chat` 同步错误响应修复——三端可读（可用性第 3 项）
- 老代码失败时回 `{"status":"sent", note}`：① `"sent"`（已发送）对失败是**误导信号**；② 缺 `session_id` 导致 iOS `ChatResponse` 解码失败（该字段非可选）→ 用户只看到 Swift 解码报错，**服务端错误说明彻底丢失**
- 改为与成功响应同构的 `{session_id, sender:"system", type:"error", status:"error", payload:友好消息, note:原始错误}`：web/iOS 读 `payload`、REPL 读 `note`（向后兼容）、`status:"error"` 去掉谎言；超时错误（`Call 超时: …`）映射为"处理超时（超过 120 秒）…可稍后重试或拆小请求"
- HTTP 保持 200（apple-core 对非 2xx 抛错，让错误以一条 system 消息内联进对话，比抛异常体验好）
- 下游三端 + 测试全部 grep 核查无依赖旧形状；启动校验（缺 API key 的 `log.Fatal` 消息）与 `/status`（JSON + `/dashboard` HTML 分工）经审已足够清晰，无需改
- async 路径同类问题（送失败不落 result、客户端轮询到超时才知）改动更大，已登记 devlog 留待后续
- `go build`+`vet`+`test`（21 包）全绿；新增代码 gofmt-clean（main.go 既存 import/缩进漂移属决策 14，未触及改动行）

### skill_factory + code_analysis 大文件拆分（可维护性第 4 项）
- `skill_factory_plugin.go`（1195）→ 三关注点：核心 246（生命周期 + `OnMessage` 分发 + list/view/delete + `buildPluginList`）/ `skill_factory_generate.go` 813（输出类型体系含 490 行 `workflowOutputTypes` 表 + LLM 生成管道）/ `skill_factory_validate.go` 155（L1–L4 硬化校验 + 落盘）
- `code_analysis_tools.go`（933）→ 按**导入不相交**切：`code_ast_scan.go` 377（`go_struct_scan` 簇，独占 go/ast·parser·token·types + regexp）/ 剩余 565（read_external / dir_scan / code_tree / code_stats / code_lang_detect / base_capability_inventory + 注册，独占 mcp）；切前 grep 验证 235–595 行外零 `ast.|parser.|token.|types.|regexp.` 命中
- 方法：`printf` 干净 import 头 + `sed` 区间字节级抽取（不手抄，零转录风险）；同包搬移调用方零改动，未用 import = 编译错误成为安全网
- 纯搬移三验证：定义计数守恒（code_analysis 27=10+17；skill_factory 各函数恰一次，无丢失无重复）；build 全绿 = 每文件 import 与用量精确匹配；剩余文件 gofmt 漂移 171→**降至** 143（簇既存漂移随簇离开、在新文件被 gofmt 清掉；若接缝引入新双空行漂移会升而非降）
- gofmt 不对称（决策 14）：带多行块注释的文件（skill_factory 核心、code_analysis 剩余）不 gofmt；无多行块注释的新文件 `gofmt -w` 清干净（顺修原 398 行多余前导空格）
- 纯包内重定位，注册 / 路由 / 调用点 / 工具名 / 消息类型全不变 → DATA_FLOW.md 无需改；`go build`+`vet`+`test`（22 包）全绿
- 有意停手：`main.go`(1017) / `think_plugin.go`(930) 无同样干净切口，不为凑数强拆，留待单独处理

### 把工作模式固化：verify.sh（机械半）+ 操作手册（判断半）
- 新增 `scripts/verify.sh`——一键变更验证（build/vet/test + 改动文件 gofmt 提示 + integration_check），「集成纪律」里可自动化的那一半收成一条命令；不用 `set -e`（跑完所有检查再汇总），gofmt 仅对改动文件提示不计失败（决策 14 不全库强制）
- 新增 `docs/REFACTOR_AUDIT_PLAYBOOK.md`——判断那一半的四条可复用配方（资源/错误审计、改一处先查三端、导入不相交拆分+三验证、完成前 INTEGRATION_PROOF），提炼自 Task H–J；CLAUDE.md Key Documents + Quick lookup 加指针
- 附"工作流模块化"可行性判断：引擎 `workflow_run` 组合机制现成（`agent_observer`/`batch_ingest` 已用），但只有**机械原语**适合做可信模块（verify/scan/inventory），**判断步骤**至多做"建议模块"（LLM step，输出非保证），把建议当决策违反硬化层原则；第一个该做的模块是 verify（verify.sh 的运行时外壳）

### `verify` 工作流模块——实跑暴露"机械≠可信"，hermetic 化坐实（可组合模块第 1 个）
- 新增 `workflows/verify.yaml`——`scripts/verify.sh` 的运行时工作流外壳：单 step `terminal_exec` 跑 verify.sh，v2.5 合规（治理头注释 + 每 step `on_error`），可经 `workflow_run` 当"提交前门禁"嵌进更大工作流
- **实跑翻车**：从现网守护进程（PID 1024，launchd 最小环境）跑当场 300s 撑爆超时、terminal_exec 超时只回空——验证了 CLAUDE.md"上一次写的代码可能没真正集成"，薄封装看着对、实跑才暴露
- **根因二（都是环境非机械）**：① launchd PATH 仅 `/usr/bin:/bin:…`，`/opt/homebrew/bin/go` 不可见；② 守护进程 env 带真实 `DEEPSEEK_API_KEY`，`go test ./...` 遂发真实 LLM 调用联网挂死
- **修法 = 让 verify.sh hermetic**：`go` 不可达时补 PATH 兜底（对开发 shell 是无害空操作）+ `unset` 所有 LLM/embedding key 走离线跳过路径；改 verify.yaml 用绝对路径（脚本自定位仓库根）、超时 300→180。daemon 实跑从 300s 超时变 **4.6s 全绿**
- **教训回写** `docs/REFACTOR_AUDIT_PLAYBOOK.md`：可信模块判据从「机械」升级为**「机械且环境无关」**——不依赖守护进程缺失的开发工具、不被其密钥扰动；hermetic 离线门禁不该依赖外部服务可用性

### `scan_large_files` 工作流模块 + verify 部署现网——「环境无关」第三味（可组合模块第 2 个）
- 把 `verify.yaml` 部署到现网 daemon `workflows/` 目录（手动 cp，引擎每次 Run fresh `os.ReadFile` 故无需重启）；daemon 实跑 `verify` → Success / ElapsedMs 6783 / "✅ 全部通过"
- 新增 `workflows/scan_large_files.yaml`——一键列出最大 Go 源文件（top-30 行数排行，拆分候选）；本仓库拆 skill_factory/code_analysis 就靠这类排行
- **选型实跑**：先试架构偏好的 L3 工具 `code_stats`（描述写明"替代 wc/find，list_files→top-N"），但现网 daemon（May 26 二进制）硬化层连报 `未知字段: limit` → `未知字段: list_files`——部署二进制的 code_stats schema 早于这两个字段（top-N 排行 May 26 后才加），stale daemon 上根本给不出排行
- **改用 `terminal_exec`+POSIX**（find|wc|sort|head，cd 绝对仓库根）→ daemon 实跑 Success / ElapsedMs 165 / 干净 top-30；command/timeout 是核心老字段，不吃 schema 版本
- **环境无关第三味**：verify 学到①不依赖 daemon 缺失的开发工具 ②不被其密钥扰动；scan_large_files 再加③**不依赖 daemon 二进制里工具的 schema/feature 版本**——要在可能 stale 的 daemon 上稳跑，最 vintage-proof 的原语是 `terminal_exec`+POSIX

### 根治 dev↔daemon 漂移：版本戳 + 只读漂移探测 + 一键部署（devlog Task N）
- 根因：Task L/M 反复撞见的「部署二进制太老 / daemon 缺工作流」都是同一个 dev↔daemon 漂移，过去无机制让它可见可消除。用户点出模式、拍板全做。
- **二进制版本戳**（`cmd/beishan/main.go` +13 行）：`-ldflags "-X main.version=<git短哈希> -X main.buildTime=<ISO8601>"` 注入；`/health` 升为 `{"status":"ok","version":…,"built":…}`，让「现网跑哪个版本」成为可 curl 的事实；加 `-version` 标志（只打印即退，不启服务/端口/sidecar）供部署冒烟校验
- **`scripts/daemon_drift.sh`**（只读）：curl `/health` 权威比对 version==HEAD（老二进制无字段→退回 mtime 比对），`cmp -s` 逐个比对工作流，daemon-only 文件只提示不算漂移；退出 0/1。刻意**独立于 verify.sh**（后者 hermetic 离线、不探活；本脚本必须探活，职责相反）。首跑量化现状：二进制 STALE + 42/45 工作流漂移
- **`scripts/deploy.sh`**（一键）：①带戳编译到 `.new` 临时文件 ②`-version` 冒烟校验（戳≠HEAD 则中止不替换）③原子 `mv` 换二进制 + 增量 `cp` 工作流（只增不删，保留 daemon-only）④`launchctl kickstart -k` 重启服务（KeepAlive 兜底）⑤轮询 `/health` 直到 version==HEAD。**绝不碰** plist 配置与含密钥的启动包装脚本——只重启服务，不改配置
- **关键顺序**：版本戳打 HEAD 短哈希，故**必须先 commit 使 HEAD==被编译源码、再 deploy**，否则二进制被打上旧 HEAD（含未提交新代码）会让漂移探测误报

### R1 收尾：Go-DSL 执行器请求路径 panic 兜底（原始可靠性清单第 1 项）
- 此前 `GoExecutor.Run`（同步执行路径）**零 recover**：开发者提供的 `TransformFn`/`BeforeExecute`/`AfterExecute` 或任一步骤实现 panic 会一路冒泡，把整个工作流请求**带栈中断**（HTTP 按请求 recover 不崩进程，但该请求异常终止、无干净错误）
- `Run` 加 `defer observatory.RecoverWith`：panic→失败 `WorkflowResult`(Success=false, Error 含 panic)，返回值改命名 `(result *WorkflowResult)`
- `runGoStepParallel`：`Recover` 升级为 `RecoverWith`——并行子步骤 panic 时**补记一条失败 StepResult**（此前被静默吞掉、结果凭空消失）；`wg.Done` 先注册→完成契约不破
- 2 个测试坐实：同步步骤 panic→失败结果含 "panic"；并行子步骤 panic→记为 error、整体不 crash
- **未改**：`gods_executor.go` 另 5 处 panic 是**构造期(boot) fail-fast**（Go-DSL 工作流引用未注册工具→启动即崩），属开发者配置守卫、语义为有意 fail-fast；是否改 log-and-skip 留待决策（见 KNOWN_LIMITATIONS §14 边界）

### U2 修复：去掉重复的 ecs-relay launchd 服务（原始可用性清单第 2 项，运维变更）
- 根因：两个 launchd 服务 `com.beishan.ecs-relay`（May 25）与 `com.fanglab.ecs-relay`（Mar 27）**都把同一远程 `:18013` 反向 SSH 隧道转发到本地 `:8013`**——撞同一远程端口，输家 ssh 报 `remote port forwarding failed for listen port 18013`、`ExitOnForwardFailure` 致 exit 255，KeepAlive 永久重启 → 两个 err 日志累计 **23k+ 行** flap 噪声（持续吃 CPU/磁盘）
- 二者功能等价（端口集相同 `8013/18013`、撞同一远程即证同一服务器）；用户拍板**留 `com.beishan`（当前命名，与 `com.beishan.mcp-knowledge` 同代）、删 `com.fanglab`（FangLab 旧命名）**
- 操作（仅动 ecs-relay，其余 `com.fanglab.*` 服务不碰）：`launchctl bootout` 停止+卸载 fanglab → 其 plist 改名 `.plist.disabled`（**可逆备份，不真删**）→ 截断 23k 行 flap 日志 → `kickstart -k` 重启 beishan 抢占释放的 `:18013`
- 验证：`com.beishan.ecs-relay` PID **稳定 30s 不变** + err 日志**零增长**（修前每几秒 flap 一次）→ 隧道已建稳；`com.fanglab` 从 `launchctl list` 消失
- 回滚：`mv ~/Library/LaunchAgents/com.fanglab.ecs-relay.plist.disabled …/.plist && launchctl bootstrap gui/$(id -u) …`（但会重新引发端口冲突，仅在改留 fanglab 时用）

### 启动降级：Go-DSL 构造期 panic → log-and-skip + /health degraded（彻底清掉 R1 构造期债务）
- 把 R1 剩下的「构造期 fail-fast」债务处理干净：原 5 处构造期 panic（Go-DSL 工作流引用未注册/未配宿主工具 → **启动即崩、拖垮整个 daemon**）改为**返回 error → 降级跳过、daemon 照常启动**
- `internal/workflow/gods_executor.go`：`validateGoStep` / `NewGoWorkflowPlugin` / `NewGoToolPlugin` 由 panic 改为返回 `error`
- `internal/observatory/health.go`：新增**可复用**降级登记 `RecordDegradation(component,reason)` / `Degradations()` / `EventDegraded`（记日志 + 发事件，线程安全、返回副本）——任何非核心模块启动失败都能用
- `cmd/beishan/legal_review_go_dsl.go`：注册拿到 error → `observatory.RecordDegradation` + 跳过；顺手删 `main.go` 一处丢弃返回值的死示例 `NewGoToolPlugin`
- `cmd/beishan/main.go`：`/health` 有降级时升为 `{"status":"degraded","degradations":[…]}`（仍 **HTTP 200**，degraded≠down），抽出 `healthResponseJSON()` 便于单测
- `scripts/daemon_drift.sh`：加健康降级提示（探 `/health` 的 degraded）——**刻意放这而非 hermetic 的 `verify.sh`**：运行期信号要探活，verify.sh 是离线门禁不依赖 daemon 在线（Task L 教训）
- **4 个新测试**：构造期坏工具→error 不 panic（×2）+ 合法插件步骤 OK + 降级登记机制（含副本语义）+ `/health` ok→degraded 且保留 `version` 字段（deploy.sh 依赖）
- 文档：`DESIGN_PRINCIPLES.md` 新增「核心 fail-fast，非核心降级」原则；`KNOWN_LIMITATIONS.md` §17 该项从「待决策」改为「已收口」

### R4 门禁硬化：core_gate 从「看着全实则只挡 test/vet」改为诚实分层 + 退出码判定（原始可靠性清单第 4 项）
- 病根：旧 `core_gate.sh` 除 test/vet 外 8 项默认全降级 ⚠️ 不阻断（仅 `--strict` 才阻断），「门禁看着全、实则只挡编译+测试」；且用 grep 末行关键词判过——`一致` 是 `不一致` 的子串，失败会被误判为通过
- **改判定方式**：改用脚本**退出码**（不再 grep 末行关键词），消除子串误判
- **诚实分层**：BLOCKING（离线确定性，恒阻断，无视 flag）= test / vet / boundary / workflow-v2.5 / docs / workspace / security 共 7 项；ADVISORY（需活服务/网络，默认只提示，`--strict` 才阻断）= core-health / core-eval / rightflower
- **修两处「检查本身是 theater」**：① `check_security_model.sh` 4 项 grep 全 `|| echo ❌` 却**从不设 FAILED**→永远 exit 0（补 `FAILED=1`，4 目标现存故仍绿但已为真）；② `scan_boundary.sh` 实际**在失败**——`workflow_plugin.go` / `knowledge_calibration.go` 两处内部路径 I/O 边界债务未登记
- **补登记 D04/D05**（`docs/reports/boundary_debt_register.md`）：均为内部固定路径 I/O（引擎读自身 workflow 目录 / 校准 JSONL 日志），同 D02/D03 性质、非用户输入、非路径穿越；`scan_boundary.sh` 的 KNOWN pattern 同步加这两文件 → 扫描转「仅已知债务」通过
- **验证**：全量 gate 7 BLOCKING 全 ✅ + 3 ADVISORY 提示、exit 0；负向单测坐实 block 失败→gate exit 1、advisory 默认不阻断 / `--strict` 阻断
- 文档：`DESIGN_PRINCIPLES.md` 更正「core_gate 只跑 build/vet/test」的过时说法

### 浏览器/搜索可靠性：止住「裸抓搜索引擎被反爬 + 静默把垃圾当成功」
- 用户实测发现「智能体用浏览器搜索出问题」。排查坐实：`browser_navigate`→`fetchAndExtract` 是裸 HTTP GET + bot UA `HermesAgent/1.0` + 不渲染 JS，用它抓搜索引擎→**实测 DDG/Bing 回 302 同意页 / 202 反爬挑战页**，且 `fetchAndExtract` 只在 ≥400 报错→**把挑战页当成功返回垃圾**，智能体据此对空内容继续推理
- **`web.go` 止血**：① `webSearchHandler` 0 结果→`Success:false`+`Error` 提示（不再静默 `Success:true`）；② `fetchAndExtract` UA 换真实 Chrome（bot UA 必被反爬）；③ 检测反爬（202/204 状态 + `unusual traffic`/`人机验证`/`/cdn-cgi/challenge` 等强信号）→报错而非返垃圾（不收 `captcha` 单词，避免误伤科普文）
- **`browser.go` 硬化**：① 补 `isSafeURL`(SSRF)+`containsSecret`(密钥外泄) 检查——与 `web_fetch` 对齐，此前 `browser_navigate` **完全没有**这两道防护（能打内网 IP）；② `isSearchEngineURL` 识别搜索引擎搜索 URL→拒绝并指向 `web_search`（别用浏览器搜）；③ `browser_scroll`/`browser_back` 从假"成功"改为诚实告知文本模式不支持
- **接入 Tavily 搜索后端**：dev `.env` 配 `TAVILY_API_KEY`（gitignored，未提交）；实测 `web_search`→Tavily 端到端返回真实结果。现网 daemon 需在其启动环境补 `TAVILY_API_KEY` 才生效（包装脚本含密钥、不由本仓库管）
- **测试**：4 个——`isSearchEngineURL`(搜索 URL 拦/非搜索放)、`looksLikeAntiBot`(反爬识别/不误伤)、`browser_navigate` 私有 IP SSRF 拦截（hermetic，前缀短路不走 DNS）、`web_search`→Tavily 集成（有 key 才跑，verify.sh 去 key 故跳过）
- **决策**：迭代不更换——免费爬搜索引擎时代已终结（DDG 都上 202 反爬），换浏览器工具也躲不开；架构（`web_search`+API、`web_render` Playwright、SSRF/密钥检查）本就齐全，是接线接错。FangLab 的「无头浏览器登录 DeepSeek 网页版用其免费搜索」方案不在本仓库，作为右花集成备选

### DeepSeek 网页版搜索：CDP-over-pipe 原生 Go 移植（吸收 FangLab，免费备选后端）
- 用户拍板把 FangLab 的「无头浏览器登录 DeepSeek 网页版、用其免费联网搜索」吸收进来、落地为**原生 Go**。机制：把 DeepSeek 网页版当成「会联网搜索的 LLM」——填提示词让它只回包在 `<BEISHAN_JSON>` 标记里的结构化 JSON，轮询正文抽取
- **`internal/tools/cdp.go`**：极小 **CDP-over-pipe** 客户端——beishan 用 `--remote-debugging-pipe`（fd 3/4 null 分隔 JSON）自己**启动并拥有**一个 headless Chrome，**不经 websocket、不用 playwright 库、不起外部 driver**。零新增依赖。这是「让浏览器成为智能体一部分」的第一步（远期 north-star：内嵌 Servo 引擎）
- **`internal/tools/deepseek_web.go`**：移植 `search_via_deepseek`——navigate→登录检测→`Input.insertText` 敲提示词→Enter→轮询 `body.innerText` 抽 `<BEISHAN_JSON>`→规范化；**新增「已思考」捕获**（FangLab 版没有）。注册工具 `deepseek_web_search`
- **`cmd/deepseek-login`**：一次性「有头」登录命令（持久化 profile，之后 headless 复用）
- **已端到端验证（live，登录后）**：用户一次性登录后实跑——`success=true / 4 条真实 2026 结果（baidu/economictimes/morphllm，智能搜索真联网）/ 已思考 2105 字节（"搜索到 34 个网页…浏览 9 个页面…"的推理）/ 45s`
- **登录后调参的 3 个真实坑**（实测取证）：① 插入提示词后**必须等 ~900ms 再回车**（React 未登记输入就 Enter→空发，正是首跑 90s 超时根因）；② 开关是「**智能搜索/深度思考**」（非"联网搜索"），用 `aria-pressed` 判状态、`ensureDeepseekToggleOn` 仅在关时点开（不盲翻）；③ 开「智能搜索」后引用角标/换行被注入 JSON 字符串值→非法 JSON，`extractMarkedJSON` 加「内部空白折空格重试」修复；「已思考」锚点是「已思考（用时」非"已深度思考"
- **测试**：纯函数单测（标记 JSON 抽取/登录态）hermetic 常驻；CDP 传输 + 端到端搜索 gate 在 `BEISHAN_DEEPSEEK_TEST=1`（verify.sh 跳过、保持 hermetic，Task L）。Tavily 仍是主搜索后端，这是免费备选 + 已思考有惊喜
- **north-star 文档**：`docs/SERVO_BROWSER_NORTHSTAR.md`——把浏览器引擎内嵌进智能体根茎的完整设计 + 验证思路（交付另一名开发者 AI 辅助推进）

## 2026-05-28 Plugin 层系统性审查 + Workflow v2.5 合规扫描

### Plugin 层修复（8 文件，按 §6.1 逐项核对）
- **browser/image_gen/tts**: 成功路径返回空 Message → 输出 JSON Payload + 检查 result.Success
- **write_plugin**: 4 个 case 从空 Message 改为输出 payload + 全部 case 添加 Success 检查
- **notify_plugin**: `result.Error != ""` → `!result.Success`（语义等价性修复）
- **search_plugin**: web_fetch/extract/render 添加 Success 检查 → `.error` 类型
- **todo_plugin/skill_factory**: 失败时返回 `.error` 类型而非 `.result`

### Workflow v2.5 合规（11 文件修复 + 41 文件补声明）
- 11 个 YAML 修复缺失的 `on_error`/`timeout` 字段
- 41 个 YAML 添加 `output_target: chat` 声明
- `absorb_right_flower.yaml` 登记为不可执行参考文档

### 文档
- CODE_REVIEW_SPEC.md §9: 新增 6 种 AI 高频错误模式
- KNOWN_LIMITATIONS.md §15: absorb_right_flower 归档标记
- DEVLOG_20260528.md: 本次审查完整记录
- DATASET.md → docs/archived/DATASET.md（已归档）

## 2026-05-26 v2.5 全量升级 + 治理框架 + Go 工具反推

### 新增
- **治理框架**: `docs/ABSORPTION_GOVERNANCE.md`（证据等级、吸收等级、风险分类、升级策略）
- **v2.5 YAML 标准**: `docs/V25_WORKFLOW_STANDARD.md`（强制项、条件项、禁止项、骨架模板）
- **base_capability_inventory L3 工具**: 返回底座自身能力资产清单（124 tools, 15 MCP, 41 workflows）
- **Failover 模型**: gemma-4-31B（port 8091）作为 API 故障切换

### 增强
- **40/40 YAML 升级至 v2.5**: 治理引用 + on_error 兜底 + evidence 等级标注
- **code_tree**: +list_files 返回文件列表 +lang 过滤
- **code_stats**: +list_files 返回 top-N 文件行数排行
- **go_struct_scan**: +root 批量目录扫描 + import 频率分析
- **code_read_external**: +paths 多文件批量读取
- **agent 系统**: 7 个缺口修复（并发安全、错误分类、截断通知、输出校验、LLM 重试、异步写盘、空 prompt 校验）

### 修复
- **workflow engine 栈溢出**: resolveJSONValue 递归无深度限制，+depth 参数 ≥10 截断

### 模型
- 本地推理: Qwen3.6-27B → gemma-4-E4B-it-Q4_K_M.gguf (4B)
- Failover: 新增 gemma-4-31B-it-Q4_K_M.gguf (31B, port 8091)

### 实测
- 17/18 工作流 API 测试通过 (94.4%)
- 2 个大 YAML（code_deep_analyze + content_review）输出质量验证通过
- evidence + risk_register + source_credibility 字段在 LLM 输出中实际出现

## 2026-05-25 FangLab 桌面操作吸收 + 101 工具

### 新增
- **desktop_actuator L3 工具**: 点击/输入/窗口树/菜单栏/菜单项操作
- 工具数 100 → **101**
- 吸收工作流全流程验证（Step 1-6）

### 来源
- FangLab scripts/desktop_actuator.py（同宗同源吸收）

### 验证
- get_window_tree ✅
- get_menu_bar_tree ✅
- build ✅ vet ✅ hardening ✅ docs ✅

## 2026-05-25 全天工作流组合 + 工具清理 + MCP 技能框架

### 新增
- **MCP 技能框架**: `internal/mcp/` 包 + 15 个技能服务器（cmd/mcp-servers/）
- **60 工作流组合设计**: 7 大类覆盖 104 工具 + 15 MCP 技能
- **首批实现 5 个工作流**: csv_data_pipeline / security_audit_parallel / quick_project_scan / kb_hygiene_plus / web_research_auto
- **desktop_actuator L3**: 桌面点击/输入/窗口/菜单操作
- **document_extract L3**: 文档内容提取（txt/md/pdf/docx/csv）
- **csv_profile + csv_sample**: 纯 Go CSV 分析工具

### 修复
- 工具去重：weather(3→1), session_list(2→1), session_search(2→1)
- read_file + patch 硬化层路径校验
- dir_scan 标记废弃，推荐 code_tree
- parallel 步骤 ctx key 格式修正
- CHANGELOG 公网 IP 替换为占位符
- 全仓库安全审计：清理硬编码路径

### 当前指标
工具数 104 | MCP 技能 15 | 右花 3 | 工作流 38 | 远程通道 ✅
## 2026-05-25 远程通车 — iOS App + ECS Relay + 安全修复

### 新增
- **iOS App**: 完整 SwiftUI 聊天客户端（Gemini 实现），支持 HTTPS + Basic Auth 远程连接
- **iOS SDK**: `clients/apple-core/` — Swift Package，封装 beishan-core API
- **ECS Relay**: 阿里云 SSH 反向隧道（18013→8013）+ Nginx HTTPS + Let's Encrypt + Basic Auth
- **远程访问**: `https://your-relay.example.com` 全球可连

### 修复
- 阿里云 ECS `AllowTcpForwarding` SSH 配置
- 清理 git 跟踪的运行时文件（eval/run/）
- gitignore 补全 runtime 目录

### iOS App 功能
- 消息对话（POST /api/chat）
- 自动健康检测
- 连接设置（URL + Basic Auth）
- 会话管理 + 仪表盘监控
## 2026-05-25 Hermes 右花 + OpenClaw 右花 + 多 Provider + 9x 代码审查

### 新增

- **右花 #2: Hermes Agent** — `cmd/rightflower-python-wrapper/hermes_agent_adapter.py`（6 方法: memory.search/store, tools.list, tool.execute, agent.chat, conversations.list）
- **右花 #3: OpenClaw** — `cmd/rightflower-python-wrapper/openclaw_adapter.py`（4 方法: agent.chat, tool.execute, skills.list, gateway.status）
- **Router prompt 优化** — 展示插件能力类型，右花可被 LLM 正确路由
- **声明式多 Provider** — `LLM_PROVIDERS_CONFIG` 环境变量 + JSON 配置文件 + 硬化校验（endpoint HTTPS/localhost、type 白名单、name 冲突检测）
- **9 路并行代码审查** — `workflows/code_review_9x.yaml`（Qwen Code 设计），37 秒完成 9 路 LLM 调用
- **通道层余量接口** — `internal/channels/`（Channel 接口 + Manager 注册表）
- **记忆存储余量接口** — `internal/memory/`（MemoryStore 接口 + FileStore 实现 + TTL 支持）
- **Query DSL 余量接口** — `internal/retrieval/query.go`（结构化检索，当前仅关键词，解析器未激活）
- **manifest 冲突检测** — `RegisterAll` 注册前检查同名 + 与已有插件重名

### 修复

- 9x 审查工作流 YAML default 分支语法（bool 不能直接跟字符串）
- next 条件语义修正（显式处理 'found' 分支，default 降级为 report）

### 测试

- **YAML 工作流测试框架** — `internal/workflow/yaml_test.go`（20 个测试: 解析/模板/条件/真实工作流）
- Router usage 埋点 — `callDeepSeek` 加 `RecordUsage("router", ...)`

### 文档

- CLAUDE.md: 右花 2→3，新增 OpenClaw 节，环境变量补 LLM_PROVIDERS_CONFIG
- DESIGN_PRINCIPLES.md: 参考项目新增 Hermes + OpenClaw
- MERGE_DECISIONS.md: 新增 #12 Hermes 吸收评估、#13 preRoute 关闭
- HANDOVER_NEXT_SESSION.md: P0 验证结论 + OpenClaw 右花状态
- worklfows/code_review_9x.yaml: 完整实现 + YAML 解析测试

### 已关闭

- ~~preRoute 长度检测~~ — 收益太小 + 绕过硬化层，改为 Router usage 埋点

## 2026-05-24 右花全链路通车 + 双花吸收三连 + Agent 委派

### 新增

- **L3: internal/discovery/** — 本地推理引擎扫描器（11 引擎 + 5 测试 + 策略状态机）
- **L3: internal/observatory/** — SetDefaultRecorder / RecordTrace 全局接口
- **L3: internal/tools/** — 工具数 96→99：code_ai_review + spawn_subagent + spawn_parallel
- **L3: internal/agent/** — 子智能体委派：AgentDefinition 注册表 + MAX_SPAWN_DEPTH + ModelSpec
- **L3: internal/llm/** — 线程安全 SetProvider/FailoverProvider 运行时切换
- **L3: kernel/local_route.go** — LocalRouteStrategy 本地模型路由 + System Prompt 硬化
- **L3: glue/** — RegisterRightFlower + RightFlowerStatus + 统一健康检查
- **L2: glue/healthCheckLoop** — 统一检测子进程 + 右花，上报 observatory Pulse
- **右花协议** — OpenHuman 全链路通车（adapter 9529 + Core 7788 + probe-methods）
- **右花吸收工作流** — `workflows/absorb_right_flower.yaml`（探→评→缺口分析→吸→测→补→记）
- **P0 吸收** — 向量语义检索（L1 并行 + 混合评分）
- **P1 吸收** — AI 代码审查（code_ai_review L3 工具）
- **P2 吸收** — Agent 委派（spawn_subagent + spawn_parallel）
- **P1 bench** — 检索质量评估套件（15 测试：L0/L1/混合/边界/一致性/排序）
- **launchd 部署** — beishan-core + openhuman-adapter 双服务注册

### 修复

- **评分归一化** — 关键词分数 (1-15) 与语义分数 (40-100) 同尺度排序
- **KnowledgeSearch 广度补全** — L2 语义回退 → L1 并行语义检索
- **tryEmbedding Auth** — 增加 Bearer token（EMBEDDING_API_KEY / LOCAL_API_KEY）
- **smoke 测试** — 无 API key 时走 offline 模式（exit 0 而非直接失败）
- **extractFieldFromValue** — 负索引防护
- **glue TCC** — macOS Desktop 路径权限容错

### 文档

- **DESIGN_PRINCIPLES.md** — 双花进化闭环 + 四项吸收条件 + 参考项目义务
- **docs/MERGE_DECISIONS.md** — 决策 #11 双花进化闭环
- **docs/KNOWN_LIMITATIONS.md** — #12 右花协议状态更新 + #14 L2 感知跨平台
- **workflows/absorb_right_flower.yaml** — 三次吸收经验 + 缺口分析 Step 2.5
- **docs/reports/openhuman_capability_map.md** — 全量实测 + 缺口分析

---

## 2026-05-24 债务清除 + 长期方案 P1-P2 + Core Gate 硬化

### 新增

- **L1: Protocol Contract Test**：TestManifestSchemaContract + TestManifestSchemaMissingFields
- **L2: OpenHuman 真实方法映射**：472 方法发现，auth 验证通过（core.ping ✅）
- **L3: observatory.Trace 链路预留**
- **L7: core-eval 纳入 core_gate**：8 项门禁全部通过
- **L10: rightflowerctl generate 子命令**：从模板生成右花 manifest

### 修复

- **D01: think_plugin.go**：`os.ReadFile` → `ValidateAndExecute("read_file")`
- **D02: review_handler.go**：读写改用 L3 工具；MkdirAll/Remove 待 `delete_file`
- **D03: skill_factory.go**：标记为 PRIVILEGED PLUGIN
- **adapter 测试方法映射**：对齐 OpenHuman schema 真实方法名
- **Core Gate workspace check**：gitignored 二进制不阻塞
- **boundary_allowlist.yaml**：D01 → RESOLVED

### 硬化层不变性测试

8 项全部通过：build/vet/tools.Execute 绕过检查/isSafePath/code_security 规则/registry.Lock/validate_file_op/clarify structured

### Core Gate 最终状态

8/8 通过：test/vet/health/boundary/core-eval/docs/workspace/rightflower smoke

## 2026-05-23 Go-DSL 工作流引擎 + 架构文档 + 目录重组

### 新增

- **Go-DSL 工作流引擎**（`internal/workflow/gods_executor.go`）：编译时安全的 L3/L4 静态硬化链，与 YAML 引擎共存。`NewGoToolPlugin` 构造时校验 Tool 注册表，未注册 panic
- **三份核心架构文档**：`docs/HARDENING_LAYER.md`（硬化层能力边界表）、`docs/MERGE_DECISIONS.md`（7 项关键决策记录）、`docs/KNOWN_LIMITATIONS.md`（10 项已知设计边界）
- **DIRECTORY.md**：目录结构声明，解释每层到每目录的映射
- **Go-DSL 版 legal_review**（`cmd/beishan/legal_review_go_dsl.go`）：声明式四步法律审查编排

### 重构

- **main.go 迁入 `cmd/beishan/`**：Go 标准项目布局，preroute.go + web 资源同步迁移
- **.gitignore 补全**：排除 `eval/run/logs/`、`eval/run/pids/`、`generated/`
- **DEVLOG 移入 `docs/devlog/`**：CHANGELOG 留在根目录作为"对外名片"

### 硬化层能力边界文档化

硬化层保证格式安全、命令安全、路径安全，但不保证逻辑正确性、安全漏洞、架构质量。详见 `docs/HARDENING_LAYER.md`。

### 代码审计

文档-代码一致性核查：修正 6 处不准确声明（Router 校验机制、自修改防护范围、工作流数量等）。
根目录存量文档（PERSONAL_KB_GUIDE.md、PLAN_CODING_AGENT.md）经核实信息准确，无需修改。

## 2026-05-20 记忆层：Agent 自动知识检索 + 双轨语义引擎

### 背景与问题

项目底座已经非常扎实——知识全链路、15 个工作流、Web 界面、多模型适配。但存在一个根本问题：**知识库中的知识是孤立的**。用户通过工作流和手动录入积累了 20+ 条验证过的知识条目（如"270m 不可用"、"M2 8GB 跑不动本地模型"等），但智能体（think_plugin）回答问题时完全不知道这些知识的存在，每次都在信息真空中对话。

### 方案探索过程

**尝试 1：另台电脑的 "硬化检索 + 自动注入"**

```
每次聊天 → 自动 KnowledgeRetrieve → 注入 system prompt
```

问题：L4（think_plugin）直接 import L3（internal/tools），突破了架构边界。且将判断权交给了 LLM（知识注入后 LLM "自行决定"是否使用），违反了硬化层原则。**方案废弃。**

**尝试 2：全局 Context Enrichment Layer**

```
用户消息 → L1 Router → ★ Context Enrichment（自动检索知识）→ L4 Plugin
```

推演后发现：并不是所有消息都需要知识。workflow_plugin、terminal_plugin、todo_plugin 等完全不需要，全局层反而增加延迟、可能干扰正常执行。**否定。**

**尝试 3：图结构神经网络**

```
L3 建知识图谱 → 带类型的关系边 → L4 消费
```

推演出核心问题：L4 不会主动消费这个神经网络。L3 建了一张死图，没有智能度。**否定。**

**尝试 4：插件级自动注入（最终方案）**

只在 think_plugin 的 OnMessage 中，在调 LLM 之前固定执行一次知识检索，结果注入 标签。其他插件零影响。

```
think_plugin.OnMessage:
  用户消息
    → 1. SearchMemory(query)     ← 固定步骤，不是 LLM 决策
    → 2. FormatForPrompt(top-3)  ← 确定性格式化
    → 3. callLLM(msg + background) ← LLM 只做生成
```

这个方案符合所有架构原则：硬化层原则（检索是确定性代码）、工具 > Agent（检索写死在代码里）、内核冻结（不动 L1）、提示词只描述格式（不注入"你可以查知识库"指令）。

### 关键决策

**决策 1：记忆检索用向量还是关键词？**

- 尝试了纯关键词加权评分（SearchWithScore：title +3, tags +2, summary +1）
- 发现中文分词瓶颈：用户说"关于本地模型"→ 切词后得到"关于本地模型"→ 无法匹配"本地模型方案已放弃"
- 改为双向子串匹配 + 字符窗口重叠解决中文切词问题
- 最终方案：双轨（embedding 向量 + LLM 关键词扩展兜底），embedding 通过环境变量配置，不写死任何工具

**决策 2：自建 vs 接外部记忆系统（Mem0 等）**

自建，理由：
- 完全控制，符合硬化层哲学
- 无外部依赖（48K stars 的 Mem0 内部用 LLM 驱动实体提取，和硬化层哲学冲突）
- 不引入黑盒

**决策 3：写入时机**

三种写入都支持：
- V1 主动记忆（knowledge_remember 工具，Agent 自主调用）
- V2 后台审查（scheduler 触发工作流批量分析对话记录）
- 实时写入不做（噪音太多）

**决策 4：embedding 不绑定任何工具**

通过环境变量 `EMBEDDING_ENDPOINT` + `EMBEDDING_MODEL` 配置，可以是 Ollama / llama.cpp / 自建 glue 子进程。参考 internal/llm 的 provider 模式，无厂商依赖。

### 实现细节

#### 双轨检索（`internal/tools/knowledge.go`）

```
SearchMemory(query)
  ├─ EMBEDDING_ENDPOINT 已配置 → tryEmbedding → searchByEmbedding
  │    向量语义检索，余弦相似度 > 0.4 返回 top-3
  │    无 embedding 的条目异步后台补全（batchFillEmbedding）
  │    不阻塞当前请求
  │
  └─ 未配置 → expandKeywordsViaAPI → SearchWithScore
       LLM 保意图扩展（prompt 锁定"专有名词保持原样"+"示例锚定"）
       扩展 3-5 个搜索词 → 加权评分 → top-3
```

#### 保意图扩展（`expandKeywordsViaAPI`）

```
问题："beishan-core 怎么启动" → beishan-core,启动,boot,main.go
问题："本地模型最终决定"      → 本地模型,local model,推理
```

使用 `llm.ChatCompletion`（新增的共享 LLM 调用函数），不经过 think_plugin 自身路径，避免递归。

#### 格式化注入（`FormatForPrompt`）

```
<background>
1. 本地模型方案已放弃（claude_memory, project）
   2026-05-07 决定放弃本地模型，M2 8GB 不够，走纯 API 路线
2. beishan-core 路由机制（architecture, routing）
   DeepSeek 路由，confidence < 0.4 拒绝，Recipient 非空时直接转发
</background>
```

最多 3 条，每条两行，summary 截断到 100 字。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/llm/config.go` | +ChatCompletion 共享 LLM 调用函数 |
| `internal/tools/knowledge.go` | +Embedding 字段、tryEmbedding、searchByEmbedding、expandKeywordsViaAPI、SearchMemory、KnowledgeReindex、saveKnowledge 自动计算向量 |
| `plugins/think_plugin.go` | OnMessage: SearchWithScore → SearchMemory；callDeepSeek 改用 llm.ChatCompletion 避免递归 |
| `plugins/memory_plugin.go` | +knowledge_remember、knowledge_reindex 路由 |
| `main.go` | +knowledge_reindex Meta.Types |
| `workflows/knowledge_enrich.yaml` | 修复 data 参数（远程合并的 bug：knowledge_update 无 data 字段，改为逐字段传） |

### 架构原则对账

| 原则 | 是否违反 |
|---|---|
| 硬化层原则 | ✅ 检索是确定性 L3 工具调用，结果可硬化校验 |
| 工具 > Agent | ✅ 检索是写死在代码里的，不是 LLM 决定的 |
| 提示词只描述格式 | ✅ system prompt 只多了知识内容，没有"你可以查知识库"指令 |
| 内核冻结 | ✅ 不动 L1，不动 kernel 代码 |
| 厂商无关 | ✅ embedding 通过环境变量配置，不绑定具体工具 |
| 避免递归 | ✅ expandKeywords 用 llm.ChatCompletion，不走 think_plugin 路径 |

### 实测验证

- "本地模型最终决定" → ✅ 命中"本地模型方案已放弃"等条目
- "beishan-core 路由机制" → ✅ 命中架构决策条目，正确描述强制 DeepSeek 路由
- knowledge_remember / knowledge_reindex → ✅ 写入/补全正常
- 全量编译通过

### 配置方式

```bash
# 可选，不配置则走 DeepSeek 扩展降级
export EMBEDDING_ENDPOINT=http://localhost:11434/v1/embeddings
export EMBEDDING_MODEL=nomic-embed-text

# 已有条目补全向量
curl -X POST ... -d '{"recipient":"memory_plugin","type":"knowledge_reindex"}'
```

### 补充：记忆优先排序 + 过期机制 + Web 记忆入口

基于第一版反馈，补充三个完善项：

**记忆优先排序**（一行改动）

检索结果按分数降序排列时，同分数下 `source_type=memory` 的条目优先排在前面。修改两处 sort 逻辑（SearchWithScore + searchByEmbedding），确保记忆比知识更易被命中。

**过期机制**

两处设计取舍：
- 不用自动时间过期（删错了找不回来，"今天"这种词不好判断，30天太武断）
- 用显式标记 `Ephemeral` + `ExpiresAt`：`knowledge_remember` 支持 `expires_in_days` 参数，>0 时标记为临时记忆并在到期后不参与检索
- 已过期条目**只屏蔽不出现在检索结果中**，不自动删除，可通过 `knowledge_list` 或手动清理

```go
type KnowledgeEntry struct {
    Ephemeral  bool      // 临时记忆，到期不参与检索
    ExpiresAt  int64     // 过期时间戳，0=永久
}
```

**Web 记忆入口**

侧栏新增"记忆"区 + "浏览记忆"按钮，调用 `knowledge_list?source_type=memory` 列出所有记忆条目。后端零改动。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | +Ephemeral、ExpiresAt 字段；KnowledgeRemember 支持 expires_in_days；两处排序加 memory 优先；两处检索过滤过期条目 |
| `web/index.html` | 侧栏新增"记忆"区域 + sendMemoryList 函数 |

### 双链知识图谱

知识库从扁平列表进化为关联网络。在此之前，知识之间没有连接，每次检索只能靠关键词碰运气。现在是：

```
检索"记忆层"
  → 命中 kn_004（记忆层双轨检索）
  → 沿 Links 找到 kn_001（路由机制也与记忆层相关）
  → 沿 Links 找到 kn_003（workflow 引擎也是记忆消费者）
  → 三条一起注入 think_plugin
```

**图遍历检索（SearchMemory 增强）**

直接命中后，沿 KnowledgeEntry.Links 字段做一跳扩展，关联条目按直接命中分数降权 50% 后加入结果集，经重排取 top-3 注入。不改变调用接口，think_plugin 零改动。

| 阶段 | 方式 | 数据 |
|---|---|---|
| 第一层 | 双轨语义检索 | 直接命中条目 |
| 第二层 | Links 图扩展 | 关联条目（降权 50%） |
| 输出 | 排序重排 | 混合 top-3 |

**自动建链（autoLinkEntry）**

KnowledgeAdd 入库后异步触发，不阻塞写入路径：

| 条件 | 分值 |
|---|---|
| 共享标签 | +2/个 |
| 共享主题 | +2/个 |
| 标题/摘要关键词重叠 | +1 |

阈值 ≥ 3 分自动双向写入 Links 字段。source_type=memory 跳过（记忆不自动建链，避免噪音）。

**改动量极小**——仅 internal/tools/knowledge.go 一个文件，+151/-10 行。

| 新增 | 行数 |
|---|---|
| loadLinkedEntries | 12 |
| autoLinkEntry | 55 |
| SearchMemory 图扩展阶段 | 40 |
| containsStr / min 辅助 | 10 |
| 已知知识间开始互相连接 | ∞ |

### Bug 修复：embedding 环境变量初始化时序

**根因**：Go 包级变量初始化发生在所有 `init()` 函数之前。`var embeddingEndpoint = os.Getenv("EMBEDDING_ENDPOINT")` 在 `tools` 包初始化时执行，此时 `main.init()` 尚未运行，`.env` 尚未加载。

**影响**：所有通过 `.env` 配置 embedding 的用户静默降级：
- `SearchMemory` 跳过向量路径，走关键词降级
- `saveKnowledge` 写入时不计算向量
- `batchFillEmbedding` / `KnowledgeReindex` 永远返回"未设置"
- 不报错、不 panic，用户不知道 embedding 从未生效

**修复**：包级变量 → 函数调用，os.Getenv 在运行时执行。

**涉及文件**：`internal/tools/knowledge.go`（+12/-5 行）

## 2026-05-21 远程合并审计 + findSemanticLinks 硬化 + system_info 环境感知

### 远程 8 commit 架构审计

另台电脑推送 8 个 commit（+4127 行），经逐条审计：

| 结论 | 内容 |
|---|---|
|  合规 | 三柱记忆架构（Episodic/Semantic/External），classifyIntent 关键词分流，纯代码 |
|  合规 | retrieval_pipe.go 检索管道，确定性编排，零 LLM |
|  合规 | code_search.go ripgrep 代码检索 |
|  合规 | review_handler.go 审查流程 + callStructuredLLM schema 校验 |
|  合规 | 内核冻结（kernel/ 未改动）、强制 AI 路由未变 |
|  修复 | findSemanticLinks 用 LLM 判断关系类型 → 改为代码判断 |

### findSemanticLinks 修复（硬化层原则）

**问题**：autoLinkEntry 中第二层建链调 llm.ChatCompletion 判断 contradicts/supersedes/supports。每次入库消耗约 500 tokens，结果不可复现。

**修复**：改为代码判断：

| 关系 | 判断逻辑 |
|---|---|
| contradicts | 同标签 + 一个有否定词一个没有 |
| supersedes | 同标签 + 否定旧条目 + 时间更新 |
| supports | 同标签 + 双方肯定词 |
| related | 默认（已由 autoLinkEntry 覆盖） |

**效果**：零 LLM 调用，结果确定可复现，每次入库节约约 500 tokens。

### system_info L3 工具

新工具 system_info（internal/tools/system_info.go）：

- 采集 CPU 型号/核心数、内存大小、OS/架构、Metal 支持
- saveKnowledge 写入时自动附加硬件快照到 summary
- 换电脑后旧知识自动标注原环境，支持硬件比对
- 确定性代码，不调 LLM

### 涉及文件

| 文件 | 变更 |
|---|---|
| internal/tools/knowledge.go | findSemanticLinks LLM→代码；saveKnowledge 自动附加硬件快照 |
| internal/tools/system_info.go | 新建：L3 系统信息工具 |
| internal/tools/tools.go | Init() 追加 registerSystemInfoTools |
| plugins/memory_plugin.go | +system_info 路由 |
| main.go | +system_info Meta.Types |


### 数据污染治理 + schema 批量修复

知识库与智能体记忆体合一的架构下，数据质量是重中之重。当天发现并修复三个污染源：

**硬件前缀重复追加**

`saveKnowledge` 每次写入时检查 `strings.Contains(summary, "硬件：")` 判断是否已添加硬件快照。但实际前缀格式为 `【darwin/arm64...】`，检测条件不匹配，导致每次写入都重复追加（最多 6 次）。

修复：改为 `strings.HasPrefix(summary, "【d")`，匹配实际前缀格式。

**TypedLinks contradicts 噪音**

`findSemanticLinks` 的逻辑：只要一条有否定词而另一条没有，就标 `contradicts`。但跨主题条目（如"Lavern 三层验证架构"和"本地模型方案已放弃"）被错误关联为矛盾关系。

修复：加 `hasSharedTagOrTopic(entry, candidate)` 过滤，只有共享标签或主题的条目才可能标矛盾。

**knowledge_* schema 全部缺 additionalProperties**

HTTP handler 注入 `session_id` 到 payload，9 个 knowledge 工具缺少 `additionalProperties: true`，导致硬化层返回"未知字段: session_id"。所有通过 API 调用的知识工具（list/get/delete/update 等）全部静默失效。

修复：批量补全 9 个 schema。

## 2026-05-19 远程合并：payload 修复 + knowledge_enrich 修复

### 多模型 API 适配（LLM_PROVIDER）

`internal/llm/config.go` 新增 Provider 系统：
- 预置 `deepseek` / `xiaomi` / `openai` 三个 provider
- 每个 provider 有独立 BaseURL、Model、RouterPrompt
- `LLM_PROVIDER=xiaomi` 一键切换小米 MiMo v2.5-pro
- Router 超时 10s→120s（适配小米慢响应）
- 错误消息从 "DeepSeek" 改为 "LLM"（厂商无关）

### 引擎：条件跳过（skip_if）

StepDef 新增 `SkipIf` 字段，引擎执行前调用 `evaluateCondition` 检查。
条件成立时产出 `"skipped: ..."` 记录，直接走 `next`。

### 引擎：嵌套工作流返回值（FinalOutput）

`WorkflowResult` 新增 `FinalOutput` 字段，存最后一步输出。
下游用 `${steps.sub_workflow.output.FinalOutput}` 获取最终结果。

### 引擎：数组索引支持

`extractJSONFieldValue` 支持 `field[0]` 语法，workflow 模板可引用数组元素。

### 引擎：buildPayload 支持 map[string]interface{} Inputs

适配 YAML 中 `max_results: 5` 等非字符串字段，`singleTemplateRef` 保持类型完整性。

### 引擎：markdown JSON 剥离

`resolveJSONValue` 自动剥离 ```json 包裹，think_plugin 偶尔返回 markdown 代码块不再影响字段提取。

### 新工作流（15 个）

| 工作流 | 文件 | 说明 |
|---|---|---|
| opensource_project_ingest | 新建 | 开源项目分层采样入库（parallel 并发 3 步） |
| code_knowledge_ingest | 新建 | 本地代码文件入库 |
| legacy_code_audit | 新建 | 旧项目结构审计 |
| legacy_module_ingest | 新建 | 逐模块质量评分 + 过滤入库 |
| legacy_doc_generate | 新建 | 反向生成文档 |
| vehicle_entry / exit | parking/ | 车辆入库/出库（备注字段） |
| parking_stats / report | parking/ | 停车统计 + CSV 报告 + 邮件 |
| writing_assistant | 更新 | 6 步完整版：检索→大纲→初稿→批评→修订→入库 |
| weekly_review | 更新 | 5 步完整版：列表→分析→入库→通知 |

### 实测修复（Apex-OS 项目真实跑通）

- file_parse 扩展 17 种代码文件类型（.py/.go/.js 等）
- knowledge_add content 字段兼容 string 和 array（oneOf schema）
- find 命令括号修复（`\( -name ... -o ... \)`）
- 4 个模块入库，2 个 SKIPPED（quality_score < 3）
- 知识库 20 条

### 模板库重构

`workflows/templates/` 拆为 `patterns/`（4 个设计模式）和 `domains/`（3 个领域模板）。
`buildWorkflowSummary` 改为 `filepath.Walk` 递归扫描子目录。

### 插件描述优化

| 插件 | 之前 | 之后 |
|---|---|---|
| write_plugin | 文本生成与写作 | 长文本生成/格式化写作/文件处理，不适合输出JSON |
| think_plugin | 通用对话问答 | 推理/分析/判断/结构化JSON，不适合直接生成长文本 |

## 2026-05-19 workflow parallel 并行步骤（goroutine + channel 并发）

### 新增：并行步骤

StepDef 新增 `steps` 字段（`ParallelSteps []StepDef`），支持工作流中定义并行子步骤。

**引擎实现：**
- 检测 `len(step.ParallelSteps) > 0` 时走并行路径
- 每个子步骤在独立 goroutine 中通过 `e.Kernel.Call` 执行
- 通过 channel 收集所有子步骤结果
- 等待全部完成后继续到 `next` 步骤
- 子步骤结果可通过 `${steps.<parent_id>.output.<sub_id>}` 引用

**YAML 用法：**
```yaml
- id: batch_search
  type: parallel
  timeout: 60
  steps:
    - id: search_go
      plugin: search_plugin
      type: web_search
      inputs:
        query: "Go framework"
    - id: search_python
      plugin: search_plugin
      type: web_search
      inputs:
        query: "Python library"
  next: summarize
```

**`_template.yaml`**：补充 `steps` 字段说明 + 并行步骤示例

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/workflow/types.go` | StepDef 新增 ParallelSteps 字段 |
| `internal/workflow/engine.go` | Run 检测并行步骤 + runParallel 方法 |
| `workflows/_template.yaml` | 文档+示例 |

## 2026-05-19 monthly_review 月报工作流 + skill_evaluate 评估工具

### monthly_review 月报工作流

`workflows/monthly_review.yaml` — 基于 weekly_review 扩展：
- 30 天/90 天灵活范围
- 分析：月度概况 → 项目进展 → 趋势分析 → 差距与机会 → 下步建议
- 邮件推送可选（NOTIFY_TARGET）

### skill_evaluate 评估工具

`internal/tools/skill_eval.go` — 工作流质量自动评估：

| 检查项 | 扣分 | 说明 |
|---|---|---|
| 步骤数 0 | -40 | 无步骤定义 |
| 步骤数 > 20 | -10 | 超过建议上限 |
| 重复/空 ID | -20 | 标识冲突 |
| 缺少 plugin/type | -15/次 | 必要字段缺失 |
| 引用不存在步骤 | -20/次 | next/goto/on_error 无效 |
| 不可达步骤 | -10 | 从首步无法到达 |
| 循环依赖 | -30 | 工作流构成环路 |

通过 skill_factory_plugin 路由，支持按名称或直接传 YAML 内容评估。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `workflows/monthly_review.yaml` | 新建 |
| `internal/tools/skill_eval.go` | 新建 |
| `internal/tools/tools.go` | Init() 追加 registerSkillEvalTools |
| `plugins/skill_factory_plugin.go` | 新增 skill_evaluate 路由 + tools 导入 |
| `main.go` | skill_factory Meta.Types 新增 skill_evaluate |

## 2026-05-19 主题图谱 + 时间线 + 6 个 Codex 历史会话入库

### Codex 历史会话导入

从 Codex 历史记录中筛选并导入 6 个与 beishan-core 项目直接相关的会话：

| 会话 | 内容 |
|---|---|
| Claude CLI → DeepSeek | 早期 API 打通与兼容方案 |
| Hermes Go + DeepSeek 集成 | Agent 移植与事件流对齐 |
| Claude Code CLI 兼容 | 工具链打通与问题解决 |
| hermes HTTP 迁移到 Go | Python→Go 架构决策 |
| Hermes Agent 部署 | 初始部署与配置 |
| Agent 意图识别与本地模型 | 本地小模型实战方案 |

知识库从 10 条增长至 **16 条**，覆盖：用户画像、项目背景、模型策略、hermes 起源、DeepSeek 配置、方向决策。

### 新增 L3 工具

**`knowledge_topic_map`** — 自动生成知识条目主题图谱。
- 按 tag 聚类条目
- 共享 ≥2 条目的 tag 自动建立关联子主题
- Top 15 主题降序排列

**`knowledge_timeline`** — 按时间回看项目演进。
- 支持 day/week/month 分组
- 按时间倒序输出
- 每条显示条目 ID 和标题

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeTopicMap、KnowledgeTimeline、2 个工具注册 |
| `plugins/memory_plugin.go` | 新增路由 |
| `main.go` | Meta.Types 新增 |

## 2026-05-19 向量检索引擎：本地词袋向量 + 余弦相似度语义搜索

### 新增

**`internal/tools/embed.go`** — 零外部依赖的本地向量检索引擎

| 工具 | 功能 |
|---|---|
| `knowledge_embed` | 为单条知识生成词袋向量 |
| `knowledge_embed_all` | 批量重嵌全部条目 |
| `knowledge_semantic_search` | 语义搜索（Bow 向量 + 余弦相似度） |

**技术方案：**
- 中文逐字 + ASCII 单词混合 tokenization
- FNV-1a 哈希映射到 512 维向量空间
- 短特征（≤4 字符）自动补充 n-gram
- L2 归一化 + 余弦相似度排序
- 阈值 0.25，默认返回 top 10
- 零外部依赖：无需 API Key、无需外部模型

**与关键词搜索的对比：**

| 特性 | `knowledge_search` | `knowledge_semantic_search` |
|---|---|---|
| 匹配方式 | 关键词精确匹配 | 语义相似度 |
| 外部依赖 | 无 | 无 |
| 场景 | 精确检索 | 模糊/概念搜索 |
| 查询示例 | "开源" | "开源笔记系统" |

**实测结果（10 条知识库）：**

```
查询: "开源笔记系统"
  1. 开源个人知识库项目选型建议 (0.347)

查询: "开发习惯和用户偏好"
  1. 用户画像与偏好 (0.423)

查询: "本地部署大语言模型"
  1. 本地模型的定位与双适配策略 (0.470)
  2. 本地模型方案已放弃 (0.358)
```

### 修复

- `.embed.json` 文件被 `knowledge_list` 误读为知识条目的 bug（添加 `.embed.json` 排除过滤）
- `textToVector` 函数结构修复（多轮编辑导致的语法错误）

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/embed.go` | 新建 — 向量检索引擎 |
| `internal/tools/tools.go` | Init() 追加 registerEmbedTools |
| `plugins/memory_plugin.go` | 新增 embed/semantic_search 路由 |
| `internal/tools/knowledge.go` | `.embed.json` 排除过滤 |
| `main.go` | Meta.Types 新增 |

## 2026-05-19 smoke 测试补全 + 短期 15 条完结

### smoke 测试

`eval/scenarios/core_smoke.yaml` 新增 4 个测试用例：
- `codex_01`：codex_session_list 基本功能
- `codex_02`：codex_session_extract 不存在的 ID（错误处理）
- `claude_01`：claude_memory_list 基本功能
- `claude_02`：claude_memory_import 不存在的名称（错误处理）

### 短期 15 条完结

```
✅ 1.  claude_memory_import suggest_links 后处理
✅ 2.  codex_session_list limit 参数
✅ 3.  codex_session_list since/until 日期过滤
⬜ 4.  codex_session_extract max_chars 参数（常量已有，待暴露）
✅ 5-6. knowledge_dedupe + knowledge_merge
✅ 7.  knowledge_review 批量模式（file_ingest 作为批量入口）
✅ 8.  weekly_review 日期过滤
⬜ 9.  writing_assistant 关键词提炼
✅ 10. file_ingest 工作流
✅ 11-12. Web 最近导入 + 复制 ID
✅ 13. knowledge_confirm_links
✅ 14. smoke 测试补全
✅ 15. PERSONAL_KB_GUIDE.md
```

剩余 2 条（#4 #9）优先级较低，留到下一阶段。

## 2026-05-19 file_ingest 工作流 + knowledge_confirm_links + 日期过滤

### 新增

**file_ingest 工作流**（`workflows/file_ingest.yaml`）
4 步骤：file_parse → think_plugin 结构化 → knowledge_add 入库 → suggest_links 关联
一条命令完成文件→知识的全链路：`"工作流":"file_ingest","input":"/path/to/doc.md"`

**knowledge_confirm_links 工具**
确认关联建议的一键写入工具：将目标 ID 列表写入源条目的 `links` 字段（去重）。
配合 `knowledge_suggest_links` 使用：先看候选 → 确认后写入。

**日期过滤增强**
- `knowledge_list` 新增 `days` 参数：限定最近 N 天（0=全部）
- `codex_session_list` 新增 `since`/`until` 参数：ISO 日期范围过滤
- `weekly_review` 工作流支持 `input: "7"` 限定最近 7 天

### 剩余清单

```
⬜ 3. codex 日期过滤          → 本轮已完成
⬜ 4. codex max_chars 参数    → 常量已有，待暴露为参数
⬜ 7. knowledge_review 批量   → 待做
⬜ 8. weekly_review 日期过滤  → 本轮已完成
⬜ 9. writing_assistant 关键词 → 待做
⬜ 10. file_ingest 工作流     → 本轮已完成
⬜ 13. suggest_links 确认写入 → 本轮已完成
⬜ 14-15. smoke测试/GUIDE     → GUIDE 已完成，smoke 待补
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | KnowledgeConfirmLinks + knowledge_list days 参数 |
| `internal/tools/codex.go` | codex_session_list since/until 参数 |
| `workflows/file_ingest.yaml` | 新建 |
| `workflows/weekly_review.yaml` | list_knowledge 传 days 参数 |
| `plugins/memory_plugin.go` | 新增 knowledge_confirm_links 路由 |
| `main.go` | Meta.Types 新增 |
| `PERSONAL_KB_GUIDE.md` | 更新 |

## 2026-05-19 codex limit + claude_memory 工作流 + PERSONAL_KB_GUIDE

### 完善

- `codex_session_list` 新增 `limit` 参数（默认 50），控制最大返回数
- 创建 `workflows/claude_memory_ingest.yaml`：Claude 记忆文件 → 知识条目的专用工作流
- `knowledge_dedupe` 和 `knowledge_merge` 实机验证通过 ✅
- 合并测试：源标签 `测试` 正确合并入目标条目，源条目自动删除

### Personal KB Guide

创建 `PERSONAL_KB_GUIDE.md`，系统梳理：
- 知识库是什么（定位）
- 数据来源（手动/Codex/Claude/文件/Web）
- 质量保障链（审查 → 去重 → 合并 → 关联）
- 使用方式（API 调用 / Web 界面）
- 短期 15 条剩余清单
- 架构原理

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/codex.go` | codex_session_list 新增 limit 参数 |
| `workflows/claude_memory_ingest.yaml` | 新建 — Claude 记忆导入工作流 |
| `PERSONAL_KB_GUIDE.md` | 新建 — 知识库使用指南 |
| `CHANGELOG.md` | 本日日志 |

## 2026-05-19 知识去重/合并工具 + Web 最近知识列表

### 新增 L3 工具

**`knowledge_dedupe`** — 查找可能重复的知识条目。
- 按 `raw_ref` 精确匹配（同一来源的导入记录）
- 按条目 `id` 语义匹配（标题相似度、标签重叠、摘要重叠）
- 评分机制：raw_ref 相同 = 80 分，标题相同 = 50 分，标签重叠 += 10/个
- 返回评分降序的候选重复列表

**`knowledge_merge`** — 合并两个知识条目。
- 合并字段：tags/topics/tasks/links 取并集
- content 拼接（去重：源内容不重复追加）
- summary 取更长者
- 合并后自动删除源条目

### Web 界面

- 侧栏新增「最近知识」区域，页面加载时自动获取最新 5 条
- 每条可点击复制 ID（`navigator.clipboard.writeText` + 按钮反馈）
- 每 30 秒自动刷新

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeDedupe、KnowledgeMerge、unionStrings、findEntry；注册 2 个新工具 |
| `plugins/memory_plugin.go` | 新增 knowledge_dedupe、knowledge_merge 路由 |
| `main.go` | Meta.Types 新增 |
| `web/index.html` | 侧栏「最近知识」区域 + loadRecentKnowledge + copyKnowledgeId |

## 2026-05-19 Claude 记忆导入 + Codex 全链路验证通过

### Claude 记忆导入

**新增 L3 工具：**
- `claude_memory_list` — 读取 MEMORY.md 索引 + 扫描目录，列出所有记忆文件
- `claude_memory_import` — 解析 YAML frontmatter（name/description/type）+ Markdown body，转为 knowledge 条目

8 个 Claude 记忆文件全部成功导入知识库，覆盖：用户画像、项目背景、架构决策、本地模型策略。

### Codex 全链路验证

完整测试 `codex_conversation_ingest` 工作流：

| 步骤 | 状态 | 结果 |
|---|---|---|
| extract | ✅ | 7 条消息 / 56 条消息，均正确提取 |
| analyze | ✅ | DeepSeek 输出结构化 JSON |
| save_knowledge | ✅ | 2 条知识条目入库 |
| suggest_links | ✅ | 自动关联已有条目 |

### 修复

`internal/workflow/engine.go` — `ctx["input"]` 正确解包 JSON 字符串引号，修复 workflow 输入参数传递问题。此前 `${input}` 插值时会带上 JSON 字符串的额外引号，导致 `codex_session_extract` 的 ID 参数匹配失败。

### 当前知识库状态

```
10 条知识条目，2 个来源：
  claude_memory × 8:  用户画像、项目背景、架构决策、本地模型
  codex × 2:           开源知识库调研、beishan-core 方向决策
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/claude.go` | 新建 — claude_memory_list + import 工具 |
| `internal/tools/tools.go` | Init() 追加 registerClaudeTools |
| `plugins/claude_plugin.go` | 新建 — Claude 记忆导入插件 |
| `main.go` | 注册 claude_plugin |
| `internal/workflow/engine.go` | 修复 input 引号解包 |
| `web/index.html` | 侧栏 Claude 导入入口 |

## 2026-05-19 Codex 对话导入：codex_session_list + extract + 入库工作流

### 新增 L3 工具

**`codex_session_list`** — 读取 `~/.codex/session_index.jsonl`，列出所有 Codex 对话。
- 支持 `keyword` 关键词过滤
- 返回结构化列表：`{id, thread_name, updated_at}`
- 索引文件不存在时自动降级为扫描 `sessions/` 目录

**`codex_session_extract`** — 提取指定 Codex 对话的完整文本。
- 按 ID 搜索 `sessions/` 和 `archived_sessions/` 目录（文件名匹配）
- 解析 JSONL，提取 `event_msg` 类型的 `user_message` 和 `agent_message`
- 过滤 tool call、token_count、session_meta 等噪音
- 近环内容去重
- 返回结构化 JSON：`{id, title, messages[], count}`

### codex_conversation_ingest 工作流

4 步骤：
1. `extract`（codex_session_extract）— 读取 Codex 会话
2. `analyze`（think_plugin + retry:1）— DeepSeek 提炼决策/模式/任务
3. `save_knowledge`（knowledge_add）— 结构化入库
4. `suggest_links`（knowledge_suggest_links）— 关联已有知识

Web 界面侧栏新增「导入」区 + 聊天前缀 "Codex 列表" / "导入 Codex xxx"。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/codex.go` | 新建 — codex_session_list + codex_session_extract 工具 |
| `internal/tools/tools.go` | Init() 追加 registerCodexTools |
| `plugins/codex_plugin.go` | 新建 — Codex 会话插件 |
| `main.go` | 注册 codex_plugin |
| `workflows/codex_conversation_ingest.yaml` | 新建 — 4 步骤入库工作流 |
| `web/index.html` | 侧栏导入入口 + 聊天前缀检测 |
| `imports/codex_sessions/raw/` | 原始 session_index 备份 |

## 2026-05-19 notify_send 工具 + 描述优化 + 短期 10 条完结

### notify_send L3 通知工具

新增 `internal/tools/notify_tool.go` + `plugins/notify_plugin.go`：

| 参数 | 说明 |
|---|---|
| `channel` | email / slack / wechat |
| `target` | SMTP URL 或 webhook URL（可设 `NOTIFY_TARGET` 环境变量兜底） |
| `subject` | 邮件主题（非邮件渠道忽略） |
| `message` | 正文 |

**weekly_review 集成**：analyze 步骤后新增 `send_report` 步骤，`on_error: done` 确保通知未配置时工作流不中断。

**notify 包增强**：`notify.go` 新增 `SendViaChannel` 结构化调用入口，供 L3 工具直接使用。

### 插件描述优化

| 插件 | 原描述 | 新描述 |
|---|---|---|
| write_plugin | 文本生成与写作 | 文件系统操作：读/写/搜索/补丁/解析文档 |
| think_plugin | 通用对话与问答 | 调用 DeepSeek 进行对话/分析/写作/总结 |
| notify_plugin | — | 通知发送：邮件/Slack/企业微信 |

Tags 同步更新：write_plugin 从 `write,generate` → `file,filesystem`

### 短期 10 条完结

```
✅ 1. personal_knowledge_ingest
✅ 2. memory_plugin tag 支持
✅ 3. weekly_review
✅ 4. writing_assistant
✅ 5. 邮件 notify 接通           ← 本轮完成
✅ 6. PDF/文档解析
✅ 7. workflow 错误重试
✅ 8. workflow_smoke 补全
✅ 9. 描述优化                   ← 本轮完成
✅ 10. workflows/templates/ 模式库
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/notify_tool.go` | 新建 — notify_send 工具 |
| `plugins/notify_plugin.go` | 新建 — 通知插件 |
| `internal/notify/notify.go` | 新增 SendViaChannel 结构化接口 |
| `internal/tools/tools.go` | Init() 追加 registerNotifyTools |
| `main.go` | 注册 notify_plugin；更新 write_plugin/think_plugin 描述和 Tags |
| `workflows/weekly_review.yaml` | 新增 send_report 邮件推送步骤 |

## 2026-05-19 writing_assistant + workflows/templates/ 模式库

### writing_assistant 工作流

创建 `workflows/writing_assistant.yaml`，三步骤（search_act 模式）：
1. `research`（knowledge_search）— 搜索知识库获取相关材料
2. `draft`（think_plugin + retry:1）— 生成大纲 + 初稿正文
3. `refine`（think_plugin）— 自我批评分析 + 修订版全文

Web 界面侧栏新增「写作助手」快捷入口 + 聊天 "帮我写一篇文章：" 检测。

### workflows/templates/ 模式库

新建 `workflows/templates/` 目录，从 7 个工作流中提炼 5 种编排模式：

| 模式 | 文件 | 步骤数 | 参考实现 |
|---|---|---|---|
| ingest | `ingest.yaml` | 3 | personal_knowledge_ingest |
| review | `review.yaml` | 4 | knowledge_review |
| suggest | `suggest.yaml` | 2 | knowledge_suggest_links |
| aggregate | `aggregate.yaml` | 3+ | weekly_review, github_radar |
| search_act | `search_act.yaml` | 3 | writing_assistant |

每个模式包含：适用场景、步骤骨架、关键设计说明、变体选项。新工作流可直接复制骨架文件替换 TODO 标记。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `workflows/writing_assistant.yaml` | 新建 — 3 步骤写作助手 |
| `workflows/templates/` (6 文件) | 新建 — 模式库目录 + README + 5 骨架 |
| `web/index.html` | 侧栏写作助手入口 + 聊天前缀检测 + JS 函数 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 writing_assistant 测试 |

## 2026-05-19 底座三连：workflow 重试增强 + weekly_review + PDF 解析

### 1. Workflow 错误重试增强

**新增字段：**
- `retry_delay`（秒）：重试间隔，指数退避（第 N 次重试等待 `retry_delay * 2^(N-1)` 秒，默认 1s）
- `on_error`（字符串）：失败后继续到指定步骤，不终止工作流

**引擎变更：**
- 重试循环添加 `time.Sleep` 退避等待
- `on_error` 分支：失败后记录错误结果并跳转到指定步骤继续执行
- 提取 `buildResult()` 辅助函数消除重复

**文档更新：** `workflows/_template.yaml` 补充 `retry_delay`、`on_error` 字段说明

### 2. weekly_review 工作流

创建 `workflows/weekly_review.yaml`，三步骤：

1. `list_knowledge`（knowledge_list）— 获取所有知识条目（按时间倒序）
2. `list_todos`（todo_list）— 获取当前待办列表
3. `analyze`（think_plugin + retry:1）— DeepSeek 输出周报

输出格式：本周概况 → 待办状态 → 主题聚类 → 差距与机会 → 下步建议

注意：无日期过滤，DeepSeek 根据 `created_at` 时间戳自行识别近期内容。

### 3. PDF/文本解析工具

**新增 `internal/tools/file_parse.go`** — L3 文件解析工具

| 文件类型 | 实现 |
|---|---|
| `.txt` | 直接 `os.ReadFile` |
| `.md`、`.markdown` | 直接 `os.ReadFile` |
| `.pdf` | `github.com/ledongthuc/pdf` 纯 Go 库逐页提取，`GetTextByRow` 按行拼接 |

安全措施：路径遍历检查、50MB 上限、文件存在性校验

工具注册到 `write_plugin`，返回结构化 JSON（path/type/size/filename/content）。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/workflow/types.go` | StepDef 新增 RetryDelay、OnError 字段 |
| `internal/workflow/engine.go` | 指数退避、on_error 分支、buildResult 辅助函数 |
| `workflows/_template.yaml` | 补充 retry_delay、on_error 文档 |
| `workflows/weekly_review.yaml` | 新建 — 3 步骤周报工作流 |
| `internal/tools/file_parse.go` | 新建 — PDF/TXT/MD 文件解析工具 |
| `internal/tools/tools.go` | Init() 追加 registerFileParseTools |
| `plugins/write_plugin.go` | 新增 file_parse 路由 + 返回 Payload |
| `main.go` | write_plugin Meta.Types 新增 file_parse |
| `eval/scenarios/core_smoke.yaml` | 新增 file_parse 和 suggest_links 错误处理测试 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 weekly_review 工作流测试 |
| `go.mod` | 新增 `github.com/ledongthuc/pdf` 依赖 |

## 2026-05-19 知识关联建议工作流 + 知识网络建设

### 新增

**`knowledge_suggest_links` L3 工具 — 基于内容的关联建议引擎**

评分算法：
- 共享标签（最大 0.70）：每共享一个标签 +0.35
- 共享主题（最大 0.60）：每共享一个主题 +0.30
- 关键词匹配（固定 +0.20）：源条目的标签/主题/标题词出现在目标条目的标题或摘要中
- 总分 ≥ 0.20 才视为候选，按降序排列

输出结构化 JSON：`source_id`、`candidates[]`（含 score/shared_tags/shared_topics/keyword_match/reason）

**`workflows/knowledge_suggest_links.yaml` — 知识关联建议工作流**
2 步骤：
1. `suggest` — L3 引擎匹配候选，最多返回 15 条
2. `report` — DeepSeek 生成可读报告（强关联 / 弱关联 / 无结果）

**Web 界面**
- 侧栏新增「关联建议」快捷操作
- 聊天输入 "帮我关联 kn_xxx" 自动调关联建议工作流

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeSuggestLinks、loadAllKnowledge、intersectStrings、extractKnowledgeKeywords；工具注册 |
| `plugins/memory_plugin.go` | 新增 knowledge_suggest_links 路由 |
| `main.go` | Meta.Types 新增 knowledge_suggest_links |
| `workflows/knowledge_suggest_links.yaml` | 新建 — 2 步骤关联建议工作流 |
| `web/index.html` | 侧栏关联建议入口 + 聊天前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新增 knowledge_suggest_links 错误处理测试 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 suggest_links 工作流失败场景 |

## 2026-05-19 知识审查工作流 + knowledge_update + 输出固化

### 新增

**`knowledge_update` 工具**
- 支持选择性地更新知识条目的任意字段（title/summary/tags/topics/tasks/source_type/links/raw_ref/content）
- 保留原始 `CreatedAt` 时间戳不变（修复了 `saveKnowledge` 覆盖 CreatedAt 的 bug）
- 仅更新显式提供的字段，未提供的字段保持不变

**`workflows/knowledge_review.yaml` — 知识条目质量审查工作流**
4 步骤：
1. `get_entry` — 获取知识条目完整内容
2. `analyze` — DeepSeek 逐字段审查（title/summary/tags/topics/tasks/source_type），输出结构化 JSON
3. `apply_fix` — 知识条目自动修复（调 `knowledge_update`）
4. `report` — 生成可读的中文审查报告

审查规则：
- title: 是否简洁有信息量
- summary: 是否描述核心内容
- tags: 数量合理（2-5个）、有辨识度
- topics: 是否与 tags 互补
- tasks: 是否具体、动词开头
- null 字段跳过，仅非 null 修复值写入

**Web 界面增强**
- 侧栏新增「审查质量」快捷操作
- 聊天输入 "帮我审查知识条目 kn_xxx" 自动调知识审查工作流

**烟雾测试补充**
- `personal_knowledge_smoke.yaml`：新增 `knowledge_get` 无效条目错误处理、`knowledge_update` 无效条目错误处理
- `workflow_smoke.yaml`：新增 `knowledge_review` 不存在的条目工作流失败场景

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeUpdate + knowledge_update 工具注册；修复 saveKnowledge 覆盖 CreatedAt |
| `plugins/memory_plugin.go` | 新增 knowledge_update 路由 |
| `main.go` | Meta.Types 新增 knowledge_update |
| `workflows/knowledge_review.yaml` | 新建 — 4 步骤知识质量审查工作流 |
| `web/index.html` | 侧栏审查入口 + 聊天前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新增 error handling 测试用例 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 knowledge_review 工作流测试 |

## 2026-05-19 个人知识系统基础建设（三步）

### 第一步：Memory Schema 规范化

**新增 `internal/tools/knowledge.go` — 统一知识条目类型 + CRUD 工具**

知识条目 `KnowledgeEntry` 统一字段：
```
id, source_type, title, summary, tags[], topics[], tasks[], created_at, links[], raw_ref, content
```

| 工具 | 用途 |
|---|---|
| `knowledge_add` | 添加结构化知识条目（含 tags/topics/tasks 数组） |
| `knowledge_search` | 按关键词匹配 title/summary/content/tags/topics |
| `knowledge_list` | 按 source_type 过滤列出 |
| `knowledge_get` | 获取完整条目 |
| `knowledge_delete` | 删除条目 |

存储位置：`~/.hermes/memory/knowledge/<id>.json`

**todo 持久化 + memory 关联**

- `internal/tools/todo.go` 重构：从 `map[string]interface{}` 切换到 `TodoItem` 结构体，文件持久化到 `~/.hermes/memory/todos.json`
- `todo_add` 新增 `source` 字段，指向关联的 memory/知识 ID
- 新增 `todo_by_source` 工具：按来源查询待办

**memory_plugin 增强**

- 新增 knowledge 消息类型路由（`knowledge_add/search/list/get/delete`）
- memory_plugin 现在返回知识工具的执行结果 Payload，供 workflow 后续步骤使用

### 第二步：personal_knowledge_ingest 工作流

创建 `workflows/personal_knowledge_ingest.yaml`，三步骤：

1. **analyze**（think_plugin）：DeepSeek 分析输入 → 输出 JSON（title/summary/source_type/tags/topics/tasks）
2. **save_knowledge**（memory_plugin knowledge_add）：结构化知识入库，返回知识 ID
3. **save_tasks**（todo_plugin todo_add）：提取的待办写待办列表，`source` 关联知识 ID

**workflow 引擎增强**

- `buildPayload` 支持 JSON 字段路径提取（`${steps.xxx.output.field}`）
- `buildPayload` 自动检测 JSON 数组/对象，保持参数类型完整性
- `resolveJSONValue` 自动解包嵌套编码的 JSON 字符串

**think_plugin 增强**

- 新增 `extractPrompt()` 函数：支持 JSON 对象 Payload 提取 `message` 字段（workflow 场景）

### 第三步：知识入口 + 烟雾测试

**Web 界面增强（`web/index.html`）**

- 侧栏新增「+ 入库新内容」按钮，打开模态框
- 模态框：textarea 粘贴内容 → 直接调 workflow API
- 侧栏知识库快捷操作：浏览知识库、搜索知识
- 聊天输入自动检测 "帮我入库" 前缀，直接触发 workflow

**烟雾测试（`eval/scenarios/personal_knowledge_smoke.yaml`）**

9 个测试用例覆盖：knowledge_add → search → list → todo_by_source → todo_add(含source) → cleanup

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新建 — 知识条目结构体 + CRUD + 工具注册 |
| `internal/tools/todo.go` | 重构 — TodoItem 结构体 + 文件持久化 + source 字段 + todo_by_source |
| `internal/tools/tools.go` | Init() 追加 registerKnowledgeTools() |
| `plugins/memory_plugin.go` | 新增 knowledge 工具路由 + 返回响应 Payload |
| `plugins/todo_plugin.go` | 新增 todo_by_source 路由 |
| `plugins/think_plugin.go` | 新增 extractPrompt() JSON 对象兼容 |
| `internal/workflow/engine.go` | buildPayload JSON 字段路径 + 数组自动检测 |
| `main.go` | Meta.Types 更新（knowledge + todo_by_source） |
| `workflows/personal_knowledge_ingest.yaml` | 新建 — 三步骤知识入库工作流 |
| `web/index.html` | 知识入库模态框 + 侧栏快捷操作 + 输入前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新建 — 9 用例烟雾测试 |

## 2026-05-18 Web 界面 — 内嵌单页应用

### 新增

- **`web/index.html`**：单文件 Web 界面，深色主题
  - 左侧栏：连接状态指示 + 工作流列表（调用 `skill_list` 自动加载，点击直接触发）
  - 主面板：聊天式交互，支持自然语言输入和结构化响应展示
  - 零外部依赖：纯 vanilla HTML/CSS/JS，无框架、无 CDN
- **`main.go` `//go:embed`**：将 `web/index.html` 编译进二进制，保持单文件部署
  - 访问 `http://localhost:8013/` 打开 Web 界面

### 效果

```bash
# 启动后浏览器打开
open http://localhost:8013
# 在输入框里直接打字：
#   "帮我跑一下开源雷达" → 触发 workflow
#   "搜索今天的AI新闻"  → Router 路由 search_plugin
#   左侧工作流列表     → 点击直接发送
```

### 架构

```
浏览器 ← HTTP → beishan-core (:8013)
                 ├─ GET  /         → web/index.html (嵌入二进制)
                 ├─ POST /api/chat → 完整消息链路
                 └─ GET  /health   → 连接状态
```

## 2026-05-18 Router 工作流发现：用户自然语言触发 workflow

### 新增

- **`kernel.Decision.Payload` 字段**：DeepSeek 路由决策时可输出 payload，`kernel.Send()` 自动应用
- **`kernel.Router.SetWorkflowSummary()`**：注入可用 workflow 列表到路由 prompt
- **`main.go` 启动时扫描 `workflows/`**：`buildWorkflowSummary()` 读取每个 YAML 的 id 和头部注释，注入 Router
- **`workflow_plugin` 纯文本降级**：payload 为裸字符串时直接作为 workflow 名处理

### 效果

```
用户说"帮我跑一下开源雷达"
  → Router 识别为 workflow_plugin (置信度 1.00)
  → 自动设置 payload: {"workflow":"github_radar"}
  → workflow_plugin 执行 7 步全链 ✅
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `kernel/router.go` | Decision 加 Payload、Router 加 workflowSummary、Route prompt 加 payload 指令 |
| `kernel/kernel.go` | Send() 应用 Decision.Payload |
| `plugins/workflow_plugin.go` | 纯文本 payload 降级为 workflow 名 |
| `main.go` | buildWorkflowSummary() 扫描 workflows/ 注入 Router |

## 2026-05-18 skill_factory 增强：Types 注册 + 硬化层第四关

### 变更

- **`kernel.Meta` 新增 `Types []string` 字段**：记录每个插件支持的 `msg.Type` 列表
- **`kernel.KnownPluginsMeta()` 新增方法**：返回 `map[string]Meta`，含 Description、Tags、Types
- **`skill_factory_plugin` 增强**：
  - `buildPluginList()` 输出带 types：`search_plugin: 通用网络搜索 (types: web_search, web_fetch)`
  - `validateAndSave()` 增加第四关校验：验证 `step.Type` 在插件注册的 `Types` 列表内
- **`main.go`**：15 个插件注册全部补上 `Types` 字段

### 效果

| 指标 | 之前 | 之后 |
|---|---|---|
| DeepSeek 生成 type 正确率 | ~0%（全靠猜） | 实测 4/4 全对 ✅ |
| 硬化层校验 | 不校验 type | 第四关拦截非法 type |
| prompt 中插件信息 | 只有名字+描述 | 名字+描述+可用 types |

### 硬化层四关

1. YAML 语法解析
2. 语义检查（id、steps、plugin、type）
3. 插件注册表校验（plugin 必须在 kernel 已注册）
4. **type 合法性校验**（step.Type 必须在插件注册的 Types 列表内）

## 2026-05-18 skill_factory_plugin — 用自然语言生成 YAML 工作流

### 新增

- **`plugins/skill_factory_plugin.go`**：技能工场插件，接收自然语言描述，用 DeepSeek 自动生成标准 YAML 工作流并保存到 `workflows/`
- **`main.go`**：注册 `skill_factory_plugin`，传入 `workflows/` 目录路径

### 消息类型

| 类型 | 用途 |
|---|---|
| `skill_create` | 根据自然语言描述生成 YAML 工作流并保存 |
| `skill_list` | 列出所有已有 skill/workflow |
| `skill_view` | 查看某个 skill 的 YAML 内容 |
| `skill_delete` | 删除一个 skill |

### 硬化层验证

生成的 YAML 经过三层校验才写入：
1. YAML 语法解析（`gopkg.in/yaml.v3`）
2. 语义检查（id 必有、steps 非空、每步有 plugin/type）
3. 插件注册表校验（引用的 plugin 必须已注册到 kernel）

文件名冲突保护：同名 workflow 已存在时拒绝覆盖。

### 用法

```bash
# 一句话生成工作流
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_create","payload":{"description":"每天早上搜索HackerNews热门技术话题，总结趋势后存入记忆","name":"hn_daily"}}'

# 列出所有 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_list"}'

# 查看某个 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_view","payload":"hn_daily"}'

# 删除 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_delete","payload":"hn_daily"}'
```

## 2026-05-18 scheduler 支持 cron 定点触发

### 新增

- **scheduler `cron` 字段**：`schedule_add` 支持标准 5 字段 cron 表达式，与 `interval` 互斥
- **`cronNext()` 最小 cron 解析器**：内建于 `plugins/scheduler_plugin.go`，支持 `*`、`*/N`、`N-M`、`N,M` 语法
- **cron 模式 timer 调度**：`time.NewTimer` 计算到下次触发的时间，触发后重算下一轮，支持 `schedule_list` 显示下次执行时间

### 用法

```bash
# 每天上午 10 点执行
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"scheduler_plugin","type":"schedule_add","payload":{"name":"daily_radar","cron":"0 10 * * *","workflow":"github_radar"}}'
```

### 测试

|cron 表达式|期望行为|验证|
|---|---|---|
|`0 10 * * *`|每天 10:00|6 场景全部 PASS ✅|
|`*/15 * * * *`|每 15 分钟|6 场景全部 PASS ✅|
|`0 9 * * 1-5`|工作日 9:00|6 场景全部 PASS ✅|

## 2026-05-18 callback webhook + workflow 超时/重试

### 新增

- **`internal/notify/` 回调推送层**：
  - `slack.go`: Slack Incoming Webhook 推送
  - `email.go`: SMTP 邮件发送
  - `wechat.go`: 企业微信机器人
  - `notify.go`: `callback:platform:地址` 格式分发
- **`kernel/deliverReply` `callback:` 分支**：调 `notify.Callback()`，goroutine 异步推送

### 增强

- **workflow 超时可配**：YAML 每个步骤支持 `timeout` 字段（秒，默认 120）
- **workflow 错误重试**：YAML 每个步骤支持 `retry` 字段（次数，默认 0）
- **legal_review.yaml**：各步骤标注 `timeout:30` / `retry:1`
- **eval/scenarios/workflow_smoke.yaml**：工作流引擎冒烟测试场景

### 用法

| 方式 | ReplyTo 格式 |
|---|---|
| Slack | `callback:slack:https://hooks.slack.com/services/xxx` |
| 邮件 | `callback:email:smtp://user:pass@smtp.qq.com:587/to@addr` |
| 企业微信 | `callback:wechat:https://qyapi.weixin.qq.com/...` |

## 2026-05-18 think_plugin + Router MsgType + 启动清理 + REPL

### 新增

- **`plugins/think_plugin.go`**：通用对话插件。`chat` 类型消息调 DeepSeek 生成回答，系统提示词同步 beishan-core 能力列表，不自称"不能XX"
- **`cmd/repl/main.go`**：REPL 交互界面。`go run cmd/repl/main.go` 启动，直接打字聊天，支持 `/todo_plugin:todo_add 买牛奶` 格式指定插件
- **`eval/scripts/run_core_smoke.sh`** 新增 think_plugin 测试用例

### 修复

- **`kernel/router.go`**: `Decision` 新增 `MsgType` 字段。用户说"搜索新闻"→ 路由到 `search_plugin` + `Type: web_search`，不再把 `chat` 类型送到所有插件
- **`kernel/kernel.go`**: `Send()` 应用决策中的 `MsgType`
- **启动清理**：编译 `go_example` / `l3_echo_go` 二进制，删除 `l4_research` / `l4_template_python` 残档，manifest 目录名校验全通过
- **think_plugin 系统提示词**：列出所有插件能力，避免 DeepSeek 说"我不能生成图片"

### 实测

| 操作 | 结果 |
|---|---|
| `{"message":"你好"}` | → think_plugin 回答 ✅ |
| `{"message":"搜索新闻"}` | → search_plugin/web_search ✅ |
| `{"message":"帮我写文件"}` | → write_plugin/write_file ✅ |
| `{"message":"生成图片"}` | → image_gen_plugin/image_generate ✅ |
| REPL 交互 | → 打字聊天可用 ✅ |
| 启动零警告 | → 20 插件全部就绪 ✅ |

## 2026-05-18 工作流引擎 + legal_review 替换为 YAML

### 新增

- **`internal/workflow/` 工作流引擎**：
  - `engine.go`：核心执行器，读 YAML → 顺序调用 `kernel.Call` → 条件分支路由
  - `types.go`：支持 `next: string` 和 `next: [{if:..., goto:...}, {default:...}]` 双格式
  - `buildPayload`：`${input}` 和 `${steps.<id>.output}` 插值解析
  - `evaluateCondition`：`steps.<id>.output.<field> == 'value'` 条件评估

- **`workflows/` 目录**：
  - `legal_review.yaml`：首个 YAML 工作流（4 步：cold_start → legal_search → clause_analysis → write_report）
  - `_template.yaml`：标准示例模板，含字段说明和条件分支示例

- **`plugins/workflow_plugin.go`**：薄插件包装，接收 `workflow_run` 消息，委托 engine 执行

### 删除

- **`plugins/legal_review_plugin.go`**（-150 行 Go 代码）：被 `workflows/legal_review.yaml` 替代
- `main.go` 中 `legal_review_plugin` 注册移除，描述合并到 `workflow_plugin`

### 替换后影响

| 项目 | 替换前 | 替换后 |
|---|---|---|
| 加新场景 | 写 Go 代码 + 编译 | YAML 文件，无需编译 |
| 法律审查调用 | `legal_review_plugin` | `workflow_plugin`（workflow: legal_review） |
| 插件数 | 18 | 17 |
| 测试 | legal_smoke 6/6 通过 | legal_smoke 6/6 通过 ✅ |

### 工作流引擎实测

```
cold_start → legal_search → clause_analysis → write_report  4步全通 ✅
```

## 2026-05-18 全功能冒烟测试 + 工具移植完结

### Eval 补全

- **新增 `eval/scenarios/core_smoke.yaml`**：12 个测试用例覆盖全部 L3/L4 插件
- **新增 `eval/scripts/run_core_smoke.sh`**：自动编译 + 启动 + 测试 + 清理
- **实测 12/12 全部通过** ✅

### hermes-go 工具移植完结

本轮完成最后 4 个工具移植：

| 工具 | 文件 | 定位 |
|---|---|---|
| vision_analyze（视觉分析） | `internal/tools/media.go` | 预留接口，需 Vision API |
| image_generate（图片生成） | `internal/tools/media.go` | 预留接口，需 DALL-E / SD |
| text_to_speech（文本转语音） | `internal/tools/media.go` | 本地 `say` 命令可用 |
| clarify（意图澄清+学习） | `internal/tools/clarify.go` | 3 次学习后自动推断 |

### 最终统计

| 指标 | 数值 |
|---|---|
| 工具注册数 | 34 |
| 插件注册数 | 17（含 3 glue 子进程） |
| Eval 场景 | legal_smoke（6 用例） + core_smoke（12 用例） |
| 内核文件 | 冻结不改 |
| 全部 hermes-go 工具 | 移植完毕 ✅ |

## 2026-05-18 开发日志归档

## 2026-05-18 memory continuity（路线 A：session 内含 evidence）

### 新增

- **`internal/tools/memory.go` 重写为 session 感知存储**：
  - 存储结构：`~/.hermes/memory/sessions/<session_id>.json`
  - 每条 session 内包含 `messages[]` + `evidence[]`
  - 7 个新工具：session_add、session_get、session_search、session_list、session_delete、evidence_add、evidence_search
  - 并发安全：`sync.RWMutex` 保护读写
  - 威胁扫描：注入检测保留

- **`plugins/memory_plugin.go` 更新**：支持全部 7 种 session 消息类型

- **`main.go` HTTP handler 自动记录 session**：
  - 每次 `/api/chat` 请求生成 `session_id`
  - 同步模式下自动记录 `user → plugin → response` 到 session
  - 异步模式通过 goroutine 处理，`ReplyTo` 回程后存入

### 注册工具统计

`tools registered:` **15** 个工具（原 11 + 新增 7 个 session/evidence 工具，剔除 3 个旧 memory 工具）

### 路线选择

**路线 A**：evidence 作为 session 的子结构存储，不独立管理。
当前无跨 session 引用证据的真实需求，路线 B 留接口。

### 实测

- 发送消息 → session 自动创建 ✅
- 消息持久化到磁盘 ✅
- session_list 查询 ✅

## 2026-05-18 第三轮：glue 依赖管理（路线 A）

- **`glue/spawn()` 新增 `requirements.txt` 自动检测**：spawn 前 `os.Stat` 检测，存在则 `pip3 install -r`
- **向后兼容**：没有 `requirements.txt` 的插件行为不变
- **路线 B 预留**：未来如需独立 venv，在 `spawn()` 中 `switch m.Type` 处分支即可

## 2026-05-18 第二轮：ReplyTo 回程路由 + HTTP 异步 session

### 新增

- **`Message.ReplyTo` 字段**：支持 `plugin:`、`session:`、`callback:`、空 四种前缀
- **`deliverReply()` 内核方法**：`Send()` 完成后检查 `ReplyTo`，按前缀分派
- **`SessionHandler` 回调**：内核不持有 session 状态，由 HTTP 层注入存储函数
- **`/api/chat` 异步模式**：`{"message":"...","async":true}` 立即返回 `session_id`，后台 goroutine 处理
- **`/api/result/:session_id` 轮询端点**：有结果返回结果，无结果返回 `{"status":"pending"}`

### 清理

- `Router.checkRecipient` 和 `SetRecipientValidator` 删除，`parseDecision` 只用 `knownPlugins` 验证
- `NewKernel` 不再依赖 `tools.GetToolSchema`，内核层与工具层注册表完全解耦

### 架构边界（正式确立）

```
内核路由层  → 只认识插件名（knownPlugins）
插件执行层  → 只认识工具名（tools.Registry）
两层之间    → 不互相知道对方的注册表
```

### 实测

- 异步请求 → 立即返回 session_id ✅
- goroutine 后台处理 → DeepSeek 路由 → 插件执行 → deliverReply 存储结果 ✅
- 轮询 /api/result/:session_id → 返回结果 ✅

## 2026-05-18 Meta 注册 + 路由描述增强

### 新增

- **Meta 结构体**：`kernel.Register` 新增可选的 `Meta` 参数，支持 `Description` 和 `Tags`，向后兼容
- **路由 prompt 增强**：DeepSeek 现在能看到每个插件的语义描述，路由决策质量提升
- **`AddKnownPlugin` 替代 `SetPlugins`**：注册时自动维护路由列表，不再需要手动同步
- **`KnownPlugins()`**：新增方法返回所有已注册插件
- **Markdown 容错**：`parseDecision` 自动剥离 DeepSeek 返回的 `` ```json ``、`` ``` `` 等标记
- **HTTP API 兼容**：`/api/chat` 支持 `{"message":"..."}` 简单格式和 `{"type":"...","payload":...}` 完整格式

### 修复

- **Router 不暴露工具名**：路由 prompt 不再包含 `internal/tools` 的工具名，只显示 kernel 注册的插件名
- **`checkRecipient` 改为查 `knownPlugins`**：移除对 `tools.GetToolSchema` 的最后引用，Router 不再依赖 tools 包
- **tools.Init() 调用**：main.go 缺失的初始化已补齐

### 实测

- `web_search` → `search_plugin` ✅ 搜索结果正确返回
- `write_file` → `write_plugin` ✅ 文件写入成功

## 2026-05-17 全链路冒烟测试通过 6/6

### 修复

- **Router extraNames**：DeepSeek 提示词现在包含 kernel 注册的插件名，确保法律插件可被路由
- **legal_review_plugin 响应 Sender**：移除了导致 `deliverResponse` 跳过回传的 Sender 字段
- **纯文本 Payload 兼容**：legal_search_plugin 和 clause_analyzer_plugin 现在也接受纯文本输入
- **parseProfile 空值降级**：空 profile JSON 不再报错
- **WriteRequest 包装**：legal_review_plugin Step 4 正确将 AnalysisReport 包装为 WriteRequest

### 新增

- **HTTP API 服务**：main.go 改为持久 HTTP 服务（`:8013`），添加 GET /health 和 POST /api/chat
- **Router.KnownPlugins**：新增方法返回所有注册插件名

### 测试

- 法律插件冒烟测试 6/6 全量通过
- 测试链路：冷启动(2) → 法律检索(2) → 条款分析(1) → 全链路审查(1)

## 2026-05-17 L3 法律分析插件簇 + 中国法律适配

### 新增

| 文件 | 类型 | 用途 |
|---|---|---|
| `plugins/legal_review_plugin.go` | L4 编排 | 法律审查编排：访谈→检索→分析→生成四步流程 |
| `plugins/l3_echo_go/` | L3 Go 示例 | Go 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/l3_echo_python/` | L3 Python 示例 | Python 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/cold_start_plugin.go` | L3 插件 | 冷启动访谈：合同类型识别、角色分析、法律画像构建 |
| `plugins/legal_search_plugin.go` | L3 插件 | 中国法律检索：适配北大法宝/威科先行查询结构，法律效力层级排序 |
| `plugins/clause_analyzer_plugin.go` | L3 插件 | 条款分析：三段论（大前提-小前提-结论）替代 IRAC，风险三档评级 |
| `plugins/legal_write_plugin.go` | L3 插件 | 法律文书生成：合同审查报告/法律意见书/风险矩阵，AI 标识合规 |

### 中国法律适配

所有法律插件遵循以下中国法域适配规则：
- **法律关系分析**：使用《民法典》合同编典型合同分类体系（第595-978条）
- **三段论推理**：大前提（法律规则）→ 小前提（合同约定）→ 结论（法律评价），替代 IRAC
- **法律效力层级**：宪法 > 法律 > 司法解释 > 行政法规 > 部门规章
- **风险评级**：🟢 合规 / 🟡 提示 / 🔴 违规（参考 claude-for-legal 三档制）
- **文书模板**：合同审查报告、法律意见书、风险矩阵，均使用中国法律文书格式
- **AI 标识**：根据《人工智能生成合成内容标识办法》，所有输出标注 AI 生成身份

### 参考 claude-for-legal 的模式迁移

| 模式 | 迁移比例 | 用途 |
|---|---|---|
| 冷启动访谈（SKILL.md） | ~10% | cold_start_plugin.go 的法律画像构建流程 |
| 风险评级（GREEN/YELLOW/RED） | ~10% | clause_analyzer_plugin.go 的三档评级体系 |
| 文书模板（法律意见书/审查报告） | ~8% | legal_write_plugin.go 的输出模板 |
| 免责声明分级（律师/法务/个人） | ~5% | 按用户角色生成不同的免责声明 |
| MCP 连接器接口（北大法宝预留） | ~3% | legal_search_plugin.go 的 tryPkulawSearch 预留接口 |

### 主程序变更

`main.go` 新增全部法律插件注册：
- 通用工具插件（search/write/memory/scheduler）
- 法律审查插件簇（legal_review/cold_start/legal_search/clause_analyzer/legal_write）

### design principles 新增

`DESIGN_PRINCIPLES.md` 新增 **"Type 即意图，Payload 即数据"** 章节，确立三条子原则：
- 路由只认 Type：Router 做机械映射，不看 Payload
- Payload 不参与决策：Router 永不解析 Payload 字段
- 不能把路由判断权交给 LLM

### 司法数据源集成

新增 `judicial_search` 工具，接入中国司法大数据服务网 (data.court.gov.cn) 和中国裁判文书网 (wenshu.court.gov.cn) 公开数据：

| 变更 | 文件 | 说明 |
|---|---|---|
| 新增工具 | `internal/tools/judicial.go` | judicial_search 工具：HTTP 封装 + HTML 解析 + 结果格式化 |
| 检索链路更新 | `plugins/legal_search_plugin.go` | searchStatutes/searchCases 优先调用 judicial_search |
| 注册入口 | `internal/tools/tools.go` | Init() 追加 registerJudicialTools |

**数据源优先级**：司法大数据服务网 → 裁判文书网 → 通用 web_search 回退

**免费接口限制**：非注册用户仅支持部分案由（民间借贷、离婚、买卖合同等）的统计查询，裁判文书网有反爬机制。所有结果标记 `source` 字段，供审查者验证。

### 测试基础设施 + Router 验证修复

从 `66/FangLab` 项目移植评估基础设施：

| 新增 | 文件 | 说明 |
|---|---|---|
| 测试场景 | `eval/scenarios/legal_smoke.yaml` | 6 个法律插件测试用例（冷启动/检索/分析/全链路） |
| 运行脚本 | `eval/scripts/run_legal_smoke.sh` | 启动服务→发送测试→验证响应→汇总结果 |
| 共享库 | `eval/lib/lib.sh` | 端口检查/进程管理/日志（移植自 runtime_stack_lib.sh） |
| 文档 | `eval/README.md` | 测试方案说明 |
| 环境配置 | `.env` | DeepSeek API Key 配置（已 gitignore） |

**Router 验证修复** (`kernel/kernel.go`, `kernel/router.go`)：

Router.parseDecision 原先只查 `tools.GetToolSchema`，但法律插件通过 `kernel.Register` 注册而非工具注册中心。导致 DeepSeek 路由到合法插件时被误判为"无效收件人"。

修复：`NewKernel` 注入 `SetRecipientValidator`，同时检查内核插件表和工具注册中心。kernel.go 新增 .env 自动加载（init 函数）。

### 架构对齐确认

`legal_review_plugin.go` 作为 L4 编排插件，遵循以下契约：
- 每个步骤通过 `kernel.Call()` 调用 L3 插件，Type 字段精确指定路由目标
- Payload 只传数据（`json.RawMessage`），不做 type assertion
- 导入路径使用 `beishan/kernel`（非 `github.com/...`）
- 错误处理硬编码：任一步骤失败立即终止，不降级

## 2026-05-16 L3/L4 边界硬化

### 背景

文档定义的"三层架构"（第一核心/胶水层/插件）与实际代码的"四层架构"（L1 kernel/L2 glue/L3 internal/tools/L4 plugins）不一致，导致 L3 硬化职责掉落在 L3/L4 之间的缝隙中。

根因：L4 plugins 直接调用 `tools.Execute`（L3 内部调度），跳过了参数校验。且 `tools.Execute` 内部对 JSON 解析失败的 payload 做 lenient fallback（包成 `{"raw": ...}`），等于对 LLM 不可靠输出零防御。

### 本次变更

| 变更 | 影响文件 |
|---|---|
| **Schema 注册中心**：新增 `RegisterToolSchema` / `GetToolSchema` / `GetAvailableTools`，Router 查询时不触碰 Payload | `internal/tools/schema_registry.go` |
| **ValidateAndExecute**：L4 调用 L3 的唯一入口，先 Schema 校验再执行 | `internal/tools/validate.go` |
| **Router 路由验证**：`parseDecision` 改用 `GetToolSchema` 验证 Recipient，移除 whitelist 字段 | `kernel/router.go` |
| **L4 插件重构**：search/write/memory 全部改调 `ValidateAndExecute` | `plugins/*.go` |
| **L2 IPC 强化**：ProtocolMessage 增加 TraceID/Timestamp/RetryCount，dispatch 时自动注入 | `glue/protocol.go`, `glue/glue.go` |
| **Execute 硬化**：移除 lenient fallback，不合法的 JSON 直接报错 | `internal/tools/tools.go` |
| **文档对齐**：三层 → 四层架构，"第二核心" → "胶水层" | `CHANGELOG.md`, `DESIGN_PRINCIPLES.md` |

### 架构合约（写入后不再修改）

```
L1 kernel/        注册 + 路由 + 转发                   Payload 永不透明
L2 glue/          IPC + 进程管理                       不接触 Payload 内容
L3 internal/tools/ Schema 注册 + 强校验 + 执行          硬化的唯一关卡
L4 plugins/       编排 L3 完成多步任务                   只写业务逻辑，零防御代码

调用链：
  User → L1 Route (强制 DeepSeek, 仅查路由表)
       → L1 Send → L4 OnMessage
       → L3 ValidateAndExecute (Schema 校验)
       → L3 Execute → handler
       → 响应原路返回
```

### 设计纪律

见 DESIGN_PRINCIPLES.md

## 2026-05-11 架构重建

### 背景

从 1 月到 5 月，经历了多个版本的迭代（Beishan 微内核 → V9 → Mini v2.0），最后发现所有版本走向规则型的根本原因相同：**大语言模型被放在了错误的位置，或者根本没有位置。**

### 核心认知

大语言模型是文本补全器，不是通用智能。它擅长生成，不擅长思考、路由、决策。所有把 LLM 当聪明人用的架构，最终都会变成规则型。

### 本次重建的设计决策

#### 两模式路由

```
Recipient == "" → DeepSeek 决策路由（慢，但智能）
Recipient != "" → 直接转发（快，L4 已决策）
```

不给快捷方式。DeepSeek 不可用时系统不降级为规则匹配。

#### 四层架构

| 层 | 包 | 职责 | 语言 | 可修改 |
|---|---|---|---|---|
| L1 内核 | `kernel/` | 注册 + 路由 + 转发 | Go | 冻结不改 |
| L2 胶水层 | `glue/` | IPC + 进程管理 | Go | 可迭代 |
| L3 工具层 | `internal/tools/` | 工具注册 + 执行 + schema 清理 | Go | 可迭代 |
| L4 编排层 | `plugins/*.go` | 编排 L3 完成多步任务 | Go / Python | 随意改 |

#### 消息格式

Message 只有 4 个字段：Sender、Recipient、Type、Payload。
Payload 对内核永不透明。

### L4 插件清单

| 插件 | 职责 | 实现来源 |
|---|---|---|
| search_plugin | 网页搜索、内容抓取（L4 编排 → tools.ValidateAndExecute） | hermes-go tools/web.go |
| write_plugin | 文件读写、搜索、修改 | hermes-go tools/file.go |
| memory_plugin | 记忆存储、检索 | hermes-go tools/services.go（内存部分） |
| scheduler_plugin | 定时触发任务 | 新写 |

### 设计纪律

见 DESIGN_PRINCIPLES.md

### 参考项目

ds4.c（Redis 作者 antirez 的 DeepSeek V4 Flash 推理引擎）的设计哲学：
- 故意的狭窄，不做通用框架
- 接口极窄（173 行 header）
- 没有功能开关
- 注释写在代码旁边
- 正确性优先于速度

claude-for-legal（Anthropic 的法律 AI 智能体框架）：
- 工作流编排骨架（冷启动访谈→分析→输出）
- 文档自动化模板（风险矩阵、法律意见书）
- MCP 连接器接口定义
- 本项目的 L4 法律编排插件设计受此项目启发

