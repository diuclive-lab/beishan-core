package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildLocalGraph(t *testing.T) {
	oldKD := knowledgeDir
	t.Cleanup(func() { knowledgeDir = oldKD })

	dir := t.TempDir()
	knowledgeDir = filepath.Join(dir, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)
	nd := filepath.Join(dir, "notebooks")
	os.MkdirAll(nd, 0755)

	docs := []*Document{
		{ID: "doc_a", Title: "文档 A", Refs: []string{"doc_b"}},
		{ID: "doc_b", Title: "文档 B", Refs: []string{"doc_c"}, Backlinks: []string{"doc_a"}},
		{ID: "doc_c", Title: "文档 C", Backlinks: []string{"doc_b"}},
	}
	for _, d := range docs {
		data, _ := json.MarshalIndent(d, "", "  ")
		os.WriteFile(filepath.Join(nd, d.ID+".sy"), data, 0644)
	}

	nodes, links, err := BuildLocalGraph("doc_a", 2)
	if err != nil {
		t.Fatalf("BuildLocalGraph failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if len(links) == 0 {
		t.Fatal("expected links")
	}
	t.Logf("nodes=%d links=%d top=%s", len(nodes), len(links), nodes[0].ID)
}

func TestBuildGlobalGraph(t *testing.T) {
	oldKD := knowledgeDir
	t.Cleanup(func() { knowledgeDir = oldKD })

	dir := t.TempDir()
	knowledgeDir = filepath.Join(dir, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)
	nd := filepath.Join(dir, "notebooks")
	os.MkdirAll(nd, 0755)

	docs := []*Document{
		{ID: "d1", Title: "条目一", Refs: []string{"d2"}},
		{ID: "d2", Title: "条目二", Refs: []string{"d3"}},
		{ID: "d3", Title: "条目三"},
	}
	for _, d := range docs {
		data, _ := json.MarshalIndent(d, "", "  ")
		os.WriteFile(filepath.Join(nd, d.ID+".sy"), data, 0644)
	}

	nodes, links, err := BuildGlobalGraph(0)
	if err != nil {
		t.Fatalf("BuildGlobalGraph failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}
}

func TestBuildGlobalGraph_FilterByMinRefs(t *testing.T) {
	oldKD := knowledgeDir
	t.Cleanup(func() { knowledgeDir = oldKD })

	dir := t.TempDir()
	knowledgeDir = filepath.Join(dir, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)
	nd := filepath.Join(dir, "notebooks")
	os.MkdirAll(nd, 0755)

	docs := []*Document{
		{ID: "d1", Title: "中心条目", Refs: []string{"d2", "d3", "d4", "d5"}},
		{ID: "d2", Title: "被引条目"}, {ID: "d3", Title: "被引条目2"},
		{ID: "d4", Title: "被引条目3"}, {ID: "d5", Title: "被引条目4"},
		{ID: "d6", Title: "孤立条目"},
	}
	for _, d := range docs {
		data, _ := json.MarshalIndent(d, "", "  ")
		os.WriteFile(filepath.Join(nd, d.ID+".sy"), data, 0644)
	}

	nodes, _, err := BuildGlobalGraph(2)
	if err != nil {
		t.Fatalf("BuildGlobalGraph failed: %v", err)
	}
	for _, n := range nodes {
		if n.ID == "d6" {
			t.Error("孤立条目 d6 不应出现在 minRefs=2 的结果中")
		}
	}
	t.Logf("minRefs=2 返回 %d 个节点", len(nodes))
}
