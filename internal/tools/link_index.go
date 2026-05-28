package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

/* ─── LinkIndex：自动双向链接索引 ──────────────

   两种链接语法：
     [[页面标题]]          → wikilink，指向另一篇文档
     ((block-id))          → 块引用，指向另一篇文档中的特定块

   写入文档时自动扫描，更新目标文档的 Backlinks 列表。
   与 typed_links（手动标记的矛盾/支持/替代关系）共存，互不覆盖。
*/

// wikilinkRe 匹配 [[页面标题]] 或 [[页面标题|显示文本]]
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]]+?)(?:\|([^\[\]]*))?\]\]`)

// blockRefRe 匹配 ((block-id))
var blockRefRe = regexp.MustCompile(`\(\(([a-zA-Z0-9_-]+)\)\)`)

// AutoLink 自动提取的链接（区别于 typed_links 的手动标记）。
type AutoLink struct {
	TargetID string `json:"target_id"` // 目标条目 ID
	SourceID string `json:"source_id"` // 来源条目 ID
	LinkText string `json:"link_text"` // 链接显示文本
	BlockID  string `json:"block_id"`  // 链接所在的块 ID
}

// LinkIndex 管理文档间的自动链接索引。
type LinkIndex struct {
	mu   sync.RWMutex
	dir  string // notebooks/ 目录
}

// NewLinkIndex 创建链接索引。
func NewLinkIndex(notebooksDir string) *LinkIndex {
	return &LinkIndex{dir: notebooksDir}
}

// ExtractWikilinks 从文本中提取 [[wikilink]] 引用。
// 返回引用文本列表（不含语法标记）。
func ExtractWikilinks(text string) []string {
	matches := wikilinkRe.FindAllStringSubmatch(text, -1)
	var refs []string
	seen := map[string]bool{}
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		refs = append(refs, target)
	}
	return refs
}

// ExtractBlockRefs 从文本中提取 ((block-id)) 引用。
func ExtractBlockRefs(text string) []string {
	matches := blockRefRe.FindAllStringSubmatch(text, -1)
	var refs []string
	seen := map[string]bool{}
	for _, m := range matches {
		id := strings.TrimSpace(m[1])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		refs = append(refs, id)
	}
	return refs
}

// ResolveWikilink 根据引用文本在 notebooks/ 中查找匹配的文档。
// 按 title 精确匹配，找不到则返回空。
func ResolveWikilink(linkText string, docs map[string]*Document) string {
	lower := strings.ToLower(linkText)
	for id, doc := range docs {
		if strings.ToLower(doc.Title) == lower {
			return id
		}
	}
	// 第二次遍历：前缀匹配（标题以 linkText 开头）
	for id, doc := range docs {
		if strings.HasPrefix(strings.ToLower(doc.Title), lower) {
			return id
		}
	}
	return ""
}

// UpdateBacklinks 扫描文档中的所有块，提取 wikilink 和 block-ref，
// 更新目标文档的 Backlinks 列表。
// 需要在文档保存后调用。
func UpdateBacklinks(doc *Document, docs map[string]*Document) []AutoLink {
	var links []AutoLink
	seenRefs := map[string]bool{}

	var scanBlocks func(blocks []*Block, docID string)
	scanBlocks = func(blocks []*Block, docID string) {
		for _, b := range blocks {
			text := b.Content + " " + b.Markdown

			// 提取 [[wikilink]]
			for _, ref := range ExtractWikilinks(text) {
				targetID := ResolveWikilink(ref, docs)
				if targetID == "" || targetID == docID || seenRefs[targetID] {
					continue
				}
				seenRefs[targetID] = true
				links = append(links, AutoLink{
					TargetID: targetID,
					SourceID: docID,
					LinkText: ref,
					BlockID:  b.ID,
				})
			}

			// 提取 ((block-ref))
			for _, ref := range ExtractBlockRefs(text) {
				if _, exists := docs[ref]; !exists || ref == docID || seenRefs[ref] {
					continue
				}
				seenRefs[ref] = true
				title := docs[ref].Title
				links = append(links, AutoLink{
					TargetID: ref,
					SourceID: docID,
					LinkText: title,
					BlockID:  b.ID,
				})
			}

			if len(b.Children) > 0 {
				scanBlocks(b.Children, docID)
			}
		}
	}
	scanBlocks(doc.Blocks, doc.ID)

	// 更新目标文档的 Backlinks
	for _, link := range links {
		if target, ok := docs[link.TargetID]; ok {
			// 检查是否已有这个 backlink
			alreadyLinked := false
			for _, bl := range target.Backlinks {
				if bl == link.SourceID {
					alreadyLinked = true
					break
				}
			}
			if !alreadyLinked {
				target.Backlinks = append(target.Backlinks, link.SourceID)
			}
		}
	}

	return links
}

// SyncAllBacklinks 全量重建所有文档的反向链接索引。
// 在迁移后或手动修复时调用。
func SyncAllBacklinks(notebooksDir string) (int, error) {
	// 加载所有文档到内存
	entries, err := os.ReadDir(notebooksDir)
	if err != nil {
		return 0, fmt.Errorf("读取 notebooks 目录失败: %w", err)
	}

	docs := make(map[string]*Document)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sy") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(notebooksDir, e.Name()))
		if err != nil {
			continue
		}
		var doc Document
		if json.Unmarshal(data, &doc) == nil && doc.ID != "" {
			// 清除现有 backlinks（全量重建）
			doc.Backlinks = nil
			doc.Refs = nil
			docs[doc.ID] = &doc
		}
	}

	// 第一遍：收集所有出链
	linkCount := 0
	for _, doc := range docs {
		links := UpdateBacklinks(doc, docs)
		doc.Refs = nil
		for _, link := range links {
			doc.Refs = append(doc.Refs, link.TargetID)
		}
		linkCount += len(links)
	}

	// 第二遍：将更新后的文档写回磁盘
	for _, doc := range docs {
		data, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(filepath.Join(notebooksDir, doc.ID+".sy"), data, 0644); err != nil {
			log.Printf("[link_index] 写入 %s 失败: %v", doc.ID, err)
		}
	}

	return linkCount, nil
}
