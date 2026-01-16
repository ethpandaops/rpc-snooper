package snooper

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// blobSizeBytes is the standard blob size (128KB).
const blobSizeBytes = 128 * 1024

// BenchmarkLargeBlobResponse benchmarks response streaming with blob-like payloads.
// Simulates engine_getBlobs responses with varying blob counts.
func BenchmarkLargeBlobResponse(b *testing.B) {
	// Test different blob counts (each blob is ~128KB)
	blobCounts := []int{1, 6, 12, 21}

	for _, blobCount := range blobCounts {
		totalSize := blobCount * blobSizeBytes

		// Create response payload simulating JSON-RPC response with blobs
		responseData := createBlobResponse(blobCount)

		b.Run(fmt.Sprintf("blobs_%d_size_%dMB", blobCount, totalSize/(1024*1024)), func(b *testing.B) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(responseData)
			}))
			defer upstream.Close()

			logger := logrus.New()
			logger.SetLevel(logrus.PanicLevel) // Suppress all logs

			snooper, err := NewSnooper(upstream.URL, logger, nil, "")
			if err != nil {
				b.Fatalf("failed to create snooper: %v", err)
			}

			defer snooper.Shutdown()

			reqBody := []byte(`{"jsonrpc":"2.0","method":"engine_getBlobsV1","params":[["0x01"]],"id":1}`)

			b.ResetTimer()
			b.SetBytes(int64(len(responseData)))

			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
				req.Header.Set("Content-Type", "application/json")

				rec := httptest.NewRecorder()

				snooper.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					b.Fatalf("unexpected status: %d", rec.Code)
				}
			}
		})
	}
}

// BenchmarkResponseLatency measures time-to-first-byte and total latency.
func BenchmarkResponseLatency(b *testing.B) {
	// 21 blobs × 128KB = ~2.7MB (max blobs per block)
	blobCount := 21
	responseData := createBlobResponse(blobCount)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseData)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	if err != nil {
		b.Fatalf("failed to create snooper: %v", err)
	}

	defer snooper.Shutdown()

	reqBody := []byte(`{"jsonrpc":"2.0","method":"engine_getBlobsV1","params":[["0x01"]],"id":1}`)

	b.ResetTimer()

	var totalLatency time.Duration

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		start := time.Now()

		snooper.ServeHTTP(rec, req)

		totalLatency += time.Since(start)
	}

	b.ReportMetric(float64(totalLatency.Microseconds())/float64(b.N), "µs/op")
	b.ReportMetric(float64(len(responseData))/(float64(totalLatency.Seconds())/float64(b.N))/1024/1024, "MB/s")
}

// BenchmarkConcurrentBlobRequests benchmarks concurrent large responses.
func BenchmarkConcurrentBlobRequests(b *testing.B) {
	concurrencyLevels := []int{1, 4, 8, 16}

	// 6 blobs per request (~768KB each)
	blobCount := 6
	responseData := createBlobResponse(blobCount)

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("concurrent_%d", concurrency), func(b *testing.B) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(responseData)
			}))
			defer upstream.Close()

			logger := logrus.New()
			logger.SetLevel(logrus.PanicLevel)

			snooper, err := NewSnooper(upstream.URL, logger, nil, "")
			if err != nil {
				b.Fatalf("failed to create snooper: %v", err)
			}

			defer snooper.Shutdown()

			reqBody := []byte(`{"jsonrpc":"2.0","method":"engine_getBlobsV1","params":[["0x01"]],"id":1}`)

			b.ResetTimer()
			b.SetBytes(int64(len(responseData)) * int64(concurrency))

			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup

				for j := 0; j < concurrency; j++ {
					wg.Add(1)

					go func() {
						defer wg.Done()

						req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
						req.Header.Set("Content-Type", "application/json")

						rec := httptest.NewRecorder()

						snooper.ServeHTTP(rec, req)
					}()
				}

				wg.Wait()
			}
		})
	}
}

// createBlobResponse creates a JSON-RPC response simulating engine_getBlobs output.
func createBlobResponse(blobCount int) []byte {
	// Create fake blob data
	blob := make([]byte, blobSizeBytes)
	for i := range blob {
		blob[i] = byte(i % 256)
	}

	// Build JSON response manually for efficiency
	var buf bytes.Buffer

	buf.WriteString(`{"jsonrpc":"2.0","id":1,"result":[`)

	for i := 0; i < blobCount; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}

		buf.WriteString(`{"blob":"0x`)
		// Write hex-encoded blob data
		for _, b := range blob {
			fmt.Fprintf(&buf, "%02x", b)
		}

		buf.WriteString(`","kzg_commitment":"0x`)
		// 48-byte commitment
		for j := 0; j < 48; j++ {
			buf.WriteString("00")
		}

		buf.WriteString(`","kzg_proof":"0x`)
		// 48-byte proof
		for j := 0; j < 48; j++ {
			buf.WriteString("00")
		}

		buf.WriteString(`"}`)
	}

	buf.WriteString(`]}`)

	return buf.Bytes()
}

// BenchmarkMaxBlobs benchmarks the worst case: 21 blobs per block.
func BenchmarkMaxBlobs(b *testing.B) {
	// 21 blobs × 128KB
	blobCount := 21
	responseData := createBlobResponse(blobCount)

	b.Logf("Response size: %.2f MB", float64(len(responseData))/(1024*1024))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseData)
	}))
	defer upstream.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper, err := NewSnooper(upstream.URL, logger, nil, "")
	if err != nil {
		b.Fatalf("failed to create snooper: %v", err)
	}

	defer snooper.Shutdown()

	reqBody := []byte(`{"jsonrpc":"2.0","method":"engine_getBlobsV1","params":[["0x01"]],"id":1}`)

	b.ResetTimer()
	b.SetBytes(int64(len(responseData)))

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		snooper.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", rec.Code)
		}

		if rec.Body.Len() != len(responseData) {
			b.Fatalf("response size mismatch: got %d, want %d", rec.Body.Len(), len(responseData))
		}
	}
}
