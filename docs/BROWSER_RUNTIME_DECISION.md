# Browser Runtime 路线决策

> 创建：2026-05-30
> 状态：已决定，立即执行

---

## 决策

**只做 Chromium 深绑，Servo 暂停，不再碰 Servo FFI。**

## 理由

| 路线 | 状态 | 结论 |
|------|:----:|------|
| Chromium (CDP-over-pipe) | 生产可用，5 阶段完成 | **主力路线，继续深绑** |
| Servo WebDriver (方式 B) | 实验性，已验证通过 | **冻结，不移除** |
| Servo Embedder (方式 C) | Rust 嵌入器已实现 | **删除** |
| Servo in-process FFI (方式 A) | 5 项未知，探索成本高 | **永久取消** |

## 执行步骤

### Stage D（立即）：删除 servo-embed 清理

- 删除 `cmd/servo-embed/`（Rust 项目）
- 删除 `internal/browser/servo_embed.go`（Go 集成）
- 删除 `internal/browser/servo_test.go`
- 从 `factory.go` 移除 `EngineServoEmbed`、`EngineServo`
- 从 `right_flowers/` 移除 `servo.yaml`
- `go build` + `go test` 验证

### Stage B（下一步）：反爬实测拿 baseline

在修改指纹代码之前，先对真实高防站跑一轮测试：

| 目标 | 检测内容 |
|------|----------|
| `bot.sannysoft.com` | 经典 headless 检测 |
| `creepjs.site` | 现代 JS 指纹 + 一致性 |
| `fingerprint.com/products/bot-detection/demo` | 商业 bot 检测 |
| Cloudflare 挑战页 | CDN 层 TLS/HTTP |
| Datadome 保护站点 | 商业行为检测 |

先 baseline，再改代码，再对比。

### Stage C（待 baseline 后）：Chromium 深绑

- JS property graph consistency
- CDP DevTools hook 检测绕过
- TLS/JA3（依赖系统 Chrome 原生栈）
- Behavioral 仿真
- 持久化 identity
