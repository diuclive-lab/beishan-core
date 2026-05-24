# beishan-core — AI Agent Framework

硬化底座 + 左花（内置插件）+ 右花（外部协议）。Go 1.26。

## Architecture

```
kernel/ (L1)  → glue/ (L2)  → internal/ (L3)  → plugins/ (L4)
                └── rightflower/ → HTTP dispatch → 右花 adapter
```

| Layer | Dir | Frozen? | Purpose |
|-------|-----|---------|---------|
| L1 | kernel/ | ✅ YES | Plugin interface, message routing, Router (LLM) |
| L2 | glue/ | No | Subprocess IPC, right flower health monitoring |
| L3 | internal/tools/ | No | 99 registered tools with hardening |
| L3 | internal/agent/ | No | Sub-agent delegation (spawn_subagent, spawn_parallel) |
| L3 | internal/observatory/ | No | Decision traces, health checks, event bus |
| L3 | internal/llm/ | No | LLM provider config, thread-safe provider switching |
| L3 | internal/discovery/ | No | Local engine scanner + failover strategy |
| L3 | internal/rightflower/ | No | Manifest loading, HTTP dispatch, audit |
| L3 | internal/retrieval/ | No | Vector search (L0 keyword + L1 semantic + L0.5 graph) |
| L3 | internal/workflow/ | No | YAML + Go-DSL dual engine |
| L4 | plugins/ | No | 23 orchestration plugins |
| L4 | workflows/ | No | 33 YAML workflow definitions |

## Key Design Rules (from DESIGN_PRINCIPLES.md)

1. **Kernel frozen** — never modify kernel/ once stable. Only register, route, forward.
2. **Hardening layer** — tools go through ValidateAndExecute. Never call tools.Execute directly.
3. **Type = intent, Payload = data** — routing only reads msg.Type, never Payload.
4. **Right flower = protocol, not integration** — manifest yaml + HTTP dispatch.
5. **Gap analysis required** — every absorption must document what was NOT absorbed and why.
6. **Design decision vs omission** — if 3 lines can fix it, it was an omission, not a decision.

## Build & Test

```bash
go build ./...          # full build
go vet ./...            # static analysis
go test ./...           # all tests (21 packages)
go run ./cmd/core-health  # system health check
go run ./cmd/beishan/   # start server (port :8013)
```

## Key Files

| File | Purpose |
|------|---------|
| `cmd/beishan/main.go` | Entry point, plugin registration, agent setup |
| `internal/tools/knowledge.go` | Knowledge search (L0+L1+L0.5 pipeline) |
| `internal/tools/code_security.go` | Security check + code_ai_review |
| `internal/agent/runner.go` | Sub-agent execution loop |
| `internal/observatory/events.go` | Event bus (PublishEvent + Subscribe) |
| `internal/observatory/trace.go` | Decision traces + default recorder |
| `internal/llm/config.go` | LLM provider config + SetProvider |
| `kernel/router.go` | LLM router + parseDecision hardening |
| `glue/glue.go` | IPC subprocess manager + right flower health |
| `cmd/openhuman-flower-adapter/main.go` | OpenHuman bridge adapter |

## Key Env Vars

```
DEEPSEEK_API_KEY       — required LLM API key
EMBEDDING_ENDPOINT     — vector embedding API (enables semantic search)
EMBEDDING_API_KEY      — embedding auth key
RIGHTFLOWER_ENDPOINT   — right flower dispatch URL
LLM_PROVIDER           — deepseek (default), openai, xiaomi, local
PORT                   — HTTP server port (default 8013)
```

## Recent Milestones (2026-05-24/25)

| What | Status |
|------|--------|
| Right flower通车 (OpenHuman) | ✅ Full chain: dispatch → probe → memory search |
| P0: Vector semantic search | ✅ L1 parallel embedding + hybrid scoring |
| P1: AI code review | ✅ code_ai_review tool + SESSION_EXPIRED fallback |
| P2: Agent delegation | ✅ spawn_subagent/parallel + delegate_to_* tools |
| Event bus | ✅ observatory.PublishEvent + Subscribe |
| Conversation persistence | ✅ agent.complete → eval/run/conversations/ |
| L2 right flower health | ✅ glue.RegisterRightFlower + unified Pulse |
| Design docs | ✅ DESIGN_PRINCIPLES + MERGE_DECISIONS + KNOWN_LIMITATIONS |
| Gap analysis | ✅ openhuman_capability_map.md Step 2.5 |

## Unfinished

- Embedding endpoint not configured (was Qwen 27B — too heavy)
- LLM function calling API (text-mode JSON parsing instead)
- Cross-platform deploy (launchd is macOS-only, needs systemd for Linux)
- Event subscribers are log-only (no automated reactions yet)
