# 已知限制与设计边界

本文件诚实声明 beishan-core 的设计边界和已知限制。
和硬化层一样，文档的完整性和透明性是项目信任的基础。

---

> **AI Summary:** 15 known limitations. Current: 115 tools, 0 MCP skills (框架保留), 3 right flowers, 40+ workflows, llmguard (7 files).
> Key: hardening only guarantees surface safety (not logic correctness).
> No sandbox, no workflow persistence, no gate automation.
> L2 glue doesn't manage right flower lifecycle (OS process manager does).

## 1. 硬化层完备性边界

硬化层只保证表层安全（格式、命令、路径），不保证深层正确性（逻辑、架构、性能）。
详见 [HARDENING_LAYER.md](./HARDENING_LAYER.md) 的"不保证的"表格。

**影响**：通过了硬化层的请求仍可能产生错误的业务结果。这不是 bug，是设计边界。

**缓解**：
- 烟雾测试套件（`eval/`）覆盖硬化层的拒绝逻辑
- 工作流输出可以通过 AfterExecute 钩子做质量门禁（如 `checkSearchRelevance`）
- 关键路径（legal_review）有全链路烟雾测试

---

## 2. 硬化层边界维护依赖开发者纪律

这是最大的软性问题。硬化层无法用代码强制禁止开发者：
- 直接调用 `tools.Execute` 而不是 `ValidateAndExecute`
- 在 L4 插件中直接操作文件系统
- 修改 `kernel/` 的冻结代码

**影响**：一次绕过就可以让硬化层完全失效。

**缓解**（无代码级解决方案，只有流程级）：
- 日志审计：搜索 `tools.Execute` 在非 `validate.go` 中的调用
- 烟雾测试：验证硬化层的拒绝行为
- 代码审查：每条 PR 检查是否出现绕过硬化层的模式
- 本文档：明确告知贡献者硬化层边界

---

## 3. 上下文注入无反馈确认

beishan-core 可以向外部工具（Claude CLI 等）注入知识上下文，但**无法验证外部工具是否真正使用了这些上下文**。这是系统边界之外的问题。

**影响**：上下文注入可能无效，但系统无法检测到。

**缓解**：无。

---

## 4. 本地模型依赖

部分工作流（特定的 `provider: local` 步骤）依赖本地模型（如 Qwen3.6）。
本地模型不可用时，这些步骤会自动降级或失败。

**影响**：
- 无 GPU 环境无法运行本地模型步骤
- 模型加载有冷启动延迟

**缓解**：
- 所有关键工作流已迁移为 `provider: deepseek`（全 API）
- 本地模型仅用于低优先级批量任务

---

## 5. AI 生成的工作流质量

YAML 工作流引擎允许 AI 直接生成和修改工作流定义，但硬化层**不校验工作流的逻辑正确性**——只校验每一步的参数格式。

**影响**：AI 可能生成逻辑上错误的工作流（如循环引用、步骤顺序错误、数据依赖断裂）。

**缓解**：
- `skill_evaluate` 工具可以评估工作流质量
- 工作流模板库（`workflows/templates/`）提供经过验证的模式
- `max_iterations`（默认 200）防止死循环

---

## 6. Go-DSL 引擎的工具映射

Go-DSL 引擎的 `toolHost` 映射（tool 名 → 宿主插件名）是**硬编码的**。新增工具时需要同时在 `tools.Registry` 和 `toolHost` 中注册。

**影响**：遗忘更新 `toolHost` 会导致 Go-DSL 步骤在构造时 panic。

**缓解**：
- `NewGoToolPlugin` 在构造时检查 `toolHost` 完整性
- 主启动流程中 `main.go` 集中管理 `toolHost`

---

## 7. 双引擎类型共享

YAML 引擎和 Go-DSL 引擎共享 `StepResult`/`WorkflowResult` 类型，但状态传递方式不同：
- YAML 引擎：`ctx["steps.xxx.output"]` 字符串
- Go-DSL 引擎：`StateStore.Get("step.field")` map 结构化取值

**影响**：混合使用两个引擎时需要理解状态传递差异。

**缓解**：无——这是设计选择，不是待修复项。

---

## 8. 无工作流持久化

所有工作流（YAML 和 Go-DSL）的状态在单次 `OnMessage` 调用期间存在于内存中。
没有检查点、断点续跑、长时间运行工作流支持。

**影响**：
- 长时间工作流（如 `code_deep_analyze` 约 8 分钟）中途失败必须重头开始
- 无法在工作流中间手动介入或调试

**缓解**：步骤超时 + 重试机制减少失败概率。注意：步骤不保证幂等——失败后重试可能产生副作用。

---

## 9. 无安全沙箱

L3 工具在宿主进程中执行，没有沙箱隔离。
`code_apply` 的补丁应用直接操作文件系统。

**影响**：代码路径穿越或危险命令可能影响宿主机。

**缓解**：
- `code_security_check` 在补丁应用前做危险模式检测
- `isSafePath` 阻止写入工作目录外的路径
- `code_apply` 有二次安全检查
- 但不提供容器级或虚拟化级隔离

---

## 10. 茎注册表运行时过滤未启用

`internal/registry/` 的 `Policy.Filter()` 已实现但未接入任何调用路径。`NewPolicy` 从未被调用，无 profile 配置，`validateGoStep` 中也无 TODO 注释（代码已改，注释未跟上）。

**影响**：所有工具对所有角色可见，无 profile 过滤。

**阻塞点**：接入 `Policy.Filter()` 需要先解决三个前置问题：
1. 在哪里定义 profiles（配置文件？硬编码？）
2. Go-DSL 工作流如何声明所属 profile
3. `validateGoStep` 加 `*Policy` 参数会破坏当前调用方签名

**缓解**：该能力对当前单节点部署场景无实际需求。等多租户/角色隔离成为真实需求时再实现。

---

## 11. bench 评估框架无自动化流水线

`internal/bench/` 已就绪（3 个套件 + runner），但未接入 CI 或定时执行。评估框架目前只能手动触发。

**影响**：能力退化需要通过手动运行烟雾测试发现，无自动化预警。

**缓解**：接入 CI 的 `eval/scripts/` 流程。

---

## 12. 右花协议完成（2026-05-28 更新）

`docs/RIGHT_FLOWER_PROTOCOL.md` 已定义三层契约（通信/安全/注册）。

**已实现**：
- `internal/rightflower/plugin.go` — RegisterAll 扫描 `right_flowers/` 目录 ✅
- `right_flowers/openhuman.yaml` — 首个右花 manifest 已激活 ✅
- adapter 桥接 + dispatch + probe-methods 已贯通 ✅
- `k.RegisterUnlisted` — 不暴露给路由器但可通过 `kernel.Call` 直达 ✅
- **工作流已可直接编排右花**：YAML 中用 `plugin: openhuman / hermes_agent` 即可，`kernel.Call` 通过 `k.plugins` 直达右花（见 `workflows/test_right_flower.yaml`） ✅

**不实现**（明确放弃）：
- `external_flower` 消息类型 — 右花通过 `kernel.Plugin` 标准接口注册，不需要专用协议消息。右花在系统中等同于普通 L4 插件，无需特殊步骤类型。

---


## 14. Go-DSL 执行器不走硬化层（设计决策）

`internal/workflow/gods_executor.go` 的 `callStep` 直接调 `kernel.Call`，不经 `ValidateAndExecute`。

**不是遗漏**：`ValidateAndExecute` 的防护目标是用户输入（路径穿越、命令注入）。Go-DSL 步骤由开发者定义在 Go 代码中，不含用户输入，该层防护在此无意义。

**边界**：若未来允许用户通过 UI 动态定义 Go-DSL 步骤，须重新评估此决策。

---

## 15. absorb_right_flower.yaml 是不可执行的参考文档

`workflows/absorb_right_flower.yaml` 是一个 14 步的参考文档型工作流，使用非标准步骤格式（`title`/`description`/`deliverables`/`checks` 而非 `plugin`/`type`/`timeout`/`on_error`）。标准 YAML 引擎无法执行此工作流。

**影响**：尝试通过 `{"workflow":"absorb_right_flower"}` 手动触发将导致引擎对每个空步骤执行 `kernel.Call("", "", input)`，行为未定义。

**这不是 AI 生成的错误**：该文件由人类编写作为吸收流程的参考指南。设计意图是让开发者沿着步骤清单手动执行，而非自动化执行。

**建议**：标示该文件的非可执行状态，或在引擎中添加对该格式的识别和跳过逻辑。

---

## 16. deleteReviewFile 不走 delete_file 工具（设计决策）

`plugins/review_handler.go` 的 `deleteReviewFile` 直接调 `os.Remove`，与 `saveReviewToFile` 走 `write_file` 工具不一致。

**不是安全问题**：删除路径由系统构造（`getReviewDir() + reviewID + ".json"`），`reviewID` 只来自系统生成的 ID，从不含用户输入，无路径穿越风险。

**何时替换**：等项目新增 `delete_file` 工具（处理用户发起的文件删除需求）时，顺手替换此处即可。

---

## 13. L2 胶水层对右花无感知（已缓解）

L2 glue 层原设计只管理子进程（Python/Go 插件）的 stdin/stdout IPC 生命周期。
右花使用 HTTP 通信，生命周期由**平台进程管理器**负责（macOS 上为 launchd，Linux 上为 systemd），
完全独立于 glue 的监控范围。

**影响**：
- 右花不可用时 glue 不感知，无自动恢复
- observatory Pulse 不包含右花健康数据
- 运维人员需要分别查看进程管理器 + glue 两层状态

**缓解**（2026-05-24 实现）：
- glue 新增 `RegisterRightFlower(name, healthEndpoint)` — 右花注册接口
- `RightFlowerStatus()` — 右花健康查询
- `healthCheckLoop` 统一检查子进程 + 右花，报告到 observatory Pulse
- 主启动流程自动注册 OpenHuman adapter 到 glue

**设计边界**：glue 不管理右花生命周期（由平台进程管理器负责），仅做状态感知和报告。
这是明确的分工——进程管理器保证进程活着（进程级），glue 保证健康可见（应用级）。

**跨平台注意**：如果你的部署环境无进程管理器（如裸容器），需要外部 watchdog 或
Docker restart policy (`--restart=always`) 来保证右花的进程级可用性。

---

## 17. goroutine panic 兜底未全覆盖（R1+R3，刻意分级）

裸 goroutine 里的 panic 不会被调用方 recover 捕获，会终止整个进程。R1 建了
`observatory.Recover/RecoverWith/SafeGo` 基础设施并接入 8 个调用点，R3 又覆盖了其余
跑应用逻辑的 goroutine（事件总线、调度器、知识后台、web 搜索、工作流异步/并行、通知、
会话落盘/故障切换）。下列三类曾被单列，**第 1、2 类已于 2026-05-29 修复，仅第 3 类刻意未覆盖**：

1. ~~**glue 长循环**（`glue/glue.go` 的 `healthCheckLoop` / `demuxLoop`）~~ —— **已修复（2026-05-29，
   逐迭代兜底）**。把循环体抽成单次处理函数（`demuxOne` / `runHealthCheck`），在函数顶部
   `defer observatory.Recover`。这样单次迭代 panic 被吞、循环存活——而非在循环顶部 `defer Recover`
   （那样 panic 会**杀死整个循环**，IPC demux / 健康检查静默停摆，比进程崩溃更隐蔽）。循环控制本身
   （`Scan()` / `<-ticker.C` / `<-stopHealth`）不会 panic，无需覆盖。

2. ~~**`kernel/kernel.go:246`** 的 `go notify.Callback(msg.ReplyTo, msg.Payload)`~~ —— **已修复
   （2026-05-29，在 kernel 外解决，kernel 未触碰）**。该 goroutine 执行体 100% 是
   `notify.Callback`（唯一非测试调用点就是这行 `go`），故在 `internal/notify/notify.go` 的
   `Callback` 顶部加 `defer observatory.Recover("notify.Callback")` 即等价兜底。用户当时已批准
   修改 kernel，但优先走了 kernel 外路径（详见 MERGE_DECISIONS 决策 15 与 DESIGN_PRINCIPLES
   「内核冻结治理」）。`notify` 不在冻结区，`notify → observatory` 无环。

3. **`done <- cmd.Run()` / `cmd.Wait()` 系列**（`terminal.go` / `media.go` / `code_exec.go` /
   `glue.go` 多处）——goroutine 体只调 `os/exec` 的 `Run`/`Wait`，stdlib 不 panic，加兜底
   纯属噪声，刻意跳过。

**影响**：第 1、2 类已修复后，仅剩第 3 类——而它跑的全是 `os/exec` 的 `Run`/`Wait`（stdlib 不 panic），
实际风险可忽略。panic 安全这条弧线（R1→R3→kernel.go:246→glue 循环）至此基本收口。

**缓解**：第 3 类风险极低（不 panic 的 stdlib）；第 1 类已于 2026-05-29 逐迭代兜底、第 2 类已在
`notify.Callback` 内兜底解决。新增持有 WaitGroup/channel 完成契约的 goroutine 时，务必遵守
LIFO——`Done`/send 的 defer 先注册（后执行），`Recover`/`RecoverWith` 后注册（先执行），
否则 panic 会让等待方死锁。新增**长循环** goroutine 时，兜底放在单次迭代函数顶部，**不要**放在循环顶部。

**审计印证（2026-05-29，Task H）**：随后做了一轮全量 goroutine-leak / HTTP-body / 吞错审计，
逐一读过全部 ~20 个 `go` 启动点，结论是**零泄漏**——每个要么是 buffered `done`(cap 1)、要么是
buffered-to-N 扇出 + `RecoverWith` 兜底发送、要么是带 `stop` 路径的 daemon 或进程生命周期级。
值得记的因果关系：这些扇出 goroutine 之所以 **leak-safe**，正是因为上面 R1→R3 给它们都加了
`RecoverWith` 兜底发送——panic 路径也会把 channel 发满 / WaitGroup 减完，于是消费方不会永久阻塞。
**panic 安全和 goroutine 不泄漏在这里是同一件事的两面。**（详见 devlog Task H。）

**补：Go-DSL 执行器 panic 已全面收口（2026-05-30，R1 + 启动降级）**——分两段，均已完成、不再是 limitation：

1. **请求路径（R1）**：`gods_executor.go` 的 `GoExecutor.Run`（**同步**执行路径）此前无 recover，
   开发者提供的 `TransformFn`/`BeforeExecute`/`AfterExecute` panic 会带栈中断请求。现 `Run` 加
   `defer RecoverWith`→panic 转失败 `WorkflowResult`；并行子步骤的 `Recover` 升级为 `RecoverWith`
   →panic 子步骤补记失败 `StepResult`（不再静默消失）。

2. **构造期（启动降级）**：该文件原另有 5 处 panic 在**构造期(boot)**——Go-DSL 工作流引用未注册/
   未配宿主的工具时**启动即崩、拖垮整个 daemon**。已全部**改为返回 error**：`validateGoStep`/
   `NewGoWorkflowPlugin`/`NewGoToolPlugin` 现返回 `error`，注册方（`legal_review_go_dsl.go`）拿到 error
   即 `observatory.RecordDegradation(...)` + 跳过注册，**daemon 照常启动其余功能**；`/health` 随之报
   `{"status":"degraded","degradations":[…]}`（仍 HTTP 200，degraded≠down），`scripts/daemon_drift.sh`
   会探活并提示。即「**核心 fail-fast，非核心降级**」原则（见 DESIGN_PRINCIPLES.md）。
   2 个注册点（原 `main.go:283` 的死示例已删、`main.go:288` legal_review）已切换。降级登记机制
   `observatory.RecordDegradation/Degradations/EventDegraded` 可复用于任何非核心模块的启动失败。

## 18. 版本戳用 git 短哈希——文档提交也会被漂移探测算作「二进制落后」（设计取舍，2026-05-30）

`scripts/daemon_drift.sh` 把 `/health` 自报的 `version`（编译期 `-ldflags` 注入的 **git 短哈希**）
与 `git rev-parse --short HEAD` 比对。短哈希是**整个 HEAD** 的标识，不区分这个 commit 改的是 `.go`
还是纯文档。于是：**部署后任何 docs-only 提交都会让 HEAD 前进、使探测报告「二进制 version != HEAD」**，
即便部署的二进制确实含最新代码（仅缺文档差异）。本次 Task N 自己就触发了这点——部署 `87bd73c` 后，
回写本节及 devlog 的 docs-only 提交立即让 HEAD 越过 `87bd73c`。

**影响**：漂移探测对「代码是否 stale」是**保守过近似**——它会把 docs 漂移也报成漂移，可能产生
「明明代码最新却报落后」的噪声。不会漏报真正的代码 stale（那是它的本职），只会多报文档差异。

**缓解**：看到「二进制 != HEAD」时，一条命令即可区分是代码 stale 还是纯文档差异：
`git diff <二进制version> HEAD -- '*.go'`——**输出为空 = 仅文档差异、无需重新部署**；非空 = 代码确实
落后、应跑 `deploy.sh`。刻意不做更精「只哈希影响构建的文件」的版本戳：那需要界定「哪些路径进构建」
并维护，复杂度远超收益；短哈希 + 一条 `git diff` 校验是更简单且够用的取舍（契合本项目「简单便用」取向）。