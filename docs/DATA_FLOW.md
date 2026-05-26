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
