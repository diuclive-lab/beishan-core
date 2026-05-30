# 边界债务登记册

> 记录已知的层边界违反，区分 known debt vs new violation。

## 当前债务

| ID | 位置 | 违规类型 | 描述 | 引入时间 | 状态 |
|----|------|---------|------|---------|------|
| D01 | plugins/think_plugin.go:488 | L4 直接文件系统 | ✅ 已修复：改用 `ValidateAndExecute("read_file")` | 2026-05 | 已修复 |
| D02 | plugins/review_handler.go:210 | L4 直接文件系统 | 读写已改用 L3 工具；MkdirAll/Remove 待 `delete_file` 工具 | 2026-05 | ⚠️ 部分修复 |
| D03 | plugins/skill_factory_plugin.go:522 | L4 直接文件系统 | PRIVILEGED PLUGIN：工作流编辑器固有行为 | 2026-05 | ✅ 已标记 |
| D04 | plugins/workflow_plugin.go:36,66 | L4 直接文件系统 | 工作流引擎读自身 workflow 目录：`os.ReadDir(Engine.Dir)` + 读过滤后的 `.yaml` 条目（路径由内部 `Engine.Dir`+目录项拼成，非用户输入）。引擎固有行为，同 D03 | 2026-05-30 | ✅ 已标记 |
| D05 | plugins/knowledge_calibration.go:51,64 | L4 直接文件系统 | 校准事件 JSONL 内部固定路径读写：`MemoryDir/knowledge_calibration.jsonl`（非用户输入）。内部遥测日志，同 D02 性质 | 2026-05-30 | ✅ 已标记 |

> D04/D05 于 2026-05-30 R4 门禁硬化时补登记：原先 `scan_boundary.sh` 一直 ⚠️ 不阻断，
> 这两处内部路径 I/O 债务静默累积未登记。补登后扫描转为「仅已知债务」通过，门禁方可硬化。

## 扫描规则

- `scan_boundary.sh` 对已登记债务不做 fail，仅 warn
- 新违规仍 fail 阻断
- 债务修复后从本表移除
