# 开发日志

## 2026-05-11 架构重建

### 背景

从 1 月到 5 月，经历了多个版本的迭代（Beishan 微内核 → V9 → Mini v2.0），最后发现所有版本走向规则型的根本原因相同：**大语言模型被放在了错误的位置，或者根本没有位置。**

### 核心认知

大语言模型是文本补全器，不是通用智能。它擅长生成，不擅长思考、路由、决策。所有把 LLM 当聪明人用的架构，最终都会变成规则型。

### 本次重建的设计决策

#### 两模式路由

```
Recipient == "" → DeepSeek 决策路由（慢，但智能）
Recipient != "" → 直接转发（快，L4 已决策）
```

不给快捷方式。DeepSeek 不可用时系统不降级为规则匹配。

#### 三层架构

| 层 | 职责 | 语言 | 可修改 |
|---|---|---|---|
| 第一核心 | 注册 + 路由 + 强制 DeepSeek | Go | 冻结不改 |
| 第二核心（胶水层） | IPC + 进程管理 | Go | 可迭代 |
| 插件（Python / Go） | 业务逻辑 | 任意 | 随意改 |

#### 消息格式

Message 只有 4 个字段：Sender、Recipient、Type、Payload。
Payload 对内核永不透明。

### L3 插件清单

| 插件 | 职责 | 实现来源 |
|---|---|---|
| search_plugin | 网页搜索、内容抓取 | hermes-go tools/web.go |
| write_plugin | 文件读写、搜索、修改 | hermes-go tools/file.go |
| memory_plugin | 记忆存储、检索 | hermes-go tools/services.go（内存部分） |
| scheduler_plugin | 定时触发任务 | 新写 |

### 设计纪律

见 DESIGN_PRINCIPLES.md

### 参考项目

ds4.c（Redis 作者 antirez 的 DeepSeek V4 Flash 推理引擎）的设计哲学：
- 故意的狭窄，不做通用框架
- 接口极窄（173 行 header）
- 没有功能开关
- 注释写在代码旁边
- 正确性优先于速度
