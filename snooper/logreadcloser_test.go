package snooper

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func raceTestSnooper() *Snooper {
	lg := logrus.New()
	lg.SetOutput(io.Discard)

	return &Snooper{logger: lg}
}

// A reader concurrently Read by one goroutine (as net/http's transport does when
// streaming the request body) and Closed by another (the handler's deferred
// Close) must not touch its backing buffer concurrently. Run under -race.
func TestLogReadCloserConcurrentReadClose(t *testing.T) {
	s := raceTestSnooper()

	for iter := 0; iter < 200; iter++ {
		pr, pw := io.Pipe()

		go func() {
			for i := 0; i < 40; i++ {
				if _, err := pw.Write([]byte("some-body-chunk")); err != nil {
					return
				}
			}
			_ = pw.Close()
		}()

		var logged int32
		rc := s.createTeeLogStream(pr, func(data []byte) {
			atomic.StoreInt32(&logged, int32(len(data)))
		})

		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			_, _ = io.Copy(io.Discard, rc) // transport-style reader
		}()

		time.Sleep(time.Microsecond) // let some reads happen, then close mid-stream
		_ = rc.Close()
		_ = rc.Close() // idempotent second close must be safe
		wg.Wait()
	}
}

// Close must be idempotent and the logging callback must fire exactly once.
func TestLogReadCloserCloseIdempotent(t *testing.T) {
	s := raceTestSnooper()

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("hello"))
		_ = pw.Close()
	}()

	var calls int32
	rc := s.createTeeLogStream(pr, func([]byte) {
		atomic.AddInt32(&calls, 1)
	})

	_, _ = io.Copy(io.Discard, rc)

	if err := rc.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // allow the log goroutine to run

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected the log callback to fire exactly once, got %d", got)
	}
}

// After Close, further Reads must return EOF rather than racing the drain.
func TestLogReadCloserReadAfterClose(t *testing.T) {
	s := raceTestSnooper()

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("data"))
		_ = pw.Close()
	}()

	rc := s.createTeeLogStream(pr, func([]byte) {})
	_ = rc.Close()

	n, err := rc.Read(make([]byte, 8))
	if n != 0 || err != io.EOF {
		t.Fatalf("read after close: got n=%d err=%v, want 0, EOF", n, err)
	}
}
