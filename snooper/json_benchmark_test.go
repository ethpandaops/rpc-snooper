package snooper

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

var jsonBenchSizes = []struct {
	name string
	size int
}{
	{"1KB", 1 * 1024},
	{"10KB", 10 * 1024},
	{"50KB", 50 * 1024},
	{"100KB", 100 * 1024},
	{"200KB", 200 * 1024},
	{"500KB", 500 * 1024},
	{"1MB", 1 * 1024 * 1024},
	{"2MB", 2 * 1024 * 1024},
	{"5MB", 5 * 1024 * 1024},
}

// BenchmarkJSONUnmarshal measures json.Unmarshal into any, which is the
// first step in beautifyJSON and used directly for method extraction in
// logRequest/logResponse.
func BenchmarkJSONUnmarshal(b *testing.B) {
	for _, size := range jsonBenchSizes {
		payload := generateJSONPayload(size.size)

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				var obj any
				_ = json.Unmarshal(payload, &obj)
			}
		})
	}
}

// BenchmarkJSONMarshalIndent measures json.MarshalIndent from a pre-parsed
// object, which is the second step in beautifyJSON.
func BenchmarkJSONMarshalIndent(b *testing.B) {
	for _, size := range jsonBenchSizes {
		payload := generateJSONPayload(size.size)

		var obj any

		if err := json.Unmarshal(payload, &obj); err != nil {
			b.Fatalf("failed to unmarshal payload: %v", err)
		}

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				_, _ = json.MarshalIndent(obj, "", "  ")
			}
		})
	}
}

// BenchmarkBeautifyJSON measures the full beautifyJSON round-trip
// (unmarshal + marshal indent) as used in logging.go.
func BenchmarkBeautifyJSON(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper := &Snooper{logger: logger}

	for _, size := range jsonBenchSizes {
		payload := generateJSONPayload(size.size)

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				_ = snooper.beautifyJSON(payload)
			}
		})
	}
}

// BenchmarkJSONLoggingFlow measures the full hot-path: io.ReadAll + beautifyJSON
// + json.Unmarshal for method extraction, mirroring the default branch in
// logRequest/logResponse.
func BenchmarkJSONLoggingFlow(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper := &Snooper{logger: logger}

	for _, size := range jsonBenchSizes {
		payload := generateJSONPayload(size.size)

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				body := io.NopCloser(bytes.NewReader(payload))

				bodyData, _ := io.ReadAll(body)

				_ = snooper.beautifyJSON(bodyData)

				var parsedData any
				_ = json.Unmarshal(bodyData, &parsedData)
			}
		})
	}
}
