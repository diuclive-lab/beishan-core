# beishan-core — AI Agent Framework

硬化底座 + 左花（内置插件）+ 右花（外部协议）。Go 1.26。

## Project Snapshot (live)

- **git**: `main`, clean
- **build**: `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ (21 packages)
- **health**: `go run ./cmd/core-health` → pass
- **tools**: 115 registered (105 base + 8 filesystem + 2 workspace)
- **plugins**: 25 L4 + 40 YAML workflows (all v2.5 standard)
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
| L3 | internal/tools/ | No | Tool registry + ValidateAndExecute (99 tools) | registry |
| L3 | internal/agent/ | No | Sub-agent delegation (spawn_subagent/parallel) | llm, tools, observatory |
| L3 | internal/observatory/ | No | Trace recorder, health Pulse, event bus (PublishEvent) | — |
| L3 | internal/llm/ | No | LLM provider config + thread-safe SetProvider | — |
| L3 | internal/discovery/ | No | Local engine scanner + API→local failover | — |
| L3 | internal/rightflower/ | No | Manifest loading, HTTP dispatch, audit | kernel |
| L3 | internal/retrieval/ | No | L0 keyword + L1 semantic + L0.5 graph search | tools |
| L3 | internal/workflow/ | No | YAML engine + Go-DSL engine | kernel, tools |
| L4 | plugins/ | No | 23 orchestration plugins | kernel, tools |
| L4 | workflows/ | No | 33 YAML workflow definitions | workflow_plugin |

## Key Design Rules

1. **Kernel frozen** — never modify kernel/ once stable. Only register, route, forward.
2. **Hardening layer** — tools go through ValidateAndExecute. NEVER call tools.Execute directly.
3. **Type = intent, Payload = data** — routing only reads msg.Type, never Payload.
4. **Right flower = protocol, not integration** — manifest yaml + HTTP dispatch. No right flower code in the base.
5. **Gap analysis required** — every absorption must document what was NOT absorbed and why.
6. **Design decision vs omission** — if 3 lines can fix it, it was an omission, not a decision.

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

## AI Guardrails (what NOT to do)

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
| `internal/tools/knowledge.go` | Knowledge search (L0+L1+L0.5 pipeline) |
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
| EMBEDDING_ENDPOINT | No | — | Enables semantic search (not configured yet) |
| RIGHTFLOWER_ENDPOINT | No | localhost:9529/dispatch | Right flower dispatch |
| LLM_PROVIDER | No | deepseek | deepseek / openai / xiaomi / local |
| LLM_PROVIDERS_CONFIG | No | — | Path to extra providers JSON config file |
| PORT | No | 8013 | HTTP server port |

## Recent State

**Last 5 commits** (2026-05-27):
```
（本次会话改动尚未 commit）
afdea75 docs: 更新开发日志 — 全面审查 + 6 bug 修复
fa63a5a fix: terminal_plugin Payload JSON 编码 + preRoute 全面审查修复
98e118b fix: RouteResult 缺 MsgType 导致搜索不工作
4dca4bf refactor: 用 evidence_router 替代 preRoute 关键词匹配
```

**Key milestones (May 27 — 代码梳理轮)**:
- 新建 `plugins/intent_keywords.go`，统一所有意图判断词表（删除 13 个重复变量）✅
- `think_plugin.OnMessage` 178 行 → 26 行纯分发器，引入 `isSystemCommand / handleSystemCommand` ✅
- `loadRecentSessionMessages` 改用 `tools.SessionGet`（有 sessionMu 锁保护）✅
- `GenerateSessionSummary` 接通：main.go 对话结束后异步触发，跨 session 检索不再空过 ✅
- `review_handler.saveReviewToFile` 改用 `write_file` 工具，删除错误的 patch 调用 ✅
- 删除 `internal/legacy/`（功能已内联到 adapter，整包无调用点）✅
- 全测试：21 个包全绿 ✅

**Unfinished** (ask user before implementing):
- Embedding endpoint (was Qwen 27B — too heavy, use gemma-4-E4B next)
- LLM function calling API (currently text-mode JSON parsing)
- Cross-platform deploy (launchd is macOS-only, needs systemd/Docker)
- Event subscribers (log-only, no automated reactions)
- `knowledge.go` 查询参数（q.Tags/q.Types/q.DateAfter）有 TODO 未实现
- `workflow/gods_executor` 使用 kernel.Call 零校验（与 agent runner 不一致）

**Known friction** (future improvements):
- **Learning curve**: "hardening layer", "dual flower", "right flower protocol" — too many concepts. Needs a 5-min walkthrough.
- **Missing demo**: 99 tools, 33 workflows — but no end-to-end example showing "what beishan-core can do for you".
- **Right flower cold-start**: 3 right flowers running. Protocol generality verified.
- **DeepSeek dependency**: Router prompt is DeepSeek-optimized. Provider switching is untested for routing with non-DeepSeek models.

## Logs & Debugging

```
# Server logs (launchd):
tail -f ~/Library/Logs/FangLab/api.err.log
tail -f ~/Library/Logs/FangLab/api.log

# Agent events:
tail -f eval/run/events/events_$(date +%Y%m%d).jsonl

# Agent conversations:
ls eval/run/conversations/

# Embedding model (llama.cpp on port 8090):
curl http://127.0.0.1:8090/v1/chat/completions -H "Authorization: Bearer local-dev" ...
```

## When to Ask the User

| Situation | Action |
|-----------|--------|
| New right flower project to absorb | Propose plan, wait for approval |
| kernel/ change needed | STOP — must get explicit approval |
| Adding a new dependency | Ask first (zero deps policy) |
| Changing launchd config | Ask first (system-level) |
| Everything else | Proceed, report when done |

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
| `workflows/absorb_right_flower.yaml` | Absorption process (v2.5, 14步, 引用治理框架) |

**Quick lookup**:
- "Why is X this way?" → DESIGN_PRINCIPLES.md
- "Has this been decided before?" → MERGE_DECISIONS.md
- "Is this known broken?" → KNOWN_LIMITATIONS.md
- "What absorption level / evidence standard?" → docs/ABSORPTION_GOVERNANCE.md
- "How to write a v2.5 workflow?" → docs/V25_WORKFLOW_STANDARD.md
- "Where does this code live?" → DIRECTORY.md
