# OpenHuman 能力地图与吸收方案

> 基于 2026-05-24 全量实测。目标：右花探路 → 左花吸收。

---

## 一、实测结果

### Core — 全部通过

| 方法 | 实测 | 说明 |
|------|------|------|
| `core.ping` | ✅ | `{"ok": true}` |
| `core.version` | ✅ | `v0.54.10` |

### Memory — 全部通过

| 方法 | 实测 | 说明 |
|------|------|------|
| `memory.search`(recall) | ✅ | 返回 namespace 内全部记忆，无搜索词过滤 |
| `memory.store`(doc_put) | ✅ | key 去重，存储文档并向量化 |
| `context.retrieve`(context_query) | ✅ | **语义搜索** — 接受自然语言 query，返回最相关上下文 |
| `memory.list_namespaces` | ✅ | 列出所有 namespace |

**关键发现**：`context.retrieve` 是真正的向量语义搜索。存入文档后，语义匹配检索生效。

### 其他

| 方法 | 实测 | 说明 |
|------|------|------|
| `code.review`(agent_chat) | ⚠️ | 需 `message` 参数（对话模式），非纯代码审查 |
| `security_policy_info` | ✅ | 返回允许的命令列表 |

### 待深层探测

OpenHuman 实际包含 40+ 领域（tools/impl/ 下的 13 个工具模块），但 adapter 只暴露了 4 个 method。完整能力列表：

| 模块 | 能力 | 当前是否暴露 | 对底座的价值 |
|------|------|-------------|-------------|
| memory | 向量存储/召回/遗忘 | 部分 | ⭐⭐⭐ 最高 |
| agent | 委派/子智能体/偏好记忆 | ❌ | ⭐⭐ 参考设计 |
| filesystem | Git/grep/glob/文件操作 | ❌ | ⭐ 底座已有 96 工具 |
| browser | 浏览器自动化 | ❌ | ⭐ 底座已有 BrowserPlugin |
| network | MCP 工具链/Composio | ❌ | ⭐⭐ 右花协议可借鉴 |
| cron | 定时任务 CRUD | ❌ | ⭐ 底座已有 Scheduler |
| system | Shell/node/npm/LSP | ❌ | ⭐ 底座已有 TerminalPlugin |

---

## 二、吸收方案：memory.search 向量化

### 现状（beishan-core）

```
internal/tools/knowledge.go
  ├─ Embedding 字段 ✅ 已存在
  ├─ tryEmbedding() ✅ 可计算向量
  ├─ batchFillEmbedding() ✅ 批量补全
  ├─ embeddingEndpoint()/Model() ✅ 可配置
  └─ knowledge_search 目前以关键词匹配为主
```

`internal/retrieval/contract.go` 已定义 `KindSemantic` 检索类型和统一 `RetrievalResult` 格式。但实际的语义检索链路未完全打通。

### 吸收目标

将 OpenHuman 的 `memory_context_query`（自然语言 → 向量检索 → 召回最相关上下文）的模式吸收到底座中，补全 `keyword_match → vector_retrieve → rerank` 的完整链路。

### 吸收范围（深度分析）

| 组件 | 当前 | 吸收后 | 风险 |
|------|------|--------|------|
| `internal/tools/knowledge.go` | 关键词匹配 + embedding 字段 | 向量检索评分 + rerank | 低 |
| `internal/retrieval/retrieval.go` | 仅 contract.go | 实际检索执行引擎 | 低 |
| `plugins/memory_plugin.go` | knowledge_search 路由 | 自动语义回退 | 中 |
| `kernel/router.go` | 不涉及 | 不涉及 | ✅ 隔离 |

### 不吸收的部分

| 功能 | 理由 |
|------|------|
| OpenHuman 的树摘要(TreeSummarizer) | 底座知识库规模小，无需分层摘要 |
| 向量数据库 | 底座使用 JSON 文件 + 内存索引，小于 SVM |
| agent_chat(code.review) | 底座有自己的 code_security 硬化层 |

### 广度分析（非孤立检查）

```
调用链（吸收前）：
  think_plugin → knowledge_search → 关键词匹配 → 返回结果
                                    └─ 语义回退（仅关键词不足时）

调用链（吸收后）：
  think_plugin → knowledge_search → L0 关键词 + L1 语义并行
                                    → hybrid 混合评分 → rerank → 返回
```

**受影响的调用者**：
- `plugins/think_plugin.go` — 检索知识时调用 knowledge_search
- `plugins/memory_plugin.go` — knowledge_search handler
- `internal/tools/knowledge.go` — 检索执行 + embedding

**谁被它调用**：
- LLM embedding API（外部 HTTP）
- `internal/retrieval/` — 统一结果渲染

---

## 三、吸收优先级

| 优先级 | 能力 | 预估工作量 | 依赖 |
|--------|------|-----------|------|
| P0 | 向量语义检索（吸收 context_query 模式） | 2-3h | ✅ 已完成 |
| P1 | 检索质量评估（bench 套件） | 1h | 🔲 |
| P2 | 自动语义回退（关键词无结果时降级） | 1h | ✅ 已包含在 P0 中 |
| P3 | adapter methodMap 扩展（更多 OpenHuman 能力暴露） | 0.5h | 🔲 |

### 运行时经验

- **Qwen3.6-27B 太重**：~3-4s/条 embedding，237 条需 ~15min。建议用 nomic-embed-text（137MB）或 bge-small
- **配置方式**：`EMBEDDING_ENDPOINT=http://<host>:<port>/v1/embeddings` + `EMBEDDING_API_KEY=xxx`
- **当前状态**：236/237 条已有 embedding（Qwen 完成），配置已注释掉。下次需轻量模型时重新启用

---

## 四、参考项目义务

被吸收来源：OpenHuman (github.com/tinyhumansai/openhuman)

| 吸收项 | 内化位置 | 状态 |
|--------|---------|------|
| `memory_context_query` 自然语言→向量检索模式 | `internal/tools/knowledge.go` 检索管线 L1 层 | ✅ 已完成 |

### 具体变更

| 文件 | 改动 |
|------|------|
| `internal/tools/knowledge.go` | L2 语义回退 → L1 并行语义检索，关键词与语义混合评分（max 合并），检索管线不再因关键词不足才触发语义搜索 |

### 验证

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./internal/tools/...` ✅

未吸收：
- TreeSummarizer — 底座知识库规模不匹配
- agent_chat — 底座已有会话管理
- 向量数据库 — 保持文件 + 内存索引
