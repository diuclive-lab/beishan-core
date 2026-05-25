// Package memory 定义记忆存储抽象层。
//
// 设计原则：
// - MemoryStore 接口是余量设计，当前仅实现 fileStore。
// - 预留实现：sqliteStore（OpenClaw 风格结构化记忆）、embeddingStore（向量检索）。
// - 不直接调用，通过 internal/tools 中的 knowledge 工具间接使用。
// - 不依赖 kernel，纯接口 + 文件实现。
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MemoryItem 单条记忆。
type MemoryItem struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Timestamp time.Time `json:"timestamp"`
	TTL       int       `json:"ttl,omitempty"` // 过期秒数，0 表示永久
}

// MemoryStore 记忆存储接口。
//
// 当前实现：FileStore（JSON 文件）
// 预留实现：SQLiteStore / EmbeddingStore
type MemoryStore interface {
	// Get 读取单条记忆。key 不存在时返回 ErrNotFound。
	Get(key string) (*MemoryItem, error)

	// Set 写入单条记忆。
	Set(item MemoryItem) error

	// Delete 删除单条记忆。
	Delete(key string) error

	// Search 按查询词搜索记忆（具体实现决定搜索方式）。
	Search(query string, limit int) ([]MemoryItem, error)

	// List 列出所有记忆 key。
	List() ([]string, error)
}

// ErrNotFound 是 Get 在 key 不存在时返回的错误。
var ErrNotFound = fmt.Errorf("memory: key not found")

// ─── FileStore ──────────────────────────────────────

// FileStore 是基于 JSON 文件的 MemoryStore 实现。
// 每条记忆存为一个独立 JSON 文件，适合少量低频访问。
type FileStore struct {
	dir string
	mu  sync.RWMutex
}

// NewFileStore 创建文件存储，dir 为存储目录。
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) Get(key string) (*MemoryItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, sanitizeKey(key)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var item MemoryItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	// TTL 检查
	if item.TTL > 0 && time.Since(item.Timestamp) > time.Duration(item.TTL)*time.Second {
		os.Remove(path) // 过期删除
		return nil, ErrNotFound
	}
	return &item, nil
}

func (s *FileStore) Set(item MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	if item.Timestamp.IsZero() {
		item.Timestamp = time.Now()
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, sanitizeKey(item.Key)+".json")
	return os.WriteFile(path, data, 0644)
}

func (s *FileStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, sanitizeKey(key)+".json")
	if err := os.Remove(path); err != nil && os.IsNotExist(err) {
		return ErrNotFound
	}
	return nil
}

func (s *FileStore) Search(query string, limit int) ([]MemoryItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []MemoryItem
	for _, e := range entries {
		if len(results) >= limit {
			break
		}
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var item MemoryItem
		if json.Unmarshal(data, &item) != nil {
			continue
		}
		// TTL 检查
		if item.TTL > 0 && time.Since(item.Timestamp) > time.Duration(item.TTL)*time.Second {
			os.Remove(filepath.Join(s.dir, e.Name()))
			continue
		}
		// 关键词匹配（简单实现，后续可按需升级）
		if contains(item.Value, query) || contains(item.Key, query) {
			results = append(results, item)
		}
	}
	return results, nil
}

func (s *FileStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := os.Stat(s.dir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			keys = append(keys, e.Name()[:len(e.Name())-5])
		}
	}
	return keys, nil
}

// sanitizeKey 将 key 转为安全的文件名。
func sanitizeKey(key string) string {
	result := make([]byte, 0, len(key))
	for _, c := range []byte(key) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

func contains(s, sub string) bool {
	return len(sub) > 0 && s != "" && searchFold(s, sub)
}

// searchFold 简单的大小写不敏感包含检测。
func searchFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	lower := []byte(s)
	pattern := []byte(sub)
	for i, b := range lower {
		if b >= 'A' && b <= 'Z' {
			lower[i] = b + 32
		}
	}
	for i, b := range pattern {
		if b >= 'A' && b <= 'Z' {
			pattern[i] = b + 32
		}
	}
	for i := 0; i <= len(lower)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if lower[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
