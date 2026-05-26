# 交接文档 — 下一会话接续指南

## 当前状态

三个右花全部通过吸收工作流 v2.5 认证：

| 右花 | 吸收等级 | 状态 | 端口 |
|:----|:--------|:----:|:----:|
| OpenHuman | L1 wrap | CERTIFIED | :9529 |
| Hermes Agent | L1 wrap | CERTIFIED (adapter 有 bug) | :9532 |
| OpenClaw | L1 wrap | CERTIFIED | :9533 |

## 已配置

- embedding 服务（nomic-embed-text-v1.5，:8090，768 维）
- 声明式多 Provider（LLM_PROVIDERS_CONFIG）
- 9 路并行代码审查工作流（code_review_9x）
- 通道层余量接口（internal/channels/）
- 记忆存储余量接口（internal/memory/）
- Query DSL 余量接口（internal/retrieval/query.go）
- Router usage 埋点

## 待办

| P0 | Hermes adapter `agent.chat` 修复 | 当前是空壳，不转发到 Hermes AIAgent |
| P0 | `internal/webhooks/` 吸收 | ~180 行，待底座部署后 |
| P1 | embedding 知识重索引 | 触发 knowledge_reindex 工作流 |
| P2 | 右花 route_exposed 评估 | 当前全部 false，是否需要按需打开 |

## 架构关键文档

- `workflows/absorb_right_flower.yaml` — 14 步吸收工作流（v2.5）
- `docs/ABSORPTION_GOVERNANCE.md` — 治理框架
- `docs/V25_WORKFLOW_STANDARD.md` — v2.5 YAML 标准
