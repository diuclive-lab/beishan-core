package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWikilinks_Simple(t *testing.T) {
	text := "参考 [[笔记软件选型]] 这篇文章"
	links := ExtractWikilinks(text)
	if len(links) != 1 {
		t.Fatalf("期望 1 个链接，实际 %d", len(links))
	}
	if links[0] != "笔记软件选型" {
		t.Errorf("链接文本 = %q，期望 笔记软件选型", links[0])
	}
}

func TestExtractWikilinks_Multiple(t *testing.T) {
	text := "[[MCP Browser]] 和 [[硬化层原则]] 是两篇不同的文章"
	links := ExtractWikilinks(text)
	if len(links) != 2 {
		t.Fatalf("期望 2 个链接，实际 %d: %v", len(links), links)
	}
}

func TestExtractWikilinks_PipeFormat(t *testing.T) {
	text := "参考 [[本地模型|本地模型方案]]"
	links := ExtractWikilinks(text)
	if len(links) != 1 {
		t.Fatalf("期望 1 个链接，实际 %d", len(links))
	}
	if links[0] != "本地模型" {
		t.Errorf("管道格式应取前半部分 = %q", links[0])
	}
}

func TestExtractBlockRefs(t *testing.T) {
	text := "详见 ((b_001)) 和 ((b_002)) 这两个块"
	refs := ExtractBlockRefs(text)
	if len(refs) != 2 {
		t.Fatalf("期望 2 个块引用，实际 %d: %v", len(refs), refs)
	}
	if refs[0] != "b_001" {
		t.Errorf("第一个引用 = %q，期望 b_001", refs[0])
	}
}

func TestResolveWikilink(t *testing.T) {
	docs := map[string]*Document{
		"id1": {ID: "id1", Title: "笔记软件选型：SiYuan vs Logseq"},
		"id2": {ID: "id2", Title: "硬化层原则（代码优先）"},
	}

	// 精确匹配
	id := ResolveWikilink("笔记软件选型：SiYuan vs Logseq", docs)
	if id != "id1" {
		t.Errorf("精确匹配失败: 期望 id1，实际 %q", id)
	}

	// 不存在的链接
	id = ResolveWikilink("不存在的页面", docs)
	if id != "" {
		t.Errorf("不存在时应返回空: %q", id)
	}
}

func TestUpdateBacklinks(t *testing.T) {
	docA := &Document{
		ID:    "doc_a",
		Title: "文档 A",
		Blocks: []*Block{
			{ID: "b1", Type: BlockParagraph, Content: "参考 [[文档 B]] 的内容"},
		},
	}
	docB := &Document{ID: "doc_b", Title: "文档 B"}

	docs := map[string]*Document{
		"doc_a": docA,
		"doc_b": docB,
	}

	links := UpdateBacklinks(docA, docs)
	if len(links) != 1 {
		t.Fatalf("期望 1 个链接，实际 %d", len(links))
	}
	if links[0].TargetID != "doc_b" {
		t.Errorf("目标应为 doc_b，实际 %q", links[0].TargetID)
	}

	// 验证 doc_b 的 backlinks 已更新
	if len(docB.Backlinks) != 1 {
		t.Fatalf("doc_b 应有 1 个 backlink，实际 %d", len(docB.Backlinks))
	}
	if docB.Backlinks[0] != "doc_a" {
		t.Errorf("backlink 应为 doc_a，实际 %q", docB.Backlinks[0])
	}
}

func TestSyncAllBacklinks(t *testing.T) {
	dir := t.TempDir()

	doc1 := &Document{ID: "d1", Title: "文档一",
		Blocks: []*Block{{ID: "b1", Type: BlockParagraph, Content: "参考 [[文档二]]"}}}
	doc2 := &Document{ID: "d2", Title: "文档二"}
	doc1_bs, _ := json.MarshalIndent(doc1, "", "  ")
	doc2_bs, _ := json.MarshalIndent(doc2, "", "  ")
	os.WriteFile(filepath.Join(dir, "d1.sy"), doc1_bs, 0644)
	os.WriteFile(filepath.Join(dir, "d2.sy"), doc2_bs, 0644)

	count, err := SyncAllBacklinks(dir)
	if err != nil {
		t.Fatalf("SyncAllBacklinks 失败: %v", err)
	}
	if count == 0 {
		t.Error("期望有链接被建立")
	}

	// 验证 d2.sy 的 backlinks 已更新
	data, _ := os.ReadFile(filepath.Join(dir, "d2.sy"))
	var doc2Reloaded Document
	if err := json.Unmarshal(data, &doc2Reloaded); err == nil {
		if len(doc2Reloaded.Backlinks) == 0 {
			t.Error("doc2.sy 的 Backlinks 未被持久化")
		}
	}
}
