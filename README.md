# beishan-core

**确定性代码管住 LLM 的不可靠输出。**

beishan-core 是一个以**硬化层架构**为核心的 AI Agent 框架。核心思路：LLM 只做文本生成，路由、校验、权限、编排全用确定性代码控制。

## 产品形态三分

| 层次 | 构成 | 职责 |
|------|------|------|
| **底座** | kernel/ + glue/ + internal/ | 硬化层 + 工具集 + 工作流引擎 |
| **左花** | plugins/ + workflows/ | 内置生产执行侧 |
| **右花** | 第三方开发者通过 RIGHT_FLOWER_PROTOCOL.md 接入 | 外部工具（由社区定义） |

**第一个实际右花**：[OpenHuman](https://github.com/tinyhumansai/openhuman) — 本地运行的 AI Agent 引擎，提供向量记忆、代码审查、Agent 委派能力（2026-05-24 全链路通车）。右花是协议层概念，不绑定到任何具体项目。


**能力概览**：
- 🔒 **硬化层**：ValidateAndExecute + code_security + isSafePath + parseDecision 五道防线
- 🔍 **L3 工具（99 个）**：文件处理 · 网页搜索 · 知识管理 · 终端执行 · 浏览器自动化 · 待办管理 · 代码安全 · AI 审查 · 子智能体委派 · 图像生成 · 语音合成 · 汇率天气 · RSS · GitHub 集成 · 通知推送 · 更多
- 🧠 **双工作流引擎**：YAML（AI 可修改）+ Go-DSL（编译时安全）
- 🤖 **子智能体委派**：AgentDefinition 注册表 + spawn_subagent/spawn_parallel + 对话持久化
- 🌸 **右花协议**：HTTP dispatch + manifest 加载 + probe-methods + 统一健康监控（首个实现: OpenHuman）
- 📊 **可观测性**：决策追踪 · 因果证据图 · 评估框架（bench + 检索质量 15 测试）· 事件日志
- 🧩 **插件体系**：23 个 L4 编排插件 + 33 个 YAML 工作流
- 📝 **澄清系统**：结构化澄清契约 + EWMA 模式学习（歧义消解）
- 🔄 **本地模型故障切换**：11 引擎扫描 + API→local 自动降级 + 滞后防抖回切




## 代码架构四层

| 层 | 目录 | 职责 | 可修改 |
|---|------|------|--------|
| L1 内核 | kernel/ | 注册 + 首轮 AI 路由 + Type 转发 | 冻结 |
| L2 胶水 | glue/ | IPC + 子进程生命周期管理 | 可迭代 |
| L3 工具 | internal/tools/ + internal/workflow/ + internal/bench/ 等 | 工具注册 + Schema 校验 + 双引擎 + 评估 | 可迭代 |
| L4 编排 | plugins/ + workflows/ | 编排 L3 完成多步任务 | 随意改 |

**三层描述的是产品构成，四层描述的是代码组织。两者互补不矛盾。**

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
| `EMBEDDING_ENDPOINT` | 否 | 向量嵌入 API 端点（启用语义搜索） |
| `EMBEDDING_API_KEY` | 否 | 嵌入 API 认证密钥 |
| `RIGHTFLOWER_ENDPOINT` | 否 | 右花 dispatch 端点（默认 http://localhost:9529/dispatch） |
| `TAVILY_API_KEY` | 否 | Tavily 搜索 API key（增强搜索质量） |
| `HERMES_HOME` | 否 | 知识库存储路径（默认 `~/.hermes`） |
| `PORT` | 否 | HTTP 端口（默认 8013） |

> *需要至少设置 `DEEPSEEK_API_KEY` 或 `LLM_API_KEY` 之一。

## 右花（Right Flower）

右花是遵循 `docs/RIGHT_FLOWER_PROTOCOL.md` 的外部工具。首个完整实现是 OpenHuman：

```bash
# 启动 OpenHuman Core
/Users/dc/Desktop/cankaocangku/openhuman/target/release/openhuman-core serve &

# 启动 adapter（端口 9529）
export OPENHUMAN_TOKEN=$(cat ~/.openhuman/core.token)
./openhuman-flower-adapter &

# 启用 manifest
cp right_flowers/openhuman.yaml.example right_flowers/openhuman.yaml

# 重启 beishan-core → 验证
curl http://localhost:9529/probe-methods
curl -X POST http://localhost:8013/api/chat \\
  -H 'Content-Type: application/json' \\
  -d '{"recipient":"openhuman","type":"memory.search","payload":{"namespace":"personal"}}'
```

详情：`docs/reports/openhuman_rightflower_integration_record.md`

## 关键文档

| 文档 | 内容 |
|------|------|
| [CLAUDE.md](./CLAUDE.md) | AI 协助者项目上下文（结构化速查） |
| [DIRECTORY.md](./DIRECTORY.md) | 目录结构与物理布局 |
| [DESIGN_PRINCIPLES.md](./DESIGN_PRINCIPLES.md) | 设计哲学与铁律 |
| [docs/HARDENING_LAYER.md](./docs/HARDENING_LAYER.md) | 硬化层能力边界 |
| [docs/MERGE_DECISIONS.md](./docs/MERGE_DECISIONS.md) | 关键架构决策记录 |
| [docs/KNOWN_LIMITATIONS.md](./docs/KNOWN_LIMITATIONS.md) | 已知设计边界 |
| [docs/RIGHT_FLOWER_PROTOCOL.md](./docs/RIGHT_FLOWER_PROTOCOL.md) | 右花接入协议 |
| [CHANGELOG.md](./CHANGELOG.md) | 版本更新摘要 |

## 许可证

MIT © 2026 diuclive-lab
