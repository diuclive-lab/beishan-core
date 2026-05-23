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

## 完成情况

- ✅ method mapping 已对齐 OpenHuman schema（472 个方法）
- ✅ token 获取路径：`~/.openhuman/core.token`
- ✅ `core.ping` 认证通过
- ⚠️ method 参数形状待对齐 schema
- ⚠️ 用户未登录时大部分方法不可用（scheduler gate 为 signed_out）

### 已验证

| 方法 | 认证 | 状态 |
|------|------|------|
| core.ping | Bearer token | ✅ |
| openhuman.memory_recall_memories | Bearer token | ⚠️ 需对齐参数 |
| openhuman.memory_doc_put | Bearer token | ⚠️ 需对齐参数 |
