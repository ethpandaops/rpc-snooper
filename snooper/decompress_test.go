package snooper

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/sirupsen/logrus"
)

func quietTestSnooper() *Snooper {
	lg := logrus.New()
	lg.SetOutput(io.Discard)

	return &Snooper{logger: lg}
}

// gzipOfZeros returns a gzip stream that inflates to n zero bytes without
// holding the plaintext in memory while building it.
func gzipOfZeros(n int64) []byte {
	var buf bytes.Buffer

	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	chunk := make([]byte, 1<<20)

	for written := int64(0); written < n; {
		size := int64(len(chunk))
		if n-written < size {
			size = n - written
		}

		_, _ = zw.Write(chunk[:size])
		written += size
	}

	_ = zw.Close()

	return buf.Bytes()
}

func brotliOfZeros(n int64) []byte {
	var buf bytes.Buffer

	bw := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	chunk := make([]byte, 1<<20)

	for written := int64(0); written < n; {
		size := int64(len(chunk))
		if n-written < size {
			size = n - written
		}

		_, _ = bw.Write(chunk[:size])
		written += size
	}

	_ = bw.Close()

	return buf.Bytes()
}

// A body that decompresses beyond the cap must be rejected with an error and no
// returned payload, rather than inflating without bound.
func TestDecompressBodyRejectsOversized(t *testing.T) {
	s := quietTestSnooper()

	for _, enc := range []string{"gzip", "br"} {
		var bomb []byte
		if enc == "gzip" {
			bomb = gzipOfZeros(maxDecompressedBodySize + (8 << 20))
		} else {
			bomb = brotliOfZeros(maxDecompressedBodySize + (8 << 20))
		}

		out, err := s.decompressBody(bomb, enc)
		if err == nil {
			t.Fatalf("%s: expected an error for an oversized body, got nil", enc)
		}

		if len(out) != 0 {
			t.Fatalf("%s: expected no payload on overflow, got %d bytes", enc, len(out))
		}
	}
}

// A normal-sized body must decompress correctly, and unknown encodings must pass
// through unchanged.
func TestDecompressBodyAllowsNormal(t *testing.T) {
	s := quietTestSnooper()
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)

	var gzipped bytes.Buffer

	zw := gzip.NewWriter(&gzipped)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	out, err := s.decompressBody(gzipped.Bytes(), "gzip")
	if err != nil {
		t.Fatalf("gzip: unexpected error: %v", err)
	}

	if !bytes.Equal(out, payload) {
		t.Fatalf("gzip: roundtrip mismatch")
	}

	out, err = s.decompressBody(payload, "")
	if err != nil {
		t.Fatalf("passthrough: unexpected error: %v", err)
	}

	if !bytes.Equal(out, payload) {
		t.Fatalf("passthrough: body changed")
	}
}

// A body sitting exactly at the cap must still be accepted.
func TestDecompressBodyAllowsAtLimit(t *testing.T) {
	s := quietTestSnooper()

	out, err := s.decompressBody(gzipOfZeros(maxDecompressedBodySize), "gzip")
	if err != nil {
		t.Fatalf("body at the limit should be accepted, got: %v", err)
	}

	if int64(len(out)) != maxDecompressedBodySize {
		t.Fatalf("expected %d bytes at the limit, got %d", int64(maxDecompressedBodySize), len(out))
	}
}
