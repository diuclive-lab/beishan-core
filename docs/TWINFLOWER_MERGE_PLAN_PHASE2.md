# TwinFlower 融合第二阶段方案（设计稿）

> 基于 2026-05-23 对 TwinFlower 源码的全面探查。
> 第一阶段（observatory + ErrorKind + file_safe + EWMA）已完成。
> 本阶段聚焦剩余三个模块：茎（工具注册表）、维管束（澄清契约）、花（探索能力）。

---

## 修正的前置认知

代码探查确认了之前讨论中的几处误解：

| 之前以为的 | 实际代码 | 影响 |
|-----------|----------|------|
| stem/registry/ 是空壳 | 389 行真实代码：注册表生命周期 + Profiles + Toolsets | **需要融合，优先级中** |
| flowers/explore/ 只有 workflows/ | evals(320行) + shadow(146行) + drift(187行) + model_lab(146行) + evidence(210行) + suites(160行) + workflows(373行) = 1542 行真实代码 | **需要融合，优先级高** |
| flowers/daily/ 有部分代码 | 7 个空目录，全部为 stub | **不融合** |
| stem/tools/ 不存在 | 5 个真实工具：translate(105) + weather(85) + stock(142) + currency(126) + filesystem(107) = 565 行 | **选择吸收** |

---

## 一、架构红线（延续第一阶段）

1. **L1 kernel 零改动**
2. **不创造第二套类型系统** —— 所有流转数据兼容 internal/workflow/types.go
3. **探索花不参与线上决策** —— internal/explore/ 只做离线评估、对比、数据收集
4. **茎的注册表不替换现有 schema_registry** —— 作为上层包装存在

---

## 二、四个融合模块

### 模块 A：茎 — 工具注册表生命周期（stem/registry/，+389 行）

**源文件**：

| 文件 | 行数 | 内容 |
|------|------|------|
| stem/registry/registry.go | 138 | Registry：PhaseInit → PhaseRunning 生命周期门控 |
| stem/registry/profile.go | 113 | Profile/Policy：按角色过滤工具（full_local/safe/research） |
| stem/registry/toolset.go | 87 | Toolset：工具分组（business/web/filesystem/general）+ 递归解析 |
| stem/registry/metadata.go | 51 | Metadata/Capability：工具元数据定义 |

**融合方式**：作为 schema_registry.go 的上层包装

```
tools.Register("web_search", ...) → schema_registry.RegisterToolSchema + Registry
                                   ↓
registry.Register("web_search", metadata)
  ├─ schema_registry.RegisterToolSchema + Registry (现有注册)
  └─ 生命周期门控：PhaseInit 才允许注册，Lock() 后拒绝
```

**新增价值**：

| 能力 | 当前 beishan-core | 增加茎之后 |
|------|------------------|-----------|
| 注册锁 | 无，启动后不可控 | PhaseInit→PhaseRunning 显式转换 |
| 工具过滤 | 编译时 validateGoStep | 运行时 Policy.Filter(role, tools) |
| 工具分组 | 无 | Toolset.Resolve() 递归展平 |

**目标位置**：internal/registry/

**关键接口**：
- `Registry`：PhaseInit → PhaseRunning 生命周期，Register/Lock/Filter
- `Policy`：按 Profile 过滤工具（Allowed/Filter）
- `Toolset`：按业务域分组（Resolve 递归展平）

**代码变更**：
- 新增 internal/registry/ 目录 + 4 个文件
- cmd/beishan/main.go 中 tools.Init() 后调用 registry.New() + registry.Lock()

---

### 模块 B：茎 — 工具实现选择吸收（stem/tools/，+~300 行）

选择 3 个 beishan-core 缺失的工具：

| 工具 | 行数 | API | 价值 |
|------|------|-----|------|
| weather | 85 | Open-Meteo（免费，无需 key） | **高** |
| translate | 105 | LibreTranslate（免费） | **中** |
| currency | 126 | open.er-api.com（免费） | **中** |

**融合方式**：注册为 L3 工具，走标准 tools.Register + tools.ValidateAndExecute

**目标位置**：internal/tools/weather.go、internal/tools/translate.go、internal/tools/currency.go

**不迁移**：stock（HTML 解析不稳定）、filesystem（已有覆盖）

---

### 模块 C：维管束 — 澄清契约标准化（+67 行）

**源文件**：vascular/skills/clarify/skill.go

**当前问题**：beishan-core 的 clarify.go 返回纯文本。observatory 无法结构化消费。

**改造后**：
```go
package clarify

type Request struct {
    NeedsClarify bool
    Question     string
    Candidates   []string
    Confidence   float64
    Evidence     []string
}

type Response struct {
    Input      string
    Selected   string
    Candidates []string
    Timestamp  string
}

func BuildQuestion(input string, candidates []string, evidence []string) string
```

**目标位置**：internal/clarify/（新增包）

**代码变更**：
- 新增 `internal/clarify/`，移入 Request/Response/BuildQuestion
- 改造 `clarifyHandler`：当 `args["format"] == "structured"` 时返回 `clarify.Request` JSON，否则保持旧行为
- 新增 `workflows/clarify_learn.yaml`：通过 `memory_plugin` 调用 clarify + knowledge_add 形成学习闭环

**向后兼容**：`grep -rn "clarify" workflows/*.yaml` 确认零消费者。可直接一步到位。

---

### 模块 D：花 — 底座能力吸收 + 右花协议定义

**修正**：不将 flowers/explore/ 所有代码迁入 internal/。区分"底座能力"与"协议规范"。

#### 吸收到内部（底座能力，+690 行）

| 源文件 | 行数 | 目标 | 理由 |
|--------|------|------|------|
| evals/bench.go + runner.go | 320 | internal/bench/ | 通用评估框架，量化硬化层/工作流表现 |
| evals/suites/ | 160 | internal/bench/suites.go | 评估套件，属于 bench 的数据部分 |
| evidence/evidence.go | 210 | internal/observatory/evidence.go | 因果追踪图，observatory 的自然延伸 |

#### 不吸收，定义为右花协议

| 源文件 | 行数 | 处理方式 |
|--------|------|----------|
| shadow/shadow.go | 146 | A/B 影子对比 → 右花协议中的工作模式 |
| drift/drift.go | 187 | 漂移检测 → 耦合 TwinFlower 偏好系统 |
| model_lab/lab.go | 146 | 模型对比 → 外部工具职责 |

#### 设计原则

```
底座能力（吸收到 internal/）:
  bench/           — 评估框架（底座需要量化自身表现）
  observatory/     — 因果追踪（可观测性延伸）

右花协议（定义在 docs/）:
  Shadow 模式      — 右花提供两路输出，底座收集对比
  Evaluation 接口  — 右花如何接入 bench 测试
  注册规范         — 右花声明自身能力的方式
```

---

## 三、执行计划

| 步骤 | 模块 | 行数 | 风险 | 独立性 |
|------|------|------|------|--------|
| 1 | **C：澄清契约** | +67 | 最低 | 独立 |
| 2 | **D1：bench 评估框架** | +480 | 低 | 独立 |
| 3 | **D2：evidence 因果追踪** | +210 | 低 | 独立 |
| 4 | **A：茎注册表** | +389 | 低 | 独立 |
| 5 | **B：3 个工具** | +316 | 最低 | 独立 |

总计：约 **1,462 行**（因剔除 479 行协议层代码而减少）。

**建议顺序**：澄清契约 → bench 评估 → evidence 因果追踪 → 茎注册表 → 3 工具

---

## 四、不迁移清单

| 模块 | 行数 | 理由 |
|------|------|------|
| flowers/daily/ | 7 个空目录 | 全部为 stub |
| stem/tools/stock | 142 | HTML 解析不稳定 |
| stem/tools/filesystem | 107 | 已有 file.go 覆盖 |
| flowers/explore/workflows/ | 373 | 已被 engine.go + gods_executor.go 覆盖 |

---

## 五、整体融合总览

| 阶段 | 内容 | 行数 | 状态 |
|------|------|------|------|
| 第一阶段 | observatory + ErrorKind + file_safe + EWMA | 633 | ✅ 已完成 |
| 第二阶段 | 茎注册表 + 3 工具 + 澄清契约 + bench + evidence | ~1,462 | 📋 设计稿 |
| 右花协议 | Shadow/Evaluation/注册规范 | — | 📋 待首个接入方 |
| 总计 | TwinFlower 关键模块全部融合 | ~2,095 | — |
