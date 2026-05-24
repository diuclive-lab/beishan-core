package discovery

import "testing"

func TestDefaultOnAPI(t *testing.T) {
	s := NewStrategyState()
	if !s.OnAPI() { t.Fatal("should default to API") }
}

func TestSwitchToLocalOnAPIDown(t *testing.T) {
	s := NewStrategyState()
	r := s.Decide(false, true) // API down, local OK
	if r != "local" { t.Fatalf("expected local, got %s", r) }
}

func TestSwitchBackWithHysteresis(t *testing.T) {
	s := NewStrategyState()
	s.Decide(false, true) // API down, switch to local

	r := s.Decide(true, true) // API recovered, 1st success
	if r != "local" { t.Fatalf("1st should still use local (hysteresis), got %s", r) }

	r = s.Decide(true, true) // API recovered, 2nd success
	if r != "api" { t.Fatalf("2nd should switch back to API, got %s", r) }
}

func TestBothDownSafe(t *testing.T) {
	s := NewStrategyState()
	r := s.Decide(false, false) // both down
	if r == "" { t.Fatal("should return something, not empty") }
}

func TestLocalDownNoPanic(t *testing.T) {
	s := NewStrategyState()
	s.Decide(false, true) // switch to local
	r := s.Decide(false, false) // local also dies
	if r == "" { t.Fatal("should return something") }
}

func TestStatusStructured(t *testing.T) {
	s := NewStrategyState()
	st := s.Status()
	if st["active"] == nil { t.Fatal("expected status") }
}
