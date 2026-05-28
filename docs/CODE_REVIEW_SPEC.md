# beishan-core 代码审查规范
# AI 辅助开发质量门禁

> 创建：2026-05-28
>
> **核心前提**：绿色 CI 不等于可用代码。
> AI 会写出"编译通过、测试全绿"但语义错误的代码。
> 本规范强制要求三层验证，且每层都有 **AI 无法伪造** 的证据要求。

---

## 一、三层验证体系

```
L1 静态门禁     → 机器可验证，但可被 AI 伪造
L2 语义断言     → 部分可自动化，AI 伪造成本高
L3 端到端冒烟   → 必须人工运行，AI 无法伪造
```

**所有层必须通过，缺任何一层审查不通过。**
不允许以"时间紧"或"低风险"为由跳过 L2/L3。

---

## 二、L1 静态门禁（每次提交必跑）

```bash
go build ./...                   # 编译全通
go vet ./...                     # 静态分析
go test ./...                    # 测试全绿
bash scripts/integration_check.sh  # 集成检查
```

**不允许的情况：**
- `-count=1` 强制重跑但用 `-short` 跳过慢测试（除非明确说明哪些被跳过）
- 任何 `t.Skip()` 必须有注释说明跳过原因和预期恢复时间
- 新增功能不写测试（哪怕是桩测试也必须有）

---

## 三、L2 语义断言（每个函数/工具必须有）

### 3.1 什么是合格的语义断言

**不合格（AI 偷懒模式，不通过）：**
```go
result := KnowledgeProbe()
assert(result != nil)            // 只检查非空
assert(result.Success == true)   // 只检查成功标志
```

**合格（必须断言输出内容）：**
```go
result := KnowledgeProbe()
// 必须验证：
// 1. 输出字段存在且合理
assert(result.TotalSampled > 0)
assert(result.L0Recall >= 0 && result.L0Recall <= 1.0)
// 2. 输出可被接收方解析（端到端格式验证）
var parsed ProbeResult
err := json.Unmarshal([]byte(result.Output), &parsed)
assert(err == nil, "输出必须是可解析的 JSON")
assert(parsed.ProbeTime != "")
// 3. 副作用验证（如文件写入）
_, err = os.Stat(historyPath)
assert(err == nil, "probe_history.jsonl 必须被写入")
```

### 3.2 Payload 双向核对（Plugin 层必须执行）

对任何 Plugin 的 `OnMessage`，必须同时验证：

```
发出方编码 → 接收方解码
```

不允许只看发出方说"应该能解析"。必须写测试验证接收方能正确 unmarshal。

**典型反例**（`session_search_plugin` 历史 bug）：
```go
// 发出方：fmt.Sprintf("%q", output)
// 接收方尝试 json.Unmarshal 时得到双重转义的字符串
// 编译通过，测试如果只检查 err == nil 则通过，但内容是错的
```

**强制规则**：每个 Plugin 测试必须包含一个"格式往返测试"：
```go
// 模拟完整调用链：encode → decode → assert content
resp, _ := plugin.OnMessage(msg)
var content SomeStruct
err := json.Unmarshal(resp.Payload, &content)
assert(err == nil)
assert(content.SomeField == expectedValue)  // 必须断言具体值
```

### 3.3 工具注册完整性检查

每次新增工具，必须验证三处：

```bash
# 1. 工具在 Registry 中存在
grep -n '"<tool_name>"' internal/tools/*.go

# 2. 工具有非测试调用点（或有 UNIMPLEMENTED 标记）
grep -rn '<tool_name>' --include="*.go" . | grep -v "_test.go"

# 3. 如果通过 Plugin 调用，Plugin 能路由到它
# （memory_plugin 的 tools.HasTool 动态路由是例外，但需注明）
```

### 3.4 "已实现"声明的证据要求

**禁止说**：
- "这个功能应该可以工作"
- "logic looks correct"
- "tests should pass"

**必须提供**：
```
---INTEGRATION_PROOF---
新增符号: [具体函数名/包名]
非测试调用点: [文件路径:行号] 或 [无，原因: ...]
数据流:
  入口: [具体 HTTP 端点 / 插件名 / 工具名]
  经过: [你修改的代码位置]
  出口: [最终到达哪里，格式是什么]
测试断言: [断言了哪些具体值，不是"断言了非空"]
---END_PROOF---
```

---

## 四、L3 端到端冒烟（不可被 AI 伪造）

### 4.1 什么需要 L3 验证

以下情况必须人工启动服务验证：

| 情况 | 示例 |
|------|------|
| 新增 Plugin 或修改 OnMessage | `session_search_plugin` payload 修复 |
| 修改路由逻辑 | Router 降级路径 |
| 修改调度逻辑 | 新增 cron 任务 |
| 修改 LLM prompt | RouterPrompt 变更 |
| 任何 Payload 格式变更 | JSON vs string 编码 |

### 4.2 冒烟验证方式

```bash
# 启动服务
go run ./cmd/beishan/

# 基础聊天
curl -s -X POST http://localhost:8013/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"你好"}' | python3 -m json.tool

# 工具直接调用
curl -s -X POST http://localhost:8013/api/chat \
  -H "Content-Type: application/json" \
  -d '{"recipient":"memory_plugin","type":"knowledge_probe","payload":{}}' \
  | python3 -m json.tool

# 检查日志有无 panic / error
tail -20 ~/Library/Logs/FangLab/api.err.log
```

### 4.3 冒烟证据格式

L3 验证必须提供实际输出片段（不是"我运行了，没问题"）：

```
【冒烟证据】
命令：curl -X POST ...
实际输出：
{
  "session_id": "abc123",
  "type": "chat.response",
  "payload": {"message": "..."}   ← 必须是真实输出，不是期望输出
}
日志（如有）：[INFO] think_plugin: handleChat 完成，用时 1.2s
```

---

## 五、AI 防偷懒条款

以下行为直接视为审查未通过，不接受解释：

### 5.1 禁止的声明

| 禁止说 | 原因 |
|--------|------|
| "代码看起来正确" | 看起来正确 ≠ 运行正确 |
| "应该能工作" | 必须验证，不能假设 |
| "之前类似的代码可以，这个也可以" | 每处都要独立验证 |
| "测试通过了，功能就完整了" | 测试可能是浅层的 |
| "这是设计决策" | 必须引用文档或 commit 记录 |

### 5.2 必须提供的证据

每次代码修改，在提交前必须能回答这三个问题：

**Q1：这段代码在生产路径上被什么调用？**
```bash
grep -rn "functionName\|ToolName\|PluginType" --include="*.go" . | grep -v "_test.go"
# 必须有输出，或者明确说明为什么没有调用点
```

**Q2：输出格式和接收方期望的格式是否匹配？**
- 写一个从发出方到接收方的往返测试
- 或者贴出发出方代码和接收方解析代码，证明格式一致

**Q3：边界情况下会发生什么？**
- 空输入
- 知识库为空时
- API 不可达时
- 至少测试一个"非正常"输入

### 5.3 测试质量标准

测试文件中不允许出现以下模式而不加说明：

```go
// ❌ 禁止：只检查不出错
if err != nil { t.Fatal(err) }
// 后面没有任何内容验证

// ❌ 禁止：只检查非空
if result == nil { t.Fatal("nil") }
// 没有检查 result 的任何字段

// ❌ 禁止：硬编码成功
func TestXxx(t *testing.T) {
    // 空测试体，或只有 t.Log
}

// ✅ 正确：断言具体值
assert.Equal(t, 0.7, result.L0Recall, "recall should be ~0.7")
assert.Contains(t, result.Output, "probe_time")
assert.NoError(t, json.Unmarshal(result.RawPayload, &parsed))
```

---

## 六、特定层的审查重点

### 6.1 Plugin 层（`plugins/*.go`）

每个 Plugin 的 `OnMessage` 必须检查：

- [ ] Payload 解析失败时是否有明确错误（不能 panic）
- [ ] 返回的 `kernel.Message.Payload` 格式：JSON 对象直接透传，字符串需 `json.Marshal`
- [ ] 不使用 `fmt.Sprintf("%q", ...)` 编码 JSON（这会双重转义）
- [ ] `switch msg.Type` 的 `default` 返回 `fmt.Errorf`，不是空 Message
- [ ] 如果调用 `tools.ValidateAndExecute`，检查 `result.Success` 是否

### 6.2 Tool 层（`internal/tools/*.go`）

每个 Tool 的注册和实现必须检查：

- [ ] `Register(name, desc, schema, func)` 四参数完整
- [ ] schema 的 `required` 字段和函数体的参数提取一致
- [ ] 返回值用 `successResult(json)` 或 `errorResult(msg)`，不能返回裸字符串
- [ ] 如果函数内有"暂不实现"的分支，必须有 `UNIMPLEMENTED` 注释
- [ ] 不能有 `return nil`（必须是 `*ToolResult`）

### 6.3 Workflow 层（`workflows/*.yaml`）

参考 `docs/V25_WORKFLOW_STANDARD.md`，每个 YAML 必须有：

- [ ] 顶部注释：用途 / 触发方式 / 手动触发命令
- [ ] `id` 和文件名一致
- [ ] 每个 step 有 `timeout` 和 `on_error`
- [ ] `output_target` 声明（默认 `chat`）
- [ ] 不使用 `provider: local`（除非有明确说明）

### 6.4 main.go 注册完整性

新增 Plugin 时必须检查 main.go：

- [ ] `k.Register` 或 `k.RegisterUnlisted` 有对应注册
- [ ] `Meta.Description` 非空（影响 Router 路由质量）
- [ ] `Meta.Types` 包含该插件接受的消息类型
- [ ] 如果有调度需求，`schedulerPlugin.OnMessage` 有对应 `schedule_add`

---

## 七、审查流程（标准执行顺序）

```
1. 阅读被审查代码
   ↓
2. 运行 L1 静态门禁（必须全绿）
   ↓
3. 阅读测试，确认是否符合 L2 语义断言标准
   如不符合 → 补写测试 → 重回 L1
   ↓
4. 填写 INTEGRATION_PROOF（无法填写 = 未完成集成）
   ↓
5. L3 冒烟（修改了 Plugin / 路由 / 调度 / Payload 格式时）
   提供冒烟证据
   ↓
6. 更新 docs/DATA_FLOW.md（新路径或修复断路）
   更新 docs/KNOWN_LIMITATIONS.md（如有遗留问题）
   ↓
7. 审查通过
```

---

## 八、什么情况允许跳过 L3

只有以下情况可以只做 L1 + L2，不做 L3：

1. **纯文档修改**：`.md` 文件、注释更新
2. **纯测试修改**：只改 `_test.go`，没有修改生产代码
3. **常量/类型重命名**：不改逻辑，只改名称，且 `go vet` 通过
4. **新增工具但未接入任何插件**：明确标注 `UNIMPLEMENTED`

**以上情况需在审查记录中注明跳过 L3 的原因。**

---

## 九、已知 AI 高频错误模式（参考）

从实际审查中总结，以下是 AI 最常犯的错误：

| 错误模式 | 典型代码 | 检测方式 |
|----------|----------|----------|
| Payload 双重编码 | `fmt.Sprintf("%q", json)` | 搜索 `%q` + Payload 的组合 |
| 返回 nil 但注释说"待填充" | `return nil // 由后续填充` | 搜索 `return nil` + 注释 |
| 函数实现了但没有调用点 | 函数导出但 grep 找不到调用 | `grep -rn FuncName | grep -v test` |
| TODO 注释过时（功能已实现） | `// TODO: 待实装` 但代码已存在 | 人工比对注释和代码 |
| 测试只断言无错 | `if err != nil { t.Fatal }` 然后结束 | 搜索测试中的 assert 数量 |
| 工具注册了但 schema 和函数体不一致 | required 字段在函数体中没有提取 | 人工比对 schema.required 和函数参数 |
| 调度注册了但 workflow 文件不存在 | `schedule_add` 指向不存在的 yaml | `ls workflows/ | grep name` |
| Plugin 返回空 Message（Payload 为 nil） | `return kernel.Message{}, nil`（成功路径） | 搜索 `return kernel.Message{}, nil` + 非 default 分支 |
| ValidateAndExecute 调用未检查 result.Success | `result := tools.ValidateAndExecute(...)` 后直接使用 Output | 搜索 `ValidateAndExecute` 然后检查其后面是否有 `result.Success` |
| 用 `result.Error != ""` 代替 `!result.Success` | `if result.Error != "" { ... }` | 两者的语义不同，ErrorResult 同时设置 Error 和 Success=false，但自定义 handler 可能只设 Success=false |
| workflow YAML step 缺少 on_error / timeout | steps 中无 `on_error:` / `timeout:` 字段 | `grep -A5 "^- id:" workflow/*.yaml \| grep -c "on_error:\|timeout:"` |
| workflow YAML 缺少 output_target 声明 | 文件无 `output_target:` | `grep -L "output_target:" workflows/*.yaml` |
| 非标准步骤格式的 YAML 混入 workflows/ | 步骤使用 title/description/deliverables 而非 plugin/type | 引擎执行时会执行空 plugin 步骤，静默失败 |

---

## 十、本规范的更新规则

- 每发现一个新的 AI 错误模式，在第九节补充
- 每次系统性审查后，把发现的共性问题补充到对应章节
- 本规范本身也适用本规范（每次修改必须通过 L1 + 更新日志）

> 最后更新：2026-05-28
