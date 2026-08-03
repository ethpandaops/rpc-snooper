package snooper

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestReadSSELine_SurvivesLineLongerThanBufioBuffer(t *testing.T) {
	long := strings.Repeat("A", 5000) + "\n" // longer than a small reader buffer
	rd := bufio.NewReaderSize(strings.NewReader(long), 512)

	line, err := readSSELine(rd, 1<<20)
	if err != nil {
		t.Fatalf("readSSELine: %v", err)
	}

	if string(line) != long {
		t.Fatalf("got %d bytes, want %d", len(line), len(long))
	}
}

func TestReadSSELine_ShortLineUnaffected(t *testing.T) {
	rd := bufio.NewReaderSize(strings.NewReader("data: ok\n"), 4096)

	line, err := readSSELine(rd, 1<<20)
	if err != nil {
		t.Fatalf("readSSELine: %v", err)
	}

	if string(line) != "data: ok\n" {
		t.Fatalf("got %q", string(line))
	}
}

func TestReadSSELine_RejectsLineOverHardCap(t *testing.T) {
	huge := strings.Repeat("A", 200) + "\n"
	rd := bufio.NewReaderSize(strings.NewReader(huge), 64)

	_, err := readSSELine(rd, 100)
	if err == nil {
		t.Fatal("expected an error once the line exceeds the hard cap, got nil")
	}
}

func TestReadSSELine_PropagatesRealReadError(t *testing.T) {
	rd := bufio.NewReaderSize(strings.NewReader("no newline here"), 4096)

	_, err := readSSELine(rd, 1<<20)
	if err == nil {
		t.Fatal("expected an error for a stream that ends without a newline")
	}
}

// flushRecorder makes httptest.ResponseRecorder satisfy http.Flusher, which
// the event-stream response path requires.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() { f.ResponseRecorder.Flush() }

func TestEventStream_LongLineNoLongerKillsTheStream(t *testing.T) {
	const firstMarker = "first-event"
	const thirdMarker = "third-event"

	long := strings.Repeat("A", 5000) // one line over the old 4096-byte default

	var body bytes.Buffer
	body.WriteString("data: {\"tag\":\"" + firstMarker + "\"}\n\n")
	body.WriteString("data: " + long + "\n\n")
	body.WriteString("data: {\"tag\":\"" + thirdMarker + "\"}\n\n")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		b := body.Bytes()
		const chunk = 512

		for i := 0; i < len(b); i += chunk {
			end := i + chunk
			if end > len(b) {
				end = len(b)
			}

			_, _ = w.Write(b[i:end])

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer upstream.Close()

	s := quietTestSnooper(t, upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/eth/v1/events?topics=head", nil)
	rec := &flushRecorder{httptest.NewRecorder()}

	done := make(chan struct{})

	go func() {
		s.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP never returned")
	}

	got := rec.Body.String()

	if !strings.Contains(got, firstMarker) {
		t.Fatalf("expected the event before the long line to reach the client, got %q", got)
	}

	if !strings.Contains(got, thirdMarker) {
		t.Fatalf("expected the event after the long line to also reach the client, got %q", got)
	}
}

func TestEventStream_LineOverHardCapStillTerminatesStream(t *testing.T) {
	huge := strings.Repeat("A", maxSSELineSize+1)

	var body bytes.Buffer
	body.WriteString("data: " + huge + "\n\n")
	body.WriteString("data: {\"tag\":\"unreachable\"}\n\n")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body.Bytes())

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer upstream.Close()

	s := quietTestSnooper(t, upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/eth/v1/events?topics=head", nil)
	rec := &flushRecorder{httptest.NewRecorder()}

	done := make(chan struct{})

	go func() {
		s.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP never returned")
	}

	if strings.Contains(rec.Body.String(), "unreachable") {
		t.Fatal("a line over the hard cap should still terminate the stream")
	}
}
