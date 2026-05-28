package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempKnowledge points the package's knowledge storage at a fresh temp dir
// for the duration of the test, then fully restores global storage state —
// knowledgeDir AND the lazily-cached currentStorage — on cleanup, so no storage
// state leaks into later tests. Storage() caches currentStorage permanently with
// no reset path, so restoring knowledgeDir alone is not enough. Returns the
// notebooks dir where .sy docs should be written.
func withTempKnowledge(t *testing.T) (notebooksDir string) {
	t.Helper()
	oldKD := knowledgeDir
	storageMu.Lock()
	oldStorage := currentStorage
	currentStorage = nil
	storageMu.Unlock()

	dir := t.TempDir()
	knowledgeDir = filepath.Join(dir, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)
	notebooksDir = filepath.Join(dir, "notebooks")
	os.MkdirAll(notebooksDir, 0755)

	t.Cleanup(func() {
		knowledgeDir = oldKD
		storageMu.Lock()
		if currentStorage != nil && currentStorage != oldStorage {
			currentStorage.Close()
		}
		currentStorage = oldStorage
		storageMu.Unlock()
	})
	return notebooksDir
}

func TestBlockStorage_WriteRead(t *testing.T) {
	dir := t.TempDir()
	s := NewBlockStorage(dir)

	doc := &Document{
		ID:    "test_doc_001",
		Title: "测试文档",
		Tags:  []string{"test", "example"},
		Properties: map[string]string{
			"source_type": "test",
		},
		Blocks: []*Block{
			{ID: "b1", Type: BlockHeading, Content: "一级标题", Depth: 1},
			{ID: "b2", Type: BlockParagraph, Content: "这是一段测试内容"},
		},
		Embedding: []float64{0.1, 0.2, 0.3},
	}

	// 写入
	if err := s.saveDoc(doc); err != nil {
		t.Fatalf("saveDoc failed: %v", err)
	}

	// 验证文件存在
	path := s.docPath("test_doc_001")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal(".sy file not created")
	}

	// 重建索引后读取
	s2 := NewBlockStorage(dir)
	got := s2.GetEntry("test_doc_001")
	if got == nil {
		t.Fatal("GetEntry returned nil")
	}
	if got.Title != "测试文档" {
		t.Errorf("Title = %q, want %q", got.Title, "测试文档")
	}
	if len(got.Embedding) != 3 {
		t.Errorf("Embedding length = %d, want 3", len(got.Embedding))
	}
}

func TestBlockStorage_SearchEntries(t *testing.T) {
	dir := t.TempDir()
	s := NewBlockStorage(dir)

	docs := []*Document{
		{
			ID: "doc_1", Title: "Go 语言并发编程",
			Tags: []string{"go", "concurrency"},
			Blocks: []*Block{
				{ID: "b1", Type: BlockParagraph, Content: "goroutine 是 Go 的轻量级线程"},
			},
			Embedding: []float64{0.1, 0.2},
		},
		{
			ID: "doc_2", Title: "Python 异步编程",
			Tags: []string{"python", "async"},
			Blocks: []*Block{
				{ID: "b2", Type: BlockParagraph, Content: "asyncio 是 Python 的异步框架"},
			},
			Embedding: []float64{0.3, 0.4},
		},
	}

	for _, doc := range docs {
		s.saveDoc(doc)
		s.index[doc.ID] = doc
	}

	// 搜索 "goroutine" → 应返回 doc_1
	results := s.SearchEntries("goroutine", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'goroutine'")
	}
	if results[0].ID != "doc_1" {
		t.Errorf("top result = %s, want doc_1", results[0].ID)
	}

	// 搜索 "Python" → 应返回 doc_2
	results = s.SearchEntries("Python", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'Python'")
	}
	if results[0].ID != "doc_2" {
		t.Errorf("top result = %s, want doc_2", results[0].ID)
	}
}

func TestDocToEntry_RoundTrip(t *testing.T) {
	entry := &KnowledgeEntry{
		ID:          "kn_test",
		Title:       "测试条目",
		Summary:     "这是一条测试知识",
		SourceType:  "note",
		ContentType: "fact",
		Tags:        []string{"test"},
		Embedding:   []float64{0.1, 0.2, 0.3},
	}

	doc := EntryToDoc(entry)
	if doc.ID != "kn_test" {
		t.Errorf("doc.ID = %s, want kn_test", doc.ID)
	}
	if len(doc.Blocks) == 0 {
		t.Fatal("doc has no blocks")
	}
	if doc.Blocks[0].Content != "这是一条测试知识" {
		t.Errorf("block content = %s, want 这是一条测试知识", doc.Blocks[0].Content)
	}

	got := DocToEntry(doc)
	if got.Title != "测试条目" {
		t.Errorf("round trip title = %s", got.Title)
	}
	if len(got.Embedding) != 3 {
		t.Errorf("round trip embedding lost")
	}
}
