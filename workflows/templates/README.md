# 工作流模式库

从现有工作流中提炼的 5 种编排模式。每个模式是一个 YAML 骨架，填参数即可生成新工作流。

## 使用方式

1. 找一个匹配的模式
2. 复制对应的 `.yaml` 文件到 `workflows/`
3. 按卡片中的 `TODO` 标记替换参数
4. 调用测试

## 模式索引

| 模式 | 文件 | 步骤数 | 适用场景 | 示例 |
|---|---|---|---|---|
| ingest | `ingest.yaml` | 3 | 内容→分析→入库 | personal_knowledge_ingest |
| review | `review.yaml` | 4 | 获取→审查→修复→报告 | knowledge_review |
| suggest | `suggest.yaml` | 2 | 获取→推荐→报告 | knowledge_suggest_links |
| aggregate | `aggregate.yaml` | 3 | 多数据源聚合→聚类→报告 | weekly_review, github_radar |
| search_act | `search_act.yaml` | 3 | 检索→处理→输出 | writing_assistant |

## 层级关系

模式之间可以组合：
- `ingest` 是数据入口，产生知识条目
- `review` / `suggest` 是条目级别的质量保障
- `aggregate` 是跨条目的周期性复盘
- `search_act` 是基于知识库的输出生产
