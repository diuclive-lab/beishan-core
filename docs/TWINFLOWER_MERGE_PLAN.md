# TwinFlower 融合执行方案（终版）

> 本方案是基于代码库逐项推演后的最终可执行版本。
> 所有模块的迁移方式均已与 beishan-core 和 TwinFlower 的真实代码对齐验证。

---

## 一、架构红线（不可违反）

1. **内核零改动**：L1 kernel/ 保持完全冻结，所有新模块通过 L3 internal/ 或 L4 plugins/ 接入
2. **LLM 层保持无状态**：internal/llm/ 只做 API 调用，不参与路由决策。TwinFlower 的 Provider 接口携带路由语义，不迁移
3. **不创造第二套类型系统**：所有流转数据统一适配到 internal/workflow/types.go
4. **不建平行模式学习系统**：TwinFlower 的偏好学习 EWMA 算法整合进现有的 clarify.go:patternStore，不新建

---

## 二、六个模块的最终融合方式

### 模块 1：cognition/

| 源文件 | 行数 | 处理方式 | 目标 |
|--------|------|----------|------|
| cognition/preferences/store.go | 210 | 只提取 EWMA 衰减算法 | 整合进 internal/tools/clarify.go:patternStore |
| cognition/profile.go | 131 | 解耦 PromptTemplates 作为澄清模板定义 | internal/cognition/（仅模板） |
| cognition/calibration/record.go | 39 | 路径适配到 beishan-core 数据目录 | internal/cognition/ |

**不迁移**：profile.go 中的 ChainStyle、ToolBias、Architecture（与 TwinFlower 私有路由引擎耦合）

**代码变更**：
- 修改 internal/tools/clarify.go 的 resolve() 方法，将当前简单比率 float64(p.Count)/float64(p.Threshold) 替换为 EWMA 衰减算法
- 新增 internal/cognition/ 目录，放入解耦后的模板定义和校准记录

---

### 模块 2：tool_selection/

| 源文件 | 行数 | 处理方式 | 目标 |
|--------|------|----------|------|
| tool_selection/skill.go | 90 | AllowedTools/ForbiddenTools 增强编译时检查 | internal/workflow/gods_executor.go:validateGoStep |
| filesystem_skill/skill.go | 512 | 拆分为 L3 硬化工具 + L4 编排器 | internal/tools/file_safe.go + plugins/filesystem_plugin.go |
| search_skill/skill.go | 291 | 拆分为 L3 校验 + L4 编排，歧义字典抽成 JSON | internal/tools/search_disambiguate.json + plugins/search_skill_plugin.go |

**拆分边界**：
- L3 只做安全检查：路径白名单、操作类型校验、并发锁
- L4 做编排：意图解析、路径提取、执行策略、错误处理

**调用链**：
```
L4 filesystem_plugin
  → 解析意图 + 提取路径
  → L3 validateFileOp(path)  ← 硬化校验
  → L3 lockFile(path)
  → 执行实际操作（通过 kernel.Call 调用现有 L3 工具）
  → L3 unlockFile(path)
  → 返回结果
```

**歧义字典 JSON**（注意：不依赖 schema_registry.go，用 `//go:embed` 加载）：
```json
{
  "ambiguous_terms": {
    "苹果": ["科技公司", "水果"],
    "小米": ["科技公司", "粮食"]
  },
  "disambiguators": ["公司", "手机", "股价", "水果", "集团"]
}
```

---

### 模块 3：providers/

**决策：不迁移。**

TwinFlower 的 Provider 接口有 Plan()（意图/工具选择）和 Finalize()（结果格式化），这是路由抽象。beishan-core 的 internal/llm/ 保持无状态——只负责 API 调用。多模型扩展通过现有 config.go 中 4 个 provider 的配置扩展实现。

---

### 模块 4：recovery/

| 源文件 | 行数 | 处理方式 | 目标 |
|--------|------|----------|------|
| recovery/toolerror.go | 73 | **全量迁移** ErrorKind 分类 | internal/workflow/gods_error.go |
| recovery/fallback.go | 98 | 只提取 FallbackMap 逻辑 | 整合进 runGoStepWithRetry 重试循环 |
| recovery/retry.go | 112 | **不迁移**，功能已有 | — |

**不迁移 retry.go** 的原因：YAML 引擎和 Go-DSL 引擎都已实现指数退避重试。

**fallback 实现位置**（不放 AfterExecute，放重试循环内部）：
- 主工具失败 → 判断 ErrorKind 是否可重试 → 尝试 fallback 工具 → fallback 也失败 → 指数退避重试
- 新增 `GoStep.Fallback` 字段（string，指定降级工具名）

---

### 模块 5：observatory/

**唯一全量迁移的模块**，374 行，无依赖冲突。

| 源文件 | 行数 | 修改内容 | 目标 |
|--------|------|----------|------|
| decision_trace/trace.go | 187 | 字段重命名 | internal/observatory/trace.go |
| decision_trace/metrics.go | 135 | 字段重命名 | internal/observatory/metrics.go |
| decision_trace/health.go | 52 | 字段重命名 | internal/observatory/health.go |

**字段映射**：

| TwinFlower 字段 | beishan-core 字段 |
|-----------------|-------------------|
| Skill | Plugin |
| WhyRouted | RouteReason |
| WhyClarified | 合并入 RouteReason |
| Trace.ID | 复用 glue/protocol.go 现有的 TraceID |

TraceID 已在 IPC 层（glue/protocol.go）存在，observatory 的 Recorder 可以直接以它为锚点。

---

### 模块 6：测试方案

不依赖不存在的 require_clarify 字段，改为调用实际的 clarify 工具。

```yaml
# eval/scenarios/clarify_smoke.yaml
cases:
  - name: "澄清链路验证"
    input:
      recipient: "think_plugin"
      type: "chat"
      payload:
        message: "看看苹果怎么样"
        mode: "no_retrieval"
    expect:
      - "响应包含澄清问题或搜索结果"
```

---

## 三、五步执行计划

| 步骤 | 内容 | 行数 | 风险 | 独立性 |
|------|------|------|------|--------|
| 1 | 迁移 observatory/（字段适配） | +374 | 最低 | 纯新增 |
| 2 | 提取 ErrorKind + fallback 整合 | +73 | 低 | 独立 |
| 3 | filesystem_skill + search_skill 拆分 | +803 | 最高 | 独立但重 |
| 4 | EWMA 整合进 clarify.go | +50 | 低 | 独立 |
| 5 | 文档 + 测试 | +50 | 最低 | 依赖前面成果 |

步骤 1-4 完全独立，可并行执行。

---

## 四、不迁移的文件清单

| 文件 | 行数 | 理由 |
|------|------|------|
| providers/provider.go | 32 | 接口携带路由语义 |
| providers/local.go | 132 | beishan-core 已有同等实现 |
| recovery/retry.go | 112 | 双方引擎都已实现重试 |
| cognition/profile.go 的 ChainStyle/ToolBias | ~50 | 耦合 TwinFlower 私有路由 |

---

## 五、融合后目录变化

```
internal/
├── cognition/          新增：澄清模板 + 校准记录
├── observatory/        新增：决策追踪 + 指标 + 健康检查
├── tools/
│   ├── clarify.go      增强：EWMA 衰减算法
│   ├── file_safe.go    新增：路径白名单、操作校验、并发锁
│   └── search_disambiguate.json  新增：中文歧义字典
└── workflow/
    ├── gods_executor.go  增强：ErrorKind 分类 + fallback
    └── gods_error.go     新增：ErrorKind 类型定义

plugins/
├── filesystem_plugin.go   新增：文件操作编排
└── search_skill_plugin.go 新增：搜索策略编排

eval/scenarios/
└── clarify_smoke.yaml     新增：澄清链路烟雾测试
```

---

## 六、验证标准

| 验证项 | 方式 | 标准 |
|--------|------|------|
| 编译 | go build ./... | 通过 |
| 现有烟雾测试 | run_legal_smoke.sh | 6/6 通过 |
| 核心烟雾测试 | run_core_smoke.sh | 全部通过 |
| ErrorKind 分类 | go test ./internal/workflow/... | 测试通过 |
| 决策追踪 | 手动验证 | 工作流执行后 trace 有记录 |
