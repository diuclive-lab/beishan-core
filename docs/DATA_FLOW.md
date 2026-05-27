# 系统真实数据流

> 本文件记录系统中所有已知的端到端路径。
> 路径状态只有两种：`✅ 已验证` 或 `❌ 断路`。
> 不允许存在"可能通"或"应该通"的路径。
> 每次新增功能必须更新本文件。

---

## 已验证路径

### 路径 A：普通聊天（✅ 已验证）

```
HTTP POST /api/chat
  → cmd/beishan/main.go: mux.HandleFunc("/api/chat")
  → kernel.Call(msg, 120s)
    → kernel.Send(msg)
      → Router.Route(msg)  [Recipient 为空时触发]
        → callDeepSeek(prompt)
        → parseDecision(raw)  [三层校验: JSON + confidence≥0.4 + recipient白名单]
      → plugin.OnMessage(msg)  [路由到对应插件]
  → kernel.deliverResponse  [通过 CorrelationID channel 返回]
  → HTTP response: json.NewEncoder(w).Encode(resp)
```

### 路径 B：think_plugin 对话分发（✅ 已验证）

```
think_plugin.OnMessage(msg{Type:"chat"})
  → extractPrompt / extractSessionID / extractMode
  → mode switch: ModeReviewExtract → handleReviewExtract
                 ModeNoRetrieval  → handleChatNoRetrieval（直接调 LLM，无检索）
  → cleanupExpiredPending
  → isSystemCommand(text, sessionID)?
      true  → handleSystemCommand  [纯状态机：确认/审查/跳过，无 LLM]
      false → handleChat           [自然语言对话]

handleChat:
  → isNonsenseInput?  [纯符号/过短 → 直接返回]
  → ctxCurrentSession 匹配?
      是 → 只跑 RunEpisodicRetrieval（轻量，跳过知识库）
      否 → needsQueryRewrite(ctxVagueRef + ctxCrossSession)?
             是 → rewriteQuery（LLM 改写）
           RunFullRetrieval(query, projectPath)  [L0+L1+L0.5，按 classifyIntent 分流]
  → tools.SessionGet(sessionID)  [加载最近 5 轮历史，有 sessionMu 锁保护]
  → llmguard.Chat(messages, Contract{AntiLazy:true})  [路径 B-LLM]
      → buildBaseline + injectBaseline  [注入"防偷懒"基线 system prompt]
      → llm.ChatCompletionWithUsage(messages)
      → validateOutput  [Contract 违规则带反馈重试，AntiLazy 场景无重试]
  → StockCodeVerify + DateVerify + NumberRangeVerify + URLVerify  [质量门禁]
  → parseToolSuggestions → 工具调用结果写入 session history
  → shouldSuggestRemember?  [知识建议入库]
  → kernel.Message{Type:"chat.response", Payload: reply}

cmd/beishan/main.go（对话结束后）:
  → saveToSession(assistant reply)
  → goroutine: sleep 500ms → GenerateSessionSummary → SaveSessionSummary
    [异步生成 {sessionID}.summary.json，供跨 session 检索 Phase 1 使用]
```

**关键词分流依据**：`plugins/intent_keywords.go`（唯一词表）
- `ctxCurrentSession`：刚才/刚刚/上一句 → 不检索知识库
- `ctxCrossSession`：上次/之前/讨论过 → episodic 优先
- `ctxSemantic`：决策/结论/方案 → 知识库优先
- `ctxCode`：代码/函数/实现 → 代码检索优先

### 路径 C：glue 子进程消息（✅ 已验证）

```
kernel.Send(msg{Recipient: "python_plugin_name"})
  → glue.OnMessage(msg)
    → p.alive 检查，失败则 respawn
    → json.Marshal(ProtocolMessage{Type:"dispatch", TraceID:...})
    → p.stdin.Write(data)  [stdin/stdout JSON 行协议]
    → readResponse(p, 30s)
      → 过滤 event 类型，等待 response 类型
  → kernel.Message{Type: msg.Type+".response", Payload: ...}
```

### 路径 D：右花调用（✅ 已验证 - OpenHuman）

```
kernel.Send(msg{Recipient: "openhuman"})
  → rightflower.Plugin.OnMessage(msg)
    → Client.Dispatch(method, params)
      → HTTP POST http://localhost:9529/dispatch
        → adapter → OpenHuman Core (JSON-RPC 2.0)
  → kernel.Message{Type: "*.result", Payload: {flower, method, kind: "rightflower"}}
```

---

## 断路路径（❌ 已知问题）

### 路径 E：session 异步回程（✅ 已验证 2026-05-26）

```
HTTP POST /api/chat {async: true}
  → msg.ReplyTo = "session:" + sessionID
  → kernel.Send(msg) [goroutine]
    → plugin.OnMessage(msg)
    → kernel.Send 检查 msg.ReplyTo
    → kernel.deliverReply(response)
      → case strings.HasPrefix(msg.ReplyTo, "session:"):
        → k.SessionHandler(sessionID, response)
          → main.go: sessionResults.Store(sessionID, msg)
  → HTTP 立即返回 {status:"pending", session_id}
  → 客户端轮询 GET /api/result/{session_id}
    → sessionResults.Load(sessionID) → HTTP 返回结果
```

**注入点：** `cmd/beishan/main.go` — SessionHandler 写入 `sessionResults` (sync.Map)
**内核路由：** `kernel/kernel.go` — `deliverReply` + `ReplyTo` 字段支持
**API 端点：** `GET /api/result/{session_id}` — 异步结果轮询
**验证日期：** 2026-05-26

---

### 路径 F：LLM 工具建议（✅ 已验证 2026-05-26）

```
think_plugin.handleChat
  → llm.ChatCompletionWithUsage → reply
  → [质量门禁: StockCodeVerify + DateVerify + NumberRangeVerify + URLVerify]
  → parseToolSuggestions(reply)  ← 解析 LLM 回复中的 JSON 工具建议
    → 匹配到 {"tool":"插件名","action":"类型","reason":"..."}
    → p.Kernel.Call(Recipient:sug.Tool, Type:sug.Action, Payload)
    → 工具结果追加到 reply 末尾
  → [Suggest-to-Remember]
  → [结构化 trace]
  → kernel.Message{Type:"chat.response", Payload: reply}
```

**调用点：** `plugins/think_plugin.go` — `handleChat` 在质量门禁后、Suggest-to-Remember 前插入
**系统提示词已更新：** LLM 被告知可通过 JSON 块主动请求工具调用
**支持的工具：** search_plugin/web_search, write_plugin/read_file, terminal_plugin/terminal_exec, browser_plugin/browser_navigate
**验证日期：** 2026-05-26

---

### 路径 G：observatory 指标（✅ 已验证 2026-05-26）

```
glue.healthCheckLoop
  → observatory.Check(ok, ...)  ← ✅ 数据写入
  → observatory.RecordPulse(pulse)  ← 存储最新健康快照
  → ...
客户端请求 GET /metrics
  → observatory.CollectSnapshotJSON()
    → CollectSnapshot()
      → 1. 默认 Recorder.All() → trace 数量 + Summarize() + 最近 10 条
      → 2. events JSONL 文件 → 事件总数 + 按类型统计
      → 3. LastPulse() → 最近一次健康快照
  → HTTP 200 JSON
```

**新增端点：** `cmd/beishan/main.go` — `GET /metrics`
**新增函数：** `internal/observatory/metrics.go` — `CollectSnapshot()` / `CollectSnapshotJSON()`
**数据存储：** `glue/glue.go` — 健康检查返回的 Pulse 现在通过 `RecordPulse(pulse)` 持久化
**返回内容：** 决策迹统计（汇总 + 最近 10 条）、事件统计（按类型）、系统健康快照
**验证日期：** 2026-05-26
**优先级：** 中

---

### 路径 H：glue event 推送（✅ 已验证 2026-05-26）

```
子进程 stdout 输出 JSON 行
  → glue.demuxLoop(p)  ← 每个进程有一个独立的 goroutine 持续读取
    → json.Unmarshal → 识别 type 字段
    → type == "event":
      → observatory.PublishEvent(type:"subprocess.<name>", payload)
      → continue （不进入响应通道）
    → type != "event"（response/register 等）:
      → p.responseCh ← msg （有缓冲, cap=16）
  → glue.readResponse 从 p.responseCh 读取 ← 不再直接扫描 stdout
```

**修复内容：**
- `glue/glue.go` — `readEvents` 替换为 `demuxLoop`（goroutine），`readResponse` 改为从 channel 读取
- `proc` 新增 `responseCh chan *ProtocolMessage`（缓冲区 16）
- 子进程事件通过 `observatory.PublishEvent` 发布到事件总线
- 子进程 stdout 关闭时自动标记 `p.alive = false`
**验证日期：** 2026-05-26

---

### 路径 M：llmguard 行为契约层（✅ 已验证 2026-05-27）

```
任意调用方 → llmguard.Chat(messages, Contract{...}, timeout)
  → buildBaseline(Contract)
      → AntiLazy        → "防偷懒"基线（禁止"将会做"语态、禁止编造、引用须有源）
      → RequireEvidence → "证据等级"基线（E1/E2/E3/E4 标注强制）
      → OutputFormat="json" → "JSON 输出"基线（拒绝 markdown 包裹）
      → JSONSchema     → 字段名清单注入
  → injectBaseline(messages, baseline)  [追加到现有 system 或前插新 system]
  → chatFunc → llm.ChatCompletionWithUsage(messages, timeout)
  → validateOutput(output, Contract)
      → 合法 JSON / JSONSchema 字段 / 证据标注 三层校验
      → 违规 → 拼接反馈到下一轮 messages，重试（最多 MaxRetries 次）
  → Contract.Critique==true:
      → critiqueRevise: LLM 自审 + LLM 重写（仅在第一次成功后触发）
  → return output, accumulatedUsage, err
```

**注入点：** `plugins/think_plugin.go` — handleChat 默认 provider 路径调 `llmguard.Chat`（Contract{AntiLazy:true}）
**新增包：** `internal/llmguard/` — Contract / Chat / validate / critique 共 4 文件 + 1 测试文件
**测试覆盖：** 17 个用例，含桩函数注入，无需真实 LLM
**支持契约维度：**
  - 层1 提示词基线（AntiLazy / RequireEvidence）
  - 层2 输出校验+重试（OutputFormat / JSONSchema / MaxRetries）
  - 层3 Critique-Revise（Critique，约翻倍成本）
**已对接调用方：** 8（think_plugin 4 处 + skill_factory 3 处 + tool_synthesis 1 处）
**待迁移调用方：** 0（think_plugin / skill_factory 的所有 LLM 调用都已进 llmguard 漏斗）
**维度化 API（Contract 构造器）：**
  - `ForStructure(format, fields, retries)` — 结构维度（层 2 强制：OutputFormat + JSONSchema + retry）
  - `ForContent()` — 内容维度（层 1 半强制：AntiLazy 基线）
  - `ForFacts()` — 事实维度（层 1+4 强制：RequireEvidence + AntiLazy + Critique）
  - 组合：`.WithStructure() / .WithContent() / .WithFacts() / .WithCritique() / .WithEvidence() / .WithRetries()`
**契约使用模式：**
  - think_plugin.handleChat (默认+provider) → `ForContent()` (自然语言对话)
  - think_plugin.handleChatNoRetrieval (默认+provider) → `ForContent()` (无检索直答)
  - think_plugin.tool_synthesis → `ForContent()` (工具结果合成自然语言)
  - think_plugin.query_rewrite → `Contract{}` (机械变换，零契约省 token)
  - skill_factory.classifyOutputType → `ForContent()` (单词分类)
  - skill_factory.fillTemplate → `ForStructure("json", "name", 1).WithContent()` (结构化模板填充)
  - skill_factory.generateWorkflow → `ForContent()` (YAML 全量生成)
**ChatWithProvider 入口：** 用于 workflow per-step provider override，与 Chat 共享 chatCore 逻辑
**验证日期：** 2026-05-27 (维度化 API + 5 处新增接入)

---

### 路径 I：L4 → L3 工具调用（✅ 已验证 2026-05-27）

```
plugin.OnMessage(msg)
  → tools.ValidateAndExecute(msg.Type, msg.Payload)  [硬化层入口]
    → registry.Get(msg.Type)  [按 msg.Type 查 tool 定义]
    → schema 校验 args（JSONSchema 字段+类型）
    → ToolFunc(args) 执行（zero-IO 约定，超时由调用方控制）
    → 返回 ToolResult{Success, Output, Error}
  → plugin 包装为 kernel.Message{Type:"<原 type>.result", Payload}
```

**注入点（举例）：** 11 处主调用，含 `todo_plugin.OnMessage:16` / `search_plugin.OnMessage:212` / `claude_plugin.OnMessage:22` / `tts_plugin.OnMessage:19` 等
**硬化保证：** 所有工具入参经 schema 校验，越界/缺字段直接拒绝；NEVER 调 `tools.Execute` 跳过校验
**验证日期：** 2026-05-27

---

### 路径 J：YAML workflow 引擎执行（✅ 已验证 2026-05-27）

```
kernel.Send(msg{Type:"workflow_run", Payload:{workflow:"name"}})
  → workflow_plugin.OnMessage(msg)
    → workflow.Engine.Run(workflowID, input)
      → 加载 workflows/<name>.yaml → 解析 WorkflowDef
      → 检查 def.Trigger.Type:
          "event" → log 警告并跳过（占位 TODO）
          "scheduled" → 跳过（由 scheduler_plugin 管理）
          其他 → 正常执行
      → 顺序执行 def.Steps:
          step.Plugin == "human_confirm" → 立即返回
            WorkflowResult{NeedsConfirm:true, ConfirmMessage, ConfirmWorkflow}
          否则 → kernel.Call(plugin, type, input)
            ↑ 步骤间通过 ${steps.X.output} 引用上一步输出
          求值 step.Next 条件路由（NextList 支持 if/default）
      → if def.OutputTarget != "chat":
          go routeOutput(def, result)  [异步：notify/knowledge/dashboard]
    → 返回 WorkflowResult JSON
  → kernel.Message{Type:"workflow.result", Payload}
```

**入口：** `plugins/workflow_plugin.go` → `internal/workflow/engine.go:Engine.Run`
**支持类型：** `WorkflowDef.OutputTarget`（chat/dashboard/notify/knowledge）+ `Trigger`（manual/scheduled/event 占位）+ `human_confirm` 伪步骤
**已注册 YAML：** 40 个工作流（workflows/*.yaml）
**验证日期：** 2026-05-27

---

### 路径 K：Go-DSL workflow 引擎执行（✅ 已验证 2026-05-27）

```
kernel.Send(msg{Type:"legal_review", Recipient:"legal_review_v2_plugin"})
  → GoWorkflowPlugin.OnMessage(msg)
    → GoExecutor.Run(GoWorkflow{Name, Steps}, rawInput)
      → 顺序执行 GoStep:
          GoStepTool      → kernel.Call(toolHost[step.Tool], ...)
          GoStepPlugin    → kernel.Call(step.Recipient, step.MsgType, ...)
          GoStepTransform → step.TransformFn(ctx, input)  [纯 Go 函数]
          GoStepChain     → 顺序执行 step.SubSteps
          GoStepParallel  → 并发执行 step.SubSteps
      → BeforeExecute/AfterExecute 中间件钩子
      → MaxRetries + RetryDelay + Fallback + OnError 策略
      → step.OutputVar → 注册到 ctx 供后续步骤引用
    → 返回 WorkflowResult（与 YAML 引擎结构兼容）
  → kernel.Message{Type:"workflow.result", Payload}
```

**入口：** `cmd/beishan/legal_review_go_dsl.go` → `internal/workflow/gods_executor.go:GoExecutor.Run`
**已注册 Go-DSL：** 1 个（`legal_review`，编译时硬化链）
**与 YAML 引擎差异：** 编译时类型安全；不可热更；ToolHost 映射零校验（与 agent runner 不一致，docs/KNOWN_LIMITATIONS.md 已登记）
**验证日期：** 2026-05-27

---

### 路径 L：sub-agent 委派（✅ 已验证 2026-05-27）

```
HTTP /api/agents/run 或 think_plugin tool 调用
  → agent.RunSubagent(ctx, taskID, prompt, Definition, timeout)
    → 加载 Definition{AllowedTools, MaxSteps, SystemPrompt}
    → 多轮对话循环:
        LLM 生成回复（受 system + 历史约束）
        解析工具调用 JSON
        kernel.Call(tool, args)  [受 AllowedTools 白名单约束]
        工具结果追加到对话
    → 终止条件：完成 / 超过 MaxSteps / 超时 / 工具拒绝
  → 返回 SubagentResult{Output, Steps, TokensUsed}

并行版本：
  → agent.RunParallel(ctx, tasks, timeout)
    → 每个任务独立 goroutine + RunSubagent
    → 汇总所有结果
```

**入口：** `cmd/beishan/main.go:324/382/393`（HTTP 处理器）+ `internal/tools/tools.go:93/97`（工具入口 spawn_subagent / spawn_parallel）
**安全保证：** Definition.AllowedTools 白名单 + MaxSteps 步数上限 + 超时
**已定义 agents：** Definition 在 internal/agent/definition.go 注册
**验证日期：** 2026-05-27

---

## 待验证路径（需要补充）

---

## 维护规则

1. 每次新增功能，在本文件增加对应路径
2. 修复一个断路，将 ❌ 改为 ✅，并注明验证日期
3. 发现新的断路，立即在此记录，不要假装它不存在
4. 路径状态由人工验证（运行并观察），不由 AI 假设
