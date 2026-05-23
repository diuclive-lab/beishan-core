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
