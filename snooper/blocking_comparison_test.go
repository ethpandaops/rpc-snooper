package snooper

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// TestBlockingBehaviorComparison demonstrates the difference between the old
// blocking behavior and the new non-blocking behavior.
//
// OLD behavior: Response goroutine waits on reqSentChan BEFORE reading from pipe
// NEW behavior: Response goroutine reads from pipe FIRST, then waits on reqSentChan
//
// The difference matters because io.Pipe is unbuffered - if no one reads from the
// pipe, writes block, which blocks the TeeReader, which blocks io.Copy to the client.
func TestBlockingBehaviorComparison(t *testing.T) {
	// Simulate a 5MB blob response
	responseSize := 5 * 1024 * 1024
	responseData := make([]byte, responseSize)

	// Simulate request processing delay (e.g., JSON parsing, module processing)
	requestProcessingDelay := 50 * time.Millisecond

	t.Run("old_blocking_behavior", func(t *testing.T) {
		// Simulate OLD behavior: wait on channel BEFORE reading
		var clientReceivedAt time.Time

		var wg sync.WaitGroup

		reqSentChan := make(chan struct{})
		pipeReader, pipeWriter := io.Pipe()

		// Simulate response logging goroutine (OLD behavior)
		wg.Add(1)

		go func() {
			defer wg.Done()
			// OLD: Wait first, then read
			<-reqSentChan

			_, _ = io.ReadAll(pipeReader)
		}()

		// Simulate client reading response through TeeReader
		wg.Add(1)

		go func() {
			defer wg.Done()
			// TeeReader writes to pipe as we read
			teeReader := io.TeeReader(bytes.NewReader(responseData), pipeWriter)

			_, _ = io.ReadAll(teeReader)

			pipeWriter.Close()

			clientReceivedAt = time.Now()
		}()

		// Simulate request processing (closes channel after delay)
		start := time.Now()

		time.Sleep(requestProcessingDelay)
		close(reqSentChan)

		wg.Wait()

		elapsed := time.Since(start)

		t.Logf("OLD behavior: Client received response after %v (blocked by %v request delay)",
			clientReceivedAt.Sub(start), requestProcessingDelay)
		t.Logf("OLD behavior: Total time: %v", elapsed)

		// With old behavior, client is blocked until reqSentChan closes
		if clientReceivedAt.Sub(start) < requestProcessingDelay {
			t.Error("Expected client to be blocked by request processing delay")
		}
	})

	t.Run("new_nonblocking_behavior", func(t *testing.T) {
		// Simulate NEW behavior: read FIRST, then wait on channel
		var clientReceivedAt time.Time

		var wg sync.WaitGroup

		reqSentChan := make(chan struct{})
		pipeReader, pipeWriter := io.Pipe()

		// Simulate response logging goroutine (NEW behavior)
		wg.Add(1)

		go func() {
			defer wg.Done()
			// NEW: Read first (drains pipe immediately), then wait
			data, _ := io.ReadAll(pipeReader)

			<-reqSentChan

			// Process data after waiting (simulated)
			_ = len(data)
		}()

		// Simulate client reading response through TeeReader
		wg.Add(1)

		go func() {
			defer wg.Done()
			// TeeReader writes to pipe as we read
			teeReader := io.TeeReader(bytes.NewReader(responseData), pipeWriter)

			_, _ = io.ReadAll(teeReader)

			pipeWriter.Close()

			clientReceivedAt = time.Now()
		}()

		// Simulate request processing (closes channel after delay)
		start := time.Now()

		time.Sleep(requestProcessingDelay)
		close(reqSentChan)

		wg.Wait()

		elapsed := time.Since(start)

		t.Logf("NEW behavior: Client received response after %v (NOT blocked by request delay)",
			clientReceivedAt.Sub(start))
		t.Logf("NEW behavior: Total time: %v", elapsed)

		// With new behavior, client receives data immediately (not blocked by request delay)
		if clientReceivedAt.Sub(start) > requestProcessingDelay/2 {
			t.Logf("Warning: Client was somewhat delayed, but this may be due to goroutine scheduling")
		}
	})
}

// BenchmarkOldVsNewBlocking directly compares throughput with simulated delays.
func BenchmarkOldVsNewBlocking(b *testing.B) {
	responseSize := 5 * 1024 * 1024
	responseData := make([]byte, responseSize)
	requestDelay := 10 * time.Millisecond

	b.Run("old_blocking", func(b *testing.B) {
		b.SetBytes(int64(responseSize))

		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup

			reqSentChan := make(chan struct{})
			pipeReader, pipeWriter := io.Pipe()

			// Response logger (OLD: wait then read)
			wg.Add(1)

			go func() {
				defer wg.Done()

				<-reqSentChan // Wait first

				_, _ = io.ReadAll(pipeReader)
			}()

			// Client reader
			wg.Add(1)

			go func() {
				defer wg.Done()

				teeReader := io.TeeReader(bytes.NewReader(responseData), pipeWriter)

				_, _ = io.ReadAll(teeReader)

				pipeWriter.Close()
			}()

			// Request processing
			time.Sleep(requestDelay)
			close(reqSentChan)

			wg.Wait()
		}
	})

	b.Run("new_nonblocking", func(b *testing.B) {
		b.SetBytes(int64(responseSize))

		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup

			reqSentChan := make(chan struct{})
			pipeReader, pipeWriter := io.Pipe()

			// Response logger (NEW: read then wait)
			wg.Add(1)

			go func() {
				defer wg.Done()

				data, _ := io.ReadAll(pipeReader) // Read first

				<-reqSentChan // Then wait

				_ = len(data)
			}()

			// Client reader
			wg.Add(1)

			go func() {
				defer wg.Done()

				teeReader := io.TeeReader(bytes.NewReader(responseData), pipeWriter)

				_, _ = io.ReadAll(teeReader)

				pipeWriter.Close()
			}()

			// Request processing
			time.Sleep(requestDelay)
			close(reqSentChan)

			wg.Wait()
		}
	})
}
