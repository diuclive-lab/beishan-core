# 工作记录

## 2026-05-23 项目重构与文档体系建立

### 1. Go-DSL 工作流引擎

**背景**：L3 插件手写 OnMessage switch-case 模式重复。YAML 引擎灵活但无编译时安全。

**交付**：internal/workflow/gods_executor.go（~290 行）
- GoExecutor：编译时安全的 Go-DSL 工作流执行器
- GoWorkflowPlugin：kernel.Plugin 适配器
- NewGoToolPlugin：一行代码注册简单 L3 插件
- StateStore：map 级路径取值（Get("step.field.nested")），无字符串替换

**架构约束**：
1. 编译时校验 Tool 注册表（validateGoStep → tools.GetToolSchema）
2. 零冗余校验 — 通过 kernel.Call 走标准路由，信任 L3 硬化层
3. Before/AfterExecute 约定不做 I/O

**文件**：
- internal/workflow/types.go（+80 行：GoStep、GoWorkflow、StepStatus、ErrorStrategy）
- internal/workflow/gods_executor.go（新增）
- cmd/beishan/legal_review_go_dsl.go（新增：legal_review 的 Go-DSL 声明式版本）

### 2. 目录结构重构

**背景**：168+ 次提交后，根目录混杂二进制文件、备份目录、开发示例。Go 标准布局未落地。

**变更**：
- main.go + preroute.go → cmd/beishan/
- web/ → cmd/beishan/web/（go:embed 路径自动跟随）
- 5 个开发示例（go_example、python_example、l3_echo_*、l4_template_go.go）→ examples/
- 4 个 DEVLOG → docs/devlog/
- .gitignore 补全：eval/run/logs/、eval/run/pids/、generated/、repl、examples 二进制
- PERSONAL_KB_GUIDE.md、PLAN_CODING_AGENT.md 取消跟踪（gitignore）
- .github/workflows/ 恢复跟踪（CI 配置对外可见）

**根目录最终状态**（5 个文件）：
CHANGELOG.md  DIRECTORY.md  DESIGN_PRINCIPLES.md  go.mod  .gitignore

### 3. 文档体系建设

| 文档 | 内容 |
|------|------|
| README.md | 项目介绍、架构图、快速开始、配置表 |
| DIRECTORY.md | 目录结构声明、每层到每目录映射、关键决策 |
| LICENSE | MIT 开源许可证 |
| .env.example | 环境变量模板 |
| docs/HARDENING_LAYER.md | 硬化层三层关卡、保证/不保证表 |
| docs/MERGE_DECISIONS.md | 7 项关键决策记录 |
| docs/KNOWN_LIMITATIONS.md | 10 项已知设计边界 |
| DESIGN_PRINCIPLES.md | 文档导航表、双引擎说明补全 |

### 4. Bug 修复

- **legal_review 工作流**：消息类型 legal_write → legal_generate_report，字段名 analysis → analysis_report
- **legal_write_plugin**：兼容 workflow 引擎的 string-encoded JSON 传递
- **todo_plugin**：返回空 Message → 返回带 Type+Payload 的响应

### 5. 测试验证

全功能回归测试全部 PASS：Health / Chat / Web search/fetch / code_security / Knowledge / Todo / Legal review YAML 4/4 / Go-DSL

### 6. 未完成

| 事项 | 状态 | 说明 |
|------|------|------|
| 三项目融合（TwinFlower + FangLab） | 已分析未执行 | TwinFlower 认知层 low risk, 15-20 人天；FangLab high risk, 30-40 人天 |
| 插件开发指南 | 未创建 | docs/PLUGIN_DEVELOPMENT.md，说明 L3/L4 插件开发规范 |
| 模块名对齐 | 待决策 | go.mod 中 `module beishan` vs 仓库路径 `github.com/diuclive-lab/beishan-core`，修改涉及全部 import |
| workflows/parking/ | 未决定 | 4 个原型工作流（parking_report、parking_stats、vehicle_entry/exit），清理或保留 |
| 右花接入规范 | 讨论待执行 | 外部工具（Claude CLI、Cursor 等）通过硬化层接入的协议定义，等第一个真实接入方出现时再做 |
| 知识库治理 | 待执行 | embedding 批量补 + 重复合并（memory 中已有项目计划）|
| code_security 规则扩展 | 持续 | 覆盖更多危险模式（SQL 注入检测、网络反弹 shell 等）|
| hardening layer 完备性检验 | 待做 | 经过真实外部工具的端到端验证 |

---

## 2026-05-23 TwinFlower 融合第二阶段（下午）

### 交付统计

| 模块 | 内容 | 行数 | 状态 |
|------|------|------|------|
| C: 澄清契约 | internal/clarify/types.go + clarifyHandler 结构化返回 | +67 | ✅ |
| C: 澄清工作流 | workflows/clarify_learn.yaml | +8 | ✅ |
| D1: bench 评估框架 | internal/bench/{bench,runner}.go | +318 | ✅ 框架就绪，suites 待补 |
| D2: evidence 因果追踪 | internal/observatory/evidence.go | +210 | ✅ |
| A: 茎注册表 | internal/registry/{registry,profile,toolset,metadata}.go | +389 | ✅ |
| B: 3 个工具 | internal/tools/{weather,translate,currency}.go | +327 | ⚠️ 代码已迁入，未注册为 L3 工具 |
| 合计 | 14 个文件 | +1,319 | — |

### 未完成（Phase 2 剩余）

- **B: 3 个工具 L3 注册** — weather/translate/currency 代码已迁入但尚未注册为 L3 工具（需要写适配器包装 `Run(ctx,args)→*ToolResult`，并在 tools.Init() 中调用 `Register()`）
- **D1: bench suites 迁移** — TwinFlower 有 3 个现成的 eval suites（filesystem/search/clarify，共 160 行），尚未迁入 internal/bench/suites.go
- **A: 茎注册表 Go-DSL 集成** — `Policy.Filter()` 可用于 Go-DSL 的运行时工具过滤，尚未接入 `validateGoStep`

### 冲突处理

| 预期冲突 | 实际情况 |
|---------|---------|
| registry.Register 签名与 tools.Register 不兼容 | 简化方案：只取 Lock()+Filter()，注册仍走 tools.Register |
| weather 等工具的 Tool 类型名冲突 | 重命名 WeatherTool/TranslateTool/CurrencyTool |
| clarify schema 拦截 format 字段 | 已加到 clarify 的 properties 中 |

### 测试

- clarify 纯文本返回 ✅
- clarify structured JSON 返回（`needs_clarify=True`）✅
- legal_review YAML 4/4 ✅
- legal_review_v2 Go-DSL 4/4 ✅
- 编译 `go build ./...` ✅

---

## 2026-05-23 TwinFlower 融合执行（下午）

### 1. observatory 决策追踪（internal/observatory/，+374 行）

从 TwinFlower 全量迁移决策追踪模块，字段适配：
- `Skill` → `Plugin`，`WhyRouted` → `RouteReason`，`BySkill` → `ByPlugin`
- `TraceID` 复用 glue/protocol.go 现有字段

文件：`trace.go`(187行)、`metrics.go`(135行)、`health.go`(52行)

### 2. ErrorKind 错误分类（internal/workflow/gods_error.go，+73 行）

6 类错误：timeout / transient / permission / dependency / input / internal
- `ClassifyError()` 自动分类，`IsRetryable()` 判断是否可重试
- `GoStep` 新增 `Fallback` 字段，降级尝试在重试循环内部
- `callStep()` 重构抽离单次执行逻辑

### 3. 文件安全 L3 工具（internal/tools/file_safe.go，+156 行）

- `validate_file_op`：操作类型（读/写/删）+ 路径白名单校验
- `lock_file` / `unlock_file`：基于互斥锁的并发保护
- tools 注册数 93 → 96

### 4. 中文歧义字典（internal/tools/search_disambiguate.json）

从 TwinFlower search_skill 提取：苹果/小米/华为等歧义词对 + 消歧上下文词

### 5. EWMA 衰减算法（internal/tools/clarify.go）

- `userPattern` 新增 `LastSeen` 时间戳
- `resolve()` 置信度超过 7 天未观察则衰减（`conf *= 0.5 * (7/days)`）
- 向后兼容：旧 JSON 模式文件读入后 LastSeen=0，衰减不触发

### 6. 修正的记录

- Go-DSL resolveSource 对 `Field: "output"` 的处理：原来把 "output" 当字段路径取，改为直接返回 `StepResult.Output`
- legal_review_go_dsl.go 的 Merge 字段补充完整（clause_analysis 和 write_report）
- resolveSource 暴露 `state.results` map 供直接取值

### 7. 测试验证

| 测试项 | 结果 |
|--------|------|
| Go build ./... + vet | ✅ |
| Health / Chat / Web | ✅ |
| code_security_check（rm -rf 拦截）| ✅ |
| validate_file_op（允许/拦截）| ✅ |
| knowledge_search / add | ✅ |
| todo_add / list | ✅ |
| clarify / clarify_learn | ✅ |
| YAML legal_review 4/4 | ✅ |
| Go-DSL legal_review_v2 4/4 | ✅ |
| 全量 15 项 | ✅ 100% PASS |

### 8. 未完成（更新后）

- 步骤 3 中的 `plugins/filesystem_plugin.go` 和 `plugins/search_skill_plugin.go` 未创建（低优先级，已有覆盖）
- `internal/cognition/` 模板目录未创建（等 clarify 需要时再做）
- docs/TWINFLOWER_MERGE_PLAN.md 已更新为已完成状态


---

## 2026-05-23 晚间：Core-R1/R2 硬化基线冻结 + 右花协议实现

### Core-R1：硬化底座基线冻结

**交付**：

| 脚本 | 内容 |
|------|------|
| eval/scripts/check_hardening_invariants.sh | 8 项不变性测试（编译/vet/tools隔离/注册表/格式化） |
| eval/scripts/scan_boundary.sh | 3 项边界扫描（tools.Execute/文件系统/Payload解析） |

**扫描发现的遗留问题**（已知，非本次修复范围）：
- think_plugin.go 直接 os.ReadFile — 应走 ValidateAndExecute
- review_handler.go 直接 os.WriteFile — 应走 code_apply
- skill_factory_plugin.go 直接操作 YAML 文件 — 工作流管理器固有行为

### Core-R2：右花协议 v0 基准实现

**交付**：internal/rightflower/ 包

| 组件 | 文件 |
|------|------|
| 类型定义 | manifest.go（Manifest/Request/Response/Result/Finding） |
| YAML 加载 + HTTP 客户端 | client.go（Registry.LoadDir + Client.Dispatch） |
| kernel.Plugin 适配器 | plugin.go（Plugin.OnMessage + RegisterAll） |
| 注册目录 | right_flowers/README.md |

**集成**：main.go 启动时调用 rightflower.RegisterAll(k, "./right_flowers")

### Core-R2-fix：右花协议契约硬化

审查发现 5 项问题全部修复：

| Finding | 修复 |
|---------|------|
| ValidateManifest 缺失 | 全量校验：protocol/endpoint/format/safety/capabilities，v0 仅 localhost |
| HTTP 非 2xx 未处理 | HTTPError{StatusCode, Body} typed error |
| Request 与文档不一致 | payload 传 json.RawMessage 而非 stringified JSON |
| 文档仍写"未实现" | 改为"基准已实现，external_flower 步骤未实现" |
| RegisterAll 错误被忽略 | log.Printf 错误，右花可选不影响启动 |

### 术语修正

| 文档 | 修正 |
|------|------|
| README.md | "架构三层" → "产品形态三分" + "代码架构四层"，首轮路由图补充 DeepSeek/Type 关系 |
| DESIGN_PRINCIPLES.md | 铁律二改为"仅首轮"，"没有快捷方式"明确首轮路由语义 |
| RIGHT_FLOWER_PROTOCOL.md | 顶部 ⚠️ v0.1 草案标记 |
| L1 描述 | "注册 + 路由（强制 DeepSeek）" → "注册 + 首轮 AI 路由 + Type 转发" |

### 文档一致性修复

| 文档 | 更新内容 |
|------|---------|
| HARDENING_LAYER.md | +file_safe 工具条目 |
| MERGE_DECISIONS.md | +4 项决策（Go-DSL/ErrorKind/TwinFlower融合/底座+双花） |
| KNOWN_LIMITATIONS.md | +3 项（registry未启用/bench无自动化/右花协议未实现） |
| 根目录 | 删除 PERSONAL_KB_GUIDE.md + PLAN_CODING_AGENT.md |

### 最终统计

| 指标 | 数值 |
|------|------|
| 提交 | 198 |
| Go 文件 | 98 |
| 跟踪文件 | 291 |
| 注册工具 | 96 |
| 插件 | 22 |
| YAML 工作流 | 33 |
| 文档 | 13 份 |
| 编译 | go build ./... ✅ |


---

## 2026-05-23 深夜：Core 稳定性修复 + 右花协议闭环

### Core 稳定性修复（4 个 P2）

| 问题 | 修复 |
|------|------|
| route_exposed=false 仍进首轮路由 | 新增 kernel.RegisterUnlisted，不调 AddKnownPlugin |
| fake_example.yaml 默认启用 | → .yaml.example |
| scan_boundary 无 known debt 区分 | 排除 _test.go + D01-D03 allowlist |
| core-health 二进制进 git | git rm + .gitignore 追加 /core-health |

### 10 轮方案全部完成

| 轮次 | 交付 | 行数 |
|------|------|------|
| S1 | OpenHuman Method Mapping（4 方法 + 未知拒绝） | +84 |
| S2+S3 | Enable Gate + Route Safety（enabled/route_exposed 字段） | +21 |
| S4 | External Flower Workflow Step（test_right_flower.yaml） | +12 |
| S5 | Evidence Contract（Kind/Flower/Method 字段） | +10 |
| S6 | Core Health（cmd/core-health 轻量检查） | +80 |
| S7 | Boundary Debt Register（docs/reports/boundary_debt_register.md） | +20 |
| S8 | Kernel Payload Freeze Test（3 个 parseDecision 测试） | +45 |
| S9 | FangLab Nutrient Inventory（吸收分类文档） | +25 |
| S10 | OpenHuman Integration Record（边界/协议/安全文档） | +35 |

### OpenHuman 适配器完整状态

| 能力 | 状态 |
|------|------|
| JSON-RPC 2.0 协议 | ✅ |
| Bearer token auth（OPENHUMAN_TOKEN） | ✅ |
| 4 方法映射（memory.search/store/context.retrieve/code.review） | ✅ |
| 未知 method 返回 unsupported | ✅ |
| 非 2xx 分类为调用失败 | ✅ |
| dispatch 时 probe 存活 | ✅ |
| 单测 10 个 | ✅ |

### 最终统计

| 指标 | 数值 |
|------|------|
| 提交 | 217 |
| Go 文件 | 105 |
| 跟踪文件 | 317 |
| 测试 | 21/21 PASS |
| 根目录 | 0 未跟踪文件 |
| 边界扫描 | ✅ 全部通过 |


### 后续可改（当前不阻塞）

1. `scan_boundary.sh` allowlist 精确到行级/模式级，避免同一文件新增未知违规被当成已知债务
2. `TestKernelDoesNotParsePayload` 改为 fake plugin `Kernel.Send` 回路测试，明确断言插件收到字节未变形


---

## 2026-05-23 S11-S20：Core 稳定性 + 右花完整闭环

### 交付清单

| 轮次 | 交付 | 测试 |
|------|------|------|
| S11 | Core Health Contract — BuildHealthReport + runner 接口 | 4 |
| S12 | rightflowerctl — list/enable/disable/validate | 手动 |
| S13 | Route Prompt Test — RegisterUnlisted 不在 prompt | 3 |
| S14 | Precise Boundary Allowlist — eval/boundary_allowlist.yaml | 扫描器 |
| S15 | Kernel Payload Behavior — fake plugin 回路验证 | 1 |
| S16 | (预留 OpenHuman probe) | — |
| S17 | Response Normalizer — JSON-RPC/JSON/文本识别 | 内置 |
| S18 | Runtime Audit Log — ~/.hermes/runtime/rightflower/*.jsonl | 集成 |
| S19 | FangLab Nutrient doc — 已记录 | — |
| S20 | RightFlower Smoke Gate — 7/7 检查 | 7/7 |

### 测试统计（最终）

| 包 | 测试数 | 通过率 |
|----|--------|--------|
| kernel | 8 | 100% |
| internal/rightflower | 7 | 100% |
| cmd/openhuman-flower-adapter | 10 | 100% |
| cmd/core-health | 4 | 100% |
| 右花烟雾门禁 | 7项 | 100% |
| 边界扫描 | 3项 | 通过 |

### 项目最终统计

| 指标 | 数值 |
|------|------|
| 提交 | 224 |
| Go 文件 | 107 |
| 跟踪文件 | 327 |
| 测试包 | 4 |
| 测试总数 | 29 |
| 根目录未跟踪 | 0 |
