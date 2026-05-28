package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxGraphDepth = 6
	graphBaseSize = 10.0
	graphMaxNodes = 200
)

// BuildLocalGraph 以 targetID 为中心构建局部图谱，利用 notebooks/*.sy 的 refs/backlinks。
func BuildLocalGraph(targetID string, depth int) (nodes []GraphNode, links []GraphEdge, err error) {
	if depth <= 0 || depth > maxGraphDepth {
		depth = 2
	}
	nd := filepath.Join(knowledgeDir, "..", "notebooks")
	nd, _ = filepath.Abs(nd)
	docs := blockDocMapForDir(nd)
	if docs == nil {
		var err error
		docs, err = loadDocIndex(nd)
		if err != nil {
			return nil, nil, fmt.Errorf("加载索引失败: %w", err)
		}
	}
	if len(docs) == 0 {
		return nil, nil, fmt.Errorf("块存储不存在或为空")
	}

	visited := map[string]int{}
	var collect func(id string, d int)
	collect = func(id string, d int) {
		if d > depth || len(visited) >= graphMaxNodes {
			return
		}
		if _, ok := visited[id]; ok {
			return
		}
		visited[id] = d
		doc, ok := docs[id]
		if !ok {
			return
		}
		for _, refID := range doc.Refs {
			if _, seen := visited[refID]; !seen {
				links = append(links, GraphEdge{Source: id, Target: refID, Relation: "ref"})
				if d < depth {
					collect(refID, d+1)
				}
			}
		}
		for _, blID := range doc.Backlinks {
			if _, seen := visited[blID]; !seen {
				links = append(links, GraphEdge{Source: blID, Target: id, Relation: "ref"})
				if d < depth {
					collect(blID, d+1)
				}
			}
		}
	}
	collect(targetID, 0)

	for id := range visited {
		doc := docs[id]
		n := GraphNode{ID: id, Title: doc.Title, Tags: doc.Tags}
		for _, l := range links {
			if l.Target == id {
				n.Refs++
			}
			if l.Source == id {
				n.Defs++
			}
		}
		n.Size = math.Log2(float64(n.Refs+n.Defs+1)) * graphBaseSize
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Refs+nodes[i].Defs > nodes[j].Refs+nodes[j].Defs
	})
	return
}

// BuildGlobalGraph 构建全库图谱，minRefs 过滤低引用节点。
func BuildGlobalGraph(minRefs int) (nodes []GraphNode, links []GraphEdge, err error) {
	nd := filepath.Join(knowledgeDir, "..", "notebooks")
	nd, _ = filepath.Abs(nd)
	docs := blockDocMapForDir(nd)
	if docs == nil {
		var e error
		docs, e = loadDocIndex(nd)
		if e != nil {
			return nil, nil, fmt.Errorf("加载索引失败: %w", e)
		}
	}
	if len(docs) == 0 {
		return nil, nil, fmt.Errorf("块存储不存在或为空")
	}

	refCount := map[string]int{}
	defCount := map[string]int{}
	seen := map[string]bool{}

	for id, doc := range docs {
		for _, refID := range doc.Refs {
			if _, ok := docs[refID]; !ok {
				continue
			}
			k := id + "→" + refID
			if seen[k] {
				continue
			}
			seen[k] = true
			defCount[id]++
			refCount[refID]++
			if minRefs <= 0 || refCount[refID] >= minRefs || defCount[id] >= minRefs {
				links = append(links, GraphEdge{Source: id, Target: refID, Relation: "ref"})
			}
		}
		for _, blID := range doc.Backlinks {
			if _, ok := docs[blID]; !ok {
				continue
			}
			seen[blID+"→"+id] = true
		}
	}

	for id, doc := range docs {
		rc, dc := refCount[id], defCount[id]
		if minRefs > 0 && rc+dc < minRefs {
			continue
		}
		nodes = append(nodes, GraphNode{
			ID: id, Title: doc.Title, Tags: doc.Tags,
			Refs: rc, Defs: dc,
			Size: math.Log2(float64(rc+dc+1)) * graphBaseSize,
		})
		if len(nodes) >= graphMaxNodes {
			break
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Refs+nodes[i].Defs > nodes[j].Refs+nodes[j].Defs
	})
	return
}

// loadDocIndex 加载 notebooks/ 目录到内存索引。错误向上传递，不吞。
func loadDocIndex(nd string) (map[string]*Document, error) {
	docs := make(map[string]*Document)
	entries, err := os.ReadDir(nd)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sy") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(nd, e.Name()))
		if err != nil {
			continue
		}
		var doc Document
		if json.Unmarshal(data, &doc) == nil && doc.ID != "" {
			docs[doc.ID] = &doc
		}
	}
	return docs, nil
}
