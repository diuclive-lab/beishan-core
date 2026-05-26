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

### 路径 B：think_plugin 普通聊天（✅ 已验证）

```
think_plugin.OnMessage(msg{Type:"chat"})
  → extractPrompt(msg.Payload)
  → needsQueryRewrite?  [确定性关键词检测]
    → rewriteQuery  [LLM 改写口语查询]
  → RunFullRetrieval(query, projectPath)  [L0+L1+L0.5 检索管道]
  → loadRecentSessionMessages(sessionID, 5)  [多轮历史，含 token 截断]
  → llm.ChatCompletionWithUsage(messages)
  → StockCodeVerify + DateVerify + URLVerify  [输出质量门禁]
  → shouldSuggestRemember?  [知识建议入库]
  → kernel.Message{Type:"chat.response", Payload: reply}
```

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

### 路径 E：session 异步回程（❌ 断路）

```
kernel.Send(msg{ReplyTo: "session:xxx"})
  → kernel.deliverReply(response)
    → case strings.HasPrefix(msg.ReplyTo, "session:"):
      → k.SessionHandler(sessionID, msg)
        → ❌ SessionHandler = nil（main.go 未注入）
        → 消息在此静默丢失，打印 log 后丢弃
```

**影响：** 所有依赖 ReplyTo session: 的异步响应全部丢失。
**修复位置：** `cmd/beishan/main.go` — 注入 SessionHandler
**优先级：** 高

---

### 路径 F：LLM 工具建议（❌ 断路）

```
think_plugin.handleChat
  → llm.ChatCompletionWithUsage → reply
  → parseToolSuggestions(reply)  ← ❌ 函数存在但此处从未调用
  → [工具建议被静默丢弃]
```

**影响：** LLM 无法主动请求工具调用，think_plugin 不是真正的 Agent。
**修复位置：** `plugins/think_plugin.go` — handleChat 函数末段
**优先级：** 中

---

### 路径 G：observatory 指标（❌ 断路）

```
glue.healthCheckLoop
  → observatory.Check(ok, ...)  ← ✅ 数据写入
  → [数据存在于内存，但无消费者]
  → ❌ 无 /metrics 端点，无外部可见性
```

**影响：** 系统运行数据不可观测，无法知道插件调用频率、token 消耗、健康状态。
**修复位置：** `cmd/beishan/main.go` — 新增 /metrics 端点
**优先级：** 中

---

### 路径 H：glue event 推送（❌ 设计断路）

```
子进程推送 event 消息
  → glue.readResponse(p, 30s)
    → msg.Type == "event" → log 后 continue  ← event 在此被消费
  → glue.readEvents(p)  ← ❌ 此函数存在但永远不会被调到
                           readResponse 已把 event 消费掉了
```

**影响：** 子进程无法向系统其他部分推送异步事件。
**修复位置：** `glue/glue.go` — 需要 demultiplexer 将 stdout 分流
**优先级：** 低（有明确需求时再修复）

---

## 待验证路径（需要补充）

- 路径 I：L4 → L3 tool 调用（`kernel.Call` → `ValidateAndExecute`）
- 路径 J：YAML workflow 引擎执行
- 路径 K：Go-DSL 引擎执行
- 路径 L：sub-agent 委派（`internal/agent/runner.go`）

---

## 维护规则

1. 每次新增功能，在本文件增加对应路径
2. 修复一个断路，将 ❌ 改为 ✅，并注明验证日期
3. 发现新的断路，立即在此记录，不要假装它不存在
4. 路径状态由人工验证（运行并观察），不由 AI 假设
