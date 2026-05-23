# beishan-core

**确定性代码管住 LLM 的不可靠输出。**

beishan-core 是一个基于"硬化层"架构的 AI Agent 框架。核心思路：LLM 只做文本生成，路由、校验、权限、编排全用确定性代码控制。

```
LLM / L4 编排
    │
    ▼
① Router.parseDecision（三层校验：JSON格式→置信度≥0.4→收件人存在）
    │
    ▼
② ValidateAndExecute（JSON Schema 校验参数）
    │
    ▼
③ L3 工具执行（isSafePath + code_security 安全检查）
    │
    ▼
   返回结果
```

## 架构

四层架构，从内核到编排严格分层：

| 层 | 目录 | 职责 | 可修改 |
|---|------|------|--------|
| L1 内核 | `kernel/` | 注册 + 路由（强制 DeepSeek）+ 消息转发 | 冻结 |
| L2 胶水 | `glue/` | IPC + 子进程生命周期管理 | 可迭代 |
| L3 工具 | `internal/tools/` | 工具注册 + Schema 校验 + 硬化执行 | 可迭代 |
| L4 编排 | `plugins/` | 编排 L3 完成多步任务 | 随意改 |

详见 [DIRECTORY.md](./DIRECTORY.md)（目录结构）和 [DESIGN_PRINCIPLES.md](./DESIGN_PRINCIPLES.md)（设计哲学）。

## 快速开始

### 前置条件

- Go 1.26（`go.mod` 指定版本）
- DeepSeek API key 或其他 LLM API key（见配置表）

### 启动

```bash
# 1. 配置 API key
echo 'DEEPSEEK_API_KEY=sk-your-key-here' > .env

# 2. 编译
go build -o beishan ./cmd/beishan/

# 3. 启动（默认端口 :8013）
./beishan

# 4. 测试
curl http://localhost:8013/health
# → {"status":"ok"}
```

### 发送第一条消息

```bash
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"搜索 beishan-core 项目信息"}'
```

### 运行烟雾测试

```bash
eval/scripts/run_legal_smoke.sh
```

## 配置

| 环境变量 | 必填 | 说明 |
|----------|------|------|
| `DEEPSEEK_API_KEY` | 是* | DeepSeek API key |
| `LLM_API_KEY` | 否 | 通用 LLM API key（未设置时使用 DEEPSEEK_API_KEY） |
| `LLM_PROVIDER` | 否 | LLM 提供商：`deepseek`（默认）、`openai`、`xiaomi`、`local` |
| `LLM_MODEL` | 否 | 模型名（默认由提供商决定） |
| `TAVILY_API_KEY` | 否 | Tavily 搜索 API key（增强搜索质量） |
| `HERMES_HOME` | 否 | 知识库存储路径（默认 `~/.hermes`） |
| `PORT` | 否 | HTTP 端口（默认 8013） |

> *需要至少设置 `DEEPSEEK_API_KEY` 或 `LLM_API_KEY` 之一。

## 关键文档

| 文档 | 内容 |
|------|------|
| [DIRECTORY.md](./DIRECTORY.md) | 目录结构与物理布局 |
| [DESIGN_PRINCIPLES.md](./DESIGN_PRINCIPLES.md) | 设计哲学与铁律 |
| [docs/HARDENING_LAYER.md](./docs/HARDENING_LAYER.md) | 硬化层能力边界 |
| [docs/MERGE_DECISIONS.md](./docs/MERGE_DECISIONS.md) | 关键架构决策记录 |
| [docs/KNOWN_LIMITATIONS.md](./docs/KNOWN_LIMITATIONS.md) | 已知设计边界 |
| [CHANGELOG.md](./CHANGELOG.md) | 版本更新摘要 |

## 许可证

MIT © 2026 diuclive-lab
