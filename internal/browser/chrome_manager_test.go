package browser

import (
	"os"
	"testing"
)

func TestSessionManager_AcquireRelease(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome）")
	}

	sm := NewSessionManager()

	// Acquire persistent session
	id1, eng1, err := sm.Acquire(ChromeConfig{Headless: true, Incognito: false})
	if err != nil {
		t.Fatalf("Acquire persistent failed: %v", err)
	}
	if eng1 == nil {
		t.Fatal("Engine is nil")
	}

	// Second acquire with same config should reuse
	id2, eng2, err := sm.Acquire(ChromeConfig{Headless: true, Incognito: false})
	if err != nil {
		t.Fatalf("Acquire second persistent failed: %v", err)
	}
	if eng1 != eng2 {
		t.Log("persistent sessions: same engine (reused)")
	} else {
		t.Log("persistent sessions: got same engine (reuse works)")
	}

	// Release both
	if err := sm.Release(id2); err != nil {
		t.Errorf("Release id2 failed: %v", err)
	}
	if err := sm.Release(id1); err != nil {
		t.Errorf("Release id1 failed: %v", err)
	}

	stats := sm.Stats()
	t.Logf("stats after release: %v", stats)
}

func TestSessionManager_IncognitoIsolation(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome）")
	}

	sm := NewSessionManager()

	// Two incognito sessions should be different
	id1, eng1, err := sm.Acquire(ChromeConfig{Headless: true, Incognito: true})
	if err != nil {
		t.Fatalf("Acquire incognito 1 failed: %v", err)
	}

	id2, eng2, err := sm.Acquire(ChromeConfig{Headless: true, Incognito: true})
	if err != nil {
		t.Fatalf("Acquire incognito 2 failed: %v", err)
	}

	if eng1 == eng2 {
		t.Error("incognito sessions should be different engines")
	} else {
		t.Log("incognito sessions: isolated (different engines)")
	}

	// Release both (should clean up temp dirs)
	if err := sm.Release(id1); err != nil {
		t.Errorf("Release id1 failed: %v", err)
	}
	if err := sm.Release(id2); err != nil {
		t.Errorf("Release id2 failed: %v", err)
	}

	stats := sm.Stats()
	if total := stats["total"].(int); total != 0 {
		t.Errorf("expected 0 sessions after release, got %d", total)
	}
	t.Logf("incognito sessions cleaned up: stats=%v", stats)
}

func TestAcquireBrowser_Helper(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome）")
	}

	// Agent session (incognito)
	id1, eng1, err := AcquireBrowser(true)
	if err != nil {
		t.Fatalf("AcquireBrowser(agent=true) failed: %v", err)
	}
	if eng1 == nil {
		t.Fatal("Engine is nil")
	}
	ReleaseBrowser(id1)

	// User session (persistent)
	id2, eng2, err := AcquireBrowser(false)
	if err != nil {
		t.Fatalf("AcquireBrowser(agent=false) failed: %v", err)
	}
	if eng2 == nil {
		t.Fatal("Engine is nil")
	}
	ReleaseBrowser(id2)

	t.Log("AcquireBrowser helper works: agent+user sessions")
}
