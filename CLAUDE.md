# beishan-core — AI Agent Framework

硬化底座 + 左花（内置插件）+ 右花（外部协议）。Go 1.26。

## Project Snapshot (live)

- **git**: `main`, clean
- **build**: `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ (21 packages)
- **health**: `go run ./cmd/core-health` → pass
- **tools**: 105 registered (after cleanup + merges) (including base_capability_inventory)
- **plugins**: 23 L4 + 40 YAML workflows (all v2.5 standard)
- **right flowers**: 3 (OpenHuman personal_context + Hermes Agent coding_agent + OpenClaw agent)
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
| `internal/agent/runner.go` | Sub-agent execution loop |
| `internal/observatory/events.go` | Event bus (PublishEvent + Subscribe) |
| `internal/observatory/trace.go` | Decision traces + default recorder |
| `internal/llm/config.go` | LLM provider config + SetProvider |
| `kernel/router.go` | LLM router + parseDecision hardening |
| `glue/glue.go` | IPC manager + right flower health monitoring |
| `cmd/openhuman-flower-adapter/main.go` | OpenHuman bridge |
| `cmd/mcp-servers/` | 15 MCP skill servers |
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

**Last 5 commits** (2026-05-26):
```
b9e3f8c docs: 会话交接文档更新
7b5b714 workflow: 隐式假设挖掘 + 无源码重实现测试
8d8fd53 workflow: 吸收工作流 v2 — 从流程正确到吃透为止
5931746 workflow: 吸收工作流深度强化 — 源码研读 + 设计哲学 + 上游追踪
c90080f fix: 全量排查 — 硬化层绕过/右花暴露/股票路由
```

**Key milestones (May 26)**:
- 治理框架搭建（docs/ABSORPTION_GOVERNANCE.md）✅
- 40/40 YAML 升级至 v2.5 标准 ✅
- v2.5 参考标准文档（docs/V25_WORKFLOW_STANDARD.md）✅
- Go 工具反推：code_tree/code_stats/go_struct_scan/code_read_external 增强 + base_capability_inventory 新建 ✅
- Agent 系统 7 个缺口修复（并发安全、错误分类、截断通知、输出校验、LLM 重试、异步写盘、空 prompt）✅
- Workflow engine 栈溢出 bug 修复 ✅
- 本地模型：Qwen3.6-27B → gemma-4-E4B (4B) + failover gemma-4-31B ✅
- 17/18 工作流 API 测试通过，evidence/risk_register 实际输出验证 ✅

**Unfinished** (ask user before implementing):
- Embedding endpoint (was Qwen 27B — too heavy, use gemma-4-E4B next)
- LLM function calling API (currently text-mode JSON parsing)
- Cross-platform deploy (launchd is macOS-only, needs systemd/Docker)
- Event subscribers (log-only, no automated reactions)

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
