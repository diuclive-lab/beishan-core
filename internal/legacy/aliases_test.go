package legacy

import "testing"

func TestResolveKnown(t *testing.T) {
	tests := []struct{ input, want string }{
		{"memory.search", "openhuman.memory_recall_memories"},
		{"memory.store", "openhuman.memory_doc_put"},
		{"context.retrieve", "openhuman.memory_context_query"},
		{"code.review", "openhuman.agent_chat"},
		{"ping", "core.ping"},
	}
	for _, tc := range tests {
		got := Resolve(tc.input)
		if got != tc.want {
			t.Fatalf("Resolve(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveUnknown(t *testing.T) {
	got := Resolve("unknown.method")
	if got != "unknown.method" {
		t.Fatalf("expected original name, got %q", got)
	}
}

func TestResolveCaseInsensitive(t *testing.T) {
	got := Resolve("Memory.Search")
	if got != "openhuman.memory_recall_memories" {
		t.Fatalf("case insensitive failed: %q", got)
	}
}

func TestRegister(t *testing.T) {
	Register("old.method", "new.method")
	got := Resolve("old.method")
	if got != "new.method" {
		t.Fatalf("expected new.method, got %q", got)
	}
}
