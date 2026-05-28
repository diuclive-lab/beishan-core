# SiYuan Note（思源笔记）吸收记录

> 吸收日期：2026-05-28
> 参考版本：github.com/siyuan-note/siyuan（2026-05-28 最新 main）
> 吸收类型：L3 参考设计（零代码复制）
> 参考路径：`/Users/dc/Desktop/cankaocangku/siyuan/`

---

## 一、参考项目概况

- **仓库**: github.com/siyuan-note/siyuan (AGPL-3.0)
- **语言**: Go 30.8% + TypeScript 56.3%（kernel Go，前端 Electron）
- **核心模块**: kernel/ (17 子模块，~93,000 行)
- **适用场景**: 块级知识管理、双向链接笔记、图谱查询

---

## 二、吸收决策总表

| 能力 | 吸收等级 | 工作量 | 内化位置 |
|------|:--------:|:------:|----------|
| Block 模型（块树） | **L3** | 2 天 | `internal/tools/storage.go` |
| 块级存储（.sy JSON） | **L3** | 1 天 | `internal/tools/storage.go` |
| 块级搜索（+2/block） | **L3** | 1 天 | `internal/tools/knowledge.go` |
| [[wikilink]] 反向链接 | **L2** | 1 天 | `internal/tools/link_index.go` |
| 知识图谱 Local/Global | **L3** | 1 天 | `internal/tools/knowledge_graph.go` |
| Lute markdown 解析渲染 | **L1** | 2 小时 | `internal/tools/markdown_blocks.go` |
| Document 级属性 | **L3** | — | `storage.go` Properties（附带） |

**总计**: 6 功能，~5 天，12 文件，~1500 行

---

## 三、缺口分析（未吸收能力）

### 已评估、明确不吸收

| 能力 | 不吸收原因 |
|------|-----------|
| SQLite FTS5 全文搜索（sql/ 7,598 行） | 现有 L0 关键词 + L1 语义搜索已满足需求，不引入 SQLite 依赖。零外部 dep 原则优先 |
| Datalog 查询引擎 | 过于复杂，当前图谱查询用 typed_links + BuildGraph 替代。需要时再评估 |
| AV 属性视图（av/ 7,436 行） | 数据库式表格视图，当前无需求 |
| DejaVu 版本化存储（satellite repo） | 备份/同步不是核心能力，launchd + git 已覆盖 |
| 事务系统（model/transaction.go 2,075 行） | 当前单步写入足够，等需要原子多步操作时再补 |
| 插件市场（bazaar/ 1,315 行） | 不适用，beishan-core 的插件是 Go 代码编译，不是运行时加载 |
| Electron 前端（app/） | beishan-core 已有独立 Web UI，不重复造 |
| 间隔重复（RiffCard/FSRS） | 学习类功能，当前知识管理系统不需要 |
| 编辑器引擎（lute 已有，不用 SiYuan 封装层） | 直接 `go get github.com/88250/lute`，不经过 SiYuan 的 editor 封装 |
| 会话/认证（model/session.go） | beishan-core 有独立的 session 管理 |
| 云同步（model/sync.go, cloud_service.go） | 本地优先设计，不支持云端同步 |

### 待评估（可能遗漏）

| 能力 | 参考文件 | 行数 | 当前判断 |
|------|----------|:----:|----------|
| 块级 IAL（自定义属性） | `model/blockial.go` | 372 | 未吸收。Document 级 Properties 已满足当前需求。若未来需要按块属性查询（如"找出所有 priority=high 的段落"），需补。可在 Block 结构体加 `IAL map[string]string` 预留 |
| IAL 查询 | `model/blockial.go` (SetBlockAttr/GetBlockAttr) | — | 无独立实现，依赖 `sql/` 查询路径。当前无需实现 |
| 标签系统（#tag# 语法） | `model/tag.go` | 432 | 未独立吸收。beishan-core 已经有 `Tags []string`，通过 metadata 检索。SiYuan 的 `#tag#` 行内标签语法可通过 Lute 解析，但目前未做 |

---

## 四、吸收过程回顾

### 做得好的

- **同语言优势利用充分**：SiYuan kernel 也是 Go，直接读源码理解设计，没有跨语言翻译成本
- **只吸设计不吸代码**：Block 模型参考了 SiYuan 的 `model/block.go`，但简化了字段（去掉了 SiYuan 特有的 RiffCard、FSRS、Defs 循环引用），适配 beishan-core 的 KnowledgeEntry 管道
- **Lute 直接 go get**：MulanPSL 2.0 许可，纯 Go，无间接依赖，零集成成本
- **引用项目义务已履行**：已记录到 `DESIGN_PRINCIPLES.md` 参考项目章节

### 可以改进的

- **未运行正式吸收工作流**：`absorb_right_flower.yaml`（14 步）没有正式执行，而是直接进入实现。虽然结果完整，但缺少了 Step 0.5（冲突扫描）和 Step 2.5（缺口分析）的正式记录
- **未产出吸收报告**（本文件即为此补写）：和其他右花（OpenHuman、FangLab）不同，SiYuan 没有即时产出集成报告
- **embedding 未重算**：343 条迁移时的 `EntryToDoc` 用的是硬编码单块，Lute 集成后 `EntryToDoc` 已升级为多块解析，但磁盘上的 `.sy` 文件 embedding 仍是基于旧摘要计算的，需要触发 `knowledge_reindex` 刷新

---

## 五、验证状态

| 验证项 | 状态 |
|--------|:----:|
| `go build ./...` | ✅ |
| `go test ./...` 22 packages | ✅ |
| `integration_check.sh` | ✅ |
| L0 检索基线 | 8/10 ✅ |
| Lute 解析测试 | 7/7 ✅ |
| 块存储 343 条迁移 | ✅ |
| 反向链接自动更新 | ✅（单元测试） |
