# 开发日志

## 2026-05-17 L3 法律分析插件簇 + 中国法律适配

### 新增

| 文件 | 类型 | 用途 |
|---|---|---|
| `plugins/legal_review_plugin.go` | L4 编排 | 法律审查编排：访谈→检索→分析→生成四步流程 |
| `plugins/l3_echo_go/` | L3 Go 示例 | Go 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/l3_echo_python/` | L3 Python 示例 | Python 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/cold_start_plugin.go` | L3 插件 | 冷启动访谈：合同类型识别、角色分析、法律画像构建 |
| `plugins/legal_search_plugin.go` | L3 插件 | 中国法律检索：适配北大法宝/威科先行查询结构，法律效力层级排序 |
| `plugins/clause_analyzer_plugin.go` | L3 插件 | 条款分析：三段论（大前提-小前提-结论）替代 IRAC，风险三档评级 |
| `plugins/legal_write_plugin.go` | L3 插件 | 法律文书生成：合同审查报告/法律意见书/风险矩阵，AI 标识合规 |

### 中国法律适配

所有法律插件遵循以下中国法域适配规则：
- **法律关系分析**：使用《民法典》合同编典型合同分类体系（第595-978条）
- **三段论推理**：大前提（法律规则）→ 小前提（合同约定）→ 结论（法律评价），替代 IRAC
- **法律效力层级**：宪法 > 法律 > 司法解释 > 行政法规 > 部门规章
- **风险评级**：🟢 合规 / 🟡 提示 / 🔴 违规（参考 claude-for-legal 三档制）
- **文书模板**：合同审查报告、法律意见书、风险矩阵，均使用中国法律文书格式
- **AI 标识**：根据《人工智能生成合成内容标识办法》，所有输出标注 AI 生成身份

### 参考 claude-for-legal 的模式迁移

| 模式 | 迁移比例 | 用途 |
|---|---|---|
| 冷启动访谈（SKILL.md） | ~10% | cold_start_plugin.go 的法律画像构建流程 |
| 风险评级（GREEN/YELLOW/RED） | ~10% | clause_analyzer_plugin.go 的三档评级体系 |
| 文书模板（法律意见书/审查报告） | ~8% | legal_write_plugin.go 的输出模板 |
| 免责声明分级（律师/法务/个人） | ~5% | 按用户角色生成不同的免责声明 |
| MCP 连接器接口（北大法宝预留） | ~3% | legal_search_plugin.go 的 tryPkulawSearch 预留接口 |

### 主程序变更

`main.go` 新增全部法律插件注册：
- 通用工具插件（search/write/memory/scheduler）
- 法律审查插件簇（legal_review/cold_start/legal_search/clause_analyzer/legal_write）

### design principles 新增

`DESIGN_PRINCIPLES.md` 新增 **"Type 即意图，Payload 即数据"** 章节，确立三条子原则：
- 路由只认 Type：Router 做机械映射，不看 Payload
- Payload 不参与决策：Router 永不解析 Payload 字段
- 不能把路由判断权交给 LLM

### 司法数据源集成

新增 `judicial_search` 工具，接入中国司法大数据服务网 (data.court.gov.cn) 和中国裁判文书网 (wenshu.court.gov.cn) 公开数据：

| 变更 | 文件 | 说明 |
|---|---|---|
| 新增工具 | `internal/tools/judicial.go` | judicial_search 工具：HTTP 封装 + HTML 解析 + 结果格式化 |
| 检索链路更新 | `plugins/legal_search_plugin.go` | searchStatutes/searchCases 优先调用 judicial_search |
| 注册入口 | `internal/tools/tools.go` | Init() 追加 registerJudicialTools |

**数据源优先级**：司法大数据服务网 → 裁判文书网 → 通用 web_search 回退

**免费接口限制**：非注册用户仅支持部分案由（民间借贷、离婚、买卖合同等）的统计查询，裁判文书网有反爬机制。所有结果标记 `source` 字段，供审查者验证。

### 架构对齐确认

`legal_review_plugin.go` 作为 L4 编排插件，遵循以下契约：
- 每个步骤通过 `kernel.Call()` 调用 L3 插件，Type 字段精确指定路由目标
- Payload 只传数据（`json.RawMessage`），不做 type assertion
- 导入路径使用 `beishan/kernel`（非 `github.com/...`）
- 错误处理硬编码：任一步骤失败立即终止，不降级

## 2026-05-16 L3/L4 边界硬化

### 背景

文档定义的"三层架构"（第一核心/胶水层/插件）与实际代码的"四层架构"（L1 kernel/L2 glue/L3 internal/tools/L4 plugins）不一致，导致 L3 硬化职责掉落在 L3/L4 之间的缝隙中。

根因：L4 plugins 直接调用 `tools.Execute`（L3 内部调度），跳过了参数校验。且 `tools.Execute` 内部对 JSON 解析失败的 payload 做 lenient fallback（包成 `{"raw": ...}`），等于对 LLM 不可靠输出零防御。

### 本次变更

| 变更 | 影响文件 |
|---|---|
| **Schema 注册中心**：新增 `RegisterToolSchema` / `GetToolSchema` / `GetAvailableTools`，Router 查询时不触碰 Payload | `internal/tools/schema_registry.go` |
| **ValidateAndExecute**：L4 调用 L3 的唯一入口，先 Schema 校验再执行 | `internal/tools/validate.go` |
| **Router 路由验证**：`parseDecision` 改用 `GetToolSchema` 验证 Recipient，移除 whitelist 字段 | `kernel/router.go` |
| **L4 插件重构**：search/write/memory 全部改调 `ValidateAndExecute` | `plugins/*.go` |
| **L2 IPC 强化**：ProtocolMessage 增加 TraceID/Timestamp/RetryCount，dispatch 时自动注入 | `glue/protocol.go`, `glue/glue.go` |
| **Execute 硬化**：移除 lenient fallback，不合法的 JSON 直接报错 | `internal/tools/tools.go` |
| **文档对齐**：三层 → 四层架构，"第二核心" → "胶水层" | `CHANGELOG.md`, `DESIGN_PRINCIPLES.md` |

### 架构合约（写入后不再修改）

```
L1 kernel/        注册 + 路由 + 转发                   Payload 永不透明
L2 glue/          IPC + 进程管理                       不接触 Payload 内容
L3 internal/tools/ Schema 注册 + 强校验 + 执行          硬化的唯一关卡
L4 plugins/       编排 L3 完成多步任务                   只写业务逻辑，零防御代码

调用链：
  User → L1 Route (强制 DeepSeek, 仅查路由表)
       → L1 Send → L4 OnMessage
       → L3 ValidateAndExecute (Schema 校验)
       → L3 Execute → handler
       → 响应原路返回
```

### 设计纪律

见 DESIGN_PRINCIPLES.md

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

#### 四层架构

| 层 | 包 | 职责 | 语言 | 可修改 |
|---|---|---|---|---|
| L1 内核 | `kernel/` | 注册 + 路由 + 转发 | Go | 冻结不改 |
| L2 胶水层 | `glue/` | IPC + 进程管理 | Go | 可迭代 |
| L3 工具层 | `internal/tools/` | 工具注册 + 执行 + schema 清理 | Go | 可迭代 |
| L4 编排层 | `plugins/*.go` | 编排 L3 完成多步任务 | Go / Python | 随意改 |

#### 消息格式

Message 只有 4 个字段：Sender、Recipient、Type、Payload。
Payload 对内核永不透明。

### L4 插件清单

| 插件 | 职责 | 实现来源 |
|---|---|---|
| search_plugin | 网页搜索、内容抓取（L4 编排 → tools.ValidateAndExecute） | hermes-go tools/web.go |
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

claude-for-legal（Anthropic 的法律 AI 智能体框架）：
- 工作流编排骨架（冷启动访谈→分析→输出）
- 文档自动化模板（风险矩阵、法律意见书）
- MCP 连接器接口定义
- 本项目的 L4 法律编排插件设计受此项目启发
