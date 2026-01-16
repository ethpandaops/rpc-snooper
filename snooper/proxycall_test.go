package snooper

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientResponseNotBlockedBySlowLogging verifies that slow request logging
// does not block the response from reaching the client.
func TestClientResponseNotBlockedBySlowLogging(t *testing.T) {
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"test"}`)
	loggingDelay := 200 * time.Millisecond

	var requestLogTime time.Time

	var responseLogTime time.Time

	var logMu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	// Create a logger that delays on request logging
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetOutput(&slowLogWriter{
		delay: loggingDelay,
		onWrite: func(isRequest bool) {
			logMu.Lock()
			defer logMu.Unlock()

			if isRequest {
				requestLogTime = time.Now()
			} else {
				responseLogTime = time.Now()
			}
		},
	})

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	requestData := []byte(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(requestData))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	start := time.Now()

	snooper.ServeHTTP(rec, req)

	clientReceivedAt := time.Since(start)

	// Verify response is correct
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, responseBody, rec.Body.Bytes())

	// Wait for async logging to complete
	time.Sleep(loggingDelay + 100*time.Millisecond)

	logMu.Lock()
	requestLogDuration := requestLogTime.Sub(start)
	responseLogDuration := responseLogTime.Sub(start)
	logMu.Unlock()

	t.Logf("Logging delay: %v", loggingDelay)
	t.Logf("Client received response in: %v", clientReceivedAt)
	t.Logf("Request logged at: %v", requestLogDuration)
	t.Logf("Response logged at: %v", responseLogDuration)

	// The client should receive the response much faster than the logging delay.
	// If blocking, client would wait for request logging (200ms+) before getting response.
	assert.Less(t, clientReceivedAt, loggingDelay,
		"Client should receive response before request logging completes (got %v, logging takes %v)",
		clientReceivedAt, loggingDelay)
}

// slowLogWriter is an io.Writer that introduces delay when writing request logs.
type slowLogWriter struct {
	delay   time.Duration
	onWrite func(isRequest bool)
}

func (w *slowLogWriter) Write(p []byte) (n int, err error) {
	isRequest := bytes.Contains(p, []byte("REQUEST"))

	if isRequest {
		time.Sleep(w.delay)
	}

	if w.onWrite != nil {
		w.onWrite(isRequest)
	}

	return len(p), nil
}

// TestLogOrderingPreserved verifies that request logs appear before response logs.
func TestLogOrderingPreserved(t *testing.T) {
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":"test"}`)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

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

	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	snooper.ServeHTTP(rec, req)

	// Wait for async logging to complete
	time.Sleep(100 * time.Millisecond)

	// Verify ordering
	logMu.Lock()
	defer logMu.Unlock()

	require.Len(t, logOrder, 2, "Expected both request and response logs")
	assert.Equal(t, "request", logOrder[0], "Request should be logged before response")
	assert.Equal(t, "response", logOrder[1], "Response should be logged after request")
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

// TestLargeResponseStreaming verifies that large responses (like engine_getBlobs)
// are handled correctly.
func TestLargeResponseStreaming(t *testing.T) {
	// Simulate 6 blobs worth of data (~1.5MB)
	blobSize := 128 * 1024 // 128KB per blob
	numBlobs := 6
	largeData := make([]byte, blobSize*numBlobs)

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

	reqBody := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"engine_getBlobsV1","params":[],"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	snooper.ServeHTTP(rec, req)

	// Verify response integrity
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, len(largeData), rec.Body.Len())
	assert.Equal(t, largeData, rec.Body.Bytes())
}

// TestRequestAndResponseBodiesAreLogged verifies that request and response bodies
// are captured and logged correctly.
func TestRequestAndResponseBodiesAreLogged(t *testing.T) {
	requestBody := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	responseBody := `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify upstream receives the request body correctly
		receivedBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, requestBody, string(receivedBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	var loggedRequestBody string

	var loggedResponseBody string

	var logMu sync.Mutex

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&contentCapturingFormatter{
		underlying: &logrus.TextFormatter{},
		onLog: func(entry *logrus.Entry) {
			logMu.Lock()
			defer logMu.Unlock()

			body, ok := entry.Data["body"].(string)
			if !ok {
				return
			}

			if bytes.Contains([]byte(entry.Message), []byte("REQUEST")) {
				loggedRequestBody = body
			} else if bytes.Contains([]byte(entry.Message), []byte("RESPONSE")) {
				loggedResponseBody = body
			}
		},
	})

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	require.NoError(t, err)

	defer snooper.Shutdown()

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	snooper.ServeHTTP(rec, req)

	// Verify client receives correct response
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, responseBody, rec.Body.String())

	// Wait for async logging to complete
	time.Sleep(100 * time.Millisecond)

	// Verify logged content
	logMu.Lock()
	defer logMu.Unlock()

	require.NotEmpty(t, loggedRequestBody, "Request body should be logged")
	require.NotEmpty(t, loggedResponseBody, "Response body should be logged")

	// The logger beautifies JSON, so compare as JSON
	assert.JSONEq(t, requestBody, loggedRequestBody, "Logged request body should match sent request")
	assert.JSONEq(t, responseBody, loggedResponseBody, "Logged response body should match received response")
}

// contentCapturingFormatter wraps a logrus formatter to capture log entry data.
type contentCapturingFormatter struct {
	underlying logrus.Formatter
	onLog      func(entry *logrus.Entry)
}

func (f *contentCapturingFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	if f.onLog != nil {
		f.onLog(entry)
	}

	return f.underlying.Format(entry)
}

// TestConcurrentRequests verifies the proxy handles concurrent requests correctly.
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

	for range numRequests {
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

			if !bytes.Equal(rec.Body.Bytes(), responseBody) {
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

	assert.Zero(t, errCount, "All concurrent requests should succeed with correct response")
}
