# 交接文档 — 下一会话接续指南

> 生成时间：2026-05-26
> 前序会话覆盖：2026-05-25 全天 + 2026-05-26 全天

---

## 当前项目状态

```
beishan-core v0.2.0
  105 tools | 15 MCP skills | 3 right flowers | 40 workflows (v2.5)
  hardening 8/8 | 23 plugins | kernel frozen
```

## 你能用的关键信息

**项目根目录**: `/Users/dc/Desktop/0`

**CLAUDE.md 必须先读**。它包含了架构速查、6 条铁律、构建命令、关键文件清单、Guardrails、已完成/未完成、已知摩擦点。

**治理文档链**：
- `docs/ABSORPTION_GOVERNANCE.md` — 吸收治理框架（证据等级、风险分类等）
- `docs/V25_WORKFLOW_STANDARD.md` — v2.5 YAML 编写标准
- `docs/devlog/DEVLOG_20260526.md` — 今日完整变更记录

## 用户最在意的事

1. **吸收工作流 v2.5** — `workflows/absorb_right_flower.yaml`。14 步流程 + 治理框架。
2. **搜索源拆分** — 当前 web_search 不区分来源。需要：代码→GitHub、模型→魔塔社区。
3. **YAML 引擎 parallel 变量传递** — ctxKey 已修复但未完整验证。
4. **ECS 隧道** — SSH 反向隧道运行中，但 launchd 注册因 TCC 权限失败。

## 不要做的事

- ❌ 不要修改 kernel/（冻结）
- ❌ 不要直接调 tools.Execute（必须用 ValidateAndExecute）
- ❌ 不要加右花代码到底座（adapter 在 cmd/ 下）
- ❌ 不要跳过缺口分析
- ❌ 不要把右花 route_exposed 开 true

## 待办（优先级排序）

| P0 | 搜索源拆分 | web_search 按来源分类 |
| P0 | YAML parallel 模板验证 | 修复模板变量传递 |
| P1 | iOS 客户端重新编译 | 部署最新代码到 iPhone |
| P1 | ECS relay launchd 修复 | TCC 权限问题 |
| P2 | OpenHuman 桌面端编译 | vendored CEF 兼容性 |

## v2.5 完成情况

40/40 YAML 已升级。详见 `docs/V25_WORKFLOW_STANDARD.md` §5 状态表。

## 当前已知问题

1. 上下文断裂已修复（`tools.MemoryDir`），需客户端重新编译
2. 桌面路由已确定性地通过 preroute 走 desktop_actuator
3. 硬化层绕过已修复（`tools.Execute` → `ValidateAndExecute`）
4. 右花 route_exposed 已全部关闭
5. workflow engine 栈溢出已修复（`resolveJSONValue` + depth 限制）
6. 本地模型已从 Qwen3.6-27B 切换到 gemma-4-E4B（port 8090）+ failover gemma-4-31B（port 8091）

## 最近变更

```
(未提交 — 45 文件, +1494 / −951)
* 治理根茎搭建: ABSORPTION_GOVERNANCE.md + V25_WORKFLOW_STANDARD.md
* 40/40 YAML 升级至 v2.5
* Go 工具增强: code_tree/code_stats/ go_struct_scan/code_read_external + base_capability_inventory
* Agent 7 缺口修复: 并发安全/错误分类/截断通知/输出校验/LLM 重试/异步写盘/空 prompt
* workflow engine 栈溢出修复
* 本地模型: Qwen27B → gemma4-E4B + gemma4-31B failover
```

## 交接结束语

用户对"吃透"要求很高。不要走流程过场。每次提交前问自己：**关上源码，我能重新实现吗？** 如果不确定，说明没吃透。
