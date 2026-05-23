# OpenHuman 右花集成记录

> OpenHuman (github.com/tinyhumansai/openhuman) 是首个右花参考实现。

## 边界

- OpenHuman 是 GPL-3.0 项目，不复制任何源码进入 Core
- 仅通过 HTTP localhost 边界调用（端口 7788 → adapter 9529 → Core）
- `right_flowers/openhuman.yaml.example` 默认 `enabled: false`，不参与首轮路由
- 启用步骤：`export OPENHUMAN_TOKEN=xxx` → 启动 adapter → 改 `.example` 为 `.yaml` → 重启 Core

## 协议

- JSON-RPC 2.0 over `/rpc`
- Bearer token auth（`OPENHUMAN_TOKEN`）
- 4 个 method mapping：memory.search / memory.store / context.retrieve / code.review

## 安全

- 所有 findings 标记 `verified: false`
- 非 2xx 状态码分类为"调用失败"
- adapter 每次 dispatch 时 probe OpenHuman 存活状态

## 未完成

- method mapping 需对齐 OpenHuman 实际 RPC 方法名
- token 获取流程未自动化（需手动配置）
- 返回结构解析为结构化 findings 待实现
