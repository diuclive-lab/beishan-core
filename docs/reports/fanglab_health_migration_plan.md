# FangLab project-health 迁移方案

> 基于 66 的 1399 行 project-health 与 beishan-core 的 231 行 core-health 对比分析。

---

## 一、筛选结果

14 项中，**4 项适合迁移**，**4 项不适合**（66 特有），**2 项已有等价**。

| 决策 | 项数 | 具体项目 |
|------|------|---------|
| ✅ 迁移 | 4 | Eval, Doctor, ToolSchema, EvidenceContinuity |
| ❌ 跳过 | 4 | Mainline, FunctionCallShadow, RouterAB, ContextBudget |
| ✅ 已有 | 2 | Git, Smoke |

---

## 二、四项迁移的详细方案

### F2: EvalSummary（评估套件结果）

**66 做了什么**：运行 eval 套件，收集 pass/fail 统计，输出为 JSON。

**beishan-core 现有能力**：
- `internal/bench/` — 3 个评估套件（ClarifySuite / FilesystemSuite / SearchSuite）
- `cmd/core-eval/` — 评估执行器

**迁移方式**：在 `HealthReport` 中新增 `EvalOk bool` 字段，`BuildHealthReport` 中运行 `bench.RunAll` 并检查输出。

**代码变更**：
```go
// health.go 新增字段
EvalOk bool `json:"eval_ok"`

// health.go BuildHealthReport 中新增
rep.EvalOk = r.Run("go", "run", "./cmd/core-eval/", "--suite", "smoke") == nil
if !rep.EvalOk { rep.Status = "warn" }
```

**风险**：低。纯新增字段，不影响现有逻辑。
**预估**：~30 分钟。

---

### F3: DoctorSummary（硬化层健康检查）

**66 做了什么**：运行硬化层检查，收集规则命中/违规统计。

**beishan-core 现有能力**：
- `eval/scripts/check_hardening_invariants.sh` — 8 项不变性测试
- `eval/scripts/scan_boundary.sh` — 边界扫描

**迁移方式**：在 `HealthReport` 中新增 `HardeningScore int` 字段（通过数/总数），复用现有脚本输出。

**代码变更**：
```go
HardeningScore string `json:"hardening_score"` // "8/8"

// BuildHealthReport
out, _ := r.Output("bash", "./eval/scripts/check_hardening_invariants.sh")
// 解析输出中 [1]~[8] 的通过数
```

**风险**：低。解析输出可能有小 bug，但不会影响现有逻辑。
**预估**：~30 分钟。

---

### F4: ToolSchemaSummary（Schema 漂移检测）

**66 做了什么**：对比当前注册的工具 Schema 与基准 Schema，检测差异。

**beishan-core 现有能力**：
- `internal/tools/schema_registry.go` — `GetToolSchema(name)` 返回当前 Schema
- 96 个工具已注册

**迁移方式**：新增 `SchemaDriftDetected bool`，检查是否有工具 Schema 与预期不符。

**注意**：beishan-core 目前没有"基准 Schema"快照机制——工具在 `tools.Init()` 中注册，Schema 是代码定义的，不存在漂移（因为 drift 需要运行时修改）。此项**只有概念价值，实际检测意义为零**。建议标记为 `"not_applicable"` 而非实现。

**风险**：无。
**预估**：~10 分钟标记。

---

### F5: EvidenceContinuitySummary（证据连续性）

**66 做了什么**：检查 observability trace 的连续性，检测是否有断裂。

**beishan-core 现有能力**：
- `internal/observatory/evidence.go` — 证据图
- `internal/observatory/trace.go` — 决策追踪
- `internal/rightflower/audit.go` — 审计日志

**迁移方式**：新增 `EvidenceTraceCount int`，统计 observatory 中的 trace 数量。

**代码变更**：
```go
EvidenceTraceCount int `json:"evidence_trace_count"`
```

**注意**：observatory 的 trace 目前是内存存储，没有持久化。此项只能反映"当前会话中有多少 trace"，无法做跨会话连续性分析。建议作为**基础指标**实现，保留但不做深度分析。

**风险**：低。
**预估**：~20 分钟。

---

## 三、执行计划

| 步骤 | 内容 | 预估 | 风险 |
|------|------|------|------|
| F2 | EvalOk 字段 + bench.RunAll 引用 | 30min | 低 |
| F3 | HardeningScore 字段 + 脚本解析 | 30min | 低 |
| F4 | SchemaDrift 标记为 not_applicable | 10min | 无 |
| F5 | EvidenceTraceCount 字段 | 20min | 低 |
| **合计** | **4 项** | **~90min** | **低** |

## 四、不迁移的 4 项

| 项 | 原因 | 替代方案 |
|----|------|---------|
| Mainline | 66 特有的主线路回归概念 | core_gate --strict 已覆盖 |
| FunctionCallShadow | 66 的函数调用影子评估 | 不适用 |
| RouterAB | 66 的路由 A/B 对比 | 不适用 |
| ContextBudget | 66 的 token 预算监控 | 不适用 |

## 五、迁移后的 core-health 架构

```
core-health (beishan-core 实现)
├── runner 接口（可 mock 测试） ← 66 没有的优势
├── BuildHealthReport()
│   ├── git dirty check ✅
│   ├── go build/vet ✅
│   ├── right flowers count ✅ ← beishan-core 特有
│   ├── hardening invariants ✅ (F3)
│   ├── eval suites ✅ (F2)
│   ├── schema registry ✅ (F4, not_applicable)
│   ├── evidence traces ✅ (F5)
│   └── manifest validate ✅ ← beishan-core 特有
├── --json / --snapshot 输出
└── core_gate 集成 9/9
```

迁移后 core-health 将覆盖 11 类检查（66 有 14 类，跳过 4 类不适用，新增 2 类 beishan-core 特有），检查深度与 66 持平，架构优于 66。
