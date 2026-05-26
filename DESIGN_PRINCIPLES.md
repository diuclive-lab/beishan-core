# 设计纪律


> **AI Summary:** beishan-core design principles. Core rule: kernel is frozen (register+route+forward only).
> Hardening layer: all tools go through ValidateAndExecute. Type = intent, Payload = data (router never reads Payload).
> Right flower = protocol, not integration. MCP = skill framework (15 servers).
> Deep + breadth required for all changes.
> "Design decision vs omission" — if 3 lines fix it, it was an omission.


## 文档导航

本文件是项目的**核心设计哲学**。与其互补的文档：

| 你想了解什么 | 看哪个文件 |
|-------------|-----------|
| 目录结构——代码物理布局 | `DIRECTORY.md` |
| 硬化层能力边界——保证什么、不保证什么 | `docs/HARDENING_LAYER.md` |
| 关键决策记录——为什么系统是这个样子 | `docs/MERGE_DECISIONS.md` |
| 已知限制——诚实的设计边界 | `docs/KNOWN_LIMITATIONS.md` |
| 变更历史——面向用户的版本摘要 | `CHANGELOG.md` |
| 开发日志——每日过程记录 | `docs/devlog/` |
| 右花接入协议——外部工具如何连到底座 | `docs/RIGHT_FLOWER_PROTOCOL.md` |
| 系统真实数据流——端到端路径状态 | `docs/DATA_FLOW.md` |

**新加入者建议阅读顺序**：`DIRECTORY.md` → 本文件 → `docs/HARDENING_LAYER.md` → `docs/MERGE_DECISIONS.md`

## 四层架构

| 层 | 包 | 职责 | 可修改性 |
|---|---|---|---|
| L1 内核 | `kernel/` | 注册 + 路由（强制 DeepSeek）+ 消息转发 | 冻结不改 |
| L2 胶水层 | `glue/` | IPC + 子进程生命周期管理 | 可迭代 |
| L3 工具层 | `internal/tools/` | 工具注册、执行、参数校验、schema 清理 | 可迭代 |
| L4 编排层 | `plugins/*.go` | 编排 L3 完成多步任务，通过 Call() 同步调用 L3 | 随意改 |

**层间通信规则：**
- 所有消息必经 L1 内核转发（注册 + 路由 + 转发）
- L3 和 L4 都实现 `kernel.Plugin` 接口，在内核看来都是"插件"
- L3 自己干活（搜索、写文件、读写记忆）— 单步执行
- L4 调 L3 干活（编排）— 多步编排，通过 `kernel.Call()` 同步等待 L3 结果
- L2 胶水层是 L3/L4 子进程的 IPC 代理，不参与路由决策
- L1 内核冻结后不再修改，L2/L3 可迭代，L4 随意变更

**Router 校验机制**：收到 DeepSeek 回复后，`parseDecision` 做三层校验：
1. 必须是合法 JSON 
2. `confidence >= 0.4`
3. `recipient` 必须在已注册插件名列表（`knownPlugins`）中
校验不通过则返回错误，不给降级机会。

## 核心认知

大语言模型不是通用智能，是文本补全器。
它擅长的是"生成"，不是"思考"。
所有把 LLM 当聪明人用的架构，最终都会变成规则型。

## 两条铁律

### 铁律一：内核冻结

内核只做三件事：注册、路由、转发。
不做第五件事。
Payload 对内核永远不透明。
一旦内核稳定，不再修改。

### 铁律二：强制 AI 路由（仅首轮）

**首轮路由必经 DeepSeek**。收到用户自然语言后，强制调用 DeepSeek 产出 recipient 和 type。
不给"不走 AI 也能过"的快捷通道。DeepSeek 不可用时，系统不降级为规则匹配。

**后续 L4 编排中的 Type 是硬编码的确定值**，不再走 DeepSeek。
L4 编排器在代码中写死 `Type: "legal_generate_report"`，kernel 只看这个 Type 是否合法，不解析 Payload。

两者不冲突：
- 首轮：自然语言 → DeepSeek → recipient + type（LLM 决策）
- 后续：L4 编排 → 硬编码 Type → kernel 校验转发（代码决策）

## Type 即意图，Payload 即数据

Type 是硬判断，Payload 是参数。两者不可混淆。

### 路由只认 Type

Router 根据 `msg.Type` 做机械映射，不看 Payload 内容。
L4 在编排时确定 Type——它知道"这一步是法律检索还是网页搜索"。
不允许让 LLM 从 Payload 里"猜"路由目标。
如果一个请求可以有多个去向，必须在 Type 层面显式区分。

### Payload 不参与决策

Payload 只承载参数，不承载路由信息。
Router 永不解析 Payload 字段（内核冻结铁律的延伸）。
下游插件可以通过 L3 硬化层的 Schema 校验来验证 Payload 合法性，
但路由阶段必须以 Type 为唯一依据。

### 为什么不能把路由判断交给 LLM

如果统一用模糊的 Type（如 `"search"`），让 DeepSeek 根据 Payload 决定去向：
1. **路由不可复现**：同一请求，LLM 可能有时判为法律检索、有时判为网页搜索
2. **硬化层拦不住**：硬化层只校验返回格式，不检查"这个结果是否应该由另一个插件生成"
3. **韧性丧失**：DeepSeek 不可用时，硬编码 Type 的 L4 编排仍然可以通过 GlueLayer IPC
   调用 L3 插件（不走 Router），保留核心工作流

## 硬化层原则

能用代码写的逻辑，绝不写在提示词里。
提示词只做一件事：描述输出格式。

验证标准：
- 输出必须能用 JSON Schema 校验
- 可选值必须限制在枚举列表内
- 不合格的响应必须拒绝，不接受

删除提示词里的任意一句话，如果输出还能被硬化层校正到可用——说明那句话本来就应该写在代码里。

## 没有快捷方式（首轮路由）

每个首轮自然语言路由都是独立的 DeepSeek 调用。
不缓存首轮路由决策结果。
不写规则兜底替代首轮 AI 路由。
不给"这次特殊处理"的例外。
L4 编排内部的 Type 路由不再调用 DeepSeek。

快捷方式是之前所有版本变成规则型的死因。

## 工具 > Agent

LLM 只做它擅长的事：生成文本。
路由、校验、权限、生命周期——全用确定性代码。
把 LLM 放在它该在的地方，不是系统中心。

## 提示词工程

提示词只有三个部分：
1. 输出格式（必须是 JSON）
2. 可选值列表
3. 输入

没有角色扮演。
没有思维链。
没有"你是一个 xx 专家"。
不超过 5 行。

## 删除原则

新增一个功能之前，先问三个问题：
- 这个能删掉吗
- 这个能不能用更简单的方式表达
- 这个是不是 LLM 不该做的事

## 测试策略

内核验证：输入 -> 输出 -> 确认路由到正确的插件。
不测试 DeepSeek 的行为，只测试硬化层的校验逻辑。
不需要 mock DeepSeek，因为路由决策不是内核的职责。

## 集成纪律（AI 辅助开发的强制约束）

**这是本文件最重要的章节。**
它针对的是 AI 辅助开发产生的特有问题：代码局部正确，但全局断路。

### 问题的根源

AI 每次对话都是局部视角。它实现了一个功能，写出的代码在自己范围内是完整的，
但不会自动验证这个功能是否被系统的其他部分真正调用到。

这会产生三类死代码：

**类型一：孤岛函数。** 函数存在，但从未被调用。`parseToolSuggestions` 就是例子。
**类型二：占位符伪装成实现。** 空结构体、空接口、空包，看起来完整，实际上什么也做不到。
**类型三：断路模块。** 模块写好了，但上游没有注入，消息到不了这里。`SessionHandler = nil` 就是例子。

### 完成的定义

**一个功能"完成"，当且仅当同时满足以下三条：**

1. **有非测试调用点**：新增的导出函数或包，在 `_test.go` 以外的文件里至少有一个调用点
2. **数据流可追溯**：能在 `docs/DATA_FLOW.md` 里画出从 HTTP 入口到该功能、再到出口的完整路径
3. **机器检查通过**：`scripts/integration_check.sh` 运行结果无新增警告

不满足以上任意一条，都不叫完成，叫"实现了但未集成"。这两种状态必须显式区分。

### 占位符标准

如果一个模块当前无实现，必须诚实标记，不得伪装：

```go
// ❌ 禁止：伪装成实现的占位符
package channels
type Channel interface {
    Send(msg Message) error
}

// ✅ 要求：诚实的占位符
package channels

// UNIMPLEMENTED: 此包是预留设计，当前未实现，未被任何地方 import。
// 创建日期: YYYY-MM-DD
// 实现前提: 需要明确多通道接入的具体场景
// 超过 60 天未实现且无 issue 跟踪：直接删除
var Unimplemented = true
```

**规则：** 代码库中不允许存在既没有 `UNIMPLEMENTED` 标记、又没有调用点的导出符号。

### DATA_FLOW.md 是强制文档

`docs/DATA_FLOW.md` 记录系统中所有真实的端到端路径，格式如下：

```markdown
## 路径 A：普通聊天（✅ 已验证 2026-05-xx）

HTTP POST /api/chat
  → kernel.Call
  → Router.Route (DeepSeek)
  → think_plugin.OnMessage → handleChat
  → llm.ChatCompletionWithUsage
  → kernel.deliverResponse → HTTP response

## 路径 B：session 回程（❌ 断路：SessionHandler 未注入）

HTTP POST /api/session
  → msg.ReplyTo = "session:xxx"
  → kernel.deliverReply
  → SessionHandler ← nil，消息在此丢失
```

**维护规则：**
- 每次新增功能，必须在 DATA_FLOW.md 里增加或更新对应路径
- 路径状态只有两种：`✅ 已验证` 或 `❌ 断路`
- 不允许存在"可能通"或"应该通"的路径——只有验证过的才算通

### integration_check.sh 是强制工具

项目根目录维护 `scripts/integration_check.sh`。
每次 AI 辅助开发完成后，必须运行并确认输出无新增警告：

```bash
#!/bin/bash
set -e

echo "=== 检查未被 import 的包 ==="
UNIMPORTED=0
for pkg in $(find internal -mindepth 1 -maxdepth 2 -type d); do
    import_path="beishan/${pkg}"
    if ! grep -r "\"${import_path}\"" --include="*.go" . \
         --exclude-dir=vendor > /dev/null 2>&1; then
        # 检查是否有 UNIMPLEMENTED 标记
        if ! grep -r "UNIMPLEMENTED" "${pkg}/" > /dev/null 2>&1; then
            echo "⚠️  包未被 import 且无 UNIMPLEMENTED 标记: ${pkg}"
            UNIMPORTED=$((UNIMPORTED + 1))
        fi
    fi
done

echo "=== 检查关键注入点 ==="
if ! grep -n "SessionHandler\s*=" cmd/beishan/main.go > /dev/null 2>&1; then
    echo "❌ SessionHandler 未在 main.go 中注入"
fi

if ! grep -n "observatory" cmd/beishan/main.go > /dev/null 2>&1; then
    echo "⚠️  observatory 未在 main.go 中接出（无 /metrics 端点）"
fi

echo "=== 检查孤岛函数（导出但无非测试调用）==="
# 找新增的导出函数（可配合 git diff 使用）

echo "=== 统计 UNIMPLEMENTED 占位符 ==="
COUNT=$(grep -r "UNIMPLEMENTED" --include="*.go" . | wc -l | tr -d ' ')
echo "当前占位符数量: ${COUNT}（记录在 docs/KNOWN_LIMITATIONS.md）"

echo "=== 验证 DATA_FLOW.md 存在 ==="
if [ ! -f "docs/DATA_FLOW.md" ]; then
    echo "❌ docs/DATA_FLOW.md 不存在"
    exit 1
fi

if [ $UNIMPORTED -gt 0 ]; then
    echo "❌ 发现 ${UNIMPORTED} 个未集成包，请修复后再提交"
    exit 1
fi

echo "✅ 集成检查通过"
```

### pre-commit hook 是最终防线

`.git/hooks/pre-commit` 在提交时自动运行，不依赖 AI 的自觉性：

```bash
#!/bin/bash
# 运行集成检查，阻断不完整的提交
bash scripts/integration_check.sh
if [ $? -ne 0 ]; then
    echo ""
    echo "提交被阻断：集成检查未通过。"
    echo "请修复上述问题，或将未完成功能标记为 UNIMPLEMENTED。"
    exit 1
fi
```

安装方法：`cp scripts/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit`

## 双工作流引擎

项目有两个工作流引擎，定位不同，共存不冲突：

| 引擎 | 文件 | 定位 |
|------|------|------|
| YAML 引擎 | `internal/workflow/engine.go` | L4 高层编排 — AI 可动态修改 |
| Go-DSL 引擎 | `internal/workflow/gods_executor.go` | L3/L4 静态硬化链 — 编译时安全 |

**选择原则**：
- 工作流需要 AI 频繁修改或最终用户调整 → YAML
- 工作流是核心管道、编译时安全优先 → Go-DSL
- 既需要编译时安全又需要 AI 修改 → YAML 定义步骤，Go-DSL 包装校验

**共享类型**：两者共用 `StepResult`/`WorkflowResult`，不创造第二套状态类型。

## 底座 + 双花

beishan-core = **硬化底座 + 左花执行侧**。

- **底座** = kernel/ + glue/ + internal/（硬化层 + 工具 + 引擎）
- **左花** = plugins/ + workflows/（底座内置的生产执行侧）
- **右花** = 遵循 `docs/RIGHT_FLOWER_PROTOCOL.md` 的外部工具

### 右花是协议，不是集成

右花是 **协议层概念**，不绑定到任何具体项目。OpenHuman、FangLab、MCP 工具链，任何实现该协议的外部服务都可以作为右花接入。

```
right_flowers/
  ├── openhuman.yaml.example      ← 右花 A 的 manifest
  ├── fanglab.yaml.example        ← 右花 B 的 manifest
  └── README.md                   ← 协议说明
```

manifest 决定了连接方式和能力声明，底座不感知具体右花的内部实现。更换或增加右花不需要修改底座代码。

### 插件注册语义

| 方法 | 路由可见 | 适用场景 |
|------|---------|---------|
| `Kernel.Register` | 首轮 DeepSeek 可见 | 内置左花插件，暴露给 AI 路由 |
| `Kernel.RegisterUnlisted` | 首轮不可见，仅显式 Recipient | 右花、内部工具、不暴露给 AI 的插件 |

**规则**：
- 右花默认使用 `RegisterUnlisted`，不参与首轮 AI 路由
- `Manifest.route_exposed: true` 时使用 `Register`（仅测试/演示场景）
- 所有 `RegisterUnlisted` 插件仍可通过 `KnownPlugins()` 查询和显式 `Recipient` 调用

### 双花进化闭环

这是双生花架构的核心机制。右花不是静态的服务提供者，而是 **左花的进化探测器**：

```
右花（探路先锋）                左花（底座内核）
       │                              │
       ├─ 提供新能力 ────────────────→ 评估：值得吸收吗？
       │                              ├─ 值得 → 内化到 internal/ + hardening 覆盖
       │                              └─ 不值得 → 保持协议调用，不内化
       │                              │
       ←─ 吸收完成 ────────────────── 通知右花：此能力已接管
       │                              │
       ├─ 去探测下一个领域             │
```

**吸收条件**（同时满足）：
1. 该能力在右花上稳定运行超过 2 周，无重大事故
2. 该能力的协议格式已固化，不会随右花版本频繁变化
3. 有明确的硬化层校验方案（Schema / 参数校验 / 安全兜底）
4. 内化后不增加内核冻结区域的复杂度

**吸收方式**：
- 右花已验证的能力 → 实现移至 `internal/` 或 `plugins/`，通过硬化层验收
- 协议层保留 → 未来同类右花仍可通过同一协议接入
- 右花并不因为被吸收而失效 → 它继续提供能力，底座多了一条执行路径

**当前吸收候选**：
- `internal/discovery/` 本地引擎扫描器 — 来自 TwinFlower 的探测模式
- `internal/observatory/trace.go` 的 SetDefaultRecorder — 来自 TwinFlower 的 Recorder 模式
- `cmd/openhuman-flower-adapter/normalizeParams` 参数过滤 — 来自 OpenHuman 接入的经验

- `workflows/absorb_right_flower.yaml` — 右花吸收标准化工作流（六步：探→评→吸→测→补→记）

### 参考项目义务

当底座从某个右花项目吸收了能力后，必须在本文档的"参考项目"部分注明：

```
- 被吸收项目名（仓库地址）
  ├─ 吸收了什么能力
  ├─ 内化到哪些文件
  └─ 未吸收什么及原因
```

这样即使右花项目消亡或变更，底座的能力来源始终可追溯。

左花负责稳定生产，右花负责探索实验。底座为两者提供硬化层保护。
底座只提供三件事：硬化层、工具集、路由。

## 意图表达通道

硬化层不是堵死 LLM 的表达通道，而是在通道上设卡。

LLM 需要结构化的意图表达通道（如 tool_suggestion、search_suggestion），
来表达"我想执行 X"的意愿。硬化层的职责是校验这个意愿是否合法——参数对吗？
有权限吗？——而不是禁止 LLM 表达。

**规则**：
- LLM 可以提议工具调用（输出结构化 JSON），但不能自行执行
- 硬化层校验提议的合法性，通过后执行，拒绝时给出理由
- 拒绝不是终点——应提供替代建议（可用方法、相似路径）

## 深度与广度

新增或吸收任何模块时，必须同时检查两个维度：

| 维度 | 检查项 |
|------|--------|
| 深度 | 模块本身功能完整吗？测试覆盖了吗？ |
| 广度 | 谁调用它？谁被它调用？它的数据流向哪里？ |

没有连接的代码不是模块，是孤岛。孤岛最终变成没人敢删的债务。

## 设计决策 vs 遗漏

不是所有"设计选择"都是故意的。有些是没想明白，有些是没时间做，有些是根本没想到。

**对比暴露盲区**。2026-05-24 三次吸收中，通过与 OpenHuman 对比发现了三个真正的遗漏：
- 检索评分归一化（关键词 1-15 与语义 40-100 混排）— 不是设计，是没想到
- SESSION_EXPIRED 自动回退（3 行能修的事）— 不是设计，是代码没写完
- 子智能体对话持久化 — 以"硬化层原则"为挡箭牌，实则是底座没有存储机制

**规则**：
1. 标记一个决策为"设计选择"前，先问：这是故意的，还是只是没时间想？
2. 对比参照系（其他项目、历史方案、竞品分析）中暴露的差异，**先假设是遗漏，再证明是设计**
3. 缺口分析（`workflows/absorb_right_flower.yaml` Step 2.5）必须在吸收完成前执行
4. 每个缺口必须写"原因 + 后果 + 补救"，不能只写"没做"
5. 所有临时决策（"以后再说"）必须有对应的 todo 或 KNOWN_LIMITATIONS 记录

**验证方法**：当一个决策被质疑时，如果能用 3 行代码验证或修复，那它大概率不是设计决策，而是遗漏。

## 文档硬化层

文档和代码一样，是项目基础设施的一部分。

| 文档 | 维护规则 |
|-----|---------|
| `docs/DATA_FLOW.md` | 每次新增功能必须更新 |
| `docs/MERGE_DECISIONS.md` | 每次重大架构决策必须记录 |
| `docs/KNOWN_LIMITATIONS.md` | 每个 UNIMPLEMENTED 占位符必须在此登记 |
| `docs/HARDENING_LAYER.md` | 硬化层能力边界声明（保证什么 + 不保证什么） |
| `CHANGELOG.md` | 面向用户的版本摘要 |

代码变更时同步更新文档，如同同步更新测试一样。
**DATA_FLOW.md 的断路状态（❌）不是羞耻，是诚实。** 比不存在这个文档要好。

### 参考项目

参考项目是底座的设计灵感来源或能力吸收对象。底座的进化方向受它们启发，但不依赖于任何单个项目。

#### OpenHuman

- 仓库: github.com/tinyhumansai/openhuman (GPL-3.0)
- 类型: Rust Tauri 应用，RPC 调度 + Socket.IO 事件架构
- 角色: 首个全链路通车的右花参考实现
- 集成记录: docs/reports/openhuman_rightflower_integration_record.md

**已吸收**：
- 无（当前保持协议调用，未内化任何 OpenHuman 源码）

**参考未吸收**：
| 能力 | 现状 | 原因 |
|------|------|------|
| JSON-RPC 2.0 协议 | rightflower 协议已采用 | 未直接复制，而是适配为 rightflower HTTP dispatch |
| 事件推送 (Socket.IO) | glue protocol 已实现 event 类型 | 参考了消息推送模式，未使用 Socket.IO |
| 方法别名 (legacy_aliases) | adapter translateMethod 已实现 | 轻量实现，未复制 OpenHuman 的 429 行 Rust 代码 |
| 可观测性 (3097 行) | internal/observatory/ 4 文件 | 精简架构，深度不足，未来可吸收其指标设计 |
| WebSocket 实时通信 | 未实现 | 当前无需求，未来若有需要可参考 socketio.rs |

#### TwinFlower

- 路径: /Users/dc/Desktop/TwinFlower（本地参考项目）
- 类型: 微内核 + 认知引擎 + 工作流编排

**已吸收**：
| 能力 | 内化位置 | 说明 |
|------|---------|------|
| observatory 决策追踪 | `internal/observatory/` (trace, metrics, health) | Recorder + Summarize + Pulse |
| ErrorKind 错误分类 | `internal/workflow/gods_error.go` | 6 类错误 + IsRetryable |
| filesystem/search 工具 | `internal/tools/file_safe.go`, `search_disambiguate.json` | 文件安全操作 + 歧义字典 |
| EWMA 衰减算法 | `internal/tools/clarify.go` | 置信度超时衰减 |
| clarify 契约 | `internal/clarify/` + bench suites | 结构化澄清 + 评估套件 |
| bench 评估框架 | `internal/bench/` | ClarifySuite, FilesystemSuite, SearchSuite |

#### Hermes Agent

- 仓库: `~/Desktop/11/hermes-agent`（本地参考项目）
- 类型: Python 全功能 AI 编码智能体（自治运行、工具调用、多提供商）
- 角色: 第二个右花 + 能力吸收参考源

**已吸收**：
| 能力 | 内化位置 | 说明 |
|------|---------|------|
| 多 provider 切换 + 指定调用 | `internal/llm/config.go` | SetProvider/ChatCompletionWithProvider，基本切换 |
| Router usage 埋点 | `kernel/router.go` callDeepSeek | 记录路由调用 token 消耗 |

**吸收评估**：
| 能力 | 评估结果 | 原因 |
|------|---------|------|
| model_providers 声明式 profile | 部分吸收，gap 已知 | 只有 4 个硬编码 provider vs Hermes 30+ plugin profiles，当前够用 |
| process_registry 进程管理 | 范围偏差，非缺陷 | glue.go 是 IPC 层非进程管理器，健康检查比 Hermes 更强 |
| preRoute 快速路由 | 已关闭 | 收益太小 + 绕过硬化层，见 MERGE_DECISIONS.md #13 |

**参考未吸收**：
| 能力 | 现状 | 原因 |
|------|------|------|
| ProviderProfile 声明式 dataclass + hooks | 无等价实现 | 当前 4 provider 够用，等需要动态注册时再说 |
| 插件式 provider 发现 | 无 | 同上 |
| 崩溃恢复 checkpoint | glue 无 | glue 不管理后台进程生命周期（定位不同） |
| PTY/interactive CLI | glue 无 | 同上 |
| Watch patterns + 通知队列 | 无 | glue 只做 IPC dispatch‑response，不做后台命令监控 |
| 进程输出缓存/检索 | 无 | 同上 |

#### OpenClaw

- 仓库: npm global (`openclaw` 2026.5.22)
- 类型: Node.js AI Agent 平台（聊天原生、多通道、技能市场）
- 角色: 第三个右花 + 声明式 Provider 参考源
- 本地: Gateway :18789 + adapter :9533

**已吸收的能力**：
| 能力 | 内化位置 | 说明 |
|------|---------|------|
| 声明式多 Provider 配置 | `internal/llm/provider_config.go` | 硬化校验 + 配置文件加载，不完整吸收 |
| 通道层接口 | `internal/channels/` | 余量设计，当前无实现 |
| 记忆存储接口 | `internal/memory/` | MemoryStore + FileStore 实现 |

**保持右花协议调用**：
| 方法 | 说明 |
|------|------|
| agent.chat | 调 OpenClaw OpenAI 兼容 API |
| tool.execute | 调 OpenClaw /tools/invoke |
| skills.list | 列出 OpenClaw 已安装 skills |
| gateway.status | 检查 Gateway 健康 |

