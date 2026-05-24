package discovery

import (
	"sync"
	"time"
)

// StrategyState manages failover between API and local models.
type StrategyState struct {
	mu              sync.RWMutex
	onAPI           bool
	apiFailCount    int
	localFailCount  int
	lastSwitch      time.Time
	hysteresis      int // consecutive successes needed to switch back
}

// NewStrategyState creates a state machine defaulting to API.
func NewStrategyState() *StrategyState {
	return &StrategyState{onAPI: true, hysteresis: 2}
}

// Decide returns which provider to use: "api" or "local".
// Implements hysteresis: requires 2 consecutive API successes to switch back.
func (s *StrategyState) Decide(apiOK, localOK bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.onAPI {
		if apiOK { s.apiFailCount = 0; return "api" }
		s.apiFailCount++
		if s.apiFailCount >= 1 && localOK {
			s.onAPI = false
			s.lastSwitch = time.Now()
			s.apiFailCount = 0  // reset for hysteresis
			return "local"
		}
		if !localOK { return "api" } // both down, still return api
		return "api"
	}

	// Currently on local model
	if !localOK {
		s.localFailCount++
		if s.localFailCount >= 1 && apiOK {
			s.onAPI = true
			s.lastSwitch = time.Now()
			return "api"
		}
		return "local" // local still the best option
	}

	// Local OK, check if API recovered
	if apiOK {
		s.apiFailCount++
		if s.apiFailCount >= s.hysteresis {
			s.onAPI = true
			s.lastSwitch = time.Now()
			s.localFailCount = 0  // reset for next failover
			return "api"
		}
	}
	return "local"
}

// OnAPI returns true if currently using API.
func (s *StrategyState) OnAPI() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.onAPI }

// LastSwitch returns when the last provider switch occurred.
func (s *StrategyState) LastSwitch() time.Time { s.mu.RLock(); defer s.mu.RUnlock(); return s.lastSwitch }

// Status returns a structured status report.
func (s *StrategyState) Status() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"active":     map[string]bool{"api": s.onAPI, "local": !s.onAPI},
		"fail_count": map[string]int{"api": s.apiFailCount, "local": s.localFailCount},
		"hysteresis": s.hysteresis,
	}
}
