package snooper

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

// generateEnginePayload creates a realistic engine_newPayloadV5 JSON payload
// with the given number of transactions. Each tx is ~500 bytes of hex data,
// so 200 txs ≈ 300KB+ of JSON.
func generateEnginePayload(txCount int) []byte {
	txs := make([]string, txCount)
	// realistic tx: 250 random-ish hex bytes = 500 hex chars
	txHex := "0x02f8b20181"
	for len(txHex) < 502 {
		txHex += "deadbeef"
	}
	txHex = txHex[:502]
	for i := range txs {
		txs[i] = txHex
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "engine_newPayloadV5",
		"id":      1,
		"params": []interface{}{
			map[string]interface{}{
				"parentHash":           "0x0000000000000000000000000000000000000000000000000000000000000000",
				"feeRecipient":         "0x0000000000000000000000000000000000000000",
				"stateRoot":            "0x0000000000000000000000000000000000000000000000000000000000000000",
				"receiptsRoot":         "0x0000000000000000000000000000000000000000000000000000000000000000",
				"logsBloom":            "0x" + fmt.Sprintf("%0512x", 0),
				"prevRandao":           "0x0000000000000000000000000000000000000000000000000000000000000000",
				"blockNumber":          "0x1",
				"gasLimit":             "0x1c9c380",
				"gasUsed":              "0xe4e1c0",
				"timestamp":            "0x60000000",
				"extraData":            "0x",
				"baseFeePerGas":        "0x7",
				"blockHash":            "0x0000000000000000000000000000000000000000000000000000000000000001",
				"transactions":         txs,
				"slotNumber":           "0x1",
				"blockAccessListHash":  "0x0000000000000000000000000000000000000000000000000000000000000000",
				"blockAccessList":      []interface{}{},
			},
		},
	}

	data, _ := json.Marshal(payload)
	return data
}

// simulateOldLogRequest replicates the OLD code path: all heavy allocations
// happen before the wait barrier. This is what ran during WaitForSequence.
//
// Old flow: ReadAll → beautifyJSON → Unmarshal → string format → WAIT → modules → log
func simulateOldLogRequest(s *Snooper, bodyBytes []byte, barrier *sync.WaitGroup, done chan struct{}) {
	// Simulate io.NopCloser(bytes.NewReader(data)) → ReadAll roundtrip
	// that the old createTeeLogStreamWithSizeHint wrapping caused
	body := io.NopCloser(bytes.NewReader(bodyBytes))
	bodyData, _ := io.ReadAll(body)

	// beautifyJSON: unmarshal + marshal indent
	var obj any
	_ = json.Unmarshal(bodyData, &obj)
	beautified, _ := json.MarshalIndent(obj, "", "  ")

	// String formatting for logFields
	_ = fmt.Sprintf("%v\n\n", string(beautified))

	// Unmarshal again for parsedData (module processing)
	var parsedData any
	_ = json.Unmarshal(bodyData, &parsedData)

	// request_size
	_ = len(bodyData)

	// --- All of the above is held in memory during the wait ---
	barrier.Done()
	<-done

	// Prevent compiler from optimizing away
	runtime.KeepAlive(bodyData)
	runtime.KeepAlive(obj)
	runtime.KeepAlive(beautified)
	runtime.KeepAlive(parsedData)
}

// simulateNewLogRequest replicates the NEW code path: only decompressed raw
// bytes are held during the wait. Everything else is deferred.
//
// New flow: decompress (zero-copy) → WAIT → beautifyJSON → Unmarshal → string format → modules → log
func simulateNewLogRequest(bodyBytes []byte, barrier *sync.WaitGroup, done chan struct{}) {
	// decompressBody for non-compressed: zero-copy, just returns input slice
	bodyData := bodyBytes

	// --- Only bodyData is held during the wait ---
	barrier.Done()
	<-done

	// After wait: all heavy work happens here (sequential, not concurrent)
	var obj any
	_ = json.Unmarshal(bodyData, &obj)
	beautified, _ := json.MarshalIndent(obj, "", "  ")
	_ = fmt.Sprintf("%v\n\n", string(beautified))

	var parsedData any
	_ = json.Unmarshal(bodyData, &parsedData)
	_ = len(bodyData)

	runtime.KeepAlive(bodyData)
	runtime.KeepAlive(obj)
	runtime.KeepAlive(beautified)
	runtime.KeepAlive(parsedData)
}

// simulateOldLogRequestSSZ replicates the OLD SSZ path where body was
// hex-encoded before being stored in bodyData.
func simulateOldLogRequestSSZ(rawBytes []byte, barrier *sync.WaitGroup, done chan struct{}) {
	// Old: body = utils.NewHexEncoder(body); bodyData, _ = io.ReadAll(body)
	hexEncoded := make([]byte, len(rawBytes)*2)
	hex.Encode(hexEncoded, rawBytes)
	bodyData := hexEncoded

	_ = fmt.Sprintf("%v\n\n", string(bodyData))

	barrier.Done()
	<-done

	runtime.KeepAlive(bodyData)
}

// simulateNewLogRequestSSZ replicates the NEW SSZ path.
func simulateNewLogRequestSSZ(rawBytes []byte, barrier *sync.WaitGroup, done chan struct{}) {
	bodyData := rawBytes

	barrier.Done()
	<-done

	// Hex encoding after wait
	hexEncoded := make([]byte, len(bodyData)*2)
	hex.Encode(hexEncoded, bodyData)
	_ = fmt.Sprintf("%v\n\n", string(hexEncoded))

	runtime.KeepAlive(bodyData)
	runtime.KeepAlive(hexEncoded)
}

// measurePeakMemory launches n goroutines that each call workFn, waits for all
// of them to reach the barrier (simulating WaitForSequence), measures heap
// memory, then releases them. Returns bytes of heap allocated while all
// goroutines are blocked.
func measurePeakMemory(n int, workFn func(barrier *sync.WaitGroup, done chan struct{})) uint64 {
	// Force GC to get a clean baseline
	runtime.GC()
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	barrier := &sync.WaitGroup{}
	barrier.Add(n)
	done := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(n)

	for range n {
		go func() {
			defer wg.Done()
			workFn(barrier, done)
		}()
	}

	// Wait for all goroutines to reach the barrier (all holding their data)
	barrier.Wait()

	// Measure while all goroutines are blocked
	runtime.GC()
	var peak runtime.MemStats
	runtime.ReadMemStats(&peak)

	// Release goroutines
	close(done)
	wg.Wait()

	if peak.HeapAlloc > baseline.HeapAlloc {
		return peak.HeapAlloc - baseline.HeapAlloc
	}

	return 0
}

func TestMemoryDuringWait_JSON(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	s := &Snooper{logger: logger}

	configs := []struct {
		name    string
		txCount int
	}{
		{"50txs", 50},
		{"200txs", 200},
		{"500txs", 500},
	}

	concurrencies := []int{10, 50, 100}

	for _, cfg := range configs {
		payload := generateEnginePayload(cfg.txCount)
		payloadSize := len(payload)

		for _, n := range concurrencies {
			name := fmt.Sprintf("%s/%d_goroutines", cfg.name, n)

			t.Run(name, func(t *testing.T) {
				oldMem := measurePeakMemory(n, func(barrier *sync.WaitGroup, done chan struct{}) {
					simulateOldLogRequest(s, payload, barrier, done)
				})

				newMem := measurePeakMemory(n, func(barrier *sync.WaitGroup, done chan struct{}) {
					simulateNewLogRequest(payload, barrier, done)
				})

				oldPerGoroutine := oldMem / uint64(n)
				newPerGoroutine := newMem / uint64(n)
				reduction := float64(oldMem-newMem) / float64(oldMem) * 100

				t.Logf("payload=%dKB goroutines=%d", payloadSize/1024, n)
				t.Logf("  OLD: %6dKB total, %5dKB/goroutine", oldMem/1024, oldPerGoroutine/1024)
				t.Logf("  NEW: %6dKB total, %5dKB/goroutine", newMem/1024, newPerGoroutine/1024)
				t.Logf("  reduction=%.1f%%  saved=%dKB", reduction, (oldMem-newMem)/1024)

				if newMem >= oldMem {
					t.Errorf("new path should use less memory: old=%d new=%d", oldMem, newMem)
				}
			})
		}
	}
}

func TestMemoryDuringWait_SSZ(t *testing.T) {
	// SSZ bodies are raw binary; test with 1MB payload
	rawPayload := make([]byte, 1*1024*1024)
	for i := range rawPayload {
		rawPayload[i] = byte(i % 256)
	}

	concurrencies := []int{10, 50}

	for _, n := range concurrencies {
		name := fmt.Sprintf("1MB_ssz/%d_goroutines", n)

		t.Run(name, func(t *testing.T) {
			oldMem := measurePeakMemory(n, func(barrier *sync.WaitGroup, done chan struct{}) {
				simulateOldLogRequestSSZ(rawPayload, barrier, done)
			})

			newMem := measurePeakMemory(n, func(barrier *sync.WaitGroup, done chan struct{}) {
				simulateNewLogRequestSSZ(rawPayload, barrier, done)
			})

			t.Logf("goroutines=%d", n)
			t.Logf("  OLD: %6dKB total, %5dKB/goroutine", oldMem/1024, oldMem/uint64(n)/1024)
			t.Logf("  NEW: %6dKB total, %5dKB/goroutine", newMem/1024, newMem/uint64(n)/1024)

			reduction := float64(oldMem-newMem) / float64(oldMem) * 100
			t.Logf("  reduction=%.1f%%  saved=%dKB", reduction, (oldMem-newMem)/1024)
		})
	}
}

// BenchmarkLogRequestMemory_Old benchmarks the old code path per-operation.
func BenchmarkLogRequestMemory_Old(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	s := &Snooper{logger: logger}

	payload := generateEnginePayload(200)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()

	noop := &sync.WaitGroup{}
	done := make(chan struct{})
	close(done) // never blocks

	for b.Loop() {
		noop.Add(1)
		simulateOldLogRequest(s, payload, noop, done)
	}
}

// BenchmarkLogRequestMemory_New benchmarks the new code path per-operation.
func BenchmarkLogRequestMemory_New(b *testing.B) {
	payload := generateEnginePayload(200)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()

	noop := &sync.WaitGroup{}
	done := make(chan struct{})
	close(done) // never blocks

	for b.Loop() {
		noop.Add(1)
		simulateNewLogRequest(payload, noop, done)
	}
}
