package snooper

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethpandaops/rpc-snooper/metrics"
	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type outcomeLogs struct {
	mu      sync.Mutex
	entries []logrus.Fields
}

func (*outcomeLogs) Levels() []logrus.Level { return logrus.AllLevels }

func (h *outcomeLogs) Fire(entry *logrus.Entry) error {
	if entry.Message == "CALL-OUTCOME" {
		h.mu.Lock()
		h.entries = append(h.entries, entry.Data)
		h.mu.Unlock()
	}

	return nil
}

func (h *outcomeLogs) snapshot() []logrus.Fields {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]logrus.Fields(nil), h.entries...)
}

func newOutcomeTestSnooper(t *testing.T, target string) (*Snooper, *prometheus.Registry, *outcomeLogs) {
	t.Helper()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	hook := &outcomeLogs{}
	logger.AddHook(hook)
	s, err := NewSnooper(target, logger, nil, "")
	require.NoError(t, err)
	t.Cleanup(s.Shutdown)

	registry := prometheus.NewRegistry()
	s.callOutcomeMetrics = metrics.NewCallOutcomeMetrics(registry)
	s.metricsEnabled = true

	return s, registry, hook
}

func assertOutcomes(t *testing.T, registry *prometheus.Registry, expected map[string]uint64, started uint64) {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)

	counts := make(map[string]uint64)
	durations := make(map[string]uint64)

	var actualStarted uint64

	for _, family := range families {
		for _, metric := range family.Metric {
			outcome := ""

			for _, label := range metric.Label {
				require.Contains(t, []string{"server", "outcome"}, label.GetName())

				if label.GetName() == "outcome" {
					outcome = label.GetValue()
				}
			}

			switch family.GetName() {
			case "rpc_snooper_calls_started_total":
				actualStarted += uint64(metric.GetCounter().GetValue())
			case "rpc_snooper_call_outcomes_total":
				counts[outcome] += uint64(metric.GetCounter().GetValue())
			case "rpc_snooper_call_elapsed_seconds":
				durations[outcome] += metric.GetHistogram().GetSampleCount()
			}
		}
	}

	require.Equal(t, started, actualStarted)
	require.Equal(t, expected, counts)
	require.Equal(t, expected, durations, "one elapsed observation per terminal outcome")
}

func TestProxyCallCanceledAndSuccessfulRetryAreBothObserved(t *testing.T) {
	accepted := make(chan struct{})

	var calls int

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		calls++
		if calls == 1 {
			close(accepted)
			<-r.Context().Done()

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`)
	}))
	defer upstream.Close()

	s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
	responses := make(chan *types.ResponseContext, 2)
	module := &durationCapturingModule{
		id: s.moduleManager.GenerateModuleID(),
		onResponse: func(ctx *types.ResponseContext) {
			responses <- ctx
		},
	}
	require.NoError(t, s.moduleManager.RegisterModule(module, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		s.ServeHTTP(httptest.NewRecorder(), req)
	}()
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not accept the request")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled proxy call did not terminate")
	}

	retry := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`))
	retry.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, retry)
	require.Equal(t, http.StatusOK, rec.Code)
	select {
	case response := <-responses:
		require.Equal(t, uint64(2), response.CallCtx.ID(), "only the successful retry has an actual response")
	case <-time.After(2 * time.Second):
		t.Fatal("retry response was not observed")
	}

	assertOutcomes(t, registry, map[string]uint64{callOutcomeCanceled: 1, callOutcomeCompleted: 1}, 2)

	entries := hook.snapshot()
	require.Len(t, entries, 2)
	require.Equal(t, uint64(1), entries[0]["callidx"])
	require.Equal(t, callOutcomeCanceled, entries[0]["outcome"])
	require.Equal(t, uint64(2), entries[1]["callidx"])
	require.Equal(t, callOutcomeCompleted, entries[1]["outcome"])
}

func TestProxyCallDeadlineOutcomes(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		t.Run(strconv.FormatBool(parentDeadline), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			defer upstream.Close()

			s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
			s.CallTimeout = 50 * time.Millisecond
			ctx := context.Background()

			if parentDeadline {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()

				s.CallTimeout = time.Second
			}

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")).WithContext(ctx)
			s.ServeHTTP(httptest.NewRecorder(), req)
			assertOutcomes(t, registry, map[string]uint64{callOutcomeDeadlineExceeded: 1}, 1)

			entries := hook.snapshot()
			require.Len(t, entries, 1)
			elapsed, ok := entries[0]["elapsed_ms"].(float64)
			require.True(t, ok)
			require.GreaterOrEqual(t, elapsed, float64(40))
		})
	}
}

func TestProxyCallTransportAndPartialResponseErrors(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		target := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())
		s, registry, hook := newOutcomeTestSnooper(t, target)
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		assertOutcomes(t, registry, map[string]uint64{callOutcomeRequestError: 1}, 1)
		require.Len(t, hook.snapshot(), 1)
	})

	t.Run("partial_body", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)

			w.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(w, "short")
		}))
		defer upstream.Close()

		s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		assertOutcomes(t, registry, map[string]uint64{callOutcomeResponseError: 1}, 1)
		require.Len(t, hook.snapshot(), 1)
	})
}

func TestProxyCallOutcomeMetricsRespectDisabledFlag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
	s.metricsEnabled = false
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
	require.Len(t, hook.snapshot(), 1, "logging remains available with metrics disabled")
}

func TestProxyCallCancellationDuringResponseBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "partial")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
	s.CallTimeout = 50 * time.Millisecond
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Contains(t, rec.Body.String(), "partial")
	assertOutcomes(t, registry, map[string]uint64{callOutcomeDeadlineExceeded: 1}, 1)
	require.Len(t, hook.snapshot(), 1)
}

func TestProxyCallHTTPErrorIsCompletedTransfer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "backend unavailable")
	}))
	defer upstream.Close()

	s, registry, hook := newOutcomeTestSnooper(t, upstream.URL)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rec.Code)
	assertOutcomes(t, registry, map[string]uint64{callOutcomeCompleted: 1}, 1)
	require.Len(t, hook.snapshot(), 1)
}

func TestProxyCallCustomCancellationCause(t *testing.T) {
	s, registry, hook := newOutcomeTestSnooper(t, "http://127.0.0.1:1")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("caller stopped polling"))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	s.ServeHTTP(httptest.NewRecorder(), req)
	assertOutcomes(t, registry, map[string]uint64{callOutcomeCanceled: 1}, 1)
	require.Len(t, hook.snapshot(), 1)
}

func TestDisabledProxyFlowHasNoCallOutcome(t *testing.T) {
	s, registry, hook := newOutcomeTestSnooper(t, "http://127.0.0.1:1")
	s.flowEnabled = false
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
	require.Empty(t, hook.snapshot())
}
