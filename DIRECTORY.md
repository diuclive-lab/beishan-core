# 目录结构

本文件是项目代码布局的**物理地图**，与 `DESIGN_PRINCIPLES.md`（设计哲学）互补。
物理布局反映架构分层，但不等于架构分层——有些层（L1 kernel、L2 glue）在外层，
有些（L3 tools、L3 workflow）在 `internal/` 内。

## 顶层布局

| 目录/文件 | 职责 | 对应架构 |
|-----------|------|----------|
| `cmd/beishan/` | 生产可执行入口：组装内核、注册插件、启动 HTTP 服务 | L1 启动器 |
| `cmd/repl/` | 实验性交互式 REPL，仅开发调试用 | — |
| `kernel/` | 微内核：Plugin 接口、注册、消息路由、Router（DeepSeek 路由） | L1 |
| `glue/` | IPC 通信：子进程管理、manifest 扫描、JSON 行协议 | L2 |
| `internal/tools/` | 工具注册中心 + Schema 校验 + 硬化执行 + 安全检查（~120 个工具） | L3 |
| `internal/workflow/` | 双工作流引擎：YAML 引擎 + Go-DSL 引擎 | L3 |
| `internal/observatory/` | 决策追踪 + 健康检查 + 因果证据图 | L3 |
| `internal/bench/` | 通用评估框架（bench + runner + suites） | L3 |
| `internal/discovery/` | 本地推理引擎扫描 + 策略状态机 + 故障切换 | L3 |
| `internal/mcp/` | MCP 协议客户端框架（当前无外部 server 连接）| L3 |
| `internal/clarify/` | 澄清契约数据结构（Request/Response/BuildQuestion） | L3 |
| `internal/registry/` | 工具生命周期门控（PhaseInit→PhaseRunning）+ Profile 过滤 | L3 |
| `internal/llm/` | LLM 配置管理：API key、端点、模型选择、Router 提示词模板 + 线程安全 provider 切换 | L3 |
| `internal/retrieval/` | 知识检索：向量化嵌入、语义搜索、Query DSL 余量接口 | L3 |
| `internal/rightflower/` | 右花协议：Manifest 加载、HTTP dispatch、审计日志 | L3 |
| `internal/notify/` | 通知发送：邮件、Slack、企业微信 | L3 |
| `internal/llmguard/` | LLM 行为契约层：维度化 API + 结构/内容/事实三套契约 + Critique 自审 | L3 |
| `internal/tools/toolsets.go` | 11 工具组 + BuildToolsetSummary + ResolveToolset | L3 |
| `internal/tools/evidence_router.go` | 证据路由：规则引擎 + EWMA 自适应权重 + radix tree | L3 |
| `internal/tools/radixtree.go` + test | 压缩前缀树（高效前缀匹配） | L3 |
| `internal/tools/workspace.go` | 跨会话工作状态（workspace_save/load + 自动注入） | L3 |
| `internal/tools/storage.go` | 存储适配器（StorageAdapter）：JSONStorage + BlockStorage 双实现 | L3 |
| `internal/tools/link_index.go` | 自动双向链接索引（[[wikilink]] 解析 + UpdateBacklinks） | L3 |
| `internal/tools/knowledge_graph.go` | 知识图谱（BuildLocalGraph / BuildGlobalGraph） | L3 |
| `internal/tools/fact_check.go` | 事实核查（UngroundedNumbersWarn / StockCodeVerify / URLVerify） | L3 |
| `internal/tools/filesystem_tools.go` | 8 个文件系统工具（guess_type/preview/archive/csv/json/stat/compare/extract） | L3 |
| `plugins/session.go` | 会话状态机 SessionManager：Idle/RememberPending/ReviewPending 状态 | L4 |
| `plugins/` | 生产级 L4 插件：search、write、memory、legal 审查簇、workflow 编排等 | L4 |
| `cmd/rightflower-python-wrapper/` | Python 右花接入模板 + Hermes Agent / OpenClaw 适配器 | 右花 |
| `cmd/knowledge-migrate/` | 知识库迁移工具（硬件前缀 strip + 格式转换） | 工具 |
| `cmd/beishan-mcp/` | MCP 服务器，供 Claude Desktop 等 MCP 客户端调用 beishan-core 工具 | 工具 |
| `workflows/` | YAML 工作流定义文件，被 `workflow_plugin` 加载执行 | L4 编排 |
| `scripts/` | 辅助脚本：集成检查、桌面操作、文档提取 | 工具 |
| `eval/` | 烟雾测试：场景定义、运行脚本、测试结果 | 测试 |
| `examples/` | 开发/测试用示例插件（Go + Python 子进程模板） | 参考 |
| `docs/` | 设计文档：架构决策、硬化层声明、已知限制、治理框架、审查标准、产品愿景 | 文档 |
| `docs/devlog/` | 开发日志，按日期记录内部开发过程 | 过程档案 |
| `docs/ABSORPTION_GOVERNANCE.md` | 吸收治理框架：证据等级、吸收等级、风险分类、升级策略 | 治理根茎 |
| `docs/V25_WORKFLOW_STANDARD.md` | v2.5 YAML 工作流参考标准：强制项、条件项、禁止项、骨架模板 | 治理标准 |
| `docs/CODE_REVIEW_SPEC.md` | 代码审查规范：三层验证体系 + AI 高频错误模式 13 条 | 审查标准 |
| `docs/PRODUCT_VISION.md` | 产品定位与三阶段演进路线 | 产品方向 |
| `docs/CODING_RHYTHM.md` | 代码审计方法论：审计→诊断→执行→验证四步闭环 | 工程规范 |
| `docs/plans/` | 吸收方案、执行计划等阶段性文档 | 计划 |
| `providers.json` | 声明式多 Provider 配置（failover 模型等），由 LLM_PROVIDERS_CONFIG 加载 | 配置 |

## 关键设计决策

### `kernel/` 和 `glue/` 为什么不在 `internal/` 内

这是 Go 编译约束的权衡。`internal/` 包只能被同模块内的代码导入，而 kernel 和 glue
需要被外部项目引用（如 beishan-core 的子进程插件）。放在根目录使它们可被公开导入，
同时在 `DESIGN_PRINCIPLES.md` 中约定"冻结不改"来防御滥用。

### `cmd/beishan/` 是唯一生产入口

`cmd/repl/` 仅供开发调试，不编译进生产部署。`main.go`、`preroute.go`、`legal_review_go_dsl.go`
都属于应用组装逻辑（选择哪些插件、配置路由规则、定义 Go-DSL 工作流），不属于内核实现。

### `preroute.go` 在 `cmd/beishan/` 不在 `kernel/`

`preroute.go` 实现的是确定性预路由——高频意图关键词匹配跳过 DeepSeek 调用。
它是应用层的路由策略（选择"什么情况走 preroute"），不是内核路由机制（"怎么路由"）。
所以放在 `cmd/beishan/` 作为组装的一部分，不在 `kernel/router.go` 中。

### `workflows/` 与 `internal/workflow/` 分离

- `workflows/` 是**数据**：YAML 格式的工作流定义，AI 可直接生成和修改
- `internal/workflow/` 是**引擎**：读取 YAML 或 Go struct 定义并执行的 Go 代码

两者分离使数据修改不涉及代码变更，代码修改不影响现有工作流。

### `plugins/` 只包含生产级插件

开发/测试用示例统一放在 `examples/` 目录中。

## 架构层到目录的映射

```
L1 内核层    kernel/         （冻结不改）
L2 胶水层    glue/           （可迭代）
L3 工具层    internal/tools/ （可迭代，硬化关卡）
             internal/workflow/
             internal/llm/
             internal/retrieval/
L4 编排层    plugins/        （随意改）
              workflows/     （AI 可改）
应用入口    cmd/beishan/     （胶水代码，选型与装配）
```

## 不受版本控制的目录

以下目录被 `.gitignore` 排除：

| 目录 | 原因 |
|------|------|
| `eval/run/logs/` | 运行时临时日志 |
| `eval/run/pids/` | 运行时进程 PID 文件 |
| `generated/` | 自动生成的文件 |
| `*_backup/` | 外部系统备份（Claude 记忆、Codex 会话等） |
| `.gocache/` | Go 编译缓存 |
| `imports/` | 外部导入临时目录 |
