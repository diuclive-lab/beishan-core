# 工作记录

## 2026-05-23 项目重构与文档体系建立

### 1. Go-DSL 工作流引擎

**背景**：L3 插件手写 OnMessage switch-case 模式重复。YAML 引擎灵活但无编译时安全。

**交付**：internal/workflow/gods_executor.go（~290 行）
- GoExecutor：编译时安全的 Go-DSL 工作流执行器
- GoWorkflowPlugin：kernel.Plugin 适配器
- NewGoToolPlugin：一行代码注册简单 L3 插件
- StateStore：map 级路径取值（Get("step.field.nested")），无字符串替换

**架构约束**：
1. 编译时校验 Tool 注册表（validateGoStep → tools.GetToolSchema）
2. 零冗余校验 — 通过 kernel.Call 走标准路由，信任 L3 硬化层
3. Before/AfterExecute 约定不做 I/O

**文件**：
- internal/workflow/types.go（+80 行：GoStep、GoWorkflow、StepStatus、ErrorStrategy）
- internal/workflow/gods_executor.go（新增）
- cmd/beishan/legal_review_go_dsl.go（新增：legal_review 的 Go-DSL 声明式版本）

### 2. 目录结构重构

**背景**：168+ 次提交后，根目录混杂二进制文件、备份目录、开发示例。Go 标准布局未落地。

**变更**：
- main.go + preroute.go → cmd/beishan/
- web/ → cmd/beishan/web/（go:embed 路径自动跟随）
- 5 个开发示例（go_example、python_example、l3_echo_*、l4_template_go.go）→ examples/
- 4 个 DEVLOG → docs/devlog/
- .gitignore 补全：eval/run/logs/、eval/run/pids/、generated/、repl、examples 二进制
- PERSONAL_KB_GUIDE.md、PLAN_CODING_AGENT.md 取消跟踪（gitignore）
- .github/workflows/ 恢复跟踪（CI 配置对外可见）

**根目录最终状态**（5 个文件）：
CHANGELOG.md  DIRECTORY.md  DESIGN_PRINCIPLES.md  go.mod  .gitignore

### 3. 文档体系建设

| 文档 | 内容 |
|------|------|
| README.md | 项目介绍、架构图、快速开始、配置表 |
| DIRECTORY.md | 目录结构声明、每层到每目录映射、关键决策 |
| LICENSE | MIT 开源许可证 |
| .env.example | 环境变量模板 |
| docs/HARDENING_LAYER.md | 硬化层三层关卡、保证/不保证表 |
| docs/MERGE_DECISIONS.md | 7 项关键决策记录 |
| docs/KNOWN_LIMITATIONS.md | 10 项已知设计边界 |
| DESIGN_PRINCIPLES.md | 文档导航表、双引擎说明补全 |

### 4. Bug 修复

- **legal_review 工作流**：消息类型 legal_write → legal_generate_report，字段名 analysis → analysis_report
- **legal_write_plugin**：兼容 workflow 引擎的 string-encoded JSON 传递
- **todo_plugin**：返回空 Message → 返回带 Type+Payload 的响应

### 5. 测试验证

全功能回归测试全部 PASS：Health / Chat / Web search/fetch / code_security / Knowledge / Todo / Legal review YAML 4/4 / Go-DSL

### 6. 未完成

- 三项目融合（TwinFlower + FangLab）：已分析未执行
- 插件开发指南：未创建
- 模块名对齐（module beishan vs github.com/...）
- workflows/parking/：4 个原型未决定去留
