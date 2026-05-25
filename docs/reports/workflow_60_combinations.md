# 60 工作流组合方案

> 覆盖 104 工具 + 15 MCP 技能。目标：工具组合验证 + L2 压力测试 + L3 质量门禁。

---

## A. 信息检索与处理（10）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| A01 | web_search → web_fetch → document_extract → knowledge_add | 搜索+抓取+提取+入库 | 数据管道 |
| A02 | rss_fetch → web_fetch → spawn_subagent(summarizer) → knowledge_add | RSS+提取+摘要+入库 | 自动化 |
| A03 | knowledge_search → knowledge_get → knowledge_update | 检索+读取+更新 | 知识管理 |
| A04 | knowledge_search → knowledge_semantic_search | 关键词+语义混合检索 | 检索对比 |
| A05 | github_readme → document_extract → knowledge_add | 项目文档入库 | 数据管道 |
| A06 | stock_quote → web_search → knowledge_add | 股票+搜索+入库 | 数据管道 |
| A07 | weather → knowledge_add | 天气记录 | 数据管道 |
| A08 | session_search → session_get → session_summarize → knowledge_add | 会话检索+摘要+入库 | 知识管理 |
| A09 | rss_fetch → web_fetch → spawn_subagent(researcher) | RSS+AI 分析 | 智能 |
| A10 | web_search → spawn_subagent(summarizer) | 搜索+AI 摘要 | 智能 |

## B. 代码分析与审查（10）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| B01 | code_tree → code_stats → code_lang_detect | 项目速览 | 静态分析 |
| B02 | code_diff → code_security_check → code_ai_review | 双重审查 | 安全门禁 |
| B03 | go_struct_scan → code_read → code_apply | 结构分析+修改 | 开发辅助 |
| B04 | code_security_check → code_apply | 自动修复安全漏洞 | 安全门禁 |
| B05 | code_ai_review → code_security_check | AI+规则交叉验证 | 安全门禁 |
| B06 | code_read_external → document_extract → knowledge_add | 外部代码入库 | 数据管道 |
| B07 | code_tree → dir_scan（对比验证） | 目录扫描对比 | 测试验证 |
| B08 | code_diff → knowledge_add | 变更记录入库 | 知识管理 |
| B09 | code_task + code_apply | 任务式编码 | 开发辅助 |
| B10 | spawn_subagent(coder) → code_security_check | AI 编码+安全审查 | 智能+安全 |

## C. 桌面与文件操作（8）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| C01 | desktop_actuator(get_window) → desktop_actuator(click) → desktop_actuator(type) | 桌面操作链 | 自动化 |
| C02 | desktop_actuator(screenshot) → vision_analyze | 截图分析 | AI+桌面 |
| C03 | search_files → document_extract → spawn_subagent(summarizer) | 文件检索+分析 | 数据管道 |
| C04 | csv_profile → csv_sample → knowledge_add | CSV 入库 | 数据管道 |
| C05 | read_file → patch | 编辑文件 | 文件操作 |
| C06 | validate_file_op → lock_file → patch → unlock_file | 安全编辑 | 文件操作+安全 |
| C07 | file_parse → knowledge_add | 文件解析入库 | 数据管道 |
| C08 | read_file → code_ai_review | 文件级 AI 审查 | 安全+AI |

## D. 数据分析（8）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| D01 | csv_profile → csv_sample → spawn_subagent | AI 分析 CSV | 数据+AI |
| D02 | document_extract → web_search → fact_check | 文档交叉验证 | 数据+智能 |
| D03 | knowledge_search → csv_profile | 知识库导出分析 | 知识管理 |
| D04 | stock_multi_quote → stock_quote → skill_equity_analysis | 股票分析链 | 数据+MCP |
| D05 | weather → knowledge_search | 天气+知识关联 | 数据+知识 |
| D06 | usage_report → knowledge_add | 用量记录 | 运维 |
| D07 | system_info → knowledge_add | 系统状态记录 | 运维 |
| D08 | kb_audit → kb_repair | 知识库健康检查 | 运维 |

## E. Agent 委派与 MCP 技能（12）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| E01 | spawn_parallel(researcher + coder) → 对比结果 | 并行委派 | Agent |
| E02 | delegate_to_researcher → knowledge_add | 研究员+入库 | Agent |
| E03 | delegate_to_summarizer → knowledge_add | 摘要+入库 | Agent |
| E04 | spawn_subagent → spawn_parallel → 结果合并 | 串行+并行组合 | Agent |
| E05-E10 | skill_* → knowledge_add | 6 个 MCP 技能+入库 | MCP |
| E11 | skill_domain_synthesis → spawn_parallel | 跨领域+并行委派 | MCP+Agent |
| E12 | skill_repository_guidance → code_security_check | 规范+安全审查 | MCP+安全 |

## F. 运维与治理（8）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| F01 | kb_audit → kb_repair → knowledge_embed_all | 知识库巡检 | 运维 |
| F02 | knowledge_dedupe → knowledge_merge | 去重合并 | 治理 |
| F03 | evidence_search → session_search | 审计追溯 | 安全 |
| F04 | todo_list → todo_done → todo_add | 任务管理闭环 | 运维 |
| F05 | usage_report → todo_add | 用量触发任务 | 运维 |
| F06 | session_cleanup → session_summarize → knowledge_add | 会话归档 | 运维 |
| F07 | skill_idle_inspection → system_info | 系统自检 | 运维+MCP |
| F08 | notify_send → todo_add | 通知触发待办 | 运维 |

## G. 混合组合（4）

| ID | 链路 | 涉及工具 | 类型 |
|----|------|---------|------|
| G01 | skill_evaluate → code_ai_review → code_security_check | 质量门禁 | 安全+MCP |
| G02 | knowledge_search → spawn_subagent → knowledge_add | 知识驱动委派 | 知识+Agent |
| G03 | csv_profile → skill_math_modeling → knowledge_add | 数据建模 | 数据+MCP |
| G04 | skill_ai_frontier → web_search → knowledge_add | 前沿跟踪闭环 | MCP+搜索 |

---

## 实施说明

### 分层执行策略

| 类型 | 数量 | 执行方式 |
|------|------|---------|
| 数据管道 (A/C/D 部分) | ~20 | 直接串行调用，无需 YAML |
| 安全门禁 (B 部分) | ~8 | 需要硬化层验证 |
| Agent/MCP (E/G 部分) | ~16 | 需要 LLM 调用，耗时较长 |
| 运维 (F 部分) | ~8 | 定时触发 |

### 测试优先级

```
第一梯队（A01-A10, B01-B05）：数据管道+安全门禁 → 立即执行
第二梯队（C01-C08, D01-D08）：桌面操作+数据分析 → 需要运行环境
第三梯队（E01-E12, G01-G04）：Agent/MCP → 需要 LLM
第四梯队（F01-F08）：运维 → 需要知识库数据
```
