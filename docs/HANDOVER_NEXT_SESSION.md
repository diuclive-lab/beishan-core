# 交接文档 — 下一会话接续指南

## 当前状态

正在验证 Hermes Agent 吸收工作流的 Step 3（深度实现验证）。
Step 0-2.5 已完成，但有一个缺口：**"已吸收"的 2 个能力（model_providers→think_plugin, process_registry→glue）没有做 Step 3 验证，不确定吸收质量。**

## 下一步工作

### P0：验证吸收质量
调 code_diff 或直接读 Hermes 源码，对比真实差异：
- model_providers vs think_plugin + llm/config.go：Hermes 支持多提供商自动切换/降级，我们的 llm 是否也有？
- process_registry vs glue/glue.go：Hermes 的进程管理是否更完整，我们的健康检查是否够用？

### 已知问题（待确认）
- 知识库同步：Mac B 有 62 条知识，Mac A 还没有（export 文件在 Mac B 桌面）
- knowledage_export.json 的 git 私人仓方案待决策（知识库里有记录）
- preRoute 的搜索 query 长度检测（>15字走LLM Router 不做 preRoute）未落地

### 工具状态
- 服务运行在 :8013
- Python 右花包装器在 :9532（Hermes Agent）
- OpenHuman 适配器在 :9529
- 启动命令：`go run ./cmd/beishan/`

### 架构关键文档
- `workflows/absorb_right_flower.yaml` — 7 步吸收工作流（含经验积累）
- `workflows/project_analyze.yaml` — 源码级深度分析（L3 工具替代 terminal）
- `docs/RIGHT_FLOWER_PROTOCOL.md` — 右花接入协议
- `cmd/rightflower-python-wrapper/rightflower_adapter.py` — Python 右花模板

### 重要架构原则
- 吸收评估逐能力，不逐项目（Step 2 条件0）
- 评估前必须读源码（Step 0）
- compared ≠ skip，是取长补短
- 硬化层不保证逻辑正确性（明确声明能力边界）
