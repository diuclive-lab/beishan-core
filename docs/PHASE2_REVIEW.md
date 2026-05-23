# 第二阶段方案推演报告

基于真实代码的四个角度评估。

---

## 一、依赖角度：所有模块均使用 Go 标准库

| 模块 | 依赖 | 外部 API | 结论 |
|------|------|---------|------|
| 澄清契约 | 无 | 无 | ✅ 零依赖 |
| bench 评估框架 | fmt, math, sort, strings, time | 无 | ✅ 零依赖 |
| evidence 因果追踪 | fmt, sort, strings, time | 无 | ✅ 零依赖 |
| 茎注册表 | fmt, sort | 无 | ✅ 零依赖 |
| weather 工具 | net/http, net/url, encoding/json | Open-Meteo（免费） | ✅ 无新增 Go 依赖 |
| translate 工具 | net/http, encoding/json | LibreTranslate（免费） | ✅ 无新增 Go 依赖 |
| currency 工具 | net/http, encoding/json | open.er-api.com（免费） | ✅ 无新增 Go 依赖 |

**结论**：无需新增任何第三方 Go 依赖。

---

## 二、模块冲突角度：三处需要适配

### 冲突 1：茎注册表 Register 签名不一致

```
beishan-core: tools.Register(name, description, params, handler)
TwinFlower:   registry.Register(name, tool any, desc string) error
```

**影响**：茎不能直接包装现有 tools.Register。96 个工具签名不兼容。

**修正**：茎只取 Lifecycle + Policy，不做"替换注册 API"。注册仍走现有 tools.Register。

### 冲突 2：TwinFlower 工具接口 vs L3 ToolHandler

```
TwinFlower:     Run(ctx, args map[string]any) (string, error)
beishan-core:   func(args map[string]interface{}) *ToolResult
```

**修正**：每个工具写 10 行适配器封装。

### 冲突 3：clarify API 向后兼容

当前返回纯文本，修改后返回 JSON。

**修正**：一步到位。grep 确认 clarify 在 workflows/ 中零消费者，无需两阶段。

---

## 三、资源角度：无阻塞

所有外部 API 免费，无需 key。无新运行时依赖。

---

## 四、架构角度：各模块集成方式

| 模块 | 集成方式 | 时间 |
|------|---------|------|
| 澄清契约 | 新增 internal/clarify/，旧 handler 暂不改 | ~30min |
| bench 评估 | 新增 internal/bench/，纯复制+适配 | ~60min |
| evidence | 新增 internal/observatory/evidence.go | ~45min |
| 茎注册表 | 只取 Lifecycle+Policy，不包装 Register | ~60min |
| 3 个工具 | TwinFlower 逻辑 + L3 适配器 | ~90min |

---

## 五、总结

| 维度 | 结论 |
|------|------|
| 依赖 | ✅ 全部标准库 |
| 冲突 | ⚠️ 3 处，均有修正方案 |
| 资源 | ✅ 无新增 |
| 架构 | ✅ 方向正确 |
| 预估 | ~4.5 小时 |
