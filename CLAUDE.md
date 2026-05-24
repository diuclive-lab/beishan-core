# beishan-core — AI Agent Framework

硬化底座 + 左花（内置插件）+ 右花（外部协议）。Go 1.26。

## Project Snapshot (live)

- **git**: `main`, clean, no uncommitted changes
- **build**: `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ (21 packages)
- **health**: `go run ./cmd/core-health` → pass
- **tools**: 99 registered (97 init + spawn_subagent + spawn_parallel + per-agent delegation)
- **plugins**: 23 L4 + 33 YAML workflows
- **right flowers**: 1 (OpenHuman, all 4 methods responded)
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

## AI Guardrails (what NOT to do)

| Don't | Because |
|-------|---------|
| ❌ Don't modify kernel/ | It's frozen. Only register, route, forward. |
| ❌ Don't call tools.Execute directly | Bypasses hardening. Use ValidateAndExecute. |
| ❌ Don't add right flower code to the base | Right flower = protocol. Code goes in adapter. |
| ❌ Don't skip gap analysis | Every absorption MUST document omissions. |
| ❌ Don't assume "design decision" without proof | If 3 lines can fix it, it's an omission. |
| ❌ Don't skip breadth check | Changing A without checking B creates islands. |
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

## Key Env Vars

| Var | Required | Default | Notes |
|-----|----------|---------|-------|
| DEEPSEEK_API_KEY | ✅ Yes | — | At least one API key needed |
| LLM_API_KEY | No | same as DEEPSEEK | Fallback if DEEPSEEK not set |
| EMBEDDING_ENDPOINT | No | — | Enables semantic search (not configured yet) |
| RIGHTFLOWER_ENDPOINT | No | localhost:9529/dispatch | Right flower dispatch |
| LLM_PROVIDER | No | deepseek | deepseek / openai / xiaomi / local |
| PORT | No | 8013 | HTTP server port |

## Recent State

**Last 5 commits** (2026-05-25):
```
dd22219 docs: AI 可读性升级 — 文档索引 + AI Summary
b4cf2af docs: README 文档索引补 CLAUDE.md
6bb42ee docs: CLAUDE.md + 开发日志完整总结
8c02cb0 docs: devlog 补充深度吸收
cb554b4 feat: 子智能体自动暴露为独立工具
```

**Key milestones (May 24-25)**:
- Right flower OpenHuman 全链路通车 ✅
- 三次吸收完成（P0 向量检索 / P1 代码审查 / P2 Agent 委派）✅
- 事件总线 + 对话持久化 ✅
- 缺口分析 + 设计纪律更新 ✅
- delegate_to_\* 工具自动注册 ✅

**Unfinished** (ask user before implementing):
- Embedding endpoint (was Qwen 27B — too heavy, use gemma-4-E4B next)
- LLM function calling API (currently text-mode JSON parsing)
- Cross-platform deploy (launchd is macOS-only)
- Event subscribers (log-only, no automated reactions)

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
| `workflows/absorb_right_flower.yaml` | Absorption process (Step 2.5 gap analysis) |

**Quick lookup**:
- "Why is X this way?" → DESIGN_PRINCIPLES.md
- "Has this been decided before?" → MERGE_DECISIONS.md
- "Is this known broken?" → KNOWN_LIMITATIONS.md
- "Where does this code live?" → DIRECTORY.md
