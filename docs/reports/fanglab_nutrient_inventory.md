# FangLab 营养层吸收清单

> FangLab/66 是营养层，不是双生花架构。以下为可吸收模块分类。

## 可直接迁移

| 模块 | 说明 | 预估 |
|------|------|------|
| eval runner | 评估执行器 | 2h |
| project-health | 项目健康度检查 | 2h |

## 需重写

| 模块 | 说明 | 预估 |
|------|------|------|
| evidence contract | 证据系统 | 4h |
| tool policy | 工具策略 | 3h |

## 暂缓

| 模块 | 原因 |
|------|------|
| code graph | 依赖 GitNexus，独立项目 |
| function calling shadow | 需要 DeepSeek 配合 |

## 不吸收

| 模块 | 原因 |
|------|------|
| RadixRoute | 语义路由与 beishan-core 的 DeepSeek 路由冲突 |
| MCP server | 已有 glue IPC |


## 近期吸收候选

| 优先级 | 模块 | 来源 | 预计 |
|--------|------|------|------|
| P1 | health JSON summary | project-health | 2h |
| P2 | eval runner | eval harness | 4h |
| P3 | evidence contract | evidence system | 4h |


## 执行队列

| 轮次 | 模块 | 风险 | 验收 |
|------|------|------|------|
| F1 | health JSON summary | low | ✅ core-health --json 已有 |
| F2 | eval runner | medium | go test -run Eval |
| F3 | evidence contract | medium | evidence schema test |
