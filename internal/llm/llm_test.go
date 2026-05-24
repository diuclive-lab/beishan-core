package llm

import (
	"testing"
)

func TestRouterPrompt(t *testing.T) {
	p := RouterPrompt()
	if len(p) == 0 { t.Fatal("expected non-empty prompt") }
}

func TestModelDefault(t *testing.T) {
	m := Model()
	if m == "" { t.Fatal("expected non-empty model") }
}

func TestProviderName(t *testing.T) {
	n := ProviderName()
	if n == "" { t.Fatal("expected non-empty provider") }
}
