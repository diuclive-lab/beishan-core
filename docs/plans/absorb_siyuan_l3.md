# SiYuan L3 吸收执行计划

> 参考源：`/Users/dc/Desktop/cankaocangku/siyuan/`
> 吸收目标：块级文档存储 + 树结构内化到 beishan-core 检索管道
> 吸收等级：L3（完整内化）
> 预计工期：5 天
> 基线条件：go build ✅ go test ✅ integration_check ✅

---

## 总体策略

只吸收两个核心设计思想，不做代码级移植：

1. **Block 模型** — 文档=块树，每块有 UUID、类型、父子关系
2. **Defs/Refs 自动反向链接** — 出链/入链自动维护

不吸收：SQLite FTS、Datalog 查询、Electron UI、间隔重复、同步。

---

## 第 1 天：StorageAdapter — 多格式存储抽象

### 目标

在不破坏现有代码的前提下，让知识库支持两种存储后端：

```
knowledge/
  kn_xxx.json       ← 旧格式（只读兼容）
  notebooks/        ← 新格式（块级）
    my_notebook/
      doc_id.sy     ← 每篇文档一个 .sy 文件（JSON 格式）
      doc_id.sy     ← 另一篇
```

### 改动点

**新建 `internal/tools/storage.go`**（~200 行）：

```go
type StorageAdapter interface {
    // 读操作
    GetEntry(id string) *KnowledgeEntry
    SearchEntries(query string, limit int) []SearchResult
    
    // 写操作
    SaveEntry(entry *KnowledgeEntry) error
    DeleteEntry(id string) error
    
    // 迁移
    MigrateFromJSON() (int, error)
}

// 两个实现：
type JSONStorage struct {}    // 现有 kn_xxx.json 格式（只读兼容）
type BlockStorage struct {}   // 新块级格式
```

**影响评估**：
- 不影响现有任何 Plugin/L3 工具
- `loadAllKnowledge()` 改为通过 StorageAdapter 读取
- `saveKnowledge()` 同样通过 Adapter

### 验证标准
- [ ] 343 条旧格式知识可通过 JSONStorage 只读打开
- [ ] 新 BlockStorage 可写入一条测试文档并读回
- [ ] `go test ./internal/tools/` 全部通过

---

## 第 2 天：块级存储格式 + 343 条迁移

### 块文件格式（参考 SiYuan `.sy`，简化版）

```json
// notebooks/my_kb/doc_xxx.sy
{
  "id": "doc_uuid",
  "title": "本地模型方案已放弃",
  "created": "2026-05-20",
  "updated": "2026-05-28",
  "tags": ["decision", "architecture"],
  "properties": {
    "source_type": "claude_memory",
    "content_type": "decision",
    "confidence": "high"
  },
  "blocks": [
    {
      "id": "b_001",
      "type": "heading",
      "content": "结论",
      "depth": 2,
      "children": [
        {
          "id": "b_002", 
          "type": "paragraph",
          "content": "M2 8GB 硬件不足以运行 270m..."
        }
      ]
    }
  ],
  "refs": ["kn_xxx", "kn_yyy"],      // 出链列表
  "backlinks": ["kn_zzz"],           // 入链列表（自动维护）
  "embedding": [0.01, 0.02, ...]     // 768 维（与现有格式一致）
}
```

**关键设计决策**：
- embedding 放文档级（不是每块一个向量），保持与现有 L1 检索兼容
- `refs` 和 `backlinks` 对应 SiYuan 的 `Defs` 和 `Refs`，但简化为一维 ID 列表
- `properties` 代替 SiYuan 的 `IAL`，兼容现有 content_type / source_type

### 迁移脚本（复用 `cmd/knowledge-migrate` 模式）

```bash
# 遍历 343 条 kn_xxx.json
# 对每条：
#   1. 读 title/content/summary → 拆成 heading + paragraph 块
#   2. 提取 tags → 写 properties
#   3. 读 typed_links → 写 refs
#   4. 写新 .sy 文件
#   5. 标记旧 .json 为已迁移
```

### 验证标准
- [ ] 343 条全部迁移完毕，新旧文件一对一映射
- [ ] 迁移后 `knowledge_search` 返回结果与迁移前一致
- [ ] 标题清洗同步完成：343 条中 Q5/Q6 相关条目手动修标题

---

## 第 3 天：检索管道适配块级搜索

### 改动点

**`internal/tools/knowledge.go`** — `searchMemoryFull`：

```go
func searchMemoryFull(query string, limit int, ...) []RetrievalResult {
    // 现有逻辑不变，但 StorageAdapter 返回的条目
    // 如果是 BlockStorage，会被拆成"文档"+"块"两级
    
    // 新增：块级匹配
    // 搜索 query 时不仅匹配文档 title/summary，
    // 还遍历文档内的 blocks，匹配 block.content
    // 匹配到的块在结果中加偏移标记
}
```

**效果**：
- 检索"本地模型 放弃" → 不仅匹配文档标题，还匹配结论块内容
- 检索结果可精确到块级别（比如返回"第 2 段"的引用）
- [[wikilink]] 语法解析器：提取 `[[标题]]` 和 `((块 ID))`

### 验证标准
- [ ] 搜索关键词匹配到块内容时，结果包含块级别信息
- [ ] 10 题检索测试结果不退化（≥5/10）
- [ ] `go test ./internal/tools/` 全部通过

---

## 第 4 天：Defs/Refs 自动反向链接

### 改动点

**新建 `internal/tools/link_index.go`**（~150 行）：

```go
// 扫描文档时自动注册：
//   - [[Wikilink]] → refs 列表
//   - ((block-ref)) → refs 列表  
//   - 写入时自动更新目标文档的 backlinks

// 与现有 typed_links 的关系：
//   typed_links 保留（手动标记的矛盾/支持/替代关系）
//   新增 auto_links（自动从 wiki 语法提取）
//   两者互不覆盖
```

### 验证标准
- [ ] 新建文档包含 `[[目标页面]]` 后，目标页面 backlinks 自动更新
- [ ] `typed_links` 中原有的 `contradicts/supports/supersedes` 不受影响
- [ ] 反向链接在 API 中可见

---

## 第 5 天：端到端验证 + 文档

### 验证清单

| 检查项 | 命令 | 预期 |
|--------|------|------|
| 编译 | `go build ./...` | ✅ |
| 测试 | `go test ./...` | 22 packages ✅ |
| 集成 | `bash scripts/integration_check.sh` | ✅ |
| 检索 | 10 题测试 | L0 ≥ 7/10（标题清洗后预期） |
| 迁移完整性 | 新旧 343 条一对一 | 无遗漏 |
| 反向链接 | 新建 wikilink 后检查目标 | backlinks 自动更新 |
| 块级搜索 | 搜索匹配段落 | 结果含块偏移 |

### 需更新文档

- `CLAUDE.md` — 更新知识库架构段（新增块级存储）
- `docs/DATA_FLOW.md` — 新增路径 S（块级搜索）
- `docs/KNOWN_LIMITATIONS.md` — 新增限制（如适用）

---

## 风险与缓解

| 风险 | 可能性 | 影响 | 缓解 |
|------|:------:|:----:|------|
| 迁移 343 条时 embedding 丢失 | 中 | 高 | 迁移脚本验证 `entry.Embedding` 字段完整性 |
| .sy 格式与上下游工具不兼容 | 低 | 中 | StorageAdapter 保持 `.json` 只读兼容 |
| 标题清洗时误伤内容 | 低 | 低 | 每条手动 review，不做批量替换 |
| 检索管道修改引入回归 | 中 | 高 | 10 题检索测试 + go test 全量回归 |
