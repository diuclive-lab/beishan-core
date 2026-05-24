# 本地模型降级矩阵

> 当 API 不可用自动切换到本地模型时，各功能的可用性和质量变化。

## 降级矩阵

| 功能 | API 模式 | 本地模型模式 | 降级影响 |
|------|---------|-------------|---------|
| 路由（Router） | DeepSeek 高质量路由 | 本地模型路由（质量下降） | parseDecision 硬化层校验格式，但模型能力差异可能导致路由准确度下降 |
| 对话（think_plugin） | DeepSeek chat | 本地模型 chat（质量下降） | 回复质量取决于本地模型能力，但硬化层不变 |
| 知识检索 | LLM 不参与 | LLM 不参与 | ✅ 无影响，检索逻辑在代码层 |
| 工具调用 | DeepSeek 格式化输出 | 本地模型格式化输出（不稳定） | ⚠️ 本地模型可能输出非法 JSON，硬化层 reject 并记录到 trace |
| 硬化层校验 | ✅ 完全生效 | ✅ 完全生效 | 硬化层在代码层运行，不依赖 LLM |
| 文件操作 | ✅ 正常 | ✅ 正常 | 不依赖 LLM |
| 代码安全 | ✅ 正常 | ✅ 正常 | 不依赖 LLM |
| 工作流执行 | ✅ 正常 | ✅ 正常 | YAML/Go-DSL 引擎在代码层运行 |
| 右花协议 | ✅ 正常 | ✅ 正常 | 右花 dispatch 不依赖 LLM |
| 审计日志 | ✅ 正常 | ✅ 正常 | 不依赖 LLM |

## 切换触发条件

| 条件 | 行为 |
|------|------|
| API reachable | 使用默认 DeepSeek 策略 |
| API unreachable + local available | 自动切换到 LocalRouteStrategy（1 次失败即触发） |
| API unreachable + local unavailable | Router 返回明确错误，不崩溃 |
| API recovered | 滞后 2 次连续成功才切回（防止抖动） |

## 已知限制

1. 本地模型的路由质量可能低于 DeepSeek，但硬化层的 `parseDecision` 校验会拒绝格式不合法的输出
2. 工具调用 JSON 格式可能不稳定，硬化层 reject 时会记录到 trace（参见 `internal/observatory/trace.go`）
3. 切换过程有 ~2-3s 延迟（健康检查间隔 + 滞后阈值）
