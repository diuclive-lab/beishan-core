package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ─── StorageAdapter：多格式存储抽象 ──────────────

   知识库支持两种存储后端：
     JSONStorage   — 现有 kn_xxx.json 格式（只读兼容）
     BlockStorage  — 新块级格式（notebooks/ 目录）

   上层代码通过 StorageAdapter 接口访问，不感知底层格式。
   迁移完成后删除 JSONStorage 实现，只保留 BlockStorage。
*/

// StorageAdapter 统一知识库存储接口。
type StorageAdapter interface {
	// GetEntry 按 ID 获取单条知识。
	GetEntry(id string) *KnowledgeEntry

	// SearchEntries 按查询条件搜索条目。
	// query 是关键词，limit 是返回上限。
	SearchEntries(query string, limit int) []*KnowledgeEntry

	// SaveEntry 保存或更新一条知识。
	SaveEntry(entry *KnowledgeEntry) error

	// DeleteEntry 删除一条知识。
	DeleteEntry(id string) error

	// AllEntries 返回所有条目（用于全量搜索/reindex）。
	AllEntries() []*KnowledgeEntry

	// Count 返回条目总数。
	Count() int

	// Close 清理资源（关闭文件句柄等）。
	Close()
}

// ─── JSONStorage：现有 kn_xxx.json 格式（只读兼容） ──

type JSONStorage struct {
	dir  string
	mu   sync.RWMutex
	cache []*KnowledgeEntry // 懒加载缓存
}

func NewJSONStorage(dir string) *JSONStorage {
	return &JSONStorage{dir: dir}
}

func (s *JSONStorage) GetEntry(id string) *KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return loadKnowledge(id)
}

func (s *JSONStorage) SearchEntries(query string, limit int) []*KnowledgeEntry {
	results := SearchMemory(query, limit, nil)
	entries := make([]*KnowledgeEntry, len(results))
	for i, r := range results {
		entries[i] = loadKnowledge(r.ID)
	}
	return entries
}

func (s *JSONStorage) SaveEntry(entry *KnowledgeEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	saveKnowledge(entry)
	return nil
}

func (s *JSONStorage) DeleteEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(knowledgePath(id))
}

func (s *JSONStorage) AllEntries() []*KnowledgeEntry {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var result []*KnowledgeEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		entry := loadKnowledge(strings.TrimSuffix(e.Name(), ".json"))
		if entry != nil {
			result = append(result, entry)
		}
	}
	return result
}

func (s *JSONStorage) Count() int {
	return len(s.AllEntries())
}

func (s *JSONStorage) Close() {}

// ─── Block：块级文档的基本单元 ─────────────────

// BlockType 块类型（参考 SiYuan 但简化）
type BlockType string

const (
	BlockDocument  BlockType = "document"
	BlockHeading   BlockType = "heading"
	BlockParagraph BlockType = "paragraph"
	BlockList      BlockType = "list"
	BlockListItem  BlockType = "list_item"
	BlockCode      BlockType = "code"
	BlockQuote     BlockType = "blockquote"
	BlockImage     BlockType = "image"
	BlockTable     BlockType = "table"
)

// Block 文档树中的一个内容块。
type Block struct {
	ID        string      `json:"id"`
	Type      BlockType   `json:"type"`
	Content   string      `json:"content"`            // 纯文本内容
	Markdown  string      `json:"markdown,omitempty"` // markdown 原文（段落级）
	Children  []*Block    `json:"children,omitempty"`
	Depth     int         `json:"depth,omitempty"`    // 标题层级（heading 有效）
	CreatedAt int64       `json:"created_at"`
	UpdatedAt int64       `json:"updated_at"`
}

// Document 一篇块级文档（对应一个 .sy 文件）。
type Document struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Tags       []string            `json:"tags"`
	Properties map[string]string   `json:"properties,omitempty"` // 自定义属性（source_type / content_type 等）
	Blocks     []*Block            `json:"blocks"`               // 顶层块列表
	Refs       []string            `json:"refs,omitempty"`        // [[wikilink]] 出链
	Backlinks  []string            `json:"backlinks,omitempty"`   // 入链（自动维护）
	Embedding  []float64           `json:"embedding,omitempty"`   // 768 维向量（文档级）
	CreatedAt  int64               `json:"created_at"`
	UpdatedAt  int64               `json:"updated_at"`
}

// ─── BlockStorage：新块级格式 ─────────────────

type BlockStorage struct {
	dir    string // notebooks/ 目录
	mu     sync.RWMutex
	index  map[string]*Document // ID → 文档（内存索引）
	dirty  bool
}

func NewBlockStorage(dir string) *BlockStorage {
	s := &BlockStorage{
		dir:   dir,
		index: make(map[string]*Document),
	}
	s.loadIndex()
	return s
}

func (s *BlockStorage) docPath(id string) string {
	return filepath.Join(s.dir, id+".sy")
}

func (s *BlockStorage) loadIndex() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sy") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var doc Document
		if json.Unmarshal(data, &doc) == nil && doc.ID != "" {
			s.index[doc.ID] = &doc
		}
	}
}

func (s *BlockStorage) saveDoc(doc *Document) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.docPath(doc.ID), data, 0644)
}

// collectBlockContents 提取文档中所有块的纯文本内容（用于检索匹配）。
func collectBlockContents(blocks []*Block) []string {
	var contents []string
	for _, b := range blocks {
		if b.Content != "" {
			contents = append(contents, b.Content)
		}
		if len(b.Children) > 0 {
			contents = append(contents, collectBlockContents(b.Children)...)
		}
	}
	return contents
}

// DocToEntry 将 Document 转换为 KnowledgeEntry（供上层检索管道使用）。
func DocToEntry(doc *Document) *KnowledgeEntry {
	summary := ""
	if len(doc.Blocks) > 0 {
		var parts []string
		for i, b := range doc.Blocks {
			if i >= 3 {
				break
			}
			if b.Content != "" {
				parts = append(parts, b.Content)
			}
		}
		summary = strings.Join(parts, " | ")
		if len(summary) > 300 {
			summary = summary[:300]
		}
	}

	entry := &KnowledgeEntry{
		ID:       doc.ID,
		Title:    doc.Title,
		Summary:  summary,
		Tags:     doc.Tags,
		Embedding: doc.Embedding,
		BlockContents: collectBlockContents(doc.Blocks),
	}
	if st, ok := doc.Properties["source_type"]; ok {
		entry.SourceType = st
	}
	if ct, ok := doc.Properties["content_type"]; ok {
		entry.ContentType = ct
	}
	return entry
}

// EntryToDoc 将 KnowledgeEntry 转换为 Document（迁移用）。
func EntryToDoc(entry *KnowledgeEntry) *Document {
	doc := &Document{
		ID:        entry.ID,
		Title:     entry.Title,
		Tags:      entry.Tags,
		CreatedAt: entry.CreatedAt,
		UpdatedAt: entry.CreatedAt,
		Properties: map[string]string{
			"source_type": entry.SourceType,
		},
		Blocks: []*Block{
			{
				ID:        entry.ID + "_b1",
				Type:      BlockParagraph,
				Content:   entry.Summary,
				CreatedAt: entry.CreatedAt,
				UpdatedAt: entry.CreatedAt,
			},
		},
		Embedding: entry.Embedding,
	}
	if doc.Properties == nil {
		doc.Properties = map[string]string{}
	}
	doc.Properties["content_type"] = entry.ContentType
	doc.Properties["imported_at"] = fmt.Sprintf("%d", time.Now().Unix())

	return doc
}

// ─── StorageAdapter 实现 ───────────────────────

func (s *BlockStorage) GetEntry(id string) *KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.index[id]
	if !ok {
		return nil
	}
	return DocToEntry(doc)
}

func (s *BlockStorage) SearchEntries(query string, limit int) []*KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		doc   *Document
		score int
	}
	var results []scored
	q := strings.ToLower(query)

	for _, doc := range s.index {
		score := 0
		title := strings.ToLower(doc.Title)
		if strings.Contains(title, q) {
			score += 3
		}
		for _, b := range doc.Blocks {
			content := strings.ToLower(b.Content)
			if strings.Contains(content, q) {
				score += 2
			}
		}
		for _, tag := range doc.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				score += 1
				break
			}
		}
		if score > 0 {
			results = append(results, scored{doc, score})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) > limit {
		results = results[:limit]
	}

	entries := make([]*KnowledgeEntry, len(results))
	for i, r := range results {
		entries[i] = DocToEntry(r.doc)
	}
	return entries
}

func (s *BlockStorage) SaveEntry(entry *KnowledgeEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.index[entry.ID]
	if ok {
		// 更新已有文档
		doc.Title = entry.Title
		doc.UpdatedAt = time.Now().Unix()
		if entry.Embedding != nil {
			doc.Embedding = entry.Embedding
		}
	} else {
		// 新建文档
		doc = EntryToDoc(entry)
	}
	s.index[entry.ID] = doc
	if err := s.saveDoc(doc); err != nil {
		return err
	}

	// 自动更新反向链接
	docs, _ := loadDocIndex(s.dir)
	if current, ok := docs[entry.ID]; ok {
		UpdateBacklinks(current, docs)
		for _, linked := range docs {
			if linked.ID == entry.ID {
				continue
			}
			if len(linked.Backlinks) > 0 || len(linked.Refs) > 0 {
				data, _ := json.MarshalIndent(linked, "", "  ")
				os.WriteFile(s.docPath(linked.ID), data, 0644)
			}
		}
		s.saveDoc(current)
		s.index = make(map[string]*Document)
		for id, d := range docs {
			s.index[id] = d
		}
	}

	return nil
}

func (s *BlockStorage) DeleteEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.index, id)
	return os.Remove(s.docPath(id))
}

func (s *BlockStorage) AllEntries() []*KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]*KnowledgeEntry, 0, len(s.index))
	for _, doc := range s.index {
		entries = append(entries, DocToEntry(doc))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

func (s *BlockStorage) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

func (s *BlockStorage) Close() {}

// ─── 全局存储实例 ─────────────────────────────

var (
	currentStorage StorageAdapter
	storageMu      sync.Mutex
)

// InitStorage 初始化存储系统。优先使用 BlockStorage，若 notebooks/ 不存在则回退 JSONStorage。
func InitStorage() error {
	storageMu.Lock()
	defer storageMu.Unlock()

	if currentStorage != nil {
		currentStorage.Close()
	}

	// 尝试初始块级存储
	blockDir := filepath.Join(knowledgeDir, "..", "notebooks")
	blockDir, _ = filepath.Abs(blockDir)

	if _, err := os.Stat(blockDir); err == nil {
		currentStorage = NewBlockStorage(blockDir)
		return nil
	}

	// 回退 JSON 存储
	os.MkdirAll(knowledgeDir, 0755)
	currentStorage = NewJSONStorage(knowledgeDir)
	return nil
}

// UseBlockStorage 切换到块级存储（即使 notebooks/ 不存在也会创建）。
func UseBlockStorage() error {
	storageMu.Lock()
	defer storageMu.Unlock()

	if currentStorage != nil {
		currentStorage.Close()
	}

	blockDir := filepath.Join(knowledgeDir, "..", "notebooks")
	blockDir, _ = filepath.Abs(blockDir)
	os.MkdirAll(blockDir, 0755)
	currentStorage = NewBlockStorage(blockDir)
	return nil
}

// Storage 返回当前存储实例。首次调用时自动初始化。
func Storage() StorageAdapter {
	storageMu.Lock()
	defer storageMu.Unlock()
	if currentStorage == nil {
		// 确保 knowledgeDir 已初始化
		initKnowledgeDir()
		// 尝试初始块级存储
		blockDir := filepath.Join(knowledgeDir, "..", "notebooks")
		blockDir, _ = filepath.Abs(blockDir)
		if _, err := os.Stat(blockDir); err == nil {
			currentStorage = NewBlockStorage(blockDir)
		} else {
			// 回退 JSON 存储
			os.MkdirAll(knowledgeDir, 0755)
			currentStorage = NewJSONStorage(knowledgeDir)
		}
	}
	return currentStorage
}
