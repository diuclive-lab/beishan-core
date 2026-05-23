# L1-L10 长期方案推演报告

基于真实代码逐项评估。

---

## L1: RightFlower Protocol v1

| 维度 | 状态 |
|------|------|
| 协议文档 v1 | ✅ 已标记 |
| Schema JSON | ✅ 存在 |
| Contract test | ❌ 缺失 |

**需要的**：补一个 Protocol Contract Test，验证 manifest/dispatch/response 的 schema 合规。
**预估**：~2h | **优先级**：P2

---

## L2: OpenHuman Production Adapter

| 维度 | 状态 |
|------|------|
| 适配器编译 | ✅ |
| Method 映射 | ✅ 4 个 |
| Bearer token | ✅ |
| 真实联调 | ❌ 未做 |

**需要的**：启动 OpenHuman，验证 3 个 method 真实返回。
**阻塞项**：OpenHuman 运行 + token 获取流程。
**预估**：~4h | **优先级**：P1

---

## L3: Core Observatory v1

| 维度 | 状态 |
|------|------|
| observatory 包 | ✅ 4 文件 |
| 决策追踪 | ✅ |
| 审计日志 | ✅ |
| 链路打通 | ⚠️ 未连通 |

**需要的**：rightflower.WriteAudit → observatory.Trace 链路。
**预估**：~3h | **优先级**：P2

---

## L4: Workflow Engine Hardening

| 维度 | 状态 |
|------|------|
| YAML 引擎 | ✅ |
| Go-DSL 引擎 | ✅ |
| ErrorKind | ✅ 6 类 |
| external_flower 步骤 | ⚠️ 已支持 via plugin: |
| 工作流恢复 | ❌ 未实现 |

**预估**：~4h | **优先级**：P3

---

## L5: Boundary Debt Burn-down

| 债务 | 状态 |
|------|------|
| D01 | ✅ 已修复 |
| D02 | ✅ 已修复（os.Remove 残留） |
| D03 | ✅ PRIVILEGED |

**预估**：~1h | **优先级**：P3

---

## L6: FangLab Capability Absorption

| 维度 | 状态 |
|------|------|
| 分析完成 | ✅ |
| Inventory | ✅ |
| 吸收队列 | ✅ F1-F3 |
| 实际吸收 | ❌ 未执行 |

**预估**：每项 ~3h | **优先级**：P3

---

## L7: Core Eval Harness v1

| 维度 | 状态 |
|------|------|
| cmd/core-eval | ✅ |
| bench 套件 | ✅ 3 |
| 纳入 Gate | ❌ |

**预估**：~1h | **优先级**：P2

---

## L8: Security Model v1

| 维度 | 状态 |
|------|------|
| 文档 | ✅ |
| 门禁检查 | ❌ |

**预估**：~1h | **优先级**：P3

---

## L9: Plugin Registry v2

| 维度 | 状态 |
|------|------|
| RegisterUnlisted | ✅ |
| route_exposed 测试 | ✅ |
| 语义文档化 | ⚠️ |

**预估**：~2h | **优先级**：P3

---

## L10: Core Developer Kit

| 维度 | 状态 |
|------|------|
| SDK 模板 | ✅ |
| manifest generator | ❌ |
| 快速开始 | ⚠️ |

**预估**：~3h | **优先级**：P2

---

## 优先级排序

| 优先级 | 方案 | 预估 | 理由 |
|--------|------|------|------|
| **P1** | L2 OpenHuman 联调 | 4h | 第一个真实右花实例 |
| **P2** | L1 Contract Test | 2h | 协议需测试保护 |
| **P2** | L3 链路追踪 | 3h | 可观测性缺口 |
| **P2** | L7 Eval 纳入 Gate | 1h | 评估需 CI 保护 |
| **P2** | L10 Manifest Generator | 3h | 降低第三方门槛 |
| **P3** | L4 Workflow Hardening | 4h | 已有替代方案 |
| **P3** | L5 D02 残留 | 1h | 非阻塞 |
| **P3** | L6 FangLab 吸收 | 9h | 可并行 |
| **P3** | L8 Security Gate | 1h | 文档已就绪 |
| **P3** | L9 语义文档化 | 2h | 双轨制已有 |
