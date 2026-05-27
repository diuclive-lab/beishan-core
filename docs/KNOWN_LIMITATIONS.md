# 已知限制与设计边界

本文件诚实声明 beishan-core 的设计边界和已知限制。
和硬化层一样，文档的完整性和透明性是项目信任的基础。

---

> **AI Summary:** 14 known limitations. Current: 104 tools, 15 MCP skills, 3 right flowers, 38 workflows.
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

`internal/registry/` 的 `Policy.Filter()` 已实现但未接入 Go-DSL 的 `validateGoStep`。当前只使用了生命周期门控 `Lock()`，角色过滤能力待激活。

**影响**：理论上有 profile 过滤能力，实际上所有工具对所有角色可见。

**缓解**：`validateGoStep` 已有 TODO 注释，接入后开启。

---

## 11. bench 评估框架无自动化流水线

`internal/bench/` 已就绪（3 个套件 + runner），但未接入 CI 或定时执行。评估框架目前只能手动触发。

**影响**：能力退化需要通过手动运行烟雾测试发现，无自动化预警。

**缓解**：接入 CI 的 `eval/scripts/` 流程。

---

## 12. 右花协议未实现

`docs/RIGHT_FLOWER_PROTOCOL.md` 已定义三层契约（通信/安全/注册）。

**代码层已实现（2026-05-24）**：
- `internal/rightflower/plugin.go` — RegisterAll 扫描 `right_flowers/` 目录 ✅
- `right_flowers/openhuman.yaml` — 首个右花 manifest 已激活 ✅
- adapter 桥接 + dispatch + probe-methods 已贯通 ✅

**未实现**：
- glue/protocol.go 无 `external_flower` 消息类型
- 工作流不支持 `external_flower` 步骤类型

**缓解**：右花的 glue 集成目前通过 `RegisterRightFlower` 健康监控实现，协议层的完整步骤类型待后续。

---

## 13. Go 版本差异

开发环境（Desktop/0）使用 **Go 1.26**，公开仓库（beishan-core）使用 **Go 1.21**。
部分新标准库特性和语法糖不能在公开仓库中使用。

**影响**：同步代码时需要手动适配 Go 版本差异。

**缓解**：
- 公开仓库保持 Go 1.21 兼容
- 新特性先在开发环境验证


## 15. Go-DSL 执行器不走硬化层（设计决策）

`internal/workflow/gods_executor.go` 的 `callStep` 直接调 `kernel.Call`，不经 `ValidateAndExecute`。

**不是遗漏**：`ValidateAndExecute` 的防护目标是用户输入（路径穿越、命令注入）。Go-DSL 步骤由开发者定义在 Go 代码中，不含用户输入，该层防护在此无意义。

**边界**：若未来允许用户通过 UI 动态定义 Go-DSL 步骤，须重新评估此决策。

---

## 16. deleteReviewFile 不走 delete_file 工具（设计决策）

`plugins/review_handler.go` 的 `deleteReviewFile` 直接调 `os.Remove`，与 `saveReviewToFile` 走 `write_file` 工具不一致。

**不是安全问题**：删除路径由系统构造（`getReviewDir() + reviewID + ".json"`），`reviewID` 只来自系统生成的 ID，从不含用户输入，无路径穿越风险。

**何时替换**：等项目新增 `delete_file` 工具（处理用户发起的文件删除需求）时，顺手替换此处即可。

---

## 14. L2 胶水层对右花无感知（已缓解）

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