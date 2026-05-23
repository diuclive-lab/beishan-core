# 边界债务登记册

> 记录已知的层边界违反，区分 known debt vs new violation。

## 当前债务

| ID | 位置 | 违规类型 | 描述 | 引入时间 | 状态 |
|----|------|---------|------|---------|------|
| D01 | plugins/think_plugin.go:488 | L4 直接文件系统 | ✅ 已修复：改用 `ValidateAndExecute("read_file")` | 2026-05 | 已修复 |
| D02 | plugins/review_handler.go:210 | L4 直接文件系统 | 直接调 `os.MkdirAll`/`WriteFile`/`Remove`，应走 `code_apply` | 2026-05 | 已知 |
| D03 | plugins/skill_factory_plugin.go:522 | L4 直接文件系统 | 直接操作 YAML 工作流文件（读/写/删） | 2026-05 | 已知 |

## 扫描规则

- `scan_boundary.sh` 对已登记债务不做 fail，仅 warn
- 新违规仍 fail 阻断
- 债务修复后从本表移除
