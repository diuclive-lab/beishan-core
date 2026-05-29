# beishan-core — AI Agent Framework

硬化底座 + 左花（内置插件）+ 右花（外部协议）。Go 1.26。

## Project Snapshot (live)

- **git**: `main`, clean
- **build**: `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ (22 packages)
- **health**: `go run ./cmd/core-health` → pass
- **tools**: ~120 registered
- **plugins**: 25 L4 + 43 YAML workflows (all v2.5 standard)
- **right flowers**: 3 (OpenHuman personal_context + Hermes Agent coding_agent + OpenClaw agent)
- **MCP servers**: 0 (框架保留，模板脚本已删除)
- **UNIMPLEMENTED**: 0
- **launchd**: beishan-core + openhuman-adapter registered, KeepAlive enabled

## Architecture

### Layer map

```
User Request
  │
  ▼ POST /api/chat
cmd/beishan/main.go  ─── HTTP handler
  │
  ▼ kernel.Send(msg)
kernel/router.go  ─── RouteStrategy → LLM Router → parseDecision
  │                   ↓                           ↑
  │              (LLM decides recipient      (hardening:
  │               from natural language)      JSON + confidence
  │                                           + knownPlugin)
  ▼
kernel.Kernel  ─── 按 recipient 转发
  │
  ├──► L4 plugins/ (think_plugin, search_plugin, etc.)
  │     └── kernel.Call → L3 tools (ValidateAndExecute)
  │
  ├──► L2 glue/ (Python subprocess IPC)
  │     └── stdin/stdout JSON lines
  │
  └──► rightflower/ ─── HTTP → adapter → 右花 Core
```

### Layer details

| Layer | Dir | Frozen? | Purpose | Dependencies |
|-------|-----|---------|---------|--------------|
| L1 | kernel/ | ✅ YES | Plugin interface, message routing, Router | only llm package |
| L2 | glue/ | No | Subprocess IPC, manifest scan, right flower health | kernel, observatory |
| L3 | internal/tools/ | No | Tool registry + ValidateAndExecute (~120 tools) | registry |
| L3 | internal/agent/ | No | Sub-agent delegation (spawn_subagent/parallel) | llm, tools, observatory |
| L3 | internal/observatory/ | No | Trace recorder, health Pulse, event bus (PublishEvent) | — |
| L3 | internal/llm/ | No | LLM provider config + thread-safe SetProvider | — |
| L3 | internal/discovery/ | No | Local engine scanner + API→local failover | — |
| L3 | internal/rightflower/ | No | Manifest loading, HTTP dispatch, audit | kernel |
| L3 | internal/retrieval/ | No | L0 keyword + L1 semantic + L0.5 graph search | tools |
| L3 | internal/workflow/ | No | YAML engine + Go-DSL engine | kernel, tools |
| L4 | plugins/ | No | 25 orchestration plugins | kernel, tools |
| L4 | workflows/ | No | 43 YAML workflow definitions | workflow_plugin |

## Key Design Rules

1. **Kernel frozen** — never modify kernel/ once stable. Only register, route, forward.
2. **Hardening layer** — tools go through ValidateAndExecute. NEVER call tools.Execute directly.
3. **Type = intent, Payload = data** — routing only reads msg.Type, never Payload.
4. **Right flower = protocol, not integration** — manifest yaml + HTTP dispatch. No right flower code in the base.
5. **Gap analysis required** — every absorption must document what was NOT absorbed and why.
6. **Design decision vs omission** — if 3 lines can fix it, it was an omission, not a decision.

## 知识检索架构（两套向量，勿混淆）

主搜索管道 `searchMemoryFull` 三层：
- **L0 关键词**：`SearchWithScore`（支持中文滑动窗口 `stringContainsAny`）
- **L0.5 图扩展**：TypedLink 链式跟踪（supersedes/contradicts/evolves_from）
- **L1 API 语义**：`searchByEmbedding` → 使用 `entry.Embedding` 字段
  需配置 `EMBEDDING_ENDPOINT`，通过 `knowledge_reindex` 批量填充

独立于主管道的 BOW 词袋系统：
- **存储**：`{id}.embed.json`（512 维本地哈希向量）
- **工具**：`knowledge_embed` / `knowledge_embed_all` / `knowledge_semantic_search` / `knowledge_heal`
- **不参与主搜索管道**，仅用于手动语义搜索和重复检测

**勿将 `.embed.json`（BOW 512 维）与 `entry.Embedding`（API 向量）混淆。** 两者独立，修一个不影响另一个。

### 存储格式（2026-05-28 起双格式支持）

知识库支持两种存储后端，通过 `StorageAdapter` 接口统一访问：
- **JSON 格式**（旧）：`memory/knowledge/kn_*.json`，只读兼容
- **块级格式**（新）：`memory/notebooks/*.sy`，文档=块树结构

块级格式参考 SiYuan Note 的 Block 模型，每篇文档由多个带 UUID 的块组成：
```json
{
  "id": "doc_uuid", "title": "标题", "tags": [...],
  "blocks": [{"id": "b1", "type": "heading", "content": "..."}],
  "refs": ["目标文档ID"], "backlinks": ["来源文档ID"],
  "embedding": [0.01, ...]
}
```

搜索管道在块级存储上额外评分块内容匹配（`BlockContents` → L0 +2）。结果可定位到具体块（`BlockID`/`BlockContent`）。

## Right Flower Protocol (concrete example)

```json
// beishan-core → adapter (HTTP POST http://localhost:9529/dispatch)
{
  "id": "req_abc123",
  "type": "dispatch",
  "method": "memory.search",
  "params": {"namespace": "personal"}
}

// adapter → OpenHuman Core (JSON-RPC 2.0 over HTTP)
{
  "jsonrpc": "2.0",
  "method": "openhuman.memory_recall_memories",
  "params": {"namespace": "personal"},
  "id": "1"
}

// Response back through chain
{
  "sender": "openhuman",
  "type": "memory.search.result",
  "payload": {"findings": [...], "flower": "openhuman", "method": "memory.search", "kind": "rightflower"}
}
```

**adapter endpoint**: http://localhost:9529/dispatch (manifest → rightflower.Plugin.OnMessage → Client.Dispatch)
**probe**: GET http://localhost:9529/health → `{"adapter":"openhuman","openhuman":"reachable"}`
**method map**: memory.search, memory.store, context.retrieve, code.review

### Hermes Agent (right flower #2)

- **adapter**: `cmd/hermes-flower-adapter/` (Go) → replaced by `cmd/rightflower-python-wrapper/hermes_agent_adapter.py` (Python)
- **endpoint**: http://localhost:9532/dispatch
- **type**: coding_agent
- **methods**: memory.search, memory.store, tools.list, tool.execute, agent.chat, conversations.list
- **template**: `cmd/rightflower-python-wrapper/rightflower_adapter.py` — 标准 Python 右花接入模板

### OpenClaw (right flower #3)

- **adapter**: `cmd/rightflower-python-wrapper/openclaw_adapter.py` (Python)
- **endpoint**: http://localhost:9533/dispatch
- **type**: agent
- **methods**: agent.chat, tool.execute, skills.list, gateway.status
- **gateway**: OpenClaw Gateway on http://localhost:18789
- **config**: `LLM_PROVIDERS_CONFIG` — 声明式多 Provider JSON 配置

## AI Guardrails & 零容忍规则

架构层面的禁止事项 + 操作层面的行为红线。违反任何一条规则的输出视为无效。

### 架构禁令

| Don't | Because |
|-------|---------|
| ❌ Don't modify kernel/ | It's frozen. Only register, route, forward. |
| ❌ Don't call tools.Execute directly | Bypasses hardening. Use ValidateAndExecute. |
| ❌ Don't add right flower code to the base | Right flower = protocol. Code goes in adapter. |
| ❌ Don't skip gap analysis | Every absorption MUST document omissions. |
| ❌ Don't assume "design decision" without proof | If 3 lines can fix it, it's an omission. |
| ❌ Don't skip breadth check | Changing A without checking B creates islands. |
| ❌ Don't forget to update docs | After any code change, update relevant MD files. CI will check. |
| ❌ Don't add dependencies lightly | Zero external Go deps except stdlib. |

### 操作红线（零容忍）

| 禁止行为 | 正确做法 |
|----------|----------|
| ❌ 说"应该""可能""按理说" | 只允许说"已确认"或"无法确认" |
| ❌ 先写代码再说"需要测试" | 必须先确认非测试调用点再动手 |
| ❌ 声明"已完成"但无调用点 | 必须提供非测试调用点或标记 UNIMPLEMENTED |
| ❌ 根据文件名猜测内容 | 用 Read 工具读取后才能描述 |
| ❌ 改一处不检查下游 | 改完后 grep 确认下游不受影响 |
| ❌ 不确定时编造数字 | 用"未测量"替代估计值 |
| ❌ 把"提示词写了"等于"代码保证了" | 硬化层原则：代码层保证，提示词层提醒 |
| ❌ 声称功能"存在"而不先 grep | 必须 `grep -rn "符号名"` 验证后再声称 |
| ❌ 跳过 `go build ./...` 说代码能工作 | 每次改动后必须先编译再结论 |
| ❌ 无 INTEGRATION_PROOF 声称完成 | 完成定义=INTEGRATION_PROOF 可填写 |

## 集成纪律

### 你的默认假设

在这个项目里，你必须假设：
- 你上一次对话写的代码，**可能没有被真正集成**
- 你认为"显然会被调用"的函数，**可能根本没有调用点**
- 你更新的文档，**可能和实际代码不符**
- 你认为"完成了"的功能，**可能只是实现了但未接入**

不要假设上一次的工作是正确的。每次对话从怀疑开始。

### 对话开始时的强制检查

如果用户没有明确提供以下信息，你必须主动询问：

```
在开始实现之前，我需要确认：
1. scripts/integration_check.sh 当前的输出是什么？
2. 这次要实现的功能，在 docs/DATA_FLOW.md 里是否已有对应路径（哪怕是 ❌ 断路状态）？
```

唯一的例外：用户明确说"不用检查，直接做"——此时你必须在回复开头写一行：
`⚠️ 跳过集成检查，此功能可能未完成集成。`

### 声明"完成"前的强制输出格式

在你认为任何功能"完成"之前，你的回复必须包含以下结构。
如果你无法填写其中任意一项，你必须说"此功能未完成集成"，不得声称完成。

```
---INTEGRATION_PROOF---
新增符号: [函数名 / 包名 / 方法名]
非测试调用点: [文件路径:行号] 或 [无，原因: ...]
数据流:
  入口: [HTTP端点 / 插件名 / 工具名]
  经过: [你实现的代码位置]
  出口: [最终到达哪里]
build验证: [go build ./... 输出结果]
test验证: [go test ./... 输出结果（行数+状态）]
DATA_FLOW.md 已更新: [是 / 否，原因: ...]
integration_check.sh 无新增警告: [是 / 否，输出: ...]
状态: [已完成集成 / 已实现但未集成（标记为 UNIMPLEMENTED）]
---END_PROOF---
```

### 被允许说的话

你被允许说：
- "我实现了 X，但还没有集成进主流程，需要在 Y 处调用它，现在先标记为 UNIMPLEMENTED"
- "这个函数目前没有调用点，我认为应该在 Z 处调用，是否需要我一并实现？"
- "DATA_FLOW.md 的路径 B 仍然是 ❌ 断路，这次改动没有修复它"

你不被允许说：
- "功能已完成" 但没有 INTEGRATION_PROOF
- "应该能工作" 但没有验证
- "之前已经实现了" 但无法指出调用点

### 占位符规则

如果当前不实现某个功能，你必须在代码里加 `UNIMPLEMENTED` 注释，
**不得**写看起来完整的空结构体或空接口来"占位"。

```go
// ✅ 正确
// UNIMPLEMENTED: 此包预留设计，当前无实现，未被任何地方 import
// 创建日期: YYYY-MM-DD | 超过 60 天无进展请删除
var Unimplemented = true

// ❌ 禁止
type Channel interface {
    Send(msg Message) error  // 没有实现，也没有标记
}
```

同时在 `docs/KNOWN_LIMITATIONS.md` 里登记这个占位符。

### 关于 kernel/ 的硬性规定

`kernel/` 目录是**冻结区域**。

你不得修改 `kernel/` 下的任何文件，除非用户明确说：
"我批准修改 kernel"。

如果你认为需要修改 kernel 才能实现某功能，你必须停下来说：
"这个需求需要修改 kernel/，这违反了冻结规则。请明确批准，或者我们重新设计方案。"

### 你不是连续工作的

你没有跨会话的记忆。每次对话结束时，你完成的工作如果没有：
1. 体现在 git commit 里
2. 更新在 DATA_FLOW.md 里
3. 通过 integration_check.sh

那么从下一次对话的角度看，这个工作**不存在**。

这不是你的缺陷，是你的工作方式。接受它，并在每次对话里把工作做到可被验证的程度。

## Build & Test

```bash
go build ./...                # full build
go vet ./...                  # static analysis
go test ./...                 # 21 packages
go run ./cmd/core-health      # health check
go run ./cmd/beishan/         # start (port :8013)
go build ./... && go vet ./... && go test ./...  # full CI check
```

## Key Files

| File | Purpose |
|------|---------|
| `cmd/beishan/main.go` | Entry point, plugin + agent registration |
| `internal/tools/knowledge.go` | 知识库核心：类型/存储引擎/CRUD/共享工具 |
| `internal/tools/knowledge_search.go` | 检索管道（L0+L1+L0.5 + LRU 缓存 + 多跳 + Retrieval Trace） |
| `internal/tools/knowledge_embedding.go` | 语义向量：endpoint/tryEmbedding/searchByEmbedding/reindex |
| `internal/tools/knowledge_links.go` | TypedLink 自动关联/加载/确认/建议 |
| `internal/tools/knowledge_maintenance.go` | 版本/探针/备份/查重/合并/自愈/反馈 |
| `internal/tools/knowledge_analysis.go` | 主题图谱/时间线/知识图谱 |
| `internal/tools/knowledge_tools.go` | knowledge_* 工具注册（registerKnowledgeTools） |
| `internal/tools/code_security.go` | Security check + code_ai_review |
| `internal/tools/toolsets.go` | 12 工具组 + BuildToolsetSummary |
| `internal/tools/evidence_router.go` | 证据路由 + EWMA 自适应权重 |
| `internal/tools/workspace.go` | 跨会话工作状态 (workspace_save/load) |
| `internal/tools/radixtree.go` | 压缩前缀树 |
| `internal/tools/filesystem_tools.go` | 8 个文件系统工具 |
| `plugins/intent_keywords.go` | **唯一关键词源** — 所有意图判断词表，其他文件只引用 |
| `plugins/think_plugin.go` | OnMessage 纯分发 + handleChat + handleSystemCommand |
| `plugins/retrieval_pipe.go` | 检索管道（RunFullRetrieval / RunEpisodic / classifyIntent）|
| `plugins/review_handler.go` | 知识审查状态机（候选提取 + 确认流程）|
| `plugins/session.go` | 会话状态机 (SessionManager) |
| `internal/observatory/events.go` | Event bus (PublishEvent + Subscribe) |
| `internal/observatory/trace.go` | Decision traces + default recorder |
| `internal/llm/config.go` | LLM provider config + SetProvider |
| `kernel/router.go` | LLM router + parseDecision hardening |
| `glue/glue.go` | IPC manager + right flower health monitoring |
| `cmd/openhuman-flower-adapter/main.go` | OpenHuman bridge |
| `cmd/mcp-servers/` | (已删除，15 个 Python 模板脚本) |
| `internal/mcp/` | MCP protocol client framework |
| `internal/tools/desktop.go` | Desktop operation L3 tool |
| `internal/tools/csv.go` | CSV profile + sample (pure Go) |
| `clients/beishan-ios-client/` | iOS SwiftUI app (Gemini) |
| `clients/apple-core/` | Swift SDK for beishan-core API |

## Key Env Vars

| Var | Required | Default | Notes |
|-----|----------|---------|-------|
| DEEPSEEK_API_KEY | ✅ Yes | — | At least one API key needed |
| LLM_API_KEY | No | same as DEEPSEEK | Fallback if DEEPSEEK not set |
| EMBEDDING_ENDPOINT | No | — | 由 glue 自动启动 nomic-embed sidecar 并设置，通常不需手动配 |
| RIGHTFLOWER_ENDPOINT | No | localhost:9529/dispatch | Right flower dispatch |
| LLM_PROVIDER | No | deepseek | deepseek / openai / xiaomi / local |
| LLM_PROVIDERS_CONFIG | No | — | Path to extra providers JSON config file |
| PORT | No | 8013 | HTTP server port |

## Logs & Debugging

```
# Server logs (launchd):
tail -f ~/Library/Logs/FangLab/api.err.log
tail -f ~/Library/Logs/FangLab/api.log

# Agent events:
tail -f eval/run/events/events_$(date +%Y%m%d).jsonl

# Agent conversations:
ls eval/run/conversations/

# Embedding model (nomic-embed via glue sidecar, port 8092):
curl http://127.0.0.1:8090/v1/chat/completions -H "Authorization: Bearer local-dev" ...
```

## When to Ask the User / 遇问题处理规则

| 情况 | 正确做法 |
|------|----------|
| 新右花吸收 | 提出方案，等批准 |
| 需要改 `kernel/` | **STOP** — 必须获明确批准 |
| 新增外部依赖 | 先问（零依赖原则） |
| 改 launchd 配置 | 先问（系统级变更） |
| **不知道某个函数的作用** | 用工具读源码，不猜 |
| **修改导致 build 失败** | 立即停止，说明错误，不绕过 |
| **某任务明显超出当前上下文能力** | 明确说"此任务需要额外信息：X"，不假装完成 |
| **发现新的 bug 或问题** | 记录在遗留问题清单，不偷偷忽略 |
| **DATA_FLOW.md 与代码不符** | 以代码为准，更新文档，不以文档强解代码 |
| 以上之外 | 直接做，完成后报告 |

## Key Documents

| Read this | When you need... |
|-----------|-----------------|
| README.md | Project overview, quick start |
| DESIGN_PRINCIPLES.md | Design philosophy, why it's this way |
| MERGE_DECISIONS.md | Past decisions and rejected alternatives |
| KNOWN_LIMITATIONS.md | Design boundaries and unresolved issues |
| HARDENING_LAYER.md | What hardening guarantees |
| DIRECTORY.md | Code layout |
| RIGHT_FLOWER_PROTOCOL.md | How to connect external tools |
| CHANGELOG.md | Chronological change history |
| `docs/ABSORPTION_GOVERNANCE.md` | 吸收治理框架：证据等级、吸收等级、风险分类、升级策略、决策登记册 |
| `docs/DATA_FLOW.md` | 系统真实数据流——端到端路径状态 | |
| `docs/V25_WORKFLOW_STANDARD.md` | v2.5 YAML 工作流参考标准：强制项、条件项、禁止项、骨架模板 |
| `docs/devlog/DEVLOG_20260526.md` | 全量 v2.5 升级记录：40 YAML + Go 工具反推 + 引擎修复 |
| `docs/archived/absorb_right_flower.yaml` | Absorption process (v2.5, 14步, 已归档，不可执行) |

**Quick lookup**:
- "Why is X this way?" → DESIGN_PRINCIPLES.md
- "Has this been decided before?" → MERGE_DECISIONS.md
- "Is this known broken?" → KNOWN_LIMITATIONS.md
- "What absorption level / evidence standard?" → docs/ABSORPTION_GOVERNANCE.md
- "How to write a v2.5 workflow?" → docs/V25_WORKFLOW_STANDARD.md
- "Where does this code live?" → DIRECTORY.md
