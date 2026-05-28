# llmguard — LLM 行为契约层

> beishan-core 的 L3 中间件，给所有 LLM 调用加一层框架级行为契约。
> 创建于 2026-05-27。

---

## 这是什么

一个 Go 包，提供受契约约束的 LLM 调用入口。**所有 LLM 调用应该走这里**，
而不是直接调 `internal/llm`。

```go
import "beishan/internal/llmguard"

reply, usage, err := llmguard.Chat(messages, llmguard.ForContent(), 60*time.Second)
```

它替代了散落在每个 workflow / plugin 里的提示词规则，
把"LLM 应该怎么表现"从文本约束升级为可编程的契约对象。

---

## 设计哲学：分层强度按维度分配

### 核心洞察

> 强制性应该按维度分配，不是整体调高/调低。

LLM 的输出可以分成三个维度，每个维度有不同的"强制特性"：

| 维度 | 性质 | 强制策略 |
|------|------|---------|
| **结构**（output schema） | 机器可校验 | **层 2 满强度** — 强制不影响内容质量 |
| **内容**（reasoning quality） | 主观 | **层 1+4 半强度** — 过强让模型变笨 |
| **事实**（grounding） | 客观可校验 | **层 4 critique** — 强制查"有没有出处" |

整体调高强制性，结构是干净的但内容会变僵硬；
整体调低，结构松散但内容自然。
分维度配置 = 拿到两者的好处。

### 强度分层

| 层 | 机制 | 成本 | 适用 |
|---|------|------|------|
| 1 | 提示词基线注入（AntiLazy / RequireEvidence） | 零 | 自然语言聊天、报告生成 |
| 2 | 输出格式校验 + 重试 | 1 次重试成本 | JSON 结构化输出 |
| 4 | Critique-Revise 自审重写 | 翻倍 | 决策建议、审计报告 |

---

## 快速开始

### 维度化构造器

```go
// 仅内容维度（最常见，自然语言场景）
c := llmguard.ForContent()

// 仅结构维度 — JSON（机械变换、模板填充）
c := llmguard.ForStructure("json", "findings,risk_register", 1)

// 仅结构维度 — YAML（工作流生成）
c := llmguard.ForStructure("yaml", "id,steps", 1)

// 仅事实维度（含 critique，成本翻倍）
c := llmguard.ForFacts()
```

### 维度组合（fluent API）

```go
// V25 全合规：结构 + 内容 + 事实三维度全开
c := llmguard.ForStructure("json", "findings,risk_register", 1).
    WithContent().
    WithFacts()

// YAML 工作流生成：结构 + 内容（当前 skill_factory.generateWorkflow）
c := llmguard.ForStructure("yaml", "id,steps", 1).WithContent()

// 结构 + 内容，不要 critique（轻量 JSON 分析）
c := llmguard.ForStructure("json", "name", 1).WithContent()

// 内容 + evidence 标注，不要 critique（中等成本中等质量）
c := llmguard.ForContent().WithEvidence()
```

### 调用 LLM

```go
// 默认 provider
reply, usage, err := llmguard.Chat(messages, c, 60*time.Second)

// 指定 provider（workflow per-step override）
reply, usage, err := llmguard.ChatWithProvider("local", messages, c, 60*time.Second)
```

---

## 维度构造器

### `ForStructure(format, fields, retries) Contract`

结构维度，**层 2 强制**。

```go
ForStructure("json", "findings,risk_register", 1)
// → Contract{OutputFormat:"json", JSONSchema:"findings,risk_register", MaxRetries:1}
```

| 参数 | 含义 |
|------|------|
| `format` | 输出格式，目前支持 `"json"`。空字符串等价于无结构维度。 |
| `fields` | 必须存在的顶层字段（逗号分隔） |
| `retries` | 输出违规时的重试次数，建议 1（DeepSeek/Local 偶尔被 markdown 包裹） |

### `ForContent() Contract`

内容维度，**层 1 半强制**。仅启用 AntiLazy 基线。

```go
ForContent()
// → Contract{AntiLazy: true}
```

注入到 system prompt 的基线规则：
1. 禁止"将会做"语态，只能"已做"+证据 或 "做不到"+原因
2. 禁止编造事实，不知道就说"不知道"
3. 引用外部信息必须附来源

### `ForFacts() Contract`

事实维度，**层 1+4 强制**。启用 evidence 标注 + AntiLazy + Critique。

```go
ForFacts()
// → Contract{RequireEvidence: true, AntiLazy: true, Critique: true}
```

注意成本翻倍（critique 多一次 LLM 调用），仅推荐：
- 分析报告
- 决策建议
- 审计任务
- 安全检查

---

## 6 个 With* 方法

每个 With* 都是幂等的，可以自由叠加。返回新 Contract，不修改原值。

### `.WithStructure(format, fields, retries) Contract`

叠加结构维度。如果已有 MaxRetries，取较大值（不降低重试预算）。

```go
ForContent().WithStructure("json", "items", 1)
// → 内容基线 + JSON 结构强制
```

### `.WithContent() Contract`

叠加内容维度（AntiLazy）。

```go
ForStructure("json", "x", 1).WithContent()
// → JSON 结构强制 + AntiLazy 基线
```

### `.WithFacts() Contract`

叠加事实维度（RequireEvidence + AntiLazy + Critique）。

```go
ForStructure("json", "findings", 1).WithFacts()
// → JSON + evidence 强制 + critique
```

### `.WithEvidence() Contract`

仅叠加 evidence 标注，**不启用 critique**（区别于 WithFacts）。

用于只想要 evidence 但不想付 critique 成本的场景。

```go
ForContent().WithEvidence()
// → AntiLazy + RequireEvidence（无 critique）
```

### `.WithCritique() Contract`

显式叠加 critique-revise（层 4）。

```go
ForStructure("json", "x", 1).WithCritique()
// → JSON 结构强制 + critique
```

### `.WithRetries(n) Contract`

覆盖重试次数（直接赋值，不取大）。

```go
ForStructure("json", "x", 1).WithRetries(3)
// → MaxRetries 从 1 提升到 3
```

---

## 与 V25 工作流标准的对应关系

| V25 §  | 规则 | 对应 llmguard 维度 |
|--------|------|------------------|
| §1 | evidence 等级 E1-E4 标注 | `ForFacts()` 或 `.WithEvidence()` |
| §2 | risk_register JSON 字段强制 | `ForStructure("json", "risk_register", 1)` |
| §2.2 | output 是合法 JSON | `ForStructure("json", "", 1)` |
| §3 | 反偷懒（防"将会做"语态） | `ForContent()` |
| §4 | 引用必须有源 | `ForContent()` |
| §5 | gap_analysis 步骤 | （工作流层面，不在 llmguard） |
| §6 | Go 工具优先 | （插件选择层面，不在 llmguard） |

**典型 V25 合规组合**：

```go
// V25 结构化分析（structured_analysis 类型工作流）
ForStructure("json", "findings,risk_register", 1).
    WithContent().
    WithFacts()

// V25 简单报告（report 类型）
ForContent().WithEvidence()

// V25 统计报表（stats 类型，数据类）
ForStructure("json", "metrics", 1).WithContent()
```

---

## 常见模式

### 模式 1：自然语言聊天

```go
llmguard.Chat(messages, llmguard.ForContent(), 60*time.Second)
```

适用：think_plugin.handleChat、tool_synthesis 之类的用户可见输出。

### 模式 2：JSON 模板填充

```go
llmguard.Chat(messages,
    llmguard.ForStructure("json", "name", 1).WithContent(),
    40*time.Second)
```

适用：skill_factory.fillTemplate、提取实体、分类器输出 JSON。

### 模式 3：分析报告（V25 全合规）

```go
llmguard.Chat(messages,
    llmguard.ForStructure("json", "findings,risk_register", 1).
        WithContent().
        WithFacts(),
    180*time.Second)
```

适用：安全审计、代码审查、合规检查。

### 模式 4：机械变换（零开销）

```go
llmguard.Chat(messages, llmguard.Contract{}, 10*time.Second)
```

适用：query_rewrite、关键词提取等不需要基线的场景。降级路径已存在时
（err 或空串可以兜底），走零契约能省 token。

### 模式 5：provider 切换

```go
llmguard.ChatWithProvider("local", messages, llmguard.ForContent(), 300*time.Second)
```

适用：workflow per-step provider override（DeepSeek 做路由，本地 Qwen 做体力活）。

---

## 时间预算

`timeout` 是单次 LLM 调用的超时，**不是总超时**。

总耗时上限 = `timeout × (MaxRetries+1)` [+ `timeout × 2` if Critique]

```
ForContent()                       → 最多 timeout × 1
ForStructure(..., 1).WithContent() → 最多 timeout × 2
ForFacts()                         → 最多 timeout × 3 (主+critique+revise)
```

调用方需要自己评估总耗时是否可接受，特别是 workflow 步骤里有 step 级 timeout。

---

## 错误语义

| 场景 | 返回 |
|------|------|
| LLM 调用本身失败（网络/API key） | `("", nil, error)` — 不重试 |
| 重试用尽仍违反契约 | `(lastOutput, usage, error)` — 调用方可降级使用 |
| 契约通过（含 critique 通过） | `(output, usage, nil)` |
| critique 自身失败 | 回退原输出，log warning，不算 Chat 失败 |

调用方处理建议：
```go
reply, usage, err := llmguard.Chat(messages, c, timeout)
if err != nil {
    // 如果是"重试用尽"，reply 是最后一次输出，可以降级使用
    if reply != "" {
        log.Printf("llmguard 校验未过但降级使用: %v", err)
        // 用 reply
    } else {
        // 真实失败
        return err
    }
}
```

---

## 测试

```bash
go test ./internal/llmguard/ -v
```

25 个测试用例，使用 `withStubChatFunc` 桩函数注入，无需真实 LLM API。

---

## 已接入调用方（2026-05-27）

| 调用方 | 契约 | 维度 |
|--------|------|------|
| `think_plugin.handleChat`（默认+provider） | `ForContent()` | 内容 |
| `think_plugin.handleChatNoRetrieval`（默认+provider） | `ForContent()` | 内容 |
| `think_plugin.tool_synthesis` | `ForContent()` | 内容 |
| `think_plugin.query_rewrite` | `Contract{}` | 零契约 |
| `skill_factory.classifyOutputType` | `ForContent()` | 内容 |
| `skill_factory.fillTemplate` | `ForStructure(...).WithContent()` | 结构+内容 |
| `skill_factory.generateWorkflow` | `ForStructure("yaml","id,steps",1).WithContent()` | 结构+内容 |

**待迁移**：0（think_plugin / skill_factory 内的 LLM 调用全部进入 llmguard 漏斗）。

未来新增 plugin 的 LLM 调用应该一律走 llmguard，不要直调 `internal/llm`。

---

## 文件清单

```
internal/llmguard/
├── README.md          ← 本文件
├── contract.go        — Contract 类型定义 + 字段语义
├── presets.go         — ForStructure / ForContent / ForFacts + With* 方法
├── baseline.go        — 基线提示词文本 + 注入逻辑
├── validate.go        — 输出校验（JSON / YAML / RequiredFields / evidence）
├── feedback.go        — 重试结构化反馈（已有字段 + 缺失字段 + 具体修正指示）
├── chat.go            — Chat / ChatWithProvider / chatCore
├── critique.go        — critique-revise 二次调用
└── llmguard_test.go   — 34 个测试用例
```

---

## 未来升级方向

1. **结构维度升级到层 2.5**：接入 DeepSeek `response_format: json_object` /
   OpenAI `response_format: json_schema` 原生 API，把"事后校验+重试"升级为
   "生成时强制"。需要改 `internal/llm/config.go`。

2. **多模型 verifier**：用一个轻量模型校验主模型输出。比 Critique 强但成本更高。
