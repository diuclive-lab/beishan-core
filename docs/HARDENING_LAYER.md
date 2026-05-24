# 硬化层能力声明

> **AI Summary:** Hardening layer = 5 defenses: 
> (1) parseDecision JSON+confidence+knownPlugins, 
> (2) ValidateParams type+required+unknown-field rejection, 
> (3) isSafePath path traversal prevention, 
> (4) code_security 8 rules (SQL injection, reverse shell, etc.), 
> (5) validate_file_op read/write/delete guard.
> Does NOT guarantee: business logic correctness, LLM output quality, or absence of side effects.

## 什么是硬化层

硬化层是 beishan-core **最独特的架构决策**。核心思想：**能用确定性代码解决的问题，绝不让 LLM 参与。**

硬化层不是单一关卡，而是分布在各个层面的检查机制：

```
输入（LLM/L4 编排） ──→ ① Router.parseDecision（三层校验）
                            │
                            ↓ 通过
                        ② ValidateAndExecute（Schema 校验）
                            │
                            ├─→ Schema 注册中心（类型定义）
                            ├─→ Schema 校验（严格 JSON Schema）
                            │
                            ↓ 通过
                        ③ L3 工具执行（内部安全检查）
                            ├─→ isSafePath（文件路径安全）
                            ├─→ code_security（危险模式检测）
                            └─→ code_apply（补丁安全应用）
```

### 三层校验关卡

| # | 关卡 | 位置 | 职责 |
|---|------|------|------|
| ① | 路由校验 | `kernel/router.go:parseDecision` | 三层：JSON 格式→置信度≥0.4→收件人在 knownPlugins 中 |
| ② | Schema 校验 | `internal/tools/validate.go:ValidateAndExecute` | 参数符合 JSON Schema，拒绝非法类型/缺失字段/未知字段 |
| ③ | 工具安全 | `internal/tools/file.go:isSafePath` + `code_security.go` | 路径安全、危险命令、权限滥用、自修改防护 |

**注意**：`code_security.go` 和 `code_apply.go` 是 L3 工具（通过 ValidateAndExecute 调用），不是管道中的固定环节。

### 硬化层不是一条流水线

ValidateAndExecute 是 L4 调用 L3 的**唯一入口**，但它不做代码安全检测——那是 `code_security_check` 工具的职责，由工作流显式调用。

```
✅ 正确流程：
L4 编排
  → 1. Call("code_security_check", diff)    ← 显式安全检查
  → 2. Call("code_apply", patch)            ← 显式补丁应用
     ↳ L3 内部: tools.ValidateAndExecute("code_security_check", ...)
     ↳ L3 内部: tools.ValidateAndExecute("code_apply", ...)

❌ 不存在的流程（文档纠正）：
L4 编排 → ValidateAndExecute → 自动触发 code_security → 自动触发 code_apply
```

## 保证的（表层安全）

| 类别 | 保证 | 实现 |
|------|------|------|
| ✅ 格式安全 | 所有进出 L3 的数据必须通过 JSON Schema 校验 | `ValidateParams` |
| ✅ 类型正确 | 字段类型必须匹配 Schema 定义 | `ValidateParams` |
| ✅ 字段约束 | 必填字段不能缺失，禁止未知字段注入 | `ValidateParams` |
| ✅ 路由校验 | DeepSeek 的回复必须满足 JSON 格式、置信度≥0.4、收件人已注册 | `parseDecision` |
| ✅ 命令安全 | 禁止危险系统调用（rm -rf、强制删除、dd 覆写） | `code_security` 规则 |
| ✅ 路径安全 | 禁止工作目录外的文件读写、路径穿越（`../`） | `file.go:isSafePath` |
| ✅ 文件操作校验 | 操作类型（read/write/delete）+ 路径白名单 | `file_safe.go:validate_file_op` |
| ✅ 文件并发锁 | 防止并发写入冲突 | `file_safe.go:lock_file/unlock_file` |
| ✅ 敏感路径 | 禁止写入 `/etc/`、`/dev/`、`/sys/`、`/proc/` 等系统路径 | `file.go:isSafePath` |
| ✅ 权限安全 | 检测 `chmod 777`、`chown` 等不安全权限操作 | `code_security` 规则 |
| ✅ 自修改防护 | 禁止写入 `kernel/`、`internal/tools/schema.go`、`internal/tools/validate.go` 等核心文件 | `code_security:self_modification` 规则 |
| ✅ 命令注入 | 检测 `exec.Command` + 变量拼接的危险模式 | `code_security` 规则 |
| ✅ 硬编码密钥 | 检测代码中的 API key、密码等敏感信息 | `code_security:hardcoded_secret` 规则 |

> **自修改防护范围**（2026-05-23 确认）：正则匹配 `internal/tools/(knowledge\|tools\|validate\|schema).go` 和 `kernel/kernel.go`、`kernel/router.go`。不包括 `code_security.go` 自身。

## 不保证的（深层正确性）

| 类别 | 不保证 | 依赖什么 |
|------|--------|----------|
| ❌ 逻辑正确性 | 代码可能编译通过但业务逻辑错误 | 单元测试 + 烟雾测试 + 人工审查 |
| ❌ 安全漏洞 | SQL 注入、XSS、CSRF 等业务层漏洞 | `code_security` 规则持续更新 + 安全审计 |
| ❌ 架构质量 | 错误的抽象、循环依赖、包边界违反 | `code_deep_analyze` 工作流 + 架构审查 |
| ❌ 性能安全 | 死循环、内存泄漏、资源耗尽 | 性能测试 + `project-health` 门禁 |
| ❌ LLM 路由正确性 | DeepSeek 是否选择了正确的插件/工具 | `parseDecision` 约束（格式+置信度+存在性） |
| ❌ 业务合规 | GDPR、隐私合规、法律合规 | 领域特定审查工作流（如 `legal_review`） |

## 为什么这样设计

```
硬化层的目标不是"保证代码正确"，
而是"保证 LLM 的不可靠输出不会直接破坏系统"。
```

深层正确性依赖多层防线：

```
LLM / L4 输出 ──→ ① 路由校验（parseDecision）
                     │
                     ↓ 通过
                 ② Schema 校验（ValidateAndExecute）
                     │
                     ↓ 通过
                 ③ 工具安全检查（isSafePath + code_security）
                     │
                     ↓ 通过
               ─────────────────
              自动化测试（烟雾+单元+集成）
                     │
                     ↓ 通过
              code_deep_analyze（架构审查工作流）
                     │
                     ↓ 通过
              人工审查 + 门禁
```

硬化层是第一条防线，不是最后一条。

## 如果绕过硬化层会怎样

| 绕过方式 | 后果 |
|----------|------|
| 直接调 `tools.Execute(name, payload)` | Schema 校验被跳过，类型错误可能穿透 |
| 插件直接操作文件系统 | `isSafePath` 路径检查被跳过 |
| 修改 `kernel/` 代码 | 整层冻结合约被破坏，路由可靠性失效 |
| 在 L4 中不调 code_security_check | 危险代码变更可能被直接写入 |

**硬化层的有效性最终取决于开发者的纪律。** 这是无法用代码强制解决的根问题——正因如此，日志审计、安全 review 和本文档是硬化层不可缺少的补充。

## 验证硬化层是否生效

```bash
# 编译检查（Go-DSL 插件构造时校验 Tool 注册表）
go build ./...

# 烟雾测试
eval/scripts/run_legal_smoke.sh

# 检查是否有直接调 tools.Execute 的绕过代码
grep -rn "tools\.Execute(" plugins/ internal/tools/ 2>/dev/null | grep -v "_test.go" | grep -v "validate.go"
# 期望输出为空——唯一允许的调用在 validate.go 内部
```
