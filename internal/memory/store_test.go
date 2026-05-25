package memory

import (
	"testing"
)

func TestFileStoreSetGet(t *testing.T) {
	s := NewFileStore(t.TempDir())
	err := s.Set(MemoryItem{Key: "test-key", Value: "test-value"})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.Get("test-key")
	if err != nil {
		t.Fatal(err)
	}
	if item.Value != "test-value" {
		t.Fatalf("expected test-value, got %s", item.Value)
	}
}

func TestFileStoreNotFound(t *testing.T) {
	s := NewFileStore(t.TempDir())
	_, err := s.Get("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStoreDelete(t *testing.T) {
	s := NewFileStore(t.TempDir())
	s.Set(MemoryItem{Key: "del-key", Value: "to-delete"})
	if err := s.Delete("del-key"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("del-key")
	if err != ErrNotFound {
		t.Fatal("expected ErrNotFound after delete")
	}
}

func TestFileStoreSearch(t *testing.T) {
	s := NewFileStore(t.TempDir())
	s.Set(MemoryItem{Key: "apple", Value: "red fruit"})
	s.Set(MemoryItem{Key: "banana", Value: "yellow fruit"})
	s.Set(MemoryItem{Key: "car", Value: "vehicle"})

	results, err := s.Search("fruit", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	results, err = s.Search("car", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestFileStoreTTL(t *testing.T) {
	s := NewFileStore(t.TempDir())
	// TTL=1 秒
	s.Set(MemoryItem{Key: "ephemeral", Value: "gone soon", TTL: 1})
	item, _ := s.Get("ephemeral")
	if item == nil || item.Value != "gone soon" {
		t.Fatal("should exist before TTL")
	}
}

func TestFileStoreList(t *testing.T) {
	s := NewFileStore(t.TempDir())
	s.Set(MemoryItem{Key: "a", Value: "1"})
	s.Set(MemoryItem{Key: "b", Value: "2"})

	keys, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
}
