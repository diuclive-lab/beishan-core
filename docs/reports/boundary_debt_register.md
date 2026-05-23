# 边界债务登记册

> 记录已知的层边界违反，区分 known debt vs new violation。

## 当前债务

| ID | 位置 | 违规类型 | 描述 | 引入时间 | 状态 |
|----|------|---------|------|---------|------|
| D01 | plugins/think_plugin.go:488 | L4 直接文件系统 | ✅ 已修复：改用 `ValidateAndExecute("read_file")` | 2026-05 | 已修复 |
| D02 | plugins/review_handler.go:210 | L4 直接文件系统 | 读写已改用 L3 工具；MkdirAll/Remove 待 `delete_file` 工具 | 2026-05 | ⚠️ 部分修复 |
| D03 | plugins/skill_factory_plugin.go:522 | L4 直接文件系统 | PRIVILEGED PLUGIN：工作流编辑器固有行为 | 2026-05 | ✅ 已标记 |

## 扫描规则

- `scan_boundary.sh` 对已登记债务不做 fail，仅 warn
- 新违规仍 fail 阻断
- 债务修复后从本表移除
