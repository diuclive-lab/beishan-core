# 安全变更操作手册（审计 / 重构 / 验证的可复用配方）

CLAUDE.md 的「集成纪律」给的是**原则与红线**；本手册给**判断那一半的具体配方**。
机械验证那一半已收成 `scripts/verify.sh`（build/vet/test/gofmt/集成检查一键），不在此处。

这些配方提炼自 2026-05-29 的可靠性/可用性/可维护性优化（devlog Task H–J）。
判断工作无法自动化——这里写的是「怎么判断」，不是「替你判断」。

---

## 配方 1：资源 / 错误审计——"最好的审计结果是无需大改"

三类隐患逐一过，每一类都要给出可验证的结论（而非"看起来没问题"）：

1. **HTTP `resp.Body` 泄漏**：逐文件数 producer（发请求）vs `Body.Close()` 配平。
2. **goroutine 泄漏**：每个 `go` 启动点必须落入下列**之一**，否则才算泄漏——
   - buffered `done`(cap≥1)：超时后发送也不阻塞；
   - 扇出 buffered-to-N + `RecoverWith` 兜底发送：panic 安全 ⇒ 同时 leak-safe；
   - 带显式 `stop` 路径的长生命周期 daemon；
   - 进程生命周期级（设计如此）；
   - fire-and-forget + `Recover`。
3. **被吞的 err**：逐个判定是否良性（`json.Marshal` 惯用法 / best-effort 写 / 带注释的 unlock）。

**grep 假阳性陷阱**：`\.Do(` 会误匹配 `sync.Once.Do()`（如 `browserOnce.Do`/`usageOnce.Do`），
造出不存在的"HTTP 泄漏"。审计 pattern 必须排除同名方法，用精确式：
`http.Get|http.Post|client.Do|http.DefaultClient.Do|c.Get`。

**心法**：审计可以、且常常应当结论为"全部健康、只留证据、不动代码"。
别为了"做了点什么"而强改——无谓改动本身就是风险。

---

## 配方 2：改"被多方消费的形状"前——"改一处先查三端"

动任何被多个客户端消费的形状（HTTP 响应、消息 payload、接口字段）前，
**先 grep 全部消费方核对契约**，再动手。本项目的三端：

- **web**（`clients/.../index.html`）：读哪个字段？通常宽松、有兜底。
- **iOS**（`clients/apple-core`）：Swift 解码**严格**——非可选字段缺失会解码失败抛错；
  且对非 2xx HTTP 抛错 ⇒ 错误响应也要保持 **HTTP 200**，否则具体消息丢失只剩状态码。
- **REPL**（`cmd/repl`）：读 `note` / `payload` 的顺序。

外加 **grep `_test.go`** 确认无测试断言旧形状。
反例教训：曾把失败响应回成 `{status:"sent"}` 且缺 `session_id` → iOS 解码失败、
服务端错误说明彻底丢失。形状要与成功响应**同构**，只改语义字段（`type`/`status`）。

---

## 配方 3：拆分大文件——"导入不相交"切口 + 三重验证

1. **选切口**：优先找**导入不相交**的簇——某组 import 只被某簇代码使用
   （如 `go/ast·parser·token·types` + `regexp` 只被 `go_struct_scan` 簇用）。
   这是最低风险的边界。动手前 grep 验证：簇**外**对目标符号零命中。
2. **字节级抽取**：`sed -n 'A,Bp'` 抽函数体（不手抄，零转录错误）+ `printf` 写干净 import 头。
   同包搬移，调用方零改动（跨文件同包调用无需 import 变化）。
3. **三验证坐实"纯搬移"**（而非"改写"）：
   - **定义计数守恒**：拆前顶层定义数 = 拆后各文件之和，每个符号恰出现一次；
   - **编译器兜底**：同包未用 import = 编译错误 ⇒ `go build` 全绿即"每文件 import 与用量精确匹配"，是免费安全网；
   - **gofmt 漂移方向**：纯搬移应使既存漂移**不变或下降**；若**上升**，说明在接缝引入了新 drift（如多余双空行），需排查。
4. **gofmt 决策 14**：带**多行块注释**的文件不要 `gofmt -w`（会规范化注释缩进，混入无关 diff）；
   只含单行注释的新文件可 `gofmt -w` 清干净。

---

## 配方 4：声明"完成"前——一键验证 + INTEGRATION_PROOF

1. 跑 `bash scripts/verify.sh`（build/vet/test/gofmt/集成检查）。
2. 填 CLAUDE.md 规定的 `INTEGRATION_PROOF`——**填不出任意一项 = 未完成集成**，不得声称完成。
   - 纯重构无新符号时如实写"无新增符号、纯包内重定位、数据流无变化"，DATA_FLOW.md 无需改。
3. 提交范围：**永远排除** `.claude/settings.json` 与 `docs/plans/`；按文件名 stage，不用 `git add -A`。

---

## 关于"把这些做成工作流模块"（可行性判断，2026-05-29）

引擎**支持**模块化组合：`workflow_run`（`plugins/workflow_plugin.go`）让一个工作流把另一个
当 step 调用，`agent_observer.yaml` / `batch_ingest.yaml` 已在用；`resolveNext` 支持
`Next[].If` 条件跳转。所以"小模块自由组合成工作流"机制是现成的。

但**能不能做成可信模块，取决于它是机械的还是判断的**（正是本项目硬化层哲学）：

| 类型 | 能否做模块 | 例 |
|------|-----------|-----|
| 机械 / 确定性（代码层保证）**且环境无关** | ✅ 好模块 | verify（`terminal_exec` 跑 hermetic verify.sh）、scan_large_files（`terminal_exec` find+wc，见下）、capability_inventory |
| 判断 / 开放式（只能提示词层提醒） | ⚠️ 至多"建议模块" | "定性遗漏 vs 设计"可做成 `code_ai_review`/`prompt_analyze` 的 LLM step，但输出是**建议非保证**，决策仍归人 |

**红线**：把"判断模块"当成"决策模块"用 = 违反硬化层原则（代码层保证 vs 提示词层提醒）。
模块可以**摆出候选**，但拍板的是开发者。

**实证精修（2026-05-29，`verify` 模块已建并经现网 daemon 实跑）**：第一个模块 `verify`
（`workflows/verify.yaml`，verify.sh 的运行时工作流外壳）做出来后，从守护进程实跑**当场翻车**
——300s 撑爆超时、terminal_exec 超时只回空。根因有二，都不是"机械性"问题而是**环境**问题：
① launchd 最小 PATH（仅 `/usr/bin:/bin:…`）里 `go` 不可见；② 守护进程 env 携带真实
`DEEPSEEK_API_KEY`，`go test ./...` 遂发起真实 LLM 调用、联网挂死。把 verify.sh 改成
**hermetic**（go 不可达时补 PATH 兜底 + unset 所有 LLM/embedding key 走离线跳过路径）后，
daemon 实跑从 300s 超时变 4.6s 全绿。**教训**：能做可信模块的判据要从「机械」升级为
**「机械 _且_ 环境无关」**——不依赖守护进程缺失的开发工具、不被其环境里的密钥扰动。

**再精修（2026-05-29，第 2 个模块 `scan_large_files` 实跑）**：本想用架构偏好的 L3 工具
`code_stats`（结构化、走硬化层），但现网 daemon（May 26 二进制）连报 `未知字段: limit/list_files`
——部署二进制里 code_stats 的 schema 早于这俩字段（top-N 排行是后加的），stale daemon 上给不出排行。
改用 `terminal_exec`+POSIX（find|wc|sort）才在 daemon 上稳跑（ElapsedMs 165）。**「环境无关」由此
多出第三味**：① 不依赖 daemon 缺失的开发工具（verify）② 不被其密钥扰动（verify）③ **不依赖 daemon
二进制里工具的 schema/feature 版本**（本次）。结论：要在「可能 stale 的守护进程」上稳跑，最 vintage-proof
的原语是 `terminal_exec`+POSIX；「优先用结构化 L3 工具」的架构偏好在 stale-daemon 现实前要让位，
除非能保证 daemon 二进制与源码同步。
