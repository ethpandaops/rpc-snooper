package snooper

import (
	"bytes"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

func generateJSONPayload(size int) []byte {
	buf := bytes.Buffer{}
	buf.WriteString(`{"jsonrpc":"2.0","id":1,"result":{"data":"`)

	dataSize := size - 50
	if dataSize < 0 {
		dataSize = 0
	}

	pattern := "abcdefghijklmnopqrstuvwxyz0123456789"

	for buf.Len() < dataSize+40 {
		buf.WriteString(pattern)
	}

	buf.WriteString(`"}}`)

	return buf.Bytes()
}

func BenchmarkTeeLogStream(b *testing.B) {
	sizes := []struct {
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

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper := &Snooper{logger: logger}

	for _, size := range sizes {
		payload := generateJSONPayload(size.size)

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				source := io.NopCloser(bytes.NewReader(payload))

				reader := snooper.createTeeLogStream(source, func(reader io.ReadCloser) {
					_, _ = io.ReadAll(reader)
				})

				_, _ = io.Copy(io.Discard, reader)
				_ = reader.Close()
			}
		})
	}
}

func BenchmarkTeeLogStreamPreallocated(b *testing.B) {
	sizes := []struct {
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

	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	snooper := &Snooper{logger: logger}

	for _, size := range sizes {
		payload := generateJSONPayload(size.size)

		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()

			for b.Loop() {
				source := io.NopCloser(bytes.NewReader(payload))

				// Use size hint for pre-allocation
				reader := snooper.createTeeLogStreamWithSizeHint(source, int64(len(payload)), func(reader io.ReadCloser) {
					_, _ = io.ReadAll(reader)
				})

				_, _ = io.Copy(io.Discard, reader)
				_ = reader.Close()
			}
		})
	}
}
