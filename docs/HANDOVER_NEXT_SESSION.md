# 交接文档 — 下一会话接续指南

## 当前状态

✅ Step 0-3 全部完成（含 P0 吸收验证）。

## P0 验证结论（2026-05-25）

### model_providers → llm/config.go：**部分吸收，当前够用**
- Hermes 有 30+ 声明式 ProviderProfile 插件；我们有 4 个硬编码 provider
- 基本多 provider 切换（SetProvider/ChatCompletionWithProvider）已吸收
- 缺失：声明式 profile 系统、插件式注册、per-provider hooks、自动 availability 降级
- **决定**：当前需求满足，gap 已知，不扩展。等需要动态 provider 注册时再处理。

### process_registry → glue/glue.go：**范围偏差，非缺陷**
- 两个系统解决不同问题：process_registry = 后台命令生命周期管理，glue.go = Python 插件 IPC
- glue 的健康检查（30s 循环 + 右花 HTTP + observatory Pulse）**比 Hermes 更强**
- 输出缓存、poll/wait/kill API、崩溃恢复等没吸收 → 因为定位不同，不是遗漏
- **决定**：评估通过，无需改动。

### 已知问题（待确认）
- 知识库同步：Mac B 有 62 条知识，Mac A 还没有（export 文件在 Mac B 桌面）
- knowledage_export.json 的 git 私人仓方案待决策（知识库里有记录）

### 已关闭 / 推迟
- ~~preRoute 长度检测~~ — 2026-05-25 推演后关闭。收益太小（¥0.012/天），且绕过硬化层有架构风险。替代方案：给 Router 加 usage 埋点，等数据驱动决策。

### 工具状态
- 服务运行在 :8013
- Python 右花包装器在 :9532（Hermes Agent）
- OpenHuman 适配器在 :9529
- 启动命令：`go run ./cmd/beishan/`

### 架构关键文档
- `workflows/absorb_right_flower.yaml` — 7 步吸收工作流（含经验积累）
- `workflows/project_analyze.yaml` — 源码级深度分析（L3 工具替代 terminal）
- `docs/RIGHT_FLOWER_PROTOCOL.md` — 右花接入协议
- `cmd/rightflower-python-wrapper/rightflower_adapter.py` — Python 右花模板

### 重要架构原则
- 吸收评估逐能力，不逐项目（Step 2 条件0）
- 评估前必须读源码（Step 0）
- compared ≠ skip，是取长补短
- 硬化层不保证逻辑正确性（明确声明能力边界）
