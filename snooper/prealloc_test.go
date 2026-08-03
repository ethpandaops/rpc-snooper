package snooper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
)

func quietTestSnooper(t *testing.T, target string) *Snooper {
	lg := logrus.New()
	lg.SetOutput(io.Discard)
	lg.SetLevel(logrus.InfoLevel)

	s, err := NewSnooper(target, lg, nil, "")
	if err != nil {
		t.Fatalf("NewSnooper: %v", err)
	}

	return s
}

func TestCreateTeeLogStreamWithSizeHint_ClampsOversizedHint(t *testing.T) {
	s := quietTestSnooper(t, "http://127.0.0.1:1")

	stream := s.createTeeLogStreamWithSizeHint(io.NopCloser(bytes.NewReader(nil)), 512<<20, func([]byte) {})
	defer stream.Close()

	rc, ok := stream.(*logReadCloser)
	if !ok {
		t.Fatalf("expected *logReadCloser, got %T", stream)
	}

	if got := rc.buf.Cap(); got > maxPreallocSize {
		t.Fatalf("buffer pre-allocated %d bytes for a 512 MiB hint, want at most %d", got, maxPreallocSize)
	}
}

func TestCreateTeeLogStreamWithSizeHint_KeepsSmallHint(t *testing.T) {
	s := quietTestSnooper(t, "http://127.0.0.1:1")

	const hint = 4096

	stream := s.createTeeLogStreamWithSizeHint(io.NopCloser(bytes.NewReader(nil)), hint, func([]byte) {})
	defer stream.Close()

	rc, ok := stream.(*logReadCloser)
	if !ok {
		t.Fatalf("expected *logReadCloser, got %T", stream)
	}

	if got := rc.buf.Cap(); got < hint {
		t.Fatalf("buffer pre-allocated only %d bytes for a %d byte hint, expected at least the hint honored", got, hint)
	}
}

func TestResponseProcessing_HostileContentLengthStaysBounded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "536870912") // 512 MiB, hostile or buggy
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()

	s := quietTestSnooper(t, upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)

	s.ServeHTTP(rec, req)

	runtime.ReadMemStats(&m1)
	delta := m1.TotalAlloc - m0.TotalAlloc

	if delta > 2*maxPreallocSize {
		t.Fatalf("allocation delta %d bytes exceeds the pre-allocation cap by more than a safety margin", delta)
	}
}
