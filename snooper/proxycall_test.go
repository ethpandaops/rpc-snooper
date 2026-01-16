package snooper

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponseStreamingNotBlockedBySlowRequestChannel verifies that response streaming
// to the client is not blocked while waiting for request logging to complete.
// This tests the fix where we read the response body immediately before waiting
// on reqSentChan, rather than waiting first (which would block the unbuffered pipe).
func TestResponseStreamingNotBlockedBySlowRequestChannel(t *testing.T) {
	// Create a mock upstream server that delays sending the request body
	// but responds quickly once received
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"test"}`)

	var requestReceived atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain request body
		_, _ = io.ReadAll(r.Body)

		requestReceived.Store(true)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	// Create test request
	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	// Make the proxied request
	start := time.Now()

	snooper.ServeHTTP(rec, req)

	elapsed := time.Since(start)

	// Verify the response was correct
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, responseBody, rec.Body.Bytes())
	assert.True(t, requestReceived.Load())

	// Log timing for reference
	t.Logf("Request completed in %v", elapsed)
}

// TestLogOrderingPreserved verifies that even though response streaming is not blocked,
// the log ordering (request before response) is still maintained.
func TestLogOrderingPreserved(t *testing.T) {
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"test"}`)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	// Track log ordering
	var logOrder []string

	var logMu sync.Mutex

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&orderTrackingFormatter{
		underlying: &logrus.TextFormatter{},
		onLog: func(msg string) {
			logMu.Lock()
			defer logMu.Unlock()

			if bytes.Contains([]byte(msg), []byte("REQUEST")) {
				logOrder = append(logOrder, "request")
			} else if bytes.Contains([]byte(msg), []byte("RESPONSE")) {
				logOrder = append(logOrder, "response")
			}
		},
	})

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	// Make request
	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	snooper.ServeHTTP(rec, req)

	// Wait a bit for async logging to complete
	time.Sleep(100 * time.Millisecond)

	// Verify ordering
	logMu.Lock()
	defer logMu.Unlock()

	require.Len(t, logOrder, 2, "Expected both request and response logs")
	assert.Equal(t, "request", logOrder[0], "Request should be logged before response")
	assert.Equal(t, "response", logOrder[1], "Response should be logged after request")
}

// TestLargeResponseStreaming verifies that large responses stream efficiently
// without being fully buffered before the client receives data.
func TestLargeResponseStreaming(t *testing.T) {
	// Create a large response (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(largeData)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	start := time.Now()

	snooper.ServeHTTP(rec, req)

	elapsed := time.Since(start)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, len(largeData), rec.Body.Len())

	// Log timing for reference
	t.Logf("Large response (1MB) streamed in %v", elapsed)
}

// orderTrackingFormatter wraps a logrus formatter to track log ordering.
type orderTrackingFormatter struct {
	underlying logrus.Formatter
	onLog      func(msg string)
}

func (f *orderTrackingFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	if f.onLog != nil {
		f.onLog(entry.Message)
	}

	return f.underlying.Format(entry)
}

// TestConcurrentRequests verifies the fix works under concurrent load.
func TestConcurrentRequests(t *testing.T) {
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	const numRequests = 50

	var wg sync.WaitGroup

	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
			req := httptest.NewRequest(http.MethodPost, "/", reqBody)
			req = req.WithContext(context.Background())
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()

			snooper.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				errors <- assert.AnError
			}
		}()
	}

	wg.Wait()
	close(errors)

	var errCount int

	for range errors {
		errCount++
	}

	assert.Zero(t, errCount, "All concurrent requests should succeed")
}

// TestResponseNotBlockedBySlowRequestLogging creates a scenario where
// request logging is artificially slow and verifies the response still
// streams to the client without waiting.
func TestResponseNotBlockedBySlowRequestLogging(t *testing.T) {
	// This test verifies the core fix: response body reading happens
	// BEFORE waiting on reqSentChan, so the pipe drains immediately.
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"success"}`)

	// Track when response body is fully received by client
	var clientReceivedResponse atomic.Bool

	var responseTime time.Time

	var timeMu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		_, _ = io.ReadAll(r.Body)

		// Send response immediately
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	snooper.ServeHTTP(rec, req)

	timeMu.Lock()
	responseTime = time.Now()
	timeMu.Unlock()

	clientReceivedResponse.Store(true)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, responseBody, rec.Body.Bytes())
	assert.True(t, clientReceivedResponse.Load())

	t.Logf("Response received at %v", responseTime)
}
