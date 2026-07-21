package snooper

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The watchdog goroutine writes ProxyCallContext.cancelled on timeout while the
// request and event-stream goroutines read it. This exercises that concurrent
// access; with an atomic flag it is race-free. Run under -race.
func TestCancelledFlagRaceFree(t *testing.T) {
	s := &Snooper{}

	// Short timeout so the watchdog fires and writes cancelled during the read
	// window below.
	cc := s.newProxyCallContext(context.Background(), 5*time.Millisecond)
	defer cc.cancelFn()

	var wg sync.WaitGroup

	deadline := time.Now().Add(60 * time.Millisecond)

	for i := 0; i < 4; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for time.Now().Before(deadline) {
				_ = cc.cancelled.Load()
			}
		}()
	}

	wg.Wait()

	if !cc.cancelled.Load() {
		t.Fatal("expected cancelled to be set after the timeout fired")
	}
}
