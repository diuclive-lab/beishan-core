package observatory

import (
	"fmt"
	"log"
	"runtime"
)

// EventPanicRecovered is published whenever Recover/RecoverWith/SafeGo traps a
// panic, so subscribers can alert on recovered crashes.
const EventPanicRecovered = "system.panic_recovered"

// Recover is a deferred panic trap for goroutines and request handlers. Use it
// as the first statement of a goroutine that must not take down the process:
//
//	go func() {
//	    defer observatory.Recover("worker.name")
//	    ...risky work...
//	}()
//
// On panic it logs a stack trace and publishes EventPanicRecovered, then lets the
// goroutine exit normally (no re-panic). A goroutine holding a completion contract
// (WaitGroup / result channel) must register its Done/send defer BEFORE this one,
// so LIFO ordering runs Recover first and the contract is still satisfied.
func Recover(context string) {
	if r := recover(); r != nil {
		reportPanic(context, r)
	}
}

// RecoverWith is Recover plus a cleanup callback that runs only on panic, after
// logging. Use it when the goroutine must publish a failure result on its
// completion channel using the recovered value:
//
//	defer func() { done <- struct{}{} }()        // always runs (registered first)
//	defer observatory.RecoverWith("worker", func(r interface{}) {
//	    results[i] = Result{Err: fmt.Sprintf("panic: %v", r)}
//	})                                            // runs first on panic
func RecoverWith(context string, cleanup func(r interface{})) {
	if r := recover(); r != nil {
		reportPanic(context, r)
		if cleanup != nil {
			cleanup(r)
		}
	}
}

// SafeGo launches fn in a goroutine guarded by Recover. Use for fire-and-forget
// work with no completion contract; a panic in fn is logged, never fatal.
func SafeGo(context string, fn func()) {
	go func() {
		defer Recover(context)
		fn()
	}()
}

func reportPanic(context string, r interface{}) {
	buf := make([]byte, 64<<10)
	buf = buf[:runtime.Stack(buf, false)]
	log.Printf("[panic-recovered] %s: %v\n%s", context, r, buf)
	PublishEvent(Event{
		Type: EventPanicRecovered,
		Data: map[string]string{
			"context": context,
			"panic":   fmt.Sprintf("%v", r),
			"stack":   string(buf),
		},
	})
}
