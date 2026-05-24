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

---

## 附录 B：OpenHuman 通信架构对比（L2 层参考）

| 维度 | OpenHuman (Rust) | beishan-core (Go) | 可吸收 |
|------|-----------------|-------------------|--------|
| 路由层级 | 3 级：core → registry → legacy | 2 级：kernel → plugins | 参考 3 级设计 |
| RPC 协议 | JSON-RPC 2.0 over HTTP + Socket.IO | JSON lines over stdin/stdout (glue) | ✅ event 协议已实现 |
| 事件推送 | Socket.IO + Event Bus | 无（glue protocol 新增 event 类型） | ✅ event 已加 |
| 方法别名 | legacy_aliases.rs 显式映射表 | 无 | 未来可参考 |
| 认证 | Socket.IO auth + Bearer token | Bearer token（rightflower） | ✅ 已实现 |
| 可观测性 | observability.rs (3097 行) | internal/observatory/ (4 文件) | 架构精简，深度不足 |

### 关键代码参考

OpenHuman 项目 `/Users/dc/Desktop/cankaocangku/openhuman`：

| 文件 | 行数 | 功能 | 参考价值 |
|------|------|------|---------|
| `src/core/dispatch.rs` | 332 | 3 级 RPC 分发 | 路由架构设计 |
| `src/core/jsonrpc.rs` | 1783 | JSON-RPC 2.0 服务器 | 协议实现参考 |
| `src/core/socketio.rs` | 868 | Socket.IO 实时通信 | 事件推送架构 |
| `src/core/observability.rs` | 3097 | 可观测性系统 | 指标/事件追踪 |
| `src/core/legacy_aliases.rs` | 429 | 方法别名兼容 | 向后兼容设计 |
| `src/openhuman/tools/traits.rs` | 438 | 工具接口抽象 | 工具注册模式 |
