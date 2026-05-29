# 重要合并决策记录

本文件记录项目演进过程中的关键架构决策，以及被拒绝的方案和理由。
目的是让后来者理解"为什么系统是这个样子"，而不是"系统是什么"。

> **AI Summary:** 14 key architecture decisions. 
> #1: Four-layer architecture (L1 frozen). #7: Dual workflow engines (YAML + Go-DSL).
> #10: TwinFlower absorption (2095 lines, zero kernel changes). #11: Right flower evolution闭环.
> #12: Hermes absorption eval. #13: preRoute closure. #14: 拒绝全库强制 gofmt（`/* */` 块注释为有意风格）.
> MCP skill framework: 15 servers. Tools: 104. Right flowers: 3.

## 格式

每条决策记录包含：
- **时间** — 决策做出的时间
- **背景** — 当时面临的问题
- **决策** — 选择了什么方案
- **理由** — 为什么选这个
- **拒绝的方案** — 其他考虑过的方案及不选的理由
- **影响** — 这个决策带来的后续影响

---

## 1. 四层架构替代三层架构（2026-05-16）

### 背景
初期文档定义的是"三层架构"（第一核心/胶水层/插件），但实际代码已经演变为"四层架构"（L1 kernel/L2 glue/L3 internal/tools/L4 plugins）。L3 硬化职责掉落在 L3/L4 之间的缝隙中——L4 插件可以直接调用 `tools.Execute`，跳过了参数校验。

### 决策
正式确认为四层架构，冻结 L1，硬化 L3，L4 只能通过 `ValidateAndExecute` 调用 L3。

### 理由
- L4 直接调 `tools.Execute` 等于对 LLM 不可靠输出零防御
- 明确 L3 为"硬化的唯一关卡"，L4 为"只写业务逻辑，零防御代码"
- 防止硬化职责在层间缝隙中丢失

### 拒绝的方案
- **修复三层架构**：在胶水层加校验（胶水层不接触 Payload，违反 Payload 不透明原则）
- **在 L4 加校验**：每个 L4 插件各自校验（重复代码，不可集中管理）

### 影响
- L4 插件代码量减少（校验逻辑在 L3 集中处理）
- 新 L4 插件的编写门槛降低

---

## 2. 强制 DeepSeek 路由（2026-05-11）

### 背景
之前的版本（V9、Mini v2.0）最终都变成了规则型架构——LLM 被放在了不合适的位置，或者完全被规则替代。

### 决策
每条消息必须经过 DeepSeek 路由。不缓存决策。不给不走 AI 的快捷通道。DeepSeek 不可用时系统不降级为规则匹配。

### 理由
- 之前的每次"例外处理"都导致了架构向规则型退化
- LLM 是文本补全器，不是通用智能——但路由决策恰恰是文本补全最擅长的领域（将自然语言映射到工具名）
- 快捷方式是之前所有版本变成规则型的共同死因

### 拒绝的方案
- **规则优先路由**：先匹配关键词，匹配不到再走 LLM（现在仍保留 limited preroute，但仅限于极端高频意图）
- **缓存路由决策**：同一输入复用上次路由结果（不可复现的风险更大）

### 影响
- 每次交互的延迟增加了 1-2 次 LLM 调用
- 路由可靠性完全依赖 DeepSeek API 可用性
- 系统韧性不依赖路由正确性，依赖硬化层的拒绝能力

---

## 3. 引入 YAML 工作流引擎（2026-05-17）

### 背景
L4 编排器（legal_review）的手写 `kernel.Call` 链模式重复、维护成本高。需要一种允许 AI 动态修改编排的方式。

### 决策
引入 YAML 工作流引擎（`internal/workflow/engine.go`），工作流定义为 YAML 文件存放在 `workflows/` 目录。

### 理由
- YAML 格式允许 AI 直接生成和修改工作流定义
- 从 0 到 31 个 YAML 工作流的快速增长验证了灵活性
- 模板库（`workflows/templates/`）支持模式复用

### 拒绝的方案
- **Go struct 定义**：需要 Go 开发者修改和编译，AI 无法直接操作
- **JSON 定义**：可读性差，不支持注释

### 影响
- 工作流数量从 1 增长到 31，社区贡献门槛降低
- 需要配套的模板库和 eval 机制来保证质量
- AI 生成的工作流可能包含逻辑错误——硬化层只校验格式不校验逻辑

---

## 4. 引入 Go-DSL 工作流引擎（2026-05-23）

### 背景
YAML 引擎灵活但无类型安全——工具名拼写错误、Schema 过期、参数结构不符合预期等问题只在运行时暴露。需要一种编译时安全的替代方案用于核心硬化链。

### 决策
在同一个 `internal/workflow/` 包中引入 Go-DSL 引擎（`gods_executor.go`），与 YAML 引擎共享 `StepResult`/`WorkflowResult` 类型。

### 理由
- 编译时校验 Tool 注册表：`NewGoToolPlugin` 构造时检查所有 Tool 是否已注册，未注册 panic
- 与 YAML 引擎共享类型层，不创造第二套 StepResult/WorkflowResult
- 保持"代码优于配置"的原则——Go-DSL 用于编译时安全的静态硬化链

### 分工

| 维度 | YAML 引擎 | Go-DSL 引擎 |
|------|-----------|-------------|
| 使用者 | AI / 最终用户 | Go 开发者 |
| 定义方式 | YAML 文件 | Go struct |
| 类型安全 | 运行时检查 | 编译时检查 |
| 变更频率 | 高频（AI 可修改） | 低频（核心管道） |
| 定制逻辑 | 仅 plugin call | Before/AfterExecute 钩子 |
| 定位 | L4 高层编排 | L3/L4 静态硬化链 |

### 拒绝的方案
- **只用 YAML 引擎**：硬化逻辑散落在各 YAML 文件中，无法编译时校验
- **只用 Go-DSL**：AI 无法直接定义和修改工作流
- **JSON Schema + YAML 校验**：增加复杂度但无法覆盖所有检查

### 影响
- 新 L3 插件的创建门槛从"写一个 Go 文件"降到"定义 Step 数组"
- 需要维护 toolHost 映射（tool 名 → 宿主插件名）
- 复杂插件（如 search_plugin 含 LLM rewrite）不适合 Go-DSL，保留原实现

---

## 5. 不做编码智能体（2026-05）

### 背景
项目早期考虑过将 beishan-core 打造成编码智能体平台（类似 Claude Code），这个方向在当时的用户印象中有很高的优先级。

### 决策
不做编码智能体。只提供 `code_read`、`code_security_check`、`code_apply` 等安全工具作为 L3 工具。

### 理由
- 编码智能体是独立的问题域，有成熟的专门工具（Claude Code、Cursor 等）
- 编码智能体需要深度 IDE 集成、终端复用、diff 管理，与硬化层的"格式安全"定位冲突
- 分散硬化层核心壁垒——硬化层的价值在于"LLM 不决策"，编码智能体恰恰需要 LLM 大量决策

### 拒绝的方案
- **beishan-core 作为编码 IDE**：定位太重，核心壁垒被稀释
- **编码能力作为 L4 编排**：编码的决策密度远超法律审查，硬化层无法拦截逻辑错误

### 影响
- `code_read`/`code_security_check`/`code_apply` 作为 L3 工具使用
- 编码相关的工作流（code_deep_analyze、code_project_analyze）通过 YAML 编排，不侵入内核

---

## 6. Type 即意图，Payload 即数据（2026-05-16）

### 背景
Router 曾经允许 LLM 从 Payload 内容中推断路由目标，导致路由不可复现且硬化层无法验证。

### 决策
Type 是硬判断（路由只认 Type），Payload 是参数（Router 永不解析）。两者不可混淆。

### 理由
- 路由可复现：同 Type → 同目标，不受 Payload 内容影响
- 硬化层可审计：检查 Type 映射表即可验证路由正确性
- 韧性：DeepSeek 不可用时，硬编码 Type 的 L4 编排仍可走 IPC 调用 L3

### 拒绝的方案
- **模糊 Type + Payload 推断**：路由不可复现，硬化层拦不住
- **Router 解析 Payload**：违反内核冻结，Payload 不透明原则被破坏

### 影响
- L4 编排器必须显式指定 Type，不能"让 DeepSeek 猜"
- 新增插件必须在 main.go 中声明 Types 列表

---

## 8. 引入 Go-DSL 工作流引擎（2026-05-23）

### 背景
YAML 引擎灵活但无类型安全——工具名拼写错误、Schema 过期只在运行时暴露。

### 决策
在同一个 `internal/workflow/` 包中引入 Go-DSL 引擎（`gods_executor.go`），与 YAML 引擎共享 `StepResult`/`WorkflowResult` 类型。

### 理由
- 编译时校验 Tool 注册表：`NewGoToolPlugin` 构造时检查所有 Tool 是否已注册
- 保持"代码优于配置"原则

### 拒绝的方案
- 只用 YAML 引擎：硬化逻辑散落，无法编译时校验
- 只用 Go-DSL：AI 无法直接定义工作流

### 影响
- 新 L3 插件从"写一个 Go 文件"降到"定义 Step 数组"
- 需要维护 toolHost 映射

## 9. 引入 ErrorKind 错误分类（2026-05-23）

### 背景
Go-DSL 引擎对所有错误一视同仁重试，导致不可重试错误（如权限拒绝）也浪费重试次数。

### 决策
从 TwinFlower recovery/toolerror.go 提取 ErrorKind 6 类分类 + IsRetryable，整合进 Go-DSL 重试循环。

### 理由
- timeout/transient 重试，其余不重试——更智能的错误处理
- 改动集中在 1 个文件 73 行

### 影响
重试行为更精确，不可重试错误直接返回不浪费等待。

## 10. 吸收 TwinFlower 观测层 + 茎注册表 + 评估框架（2026-05-23）

### 背景
TwinFlower 融合方案经过两轮推演后确认：observatory(374行) + ErrorKind(73行) + file_safe(156行) + EWMA(30行) + 澄清契约(67行) + bench(480行) + evidence(210行) + 茎注册表(389行) + 3 工具(316行)。

### 决策
分两阶段执行。第一阶段（633 行）：observatory + ErrorKind/fallback + file_safe + EWMA。第二阶段（1,462 行）：澄清契约 + bench + evidence + 茎注册表 + 3 工具 + suites。

### 拒绝的方案
- 全量迁移 flowers/explore/ 所有代码（包含 shadow/drift/model_lab 等右花逻辑）→ 修正为只吸收底座能力，右花行为定义为协议

### 影响
- 新增 2,095 行代码，零 kernel 修改
- 所有新增代码在 internal/ 内，受 Go 编译强制隔离

## 11. 双花进化闭环：右花探路 → 左花吸收（2026-05-24）

### 背景
右花通车后，底座可以通过协议调用外部能力。但如果右花的能力一直停留在"通过协议远程调用"，左花永远不会变强。需要一个机制让右花的已验证能力被底座吸收内化。

### 决策
建立双花进化闭环：

```
右花提供能力 → 底座评估 → 值得吸收？ → 是：内化到 internal/ + hardening 覆盖
                                       → 否：保持协议调用
                                    → 吸收后：右花去探测下一个领域
```

### 吸收条件
1. 该能力在右花上稳定运行超过 2 周，无重大事故
2. 协议格式已固化，不随右花版本频繁变化
3. 有明确的硬化层校验方案
4. 不增加内核冻结区域的复杂度

### 拒绝的方案
- **右花即终结**：右花仅作为外部服务调用，永不吸收 → 拒绝原因是左花永远停滞，双花变成单花
- **全量吸收**：右花有什么就吸什么到底座 → 拒绝原因是底座膨胀，且右花社区更新后底座跟不上
- **vendor 依赖管理**：将右花作为 Go 依赖引入 → 拒绝原因是协议绑定，底座与具体项目耦合

### 影响
- 设计纪律（DESIGN_PRINCIPLES.md）新增"双花进化闭环"和"参考项目义务"章节
- 右花集成记录需标注吸收状态
- OpenHuman 被定义为"首个全链路通车的右花参考实现"，非唯一绑定
- TwinFlower 融合过程（#10 吸收）是此机制的第一个实际案例

## 11. 底座 + 双花定位确定（2026-05-23）

### 背景
之前的定位不清晰：beishan-core 既是底座又是左花，导致右花概念无处安放。

### 决策
beishan-core = 硬化底座 + 左花执行侧。右花是遵循 RIGHT_FLOWER_PROTOCOL.md 的外部工具。

- 底座 = kernel/ + glue/ + internal/
- 左花 = plugins/ + workflows/
- 右花 = 外部项目

### 影响
- docs/RIGHT_FLOWER_PROTOCOL.md 定义三层契约（通信/安全/注册）
- 右花代码不进底座仓库
- 首个真实右花接入时协议 bump 到 v1.0

---

## 7. 三项目融合路线（2026-05-22/23）

### 背景
beishan-core 作为主执行体，需要吸收 TwinFlower（认知侧）和 FangLab/66（营养层）的能力。核心问题：如何融合而不膨胀。

### 决策
选择性提取，分优先级：
1. **TwinFlower（low risk, 15-20 人天）**：types.go → recovery → 注册表门控 → 认知档案 → 工作流执行器 → 观测 → 路由
2. **FangLab/66（high risk, 30-40 人天）**：配置结构 → 基础类型 → 内存管理 → 工作流执行器（重设计）→ 交互层（重设计）→ 语义路由 → HTTP → CLI

### 理由
- TwinFlower 的认知层是 beishan-core 当前缺失的"大脑"能力
- FangLab 的 687 个 Go 文件中有大量与 beishan-core 重复的轮子（2 套工具注册表、2 套 Schema 验证、2 套 IPC）
- 外科手术式提取而非全量合并

### 拒绝的方案
- **TwinFlower 作为独立底座**：太重，且 OpenHuman 无法适配子进程模型
- **FangLab 全量合并**：冲突点过多（命名冲突 13+，依赖冲突 5+）

### 影响
- 融合尚未实际执行（已完成分析阶段）
- code_deep_analyze 工作流验证了"用自身分析待合并代码"策略

---

## 12. Hermes Agent 能力吸收评估（2026-05-25）

### 背景
Hermes Agent 作为第二个右花接入后，两个能力被标记为"已吸收"但未做 Step 3 验证：
- `model_providers` → `llm/config.go` + `think_plugin`
- `process_registry` → `glue/glue.go`

加上这次会话中讨论的 preRoute 功能待决策。

### 决策

#### model_providers → llm：部分吸收，当前够用
- Hermes 有 30+ 声明式 `ProviderProfile` 插件（bundled + user plugins），beishan-core 只有 4 个硬编码 provider
- 基本的多 provider 切换（`SetProvider`/`ChatCompletionWithProvider`）已吸收
- 缺失：声明式 profile 系统、插件式注册、per-provider hooks、自动 availability 降级
- **决定**：当前需求满足（DeepSeek + local 回退），gap 已知但不扩展。待需要动态 provider 注册时再处理。

#### process_registry → glue：范围偏差，非缺陷
- 两个系统解决不同问题：Hermes process_registry = 后台命令生命周期管理，beishan glue.go = Python 插件 IPC
- glue 的健康检查（30s 循环 + right flower HTTP health + observatory Pulse 集成）比 Hermes 更强
- 输出缓存、poll/wait/kill API、崩溃恢复等未吸收 → 因定位不同，非遗漏
- **决定**：评估通过，无需改动。

### 理由
- 逐步验证吸收质量，避免标记为"已吸收"但实际质量未知的债务
- 吸收评估逐能力不逐项目（Step 2 条件0）

### 拒绝的方案
- 全量移植 Hermes ProviderProfile 系统到 Go（成本太高，当前 4 个 provider 足够）
- 改造 glue.go 为通用进程管理器（定位不同，且 glue 的 IPC 职责清晰）

### 影响
- Router 调用加 usage 埋点（`callDeepSeek` 加 `RecordUsage`），为未来优化提供数据
- `docs/HANDOVER_NEXT_SESSION.md` 记录 P0 验证结论
- DESIGN_PRINCIPLES.md 参考项目部分新增 Hermes Agent 条目

---

## 13. preRoute 长度检测功能关闭（2026-05-25）

### 背景
考虑在 LLM Router 前加 preRoute 层：≤15 字查询跳过 LLM 路由，用硬编码规则快速分发。

### 决策
**关闭此功能。** 替代方案：给 Router 调用加 usage 埋点，等数据驱动后续优化。

### 理由
- 收益太小：预估每天省 ~¥0.012（基于 DeepSeek 价格），不值得为它弯曲架构
- 绕过硬化层：preRoute 的 Decision 不经过 `parseDecision` 的三层校验（JSON 格式→置信度→knownPlugin）
- 15 字阈值不可靠：短查询不等于简单路由（"帮我写个Python爬虫"11字但需要 think_plugin），长查询不一定需要 LLM 路由
- 维护成本：每新增一个插件可能需要同步更新 preRoute 规则，容易被忘

### 拒绝的方案
- **Router prompt 加短查询指令**：LLM 不保证听话，缓存命中不确定
- **Router 内部 caching**：重复查询场景少
- **降低阈值**：任何阈值都有边界案例，且 bypass 硬化层的架构问题不变

### 影响
- 改为给 `callDeepSeek` 加 `RecordUsage("router", ...)` 埋点（已实现）
- 之后跑几天可获得路由调用量的真实数据，瓶颈出现时再决策

---

## 14. 拒绝全库强制 gofmt（2026-05-29）

### 背景
R3 goroutine 兜底改动时发现：`gofmt -l .` 列出约 115 个文件"未格式化"。排查根因（读 `gofmt -d kernel/msg.go`）：
不是缩进/语法问题，而是项目大量使用手写对齐的 `/* ... */` 块注释作为散文式文档注释（约 76 个 `.go` 文件），
而 Go 1.26.1 的 gofmt 会把它们"规范化"——正文移出 `/*` 同行、续行改 tab 缩进，外加部分 struct tag 重排。
用 homebrew gofmt 和 go1.26.1 工具链 gofmt 双重验证，确认 115 这个数字是真实的、非版本错位。

### 决策
**不全库 `gofmt -w`，也不把 `gofmt -l` 加进 CI gate。** 把这套 `/* */` 散文块注释正式确认为**有意的项目风格**。
新代码靠手维持 gofmt-clean（漂移仅限"块注释规范化 + struct tag 对齐"两类纯排版）。

### 理由
- gofmt 的块注释规范化会让这些刻意排版的设计说明**更难读**（正文与 `/*` 分行 + tab 缩进破坏空格对齐）
- 全量重排会触碰**冻结的 `kernel/`** 仅为美观，违反内核冻结精神
- 一次性 115 文件的格式 churn 淹没真实变更历史，后续 `git blame` 与开发过程梳理都受害
- 收益是纯美观，代价是可读性下降 + 触碰冻结区 + 巨型 churn——不划算

### 拒绝的方案
- **全量 `gofmt -w` 一把再加 gate**：见上，得不偿失，且强制改 kernel/
- **只对新文件加 gofmt gate**：增量判定复杂（怎么界定"新"），且老文件漂移仍在，gate 输出仍噪声大
- **改用 `//` 行注释替换所有 `/* */`**：等于换一种方式重排 76 个文件，churn 同样巨大且降低可读性

### 影响
- `gofmt -l .` 输出约 115 文件是**预期且有意**的，不是疏忽——梳理开发过程时见到此数字请先读 `DESIGN_PRINCIPLES.md` 的"代码格式立场"节
- 若未来确要统一格式，是一个独立的、需评估的决策，且必然牵涉"是否愿意为美观修改 kernel/"，须走内核冻结批准流程
