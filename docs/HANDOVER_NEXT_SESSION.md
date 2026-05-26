# 交接文档 — 下一会话接续指南

## 当前状态

上次会话结束在 commit `cb66511`，工作目录 clean。远程已推送。

### Git 快照

```
cb66511 docs: 晚间开发日志 — 集成纪律框架注入 + pre-commit 首次拦截 + 反思
d56ef8c docs: 文档硬化层补充 — 维护规则表格 + DATA_FLOW.md 断路声明
d598cbd docs + tooling: 集成纪律框架 — DATA_FLOW.md + 集成纪律注入 + pre-commit
```

### 三个右花状态

| 右花 | 吸收等级 | 状态 | 端口 |
|:----|:--------|:----:|:----:|
| OpenHuman | L1 wrap | CERTIFIED | :9529 |
| Hermes Agent | L1 wrap | CERTIFIED (adapter agent.chat 空壳) | :9532 |
| OpenClaw | L1 wrap | CERTIFIED | :9533 |

## 刚完成的工作（2026-05-26 晚间）

从 `/Users/dc/Desktop/files/` 三个桌面文件整合了"集成纪律框架"：

- **docs/DATA_FLOW.md** — 新增，8 条端到端路径（4 通 4 断）
- **CLAUDE.md** — 注入"## 集成纪律"章节（7 节强制执行规则）
- **DESIGN_PRINCIPLES.md** — 合并"集成纪律"章节（6 节设计哲学）
- **scripts/integration_check.sh** — 新增集成检查脚本
- **scripts/pre-commit** — 新增，已安装到 .git/hooks/
- **三包加 UNIMPLEMENTED 标记** — channels/legacy/memory

## 优先任务（按推荐顺序）

### P0：SessionHandler 修复（路径 E）

`docs/DATA_FLOW.md` 路径 E 记录了最影响体验的断点：
```
kernel.Send(msg{ReplyTo: "session:xxx"})
  → kernel.deliverReply
  → SessionHandler ← nil，消息在此丢失
```
改动范围：`kernel/kernel.go` + `cmd/beishan/main.go`，不碰冻结区。

### P0：Hermes adapter agent.chat 修复

当前是空壳，不转发到 Hermes AIAgent。

### P0：internal/webhooks/ 吸收

~180 行代码，待底座部署后实施。

### P1：Embedding 知识重索引

配置已完成（nomic-embed-text-v1.5，:8090，768 维），运行 knowledge_reindex 工作流。

### P2：右花 route_exposed 评估

当前全部 false，是否需要按需打开。

### 后续：其余断路修复

| 路径 | 问题 | 优先级 |
|:----:|------|:------:|
| F | parseToolSuggestions 孤立函数 | 中 |
| G | observatory 数据无消费者 | 中 |
| H | glue readEvents 被遮蔽 | 低 |

## 集成纪律（新规则，对 AI 有强制约束力）

每次对话开始时必须问：
> 1. scripts/integration_check.sh 当前的输出是什么？
> 2. 这次要实现的功能在 docs/DATA_FLOW.md 里是否已有对应路径？

每次声明"完成"前必须输出：
```
---INTEGRATION_PROOF---
新增符号: [函数名 / 包名 / 方法名]
非测试调用点: [文件路径:行号]
数据流: [入口] → [你的代码] → [出口]
DATA_FLOW.md 已更新: [是 / 否]
integration_check.sh 无新增警告: [是 / 否]
状态: [已完成集成 / 已实现但未集成]
---END_PROOF---
```

占位符必须写 `// UNIMPLEMENTED` 注释，不得写空接口/空结构体。
kernel/ 是冻结区，不得修改。

## 关键架构约束

- 四层架构：L1 kernel/（冻结）→ L2 glue/ → L3 internal/tools/ → L4 plugins/
- 硬化层不可绕过：所有工具调用必须经过 ValidateAndExecute
- 右花 = 协议，不是集成：manifest yaml + HTTP dispatch，右花代码不进底座
- 100+ 工具，23 L4 插件，40 YAML 工作流
- 默认 Provider：DeepSeek，可切换 xiaomi/openai/local

## 关键文档

| 文档 | 作用 |
|------|------|
| CLAUDE.md | 项目概览 + 架构 + 集成纪律 |
| DESIGN_PRINCIPLES.md | 设计哲学 |
| docs/DATA_FLOW.md | 端到端数据流（含断路路径） |
| docs/ABSORPTION_GOVERNANCE.md | 吸收治理框架 |
| docs/KNOWN_LIMITATIONS.md | 已知限制 |
| docs/devlog/DEVLOG_20260526.md | 本次修改详细记录 |
