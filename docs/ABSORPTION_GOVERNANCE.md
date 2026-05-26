# 吸收治理框架 — Absorption Governance Framework

> 根茎文档。定义右花吸收的治理标准和引用框架。
> 被 `workflows/absorb_right_flower.yaml` 引用。
> 不描述执行步骤，只定义标准和分类。

---

## 1 证据等级 (Evidence Levels)

用于所有吸收中关键设计决策的置信度标注。每个结论必须附带证据等级。

| 等级 | 名称 | 信任度 | 允许来源 |
|------|------|--------|----------|
| E1 | Direct Source Evidence | 最高 | 源码、runtime trace、benchmark、测试结果、官方文档 |
| E2 | Behavioral Inference | 高 | 集成测试、黑盒探测、行为观察 |
| E3 | Historical / Ecosystem Evidence | 中 | git log、issues、PR 讨论、维护者注释 |
| E4 | Reasoned Hypothesis | 低 | 推理（必须标记为 hypothesis，不能直接进入设计决策） |

记录格式：

```yaml
statement: "为何使用 async queue"
evidence: E1
source: "worker.go:281"
confidence: high
```

---

## 2 吸收等级 (Absorption Levels)

不是所有能力都应该 full absorb。吸收前必须用 decision heuristic 做核心-外围切割。

| 等级 | 名称 | 含义 | 允许动作 |
|------|------|------|----------|
| L0 | observe | 只研究不实现 | 分析、文档、benchmark |
| L1 | wrap | 保留原实现，协议包装 | adapter、protocol wrapper、proxy |
| L2 | partial_internalize | 吸核心能力，外围保留外部依赖 | 核心内化，外围配置化 |
| L3 | full_internalize | 完整内化，行为兼容 | 不再依赖原右花即可提供能力 |
| L4 | rewrite_and_supersede | 超越原设计 | 替换原方案，需 design justification |

### 2.1 L2 决策启发式 (Core-Periphery Heuristic)

核心 = 去掉该右花后，底座仍有自洽价值的抽象能力。
用两个问题切割：

1. **去掉右花特定的 API 格式/协议/部署方式后，核心逻辑还能独立存在吗？**
   - 能 → 可以抽象为核心
   - 不能 → 它就是外围

2. **底座是否已有机制覆盖核心逻辑（实现方式不同也算）？**
   - 已覆盖 → 停留在 L1 即可
   - 未覆盖 → 需要 L2 以上

外围 = 与该右花绑定的实现细节（端点、认证、协议方言）。
原则：能配置化的不硬编码，能抽象化的不复制。

### 2.2 L3 兼容等级

L3 要求不再依赖原右花即可提供该能力。"行为兼容" 分为三级：

| 层 | 要求 | 说明 |
|----|------|------|
| interface_compatible | API 签名和返回格式一致 | L3 最低要求 |
| behavior_compatible | 核心功能路径下输出等价 | 涉及硬化层验证 |
| semantically_compatible | 相同输入产出语义等价的结果 | 罕见，仅在必要时追求 |

L3 pass criteria:
- 不再依赖原右花即可提供该能力
- 接口兼容已通过自动化测试验证
- 已知行为差距已记录在 gap analysis 中
- 如果原右花下线，底座可独立提供该能力（可能行为有细微差异，但可接受）

### 2.3 等级选择规则

进入 Step 3 前必须回答：
1. L0 check: 这个能力只是研究价值吗？如果是，什么时候重新评估？
2. L1 check: 当前右花协议是否已满足需求？包装就够了吗？
3. L2 check: 如果只吸核心（L2），核心是什么？用 decision heuristic 做切割。切割后的外围部分能否配置化/独立运行？
4. L3 check: 为什么 L2 不够？(a) 核心本身在底座不存在？(b) 外围太薄拆了反增复杂度？(c) 全量接管场景太多？
5. L4 check: 是否需要超越原设计？

---

## 3 风险分类 (Risk Taxonomy)

所有被识别风险必须分类登记。

| 类别 | 示例 |
|------|------|
| correctness | 逻辑错误、状态错误、数据污染 |
| compatibility | 协议破坏、旧调用失效 |
| performance | OOM、latency spike、goroutine leak |
| security | auth bypass、credential leak |
| operability | 无法 debug、无法 trace |
| dependency | 版本锁死、生态漂移 |

风险登记册格式：

```yaml
- id: "risk-001"
  title: "认证头遗漏"
  category: security
  severity: high       # low / medium / high / critical
  probability: medium
  mitigation: "统一 middleware"
  status: open         # open / resolved / accepted
```

---

## 4 升级策略 (Escalation Policy)

失败必须可治理，不允许无限循环。

| 触发条件 | 动作 |
|----------|------|
| Step 0 连续失败 >= 2 次 | 进入辅助分析模式，允许外部资料 / maintainer 研究 |
| Step 3.5 连续失败 >= 2 次 | 重新评估 absorption level，考虑降级吸收（L0-L2） |
| 实现成本 > 预期收益 | 进入 defer 决策，记录 KNOWN_LIMITATIONS |
| critical 风险无法解决 | block 吸收，输出 BLOCKER_REPORT |

BLOCKER_REPORT 包含：
- blocker 描述 / impact 范围 / root cause / 已尝试修复 / 重试条件 / 回退策略

---

## 5 成本模型 (Cost Model)

吸收前评估四个维度的成本。

| 维度 | 评估因素 |
|------|----------|
| engineering_cost | 实现工作量、依赖适配、迁移成本 |
| operational_cost | 监控、维护、事故面 |
| cognitive_cost | 复杂度、上手曲线、隐式行为 |
| ecosystem_cost | 上游追踪、兼容性负担 |

产出：cost_estimate + benefit_estimate + ROI_assessment

---

## 6 决策登记册 (Decision Registry)

吸收过程中所有关键决策必须记录，包括：

- absorption_level_decision — 吸收等级选择 + 核心-外围切割说明
- design_alignment_decision — 哲学对齐决策
- rewrite_decision — 重写 vs 保留的决策
- defer_decision — 延期吸收的决策
- permanent_boundary_decision — 永久不吸收的边界

每条决策记录包含：decision / rationale / evidence / alternatives / tradeoffs / timestamp

---

## 7 产出物清单 (Artifact Registry)

每个吸收必须产出的文档：

| 产出物 | 来源步骤 | 说明 |
|--------|----------|------|
| SOURCE_ANATOMY_REPORT | Step 0 | 源码解剖 |
| DEPENDENCY_MAP | Step 0.5 | 依赖与冲突扫描 |
| RUNTIME_TRACE | Step 0.7 | 动态行为 trace |
| CAPABILITY_MATRIX | Step 1 | 全量能力地图 |
| DESIGN_ALIGNMENT | Step 2 | 设计哲学对齐表 |
| GAP_REPORT | Step 2.5 | 缺口分析 |
| INVARIANT_REGISTER | Step 2.6 | 隐式假设清单 |
| EVOLUTION_FORECAST | Step 2.7 | 演进预测（参见 §9） |
| CORE_PERIPHERY_MAP | Step 2.8 | 核心-外围切割图 |
| COST_COMPARISON | Step 2.8 | L2 vs L3 成本对比 |
| HARDENING_REPORT | Step 3 | 硬化验证 |
| REWRITE_VALIDATION | Step 3.5 | 重写验证 |
| TEST_REPORT | Step 4 | 三层验证结果 |
| RISK_REGISTER | Step 5 | 风险登记册 |
| GOVERNANCE_PACKAGE | Step 6 | 治理收口包 |

---

## 8 退出条件 (Exit Conditions)

### 成功

- 全部 14 步完成
- 风险已登记
- 吸收等级决策明确
- 上游可追踪

### 阻塞

- critical 风险未解决
- 架构冲突不可调和
- 经济上不合理

---

## 9 演化评估框架 (Evolution Assessment)

所有吸收必须评估上游项目的演化风险，避免"今天吸收，三个月后全废"。

### 9.1 评估维度

| 维度 | 核心问题 | 判断依据 |
|------|----------|----------|
| 接口稳定性 | 目标能力是核心 API 还是内部实现？历史变更频率？ | CHANGELOG、commit history、deprecation notice |
| 依赖活跃度 | 依赖的外部项目是否活跃？最近一次 commit？ | GitHub activity、maintainer response |
| 漂移风险 | 如果上游废弃/大幅变更此能力，底座影响范围多大？ | 调用点扫描、替代方案评估 |
| 同步机制 | 吸收后的版本和上游的同步方式是手动还是自动？ | 当前底座的技术能力 |

### 9.2 产出

- `VOLATILITY_REPORT` — 接口稳定性和变更频率评估，标记高变化风险接口
- `DRIFT_RISK` — 上游漂移对底座的影响范围 + 应对策略
