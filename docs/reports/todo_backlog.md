# 待办积压（按优先级排序）

> 基于 2026-05-24 全量审计。

## P1 — 右花生态

| 项 | 说明 | 预估 |
|----|------|------|
| L2 OpenHuman method 参数对齐 | 472 schema 方法已发现，参数形状待对齐 | 2h |

## P2 — 协议与测试

| 项 | 说明 | 预估 |
|----|------|------|
| L1 Protocol Contract Suite | 已有 2 个测试，需完整 suite | 2h |
| L3 链路追踪 | rightflower.audit → observatory.Trace 打通 | 3h |

## P3 — 功能完善

| 项 | 说明 | 预估 |
|----|------|------|
| L4 external_flower 步骤 | 当前 via plugin: name，缺独立步骤类型 | 4h |
| L5 D02 残留 | MkdirAll/Remove 已标注 TODO | 1h |
| L6 FangLab F1-F3 | ✅ F1-F3 完成 | 9h |
| L8 Security Gate | ✅ 已纳入 core_gate (9/9) | 1h |
| L9 语义文档化 | ✅ 已写入 DESIGN_PRINCIPLES.md | 2h |
| OpenHuman 适配器联调 | cmd/openhuman-flower-adapter 联调 | 2h |
| SDK 三步跑通文档 | ✅ QUICK_START.md v2 | 1h |

## 边界债务

| ID | 位置 | 说明 |
|----|------|------|
| D02 | review_handler.go | MkdirAll/Remove 待修复 |
| D03 | skill_factory.go | PRIVILEGED 标记 |

## 中期方案 (M6-M10)

| 项 | 说明 |
|----|------|
| M6 | D01 完全验证 |
| M7 | D02 全面修复 |
| M8 | D03 特权边界 |
| M9 | RouteAudit → observatory |
| M10 | Security model gate |
