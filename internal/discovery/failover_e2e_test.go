package discovery

import (
	"testing"
)

func TestFailoverFullChain(t *testing.T) {
	s := NewStrategyState()
	
	// Step 1: API healthy
	r := s.Decide(true, true)
	if r != "api" { t.Fatalf("step1: expected api, got %s", r) }
	if !s.OnAPI() { t.Fatal("should be on API") }

	// Step 2: API down, switch to local
	r = s.Decide(false, true)
	if r != "local" { t.Fatalf("step2: expected local, got %s", r) }
	if s.OnAPI() { t.Fatal("should have switched to local") }

	// Step 3: Local handles requests
	r = s.Decide(false, true)
	if r != "local" { t.Fatalf("step3: expected local, got %s", r) }

	// Step 4: Local also dies
	r = s.Decide(false, false)
	if r == "" { t.Fatal("step4: should return something, not empty") }
	// Should still be on local (last known working)
	if s.OnAPI() { t.Fatal("should stay on local during double failure") }

	// Step 5: Local recovers
	r = s.Decide(false, true)
	if r != "local" { t.Fatalf("step5: expected local, got %s", r) }

	// Step 6: API recovers (need 2 successes for hysteresis)
	r = s.Decide(true, true)
	if r != "local" { t.Fatalf("step6: 1st success should still use local, got %s", r) }

	r = s.Decide(true, true)
	if r != "api" { t.Fatalf("step7: 2nd success should switch back to api, got %s", r) }
	if !s.OnAPI() { t.Fatal("should be back on API") }

	// Step 8: Verify status
	st := s.Status()
	if st["active"] == nil { t.Fatal("expected status") }
}

func TestNoFlapping(t *testing.T) {
	s := NewStrategyState()
	
	// API flapping: down→up→down→up should not cause repeated switches
	s.Decide(false, true) // down, switch to local
	r := s.Decide(true, true)  // up but 1st success
	if r != "local" { t.Fatalf("should stay local during 1st recovery, got %s", r) }
	
	r = s.Decide(false, true) // down again before hysteresis
	if r == "" { t.Fatal("should handle flapping gracefully") }
}

func TestStatusFields(t *testing.T) {
	s := NewStrategyState()
	s.Decide(false, true) // switch to local
	st := s.Status()
	
	active := st["active"].(map[string]bool)
	if active["api"] { t.Fatal("api should be inactive") }
	if !active["local"] { t.Fatal("local should be active") }
	
	fc := st["fail_count"].(map[string]int)
	if fc["api"] == 0 { t.Log("api_fail_count reset after switch") }
}

func TestLastSwitchTime(t *testing.T) {
	s := NewStrategyState()
	before := s.LastSwitch()
	s.Decide(false, true)
	after := s.LastSwitch()
	if after.Equal(before) { t.Fatal("LastSwitch should update after failover") }
}
