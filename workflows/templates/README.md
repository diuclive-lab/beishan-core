# 工作流模式库

从现有 12 个工作流中提炼的编排模式库。分两层：

## patterns/ — 设计模式卡片

抽象编排模式，告诉 skill_factory "有哪些编排方式可选"。不可直接执行。

| 模式 | 文件 | 步骤 | 适用场景 | 参考实现 |
|---|---|---|---|---|
| 线性链路 | `patterns/linear.yaml` | 3 | 简单串行处理 | personal_knowledge_ingest |
| 并行采集 | `patterns/parallel.yaml` | 2+ | 多源并发数据采集 | opensource_project_ingest |
| 审查循环 | `patterns/review_loop.yaml` | 4 | 获取→审查→修复→报告 | knowledge_review |
| 多源聚合 | `patterns/aggregate.yaml` | 3+ | 多数据源→聚类→输出 | weekly_review, github_radar |

## domains/ — 垂直领域模板

可直接复制改造的工作流起点。复制到 `workflows/` 后修改 TODO 标记即可使用。

| 模板 | 文件 | 基础模式 |
|---|---|---|
| 法律审查 | `domains/legal.yaml` | parallel + review_loop |
| 研究调研 | `domains/research.yaml` | parallel + linear |
| 定时监控 | `domains/monitoring.yaml` | parallel + aggregate |

## 使用方式

```bash
# 从模板创建新工作流
cp workflows/templates/domains/research.yaml workflows/my_research.yaml
# 修改 TODO 标记
vim workflows/my_research.yaml
# 评估质量
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_evaluate","payload":{"name":"my_research"}}'
```

## 模式组合

模式之间可以组合嵌套。例如 `domains/legal.yaml` 内部使用了：
- `parallel`（并行检索法律条文和判例）
- `review_loop`（获取→分析→输出）
