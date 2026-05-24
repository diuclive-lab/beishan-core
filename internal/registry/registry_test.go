package registry

import "testing"

func TestPhaseInitToRunning(t *testing.T) {
	r := New()
	if r.phase != PhaseInit { t.Fatalf("expected Init, got %v", r.phase) }
	r.Lock()
	if r.phase != PhaseRunning { t.Fatalf("expected Running, got %v", r.phase) }
}

func TestRegisterAfterLockFails(t *testing.T) {
	r := New()
	r.Lock()
	err := r.Register("test", nil, "desc")
	if err == nil { t.Fatal("expected error after Lock") }
}

func TestGetByName(t *testing.T) {
	r := New()
	r.Register("a", 1, "first")
	r.Lock()
	v, ok := r.Get("a")
	if !ok { t.Fatal("a not found") }
	if v != 1 { t.Fatalf("got %v", v) }
}

func TestNames(t *testing.T) {
	r := New()
	r.Register("b", 1, "")
	r.Register("a", 2, "")
	names := r.Names()
	if len(names) != 2 { t.Fatalf("expected 2 names, got %d", len(names)) }
}
