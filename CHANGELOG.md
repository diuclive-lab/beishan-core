# 开发日志

## 2026-05-18 think_plugin + Router MsgType + 启动清理 + REPL

### 新增

- **`plugins/think_plugin.go`**：通用对话插件。`chat` 类型消息调 DeepSeek 生成回答，系统提示词同步 beishan-core 能力列表，不自称"不能XX"
- **`cmd/repl/main.go`**：REPL 交互界面。`go run cmd/repl/main.go` 启动，直接打字聊天，支持 `/todo_plugin:todo_add 买牛奶` 格式指定插件
- **`eval/scripts/run_core_smoke.sh`** 新增 think_plugin 测试用例

### 修复

- **`kernel/router.go`**: `Decision` 新增 `MsgType` 字段。用户说"搜索新闻"→ 路由到 `search_plugin` + `Type: web_search`，不再把 `chat` 类型送到所有插件
- **`kernel/kernel.go`**: `Send()` 应用决策中的 `MsgType`
- **启动清理**：编译 `go_example` / `l3_echo_go` 二进制，删除 `l4_research` / `l4_template_python` 残档，manifest 目录名校验全通过
- **think_plugin 系统提示词**：列出所有插件能力，避免 DeepSeek 说"我不能生成图片"

### 实测

| 操作 | 结果 |
|---|---|
| `{"message":"你好"}` | → think_plugin 回答 ✅ |
| `{"message":"搜索新闻"}` | → search_plugin/web_search ✅ |
| `{"message":"帮我写文件"}` | → write_plugin/write_file ✅ |
| `{"message":"生成图片"}` | → image_gen_plugin/image_generate ✅ |
| REPL 交互 | → 打字聊天可用 ✅ |
| 启动零警告 | → 20 插件全部就绪 ✅ |

## 2026-05-18 工作流引擎 + legal_review 替换为 YAML

### 新增

- **`internal/workflow/` 工作流引擎**：
  - `engine.go`：核心执行器，读 YAML → 顺序调用 `kernel.Call` → 条件分支路由
  - `types.go`：支持 `next: string` 和 `next: [{if:..., goto:...}, {default:...}]` 双格式
  - `buildPayload`：`${input}` 和 `${steps.<id>.output}` 插值解析
  - `evaluateCondition`：`steps.<id>.output.<field> == 'value'` 条件评估

- **`workflows/` 目录**：
  - `legal_review.yaml`：首个 YAML 工作流（4 步：cold_start → legal_search → clause_analysis → write_report）
  - `_template.yaml`：标准示例模板，含字段说明和条件分支示例

- **`plugins/workflow_plugin.go`**：薄插件包装，接收 `workflow_run` 消息，委托 engine 执行

### 删除

- **`plugins/legal_review_plugin.go`**（-150 行 Go 代码）：被 `workflows/legal_review.yaml` 替代
- `main.go` 中 `legal_review_plugin` 注册移除，描述合并到 `workflow_plugin`

### 替换后影响

| 项目 | 替换前 | 替换后 |
|---|---|---|
| 加新场景 | 写 Go 代码 + 编译 | YAML 文件，无需编译 |
| 法律审查调用 | `legal_review_plugin` | `workflow_plugin`（workflow: legal_review） |
| 插件数 | 18 | 17 |
| 测试 | legal_smoke 6/6 通过 | legal_smoke 6/6 通过 ✅ |

### 工作流引擎实测

```
cold_start → legal_search → clause_analysis → write_report  4步全通 ✅
```

## 2026-05-18 全功能冒烟测试 + 工具移植完结

### Eval 补全

- **新增 `eval/scenarios/core_smoke.yaml`**：12 个测试用例覆盖全部 L3/L4 插件
- **新增 `eval/scripts/run_core_smoke.sh`**：自动编译 + 启动 + 测试 + 清理
- **实测 12/12 全部通过** ✅

### hermes-go 工具移植完结

本轮完成最后 4 个工具移植：

| 工具 | 文件 | 定位 |
|---|---|---|
| vision_analyze（视觉分析） | `internal/tools/media.go` | 预留接口，需 Vision API |
| image_generate（图片生成） | `internal/tools/media.go` | 预留接口，需 DALL-E / SD |
| text_to_speech（文本转语音） | `internal/tools/media.go` | 本地 `say` 命令可用 |
| clarify（意图澄清+学习） | `internal/tools/clarify.go` | 3 次学习后自动推断 |

### 最终统计

| 指标 | 数值 |
|---|---|
| 工具注册数 | 34 |
| 插件注册数 | 17（含 3 glue 子进程） |
| Eval 场景 | legal_smoke（6 用例） + core_smoke（12 用例） |
| 内核文件 | 冻结不改 |
| 全部 hermes-go 工具 | 移植完毕 ✅ |

## 2026-05-18 开发日志归档

## 2026-05-18 memory continuity（路线 A：session 内含 evidence）

### 新增

- **`internal/tools/memory.go` 重写为 session 感知存储**：
  - 存储结构：`~/.hermes/memory/sessions/<session_id>.json`
  - 每条 session 内包含 `messages[]` + `evidence[]`
  - 7 个新工具：session_add、session_get、session_search、session_list、session_delete、evidence_add、evidence_search
  - 并发安全：`sync.RWMutex` 保护读写
  - 威胁扫描：注入检测保留

- **`plugins/memory_plugin.go` 更新**：支持全部 7 种 session 消息类型

- **`main.go` HTTP handler 自动记录 session**：
  - 每次 `/api/chat` 请求生成 `session_id`
  - 同步模式下自动记录 `user → plugin → response` 到 session
  - 异步模式通过 goroutine 处理，`ReplyTo` 回程后存入

### 注册工具统计

`tools registered:` **15** 个工具（原 11 + 新增 7 个 session/evidence 工具，剔除 3 个旧 memory 工具）

### 路线选择

**路线 A**：evidence 作为 session 的子结构存储，不独立管理。
当前无跨 session 引用证据的真实需求，路线 B 留接口。

### 实测

- 发送消息 → session 自动创建 ✅
- 消息持久化到磁盘 ✅
- session_list 查询 ✅

## 2026-05-18 第三轮：glue 依赖管理（路线 A）

- **`glue/spawn()` 新增 `requirements.txt` 自动检测**：spawn 前 `os.Stat` 检测，存在则 `pip3 install -r`
- **向后兼容**：没有 `requirements.txt` 的插件行为不变
- **路线 B 预留**：未来如需独立 venv，在 `spawn()` 中 `switch m.Type` 处分支即可

## 2026-05-18 第二轮：ReplyTo 回程路由 + HTTP 异步 session

### 新增

- **`Message.ReplyTo` 字段**：支持 `plugin:`、`session:`、`callback:`、空 四种前缀
- **`deliverReply()` 内核方法**：`Send()` 完成后检查 `ReplyTo`，按前缀分派
- **`SessionHandler` 回调**：内核不持有 session 状态，由 HTTP 层注入存储函数
- **`/api/chat` 异步模式**：`{"message":"...","async":true}` 立即返回 `session_id`，后台 goroutine 处理
- **`/api/result/:session_id` 轮询端点**：有结果返回结果，无结果返回 `{"status":"pending"}`

### 清理

- `Router.checkRecipient` 和 `SetRecipientValidator` 删除，`parseDecision` 只用 `knownPlugins` 验证
- `NewKernel` 不再依赖 `tools.GetToolSchema`，内核层与工具层注册表完全解耦

### 架构边界（正式确立）

```
内核路由层  → 只认识插件名（knownPlugins）
插件执行层  → 只认识工具名（tools.Registry）
两层之间    → 不互相知道对方的注册表
```

### 实测

- 异步请求 → 立即返回 session_id ✅
- goroutine 后台处理 → DeepSeek 路由 → 插件执行 → deliverReply 存储结果 ✅
- 轮询 /api/result/:session_id → 返回结果 ✅

## 2026-05-18 Meta 注册 + 路由描述增强

### 新增

- **Meta 结构体**：`kernel.Register` 新增可选的 `Meta` 参数，支持 `Description` 和 `Tags`，向后兼容
- **路由 prompt 增强**：DeepSeek 现在能看到每个插件的语义描述，路由决策质量提升
- **`AddKnownPlugin` 替代 `SetPlugins`**：注册时自动维护路由列表，不再需要手动同步
- **`KnownPlugins()`**：新增方法返回所有已注册插件
- **Markdown 容错**：`parseDecision` 自动剥离 DeepSeek 返回的 `` ```json ``、`` ``` `` 等标记
- **HTTP API 兼容**：`/api/chat` 支持 `{"message":"..."}` 简单格式和 `{"type":"...","payload":...}` 完整格式

### 修复

- **Router 不暴露工具名**：路由 prompt 不再包含 `internal/tools` 的工具名，只显示 kernel 注册的插件名
- **`checkRecipient` 改为查 `knownPlugins`**：移除对 `tools.GetToolSchema` 的最后引用，Router 不再依赖 tools 包
- **tools.Init() 调用**：main.go 缺失的初始化已补齐

### 实测

- `web_search` → `search_plugin` ✅ 搜索结果正确返回
- `write_file` → `write_plugin` ✅ 文件写入成功

## 2026-05-17 全链路冒烟测试通过 6/6

### 修复

- **Router extraNames**：DeepSeek 提示词现在包含 kernel 注册的插件名，确保法律插件可被路由
- **legal_review_plugin 响应 Sender**：移除了导致 `deliverResponse` 跳过回传的 Sender 字段
- **纯文本 Payload 兼容**：legal_search_plugin 和 clause_analyzer_plugin 现在也接受纯文本输入
- **parseProfile 空值降级**：空 profile JSON 不再报错
- **WriteRequest 包装**：legal_review_plugin Step 4 正确将 AnalysisReport 包装为 WriteRequest

### 新增

- **HTTP API 服务**：main.go 改为持久 HTTP 服务（`:8013`），添加 GET /health 和 POST /api/chat
- **Router.KnownPlugins**：新增方法返回所有注册插件名

### 测试

- 法律插件冒烟测试 6/6 全量通过
- 测试链路：冷启动(2) → 法律检索(2) → 条款分析(1) → 全链路审查(1)

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

### 测试基础设施 + Router 验证修复

从 `66/FangLab` 项目移植评估基础设施：

| 新增 | 文件 | 说明 |
|---|---|---|
| 测试场景 | `eval/scenarios/legal_smoke.yaml` | 6 个法律插件测试用例（冷启动/检索/分析/全链路） |
| 运行脚本 | `eval/scripts/run_legal_smoke.sh` | 启动服务→发送测试→验证响应→汇总结果 |
| 共享库 | `eval/lib/lib.sh` | 端口检查/进程管理/日志（移植自 runtime_stack_lib.sh） |
| 文档 | `eval/README.md` | 测试方案说明 |
| 环境配置 | `.env` | DeepSeek API Key 配置（已 gitignore） |

**Router 验证修复** (`kernel/kernel.go`, `kernel/router.go`)：

Router.parseDecision 原先只查 `tools.GetToolSchema`，但法律插件通过 `kernel.Register` 注册而非工具注册中心。导致 DeepSeek 路由到合法插件时被误判为"无效收件人"。

修复：`NewKernel` 注入 `SetRecipientValidator`，同时检查内核插件表和工具注册中心。kernel.go 新增 .env 自动加载（init 函数）。

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
