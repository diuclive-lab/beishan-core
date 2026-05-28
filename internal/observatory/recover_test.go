package observatory

import (
	"sync"
	"testing"
	"time"
)

// SafeGo must trap a panic in fn so the goroutine exits instead of crashing the
// process. If it didn't, the panic would escape and abort the test binary.
func TestSafeGo_recoversPanic(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	SafeGo("test.safego", func() {
		defer wg.Done()
		panic("boom")
	})
	wg.Wait()
}

// RecoverWith must run its cleanup callback with the recovered value on panic.
func TestRecoverWith_runsCleanupWithValue(t *testing.T) {
	var got interface{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer RecoverWith("test.recoverwith", func(r interface{}) { got = r })
		panic("kaboom")
	}()
	<-done
	if got != "kaboom" {
		t.Fatalf("cleanup got %v, want kaboom", got)
	}
}

// The contract-preserving pattern used at the parallel-worker call sites: a
// deferred completion signal registered BEFORE RecoverWith must still fire after
// a panic, so a waiting parent never deadlocks.
func TestRecoverWith_preservesCompletionContract(t *testing.T) {
	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{} }()                    // registered first -> runs last
		defer RecoverWith("test.contract", func(interface{}) {}) // runs first on panic
		panic("worker exploded")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: done was never signaled after a recovered panic")
	}
}

// No panic means cleanup must not run and execution continues normally.
func TestRecoverWith_noPanicIsNoop(t *testing.T) {
	cleanupCalled := false
	func() {
		defer RecoverWith("test.nopanic", func(interface{}) { cleanupCalled = true })
	}()
	if cleanupCalled {
		t.Fatal("cleanup ran without a panic")
	}
}
