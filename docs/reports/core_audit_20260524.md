beishan-core 全量审查报告
=======================

## 1. 项目概览

| 指标 | 数值 |
|------|------|
| 提交数 | 238 |
| Go 文件 |      115 |
| 跟踪文件 |      326 |
| 文档 |       26 |
| 核心工程师 | 1 |


## 2. 架构

| 层 | 目录 | 职责 | 文件数 |
|---|------|------|--------|
| L1 内核 | kernel/ | 注册 + 首轮 AI 路由 + Type 转发 + LocalRouteStrategy |        5 |
| L2 胶水 | glue/ | IPC + 子进程管理 |        3 |
| L3 工具 | internal/tools/ | 96 个注册工具 + Schema 校验 |       43 |
| L3 工作流 | internal/workflow/ | YAML + Go-DSL 双引擎 |        4 |
| L3 观测 | internal/observatory/ | 决策追踪 + 健康检查 + 故障分类 |        4 |
| L3 发现 | internal/discovery/ | 本地引擎扫描 + 策略状态机 + 故障切换 |        5 |
| L3 评估 | internal/bench/ | 评估框架 + 3 套件 |        3 |
| L3 注册 | internal/registry/ | 生命周期门控 |        4 |
| L3 右花 | internal/rightflower/ | HTTP 客户端 + 审计 |        5 |
| L3 澄清 | internal/clarify/ | 结构化契约 |        1 |
| L4 编排 | plugins/ | 22 个内置插件 |       22 |
| L4 工作流 | workflows/ | 34 个 YAML 工作流 |       34 |
| 入口 | cmd/beishan/ | HTTP 服务 :8013，故障切换监控 |        3 |
| 入口 | cmd/core-health/ | 健康检查 |        3 |
| 入口 | cmd/rightflowerctl/ | 右花管理 CLI |        2 |
| 入口 | cmd/openhuman-flower-adapter/ | OpenHuman 桥接 |        2 |
| 入口 | cmd/core-eval/ | 评估执行器 |        1 |


## 3. 测试结果

| 套件 | 结果 |
|------|------|
| go build ./... | ✅ |
| go vet ./... | ✅ |
| go test ./... (26 包) | ✅ |
| 功能测试 (12 项) | 12/12 ✅ |
| Core Gate | 7/7 ✅ |
| 边界扫描 | ✅ (D01-D03 known debt) |
| 文档一致性 | 4/4 ✅ |
| 右花烟雾门禁 | 7/7 ✅ |

### 测试覆盖

| 包 | 测试数 | 覆盖内容 |
|----|--------|---------|
| kernel | 8 | Payload 冻结、parseDecision 三层校验、RegisterUnlisted、路由回路 |
| internal/discovery | 15 | 11 引擎扫描 + StrategyState 全链路(e2e/防抖动/状态/回切) |
| internal/rightflower | 8 | Manifest 校验(4)、HTTP 错误、payload contract、evidence、security |
| internal/llm | 3 | RouterPrompt、ModelDefault、ProviderName |
| cmd/openhuman-flower-adapter | 16 | Method 映射(3)、params/bearer/status、config、probe、normalizer(5) |
| cmd/core-health | 4 | clean/dirty/build fail/status |
| cmd/rightflowerctl | 3 | enable/remote reject/disable |


## 4. 右花协议状态

| 能力 | 状态 |
|------|------|
| YAML manifest 加载 | ✅ |
| HTTP dispatch | ✅ |
| JSON-RPC 2.0 | ✅ |
| Bearer token auth | ✅ |
| Method 映射 (4 个) | ✅ |
| 未知 method 拒绝 | ✅ |
| 非 2xx 分类 | ✅ |
| dispatch-time probe | ✅ |
| Response normalizer | ✅ |
| Runtime audit log | ✅ |
| RegisterUnlisted (不进 prompt) | ✅ |
| Enabled/route_exposed 门控 | ✅ |
| 文档 schema JSON | ✅ |


## 5. 安全硬化

| 防线 | 实现 | 验证 |
|------|------|------|
| 路由校验 | parseDecision (JSON + confidence + knownPlugins) | kernel 测试 ✅ |
| Schema 校验 | ValidateParams (类型 + 必填 + 未知字段) | 硬化测试 ✅ |
| 路径安全 | isSafePath (/etc/passwd/../ 拦截) | curl ✅ |
| 命令安全 | code_security (8 条规则) | curl ✅ |
| 文件操作安全 | validate_file_op (read/write/delete) | curl ✅ |
| 文件并发锁 | lock_file/unlock_file | 集成 ✅ |
| Kernel Payload 冻结 | 不解析 Payload 内容 | 8 个测试 ✅ |
| 边界扫描 | 层违反检测 | D01-D03 known debt ✅ |


## 6. 工具集

| 类别 | 数量 | 说明 |
|------|------|------|
| 注册 L3 工具 | 96 | 搜索/文件/知识/代码/天气/翻译/汇率等 |
| 内置插件 | 22 | think/search/write/memory/legal 簇等 |
| YAML 工作流 | 34 | legal_review/code_deep_analyze 等 |
| 右花 manifests | 2 (.example) | fake_example + openhuman |
| Smoke 场景 | 6 | legal/core/retrieval/personal_knowledge/workflow/clarify |


## 7. 硬化层不变性（8 项）

  [1] go build ./... ✅
  [2] go vet ./... ✅
  [3] tools.Execute 绕过检查 ✅
  [4] isSafePath 存在 ✅
  [5] code_security 规则数 ✅ (8 条)
  [6] registry.Lock 已调用 ✅
  [7] validate_file_op 已注册 ✅
  [8] clarify structured 已支持 ✅


## 8. 文档体系

- CHANGELOG.md (    1671 行, CHANGELOG.md)
- DATASET.md (     522 行, DATASET.md)
- DESIGN_PRINCIPLES.md (     182 行, DESIGN_PRINCIPLES.md)
- DIRECTORY.md (      86 行, DIRECTORY.md)
- DEVLOG_20260520.md (    1685 行, docs/devlog/DEVLOG_20260520.md)
- DEVLOG_20260521.md (     920 行, docs/devlog/DEVLOG_20260521.md)
- DEVLOG_20260522.md (    1664 行, docs/devlog/DEVLOG_20260522.md)
- DEVLOG_20260523.md (     450 行, docs/devlog/DEVLOG_20260523.md)
- HARDENING_LAYER.md (     136 行, docs/HARDENING_LAYER.md)
- KNOWN_LIMITATIONS.md (     171 行, docs/KNOWN_LIMITATIONS.md)
- MERGE_DECISIONS.md (     261 行, docs/MERGE_DECISIONS.md)
- PHASE2_IMPACT.md (     143 行, docs/PHASE2_IMPACT.md)
- PHASE2_REVIEW.md (      79 行, docs/PHASE2_REVIEW.md)
- boundary_debt_register.md (      17 行, docs/reports/boundary_debt_register.md)
- fanglab_nutrient_inventory.md (      49 行, docs/reports/fanglab_nutrient_inventory.md)
- openhuman_rightflower_integration_record.md (      28 行, docs/reports/openhuman_rightflower_integration_record.md)
- RIGHT_FLOWER_PROTOCOL.md (     155 行, docs/RIGHT_FLOWER_PROTOCOL.md)
- core_security_model_v1.md (      26 行, docs/security/core_security_model_v1.md)
- TWINFLOWER_MERGE_PLAN_PHASE2.md (     201 行, docs/TWINFLOWER_MERGE_PLAN_PHASE2.md)
- TWINFLOWER_MERGE_PLAN.md (     201 行, docs/TWINFLOWER_MERGE_PLAN.md)
- README.md (      38 行, eval/README.md)
- README.md (     109 行, README.md)
- README.md (      21 行, right_flowers/README.md)
- README.md (      43 行, workflows/templates/README.md)


## 9. 已知债务

- - id: D01
- file: plugins/think_plugin.go
- pattern: os.ReadFile
- reason: "L4 直接读文件，应走 ValidateAndExecute('read_file')"
- - id: D02
- file: plugins/review_handler.go
- pattern: os.(MkdirAll|WriteFile|Remove|ReadFile|ReadDir)
- reason: "L4 直接文件操作，应走 code_apply"
- - id: D03
- file: plugins/skill_factory_plugin.go
- pattern: os.(Stat|MkdirAll|WriteFile|ReadFile|Remove|ReadDir)
- reason: "YAML 工作流管理器固有行为"


## 10. 新能力：本地模型故障切换

2026-05-24 完成 API→本地模型自动降级方案的全链路实现：

| 组件 | 状态 | 说明 |
|------|------|------|
| 11 引擎扫描器 | ✅ | `internal/discovery/engines.go` — Ollama/llama.cpp/LM Studio 等 |
| 策略状态机 | ✅ | `StrategyState.Decide()` — API↔local 切换 + 2 次滞后防抖 |
| 健康检测扩展 | ✅ | `observatory.Check()` 读取 api_reachable / local_model_available |
| Provider 切换 | ✅ | `llm.SetProvider()` — 线程安全运行时切换 LLM 供应商 |
| LocalRouteStrategy | ✅ | `kernel/local_route.go` — 本地模型路由 + parseDecision 硬化 |
| 启动集成 | ✅ | `cmd/beishan/main.go` — 启动扫描 + monitorFailover goroutine |
| 降级矩阵文档 | ✅ | `docs/reports/local_model_degradation_matrix.md` |
| 全链路 e2e 测试 | ✅ | `TestFailoverFullChain` / `TestNoFlapping` / `TestStatusFields` |

**CI 改进：** smoke 测试无 API key 时走 offline 模式（exit 0），非直接失败。

## 11. 汇总评分

| 维度 | 评级 | 说明 |
|------|------|------|
| 代码质量 | 🟢 稳定 | 测试全部通过，边界扫描干净 |
| 架构一致性 | 🟢 清晰 | L1-L4 + 底座/左花/右花 + 故障切换 |
| 硬化层 | 🟢 可靠 | 8 项不变性 + 5 道防线 |
| 可观测性 | 🟡 发展中 | observatory + audit 已就绪 |
| 本地模型故障切换 | 🟢 完整 | 扫描→健康监测→自动切换→滞后回切 全链路 |
| 右花协议 | 🟢 可用 | v1 协议冻结，6 个 cmd 入口 |
| 文档 | 🟡 完整 | 26 份文档，已对齐代码 |
| 测试覆盖 | 🟡 增长中 | 47+ 单元测试 + 12 功能测试 |
| 安全模型 | 🟢 已文档化 | v1 安全模型就位 |
| 右花生态 | 🟡 草案 | 协议已定义，无真实右花接入 |
| 长期规划 | 🟢 明确 | L1-L10 已记录待启动 |


---
*报告生成时间: 2026-05-24 21:30:00*
*提交: 4236dd4*
