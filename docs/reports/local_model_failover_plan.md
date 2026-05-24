# 本地模型自动切换方案

> 当 API 不可用时自动切换到本地模型。

---

## 现有基础设施

| 已有能力 | 文件 | 状态 |
|---------|------|------|
| LLM_PROVIDER=local | internal/llm/config.go | ✅ provider 已注册 |
| RouteStrategy 接口 | kernel/router.go | ✅ 可替换路由策略 |
| observatory.Check() | internal/observatory/health.go | ✅ 健康检查 |
| ErrorKind 分类 | internal/workflow/gods_error.go | ✅ 可区分网络错误 |

## 设计

### 1. 本地模型扫描器

新增 `internal/discovery/` 包，扫描本地运行的推理引擎：

| 引擎 | 检测方式 | 优先级 |
|------|---------|--------|
| Ollama | curl localhost:11434/api/tags | P0 |
| llama.cpp | curl localhost:8080/inf`erence | P1 |
| LocalAI | curl localhost:8080/v1/models | P1 |

返回可用模型列表及状态。

### 2. 健康检测

扩展 `observatory.Check()` 的 metrics，传入 `api_reachable`、`local_model_available`。

当 `api_reachable=false && local_model_available=true` 时触发切换。

### 3. 路由策略切换

利用已有的 `RouteStrategy` 接口：

```go
// DeepSeek 不可用时切换到本地策略
type LocalRouteStrategy struct {
    model    string
    endpoint string
}

func (s *LocalRouteStrategy) Route(msg Message) (*Decision, error) {
    // 调用本地模型，prompt 格式与 DeepSeek 对齐
    // parseDecision 校验格式（与 DeepSeek 一致）
}
```

调用方式：
```go
if !apiReachable && localModelAvailable {
    kernel.Router.SetStrategy(&LocalRouteStrategy{...})
}
```

### 4. 降级能力矩阵

| 功能 | DeepSeek | 本地模型 | 降级 |
|------|---------|---------|------|
| 路由 | ✅ 高质量 | ⚠️ 可能下降 | parseDecision 校验一致 |
| 对话 | ✅ | ✅ | 质量下降但可用 |
| 知识检索 | ✅ | ✅ | 不依赖 LLM |
| 工具调用 | ✅ | ⚠️ | 需对齐输出格式 |
| 硬化层 | ✅ | ✅ | 代码层不变 |

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/discovery/` | ✅ 已实现：11 引擎扫描 + 5 测试 |
| `internal/llm/config.go` | 新增：FailoverProvider() 自动切换 |
| `kernel/router.go` | 新增：LocalRouteStrategy 实现 |
| `cmd/beishan/main.go` | 启动时扫描 + 注册 fallback |
| `internal/observatory/health.go` | Check() metrics 增加 api/local 状态 |

## 需要设计纪律检查

根据纪律一（通道不能堵死）和纪律二（代码不是孤岛）：

- ✅ **通道**：本地模型切换不是堵死 API 通道，是提供 fallback 通道
- ✅ **广度**：涉及 5 个文件，需要与 observatory、router、llm config 连接
- ⚠️ **测试**：需要 mock 本地引擎的 httptest 服务
