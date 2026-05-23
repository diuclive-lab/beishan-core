# 硬化层能力声明

## 什么是硬化层

硬化层是 beishan-core **最独特的架构决策**。它的核心思想是：**能用确定性代码解决的问题，绝不让 LLM 参与。** 硬化层不是后门或者补充检查——它是 L4 调用 L3 的唯一入口，是 LLM 不可靠输出进入业务逻辑之前必须通过的关卡。

### 硬化层五道防线

```
L4 编排层 ──→ ① ValidateAndExecute（Schema 校验）
                  │
                  ├─→ ② Schema 注册中心（类型+边界定义）
                  ├─→ ③ Schema 清理器（严格后端适配）
                  ├─→ ④ code_security（危险模式检测）
                  └─→ ⑤ code_apply（安全应用补丁）
                  │
                  ↓
            L3 执行层（如 file.go, web.go, memory.go）
```

| # | 防线 | 文件 | 职责 |
|---|------|------|------|
| ① | Schema 校验 | `validate.go` | 校验入参符合 JSON Schema，拒绝非法类型、缺失字段、未知字段 |
| ② | Schema 注册 | `schema_registry.go` | 工具 Schema 的单一真实来源，Router 查询时不触碰 Payload |
| ③ | Schema 清理 | `schema_sanitizer.go` | 修复裸字符串类型、数组 null 联合类型等，适配严格后端 |
| ④ | 安全检测 | `code_security.go` | 正则匹配危险模式（rm -rf、路径穿越、权限滥用、自修改） |
| ⑤ | 安全应用 | `code_apply.go` | 补丁应用时的二次安全检查，防止补丁破坏文件完整性 |

## 保证的（表层安全）

硬化层的保证范围是**格式安全和命令安全**，覆盖了 LLM 输出中最常见、最危险的错误模式：

| 类别 | 保证 | 实现层 |
|------|------|--------|
| ✅ 格式安全 | 所有进出 L3 的数据必须通过 JSON Schema 校验 | ① ValidateParams |
| ✅ 类型正确 | 字段类型必须匹配 Schema 定义（string/int/bool/array/object） | ① ValidateParams |
| ✅ 字段约束 | 必填字段不能缺失，禁止未知字段注入 | ① ValidateParams |
| ✅ 命令安全 | 禁止危险系统调用（rm -rf、强制删除、dd 覆写） | ④ code_security |
| ✅ 路径安全 | 禁止工作目录外的文件读写、路径穿越（`../`） | ⑤ isSafePath |
| ✅ 敏感路径 | 禁止写入 `/etc/`、`/dev/`、`/sys/`、`/proc/` 等系统路径 | ⑤ isSafePath |
| ✅ 权限安全 | 检测 `chmod 777`、`chown` 等不安全权限操作 | ④ code_security |
| ✅ 自修改防护 | 禁止写入 `kernel/`、`internal/tools/code_security.go` 等关键文件 | ④ code_security |
| ✅ 命令注入 | 检测 `exec.Command` + 变量拼接的危险模式 | ④ code_security |
| ✅ Schema 合规 | 工具必须有 Schema 定义才能被 Router 识别 | ② GetToolSchema |

## 不保证的（深层正确性）

硬化层**不是**代码审查器、不是测试框架、不是形式化验证。以下内容不在硬化层的保证范围内：

| 类别 | 不保证 | 依赖什么 |
|------|--------|----------|
| ❌ 逻辑正确性 | 代码可能编译通过但业务逻辑错误 | 单元测试 + 烟雾测试 + 人工审查 |
| ❌ 安全漏洞 | SQL 注入、XSS、CSRF 等业务层漏洞 | `code_security` 规则更新 + 安全审计 |
| ❌ 架构质量 | 错误的抽象、循环依赖、包边界违反 | `code_deep_analyze` 工作流 + 架构审查 |
| ❌ 性能安全 | 死循环、内存泄漏、资源耗尽、N+1 查询 | 性能测试 + `project-health` 门禁 |
| ❌ LLM 行为 | DeepSeek 是否选择了正确的路由/工具 | Router `parseDecision` 三层校验 |
| ❌ 业务合规 | GDPR、隐私合规、法律合规 | 领域特定的审查工作流（如 legal_review） |

## 为什么这样设计

```
硬化层的目标不是"保证代码正确"，
而是"保证 LLM 的不可靠输出不会直接破坏系统"。
```

深层正确性依赖多层防线：

```
LLM 输出 ──→ 硬化层（格式+安全检查）
                │
                ↓ 通过
           自动化测试（烟雾+单元+集成）
                │
                ↓ 通过  
           code_deep_analyze（架构审查工作流）
                │
                ↓ 通过
           人工审查 + project-health 门禁
```

硬化层是第一条防线，不是最后一条。

## 如果绕过硬化层会怎样

硬化层假设**所有 L4 代码都必须通过 `ValidateAndExecute`**。如果绕过——比如直接调用 `tools.Execute`——后果将由调用者承担：

| 绕过方式 | 绕过点 | 后果 |
|----------|--------|------|
| 直接调 `tools.Execute(name, payload)` | `validate.go:18` | Schema 校验被跳过，类型错误可能穿透 |
| L4 拼装 payload 时不做校验 | 编排代码 | 非预期的字段进入工具处理函数 |
| 插件直接操作文件系统 | 自定义插件 | 路径安全检查被跳过，可能越界写入 |
| 修改 kernel/ 代码 | 内核层 | 整层冻结合约被破坏，路由可靠性失效 |

**硬化层的有效性最终取决于开发者的纪律。** 这是无法用代码强制解决的根本问题——正因如此，日志审计、安全 review 和本文档是硬化层不可缺少的补充。

## 验证硬化层是否生效

```bash
# 编译检查（Go-DSL 插件构造时校验 Tool 注册表）
go build ./...

# 烟雾测试（6/6 全链路冒烟）
eval/scripts/run_legal_smoke.sh

# 完整性检查（验证 ValidateAndExecute 覆盖率）
grep -r "tools\." plugins/*.go | grep -v "ValidateAndExecute" | grep -v "_test.go"
# 合法的调用模式只有：ValidateAndExecute、HasTool、GetToolSchema
```
