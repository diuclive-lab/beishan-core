# 编码智能体实施方案

## 一、架构原则（讨论结论存档）

### 角色分工

```
用户需求
  → beishan-core（唯一入口）
    → 理解意图、拆解任务、定方案
    → Claude CLI（作为子进程调用，只负责写代码）
      → beishan-core（硬化层校验 + FactCheck 输出门禁）
        → 通过 → code_apply 写入文件 → 触发热更新
        → 不通过 → 拒绝 + 说明原因
```

| 谁 | 做什么 | 不做 |
|----|--------|------|
| beishan-core | 理解需求、拆任务、校验输出、写入代码、管理知识库 | 写代码 |
| Claude CLI | 按 prompt 生成代码 diff | 不接触知识库、不决策 |

### 约束

1. **Claude CLI 不入侵知识库** — `~/.claude/` 下的任何文件不读、不写、不导入
2. **Claude CLI 不决策** — 写什么、能不能写、写得对不对，由 beishan-core 判断
3. **硬化层守在所有入口** — 任何外部生成的代码必须经过 schema 校验 + 安全检查才能写入
4. **热更新** — beishan-core 改了自身代码后触发热重载，不需要手动重启
5. **统一入口** — 所有操作通过 beishan-core 路由，不绕行

---

## 二、实施阶段

### Phase 1：code_task L3 工具（统一入口）

**目标：** 一个统一的编码入口，接收需求描述，返回可执行的任务拆解。

```go
// internal/tools/code_task.go

type CodeTaskRequest struct {
    Goal        string   `json:"goal"`         // 用户需求
    Files       []string `json:"files"`         // 涉及的文件（可选）
    Constraint  string   `json:"constraint"`    // 约束条件
}

type CodeTaskPlan struct {
    Steps       []CodeStep `json:"steps"`       // 任务拆解
    Risk        string     `json:"risk"`        // high/medium/low
}

type CodeStep struct {
    Action      string   `json:"action"`        // read / write / refactor / search
    File        string   `json:"file"`
    Description string   `json:"description"`
    Prompt      string   `json:"prompt"`        // 给 Claude CLI 的 prompt
}
```

- `code_task` — 拆解需求 → 生成多步计划
- `code_apply` — 接收计划 + diff → 校验 → 写入 → 触发 reload

**不依赖 Claude CLI。** Phase 1 只做拆解和计划，实际的代码生成和写入在 Phase 2。

### Phase 2：Claude CLI 子进程调用（手）

**目标：** 通过 glue IPC 或 exec.Command 调 Claude CLI，传递 prompt，拿回 diff。

```
code_apply 步骤
  └→ 构建 prompt（含文件上下文 + 需求描述）
  └→ exec.Command("claude", "-p", prompt) 或 glue IPC
  └→ 解析 stdout 中的 diff
  └→ 走硬化层校验
    └→ ValidateAndExecute → schema 校验
    └→ code_security_check（新工具）
      └→ 检测危险模式：rm -rf、exec、rm -rf /*、> /dev/...
      └→ 检测超出范围的修改
    └→ FactCheck 输出门禁
  └→ 通过 → code_write（L3 工具，受控写入）
  └→ 不通过 → 返回错误 + 说明
```

**关键：** Claude CLI 的 stdout 是人类可读的混合文本 + diff。需要解析器提取结构化 diff。

### Phase 3：热更新

**目标：** `code_apply` 写入文件后自动生效，不重启进程。

分层方案：

| 层级 | 能否热更新 | 触发方式 |
|------|:---------:|----------|
| L3 tools | ✅ | `tools.Init()` 重新注册 |
| workflows YAML | ✅ | `buildWorkflowSummary()` 重新扫描 |
| 前端 HTML | ✅ | HTTP 无缓存 |
| kernel | ❌ | 重启 |

```go
// kernel/reload.go
func (k *Kernel) ReloadTools() {
    tools.Init()                     // 重新注册所有 L3 工具
    k.Router.SetWorkflowSummary(buildWorkflowSummary("./workflows"))
}
```

**不做的：**
- 不支持 kernel 层热更新（冻结原则）
- 不支持正在执行的 workflow 热更新（当前跑完再切新版本）

### Phase 4：编码观察者 Agent

**目标：** 类似 `agent_observer`，但监控的是自己的代码质量。

```
code_observer（定时触发）
  → code_grep 检测常见模式
  → code_diff 对比最新提交
  → LLM 分析代码质量趋势
  → 创建改进建议入库
```

---

## 三、硬化层扩展

### code_security_check（新 L3 工具）

```go
CheckResult struct {
    Safe    bool     `json:"safe"`
    Issues  []Issue `json:"issues"`
}
```

检测规则：
- `rm -rf` 等危险 shell 命令
- 写入敏感路径（`/etc/`、`/dev/`、`~/.ssh/`）
- 超出指定文件范围的修改
- 不安全的权限设置（`chmod 777`、`suid`）

### code_apply 增强

现有 `write_file` 工具 → 升级支持 diff 应用：

```go
func codeApplyHandler(args map[string]interface{}) *ToolResult {
    filepath := args["path"]   // 目标文件
    diff := args["diff"]       // unified diff
    plan := args["plan_id"]    // 对应 code_task 的计划 ID
    
    // 1. 安全检查
    check := codeSecurityCheck(diff)
    if !check.Safe { return errorResult(check.Issues) }
    
    // 2. 应用 diff（用 Go 的 diff 库或直接行操作）
    applyDiff(filepath, diff)
    
    // 3. 触发 reload
    triggerReload(filepath)
}
```

---

## 四、完整流程示例

```
用户：给 system_info 工具加一个磁盘空间查询功能

→ code_task（L3 beishan-core）
  → 拆解：
    1. 读 system_info.go 现有结构
    2. 在 HardwareSummary 函数里加 diskSpace 子调用
    3. 写单元测试
  → 给 Claude CLI 生成 3 个 prompt

→ 对每个 prompt：
  → Claude CLI（子进程）
    → 生成代码 diff
    → beishan-core code_security_check
      → 检查：没有危险命令、没有越界写
    → code_apply
      → 写入文件
      → 触发热更新

→ 全部 3 步完成后
  → 报告给用户
```

---

## 五、不做的事

| 项目 | 理由 |
|------|------|
| 导入 Claude CLI 的知识文件 | 两个 AI 知识体系独立，不交叉 |
| kernel 层热更新 | 冻结原则，不改 |
| 正在执行的 workflow 热切换 | 复杂度高，收益低 |
| code_task 直接调 Claude CLI | 拆解和生成分层，出错时定位更快 |

---

## 六、文件清单

| Phase | 文件 | 说明 |
|-------|------|------|
| 1 | `internal/tools/code_task.go` | code_task + code_apply 工具 |
| 2 | `plugins/claude_cli_plugin.go` | Claude CLI 子进程管理（IPC） |
| 2 | `internal/tools/code_security.go` | diff 安全检查工具 |
| 3 | `kernel/reload.go` | 热更新触发机制 |
| 4 | `workflows/code_observer.yaml` | 代码质量监控工作流 |
