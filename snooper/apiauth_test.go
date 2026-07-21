package snooper

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func authTestSnooper(t *testing.T, target string) *Snooper {
	t.Helper()

	lg := logrus.New()
	lg.SetOutput(io.Discard)

	s, err := NewSnooper(target, lg, nil, "")
	if err != nil {
		t.Fatalf("NewSnooper: %v", err)
	}

	return s
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// With api-auth configured, the control API on the proxy port must reject
// unauthenticated requests and accept correctly authenticated ones.
func TestControlAPIRequiresAuthOnProxyPort(t *testing.T) {
	s := authTestSnooper(t, "http://127.0.0.1:1")
	s.configureAPIAuth("admin:secret")

	handler := s.newRootHandler(false)

	// No credentials -> 401.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_snooper/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: got %d, want 401", rec.Code)
	}

	// Wrong credentials -> 401.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_snooper/status", nil)
	req.Header.Set("Authorization", basicAuth("admin", "wrong"))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credentials: got %d, want 401", rec.Code)
	}

	// Correct credentials -> reaches the handler (not 401).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/_snooper/status", nil)
	req.Header.Set("Authorization", basicAuth("admin", "secret"))
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("correct credentials were rejected: got 401")
	}
}

// The state-changing stop endpoint must not be reachable without credentials.
func TestControlStopRequiresAuth(t *testing.T) {
	s := authTestSnooper(t, "http://127.0.0.1:1")
	s.configureAPIAuth("admin:secret")
	s.flowEnabled = true

	handler := s.newRootHandler(false)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/_snooper/stop", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stop: got %d, want 401", rec.Code)
	}
	if !s.flowEnabled {
		t.Fatalf("unauthenticated stop disabled the proxy flow")
	}
}

// Proxied traffic must never be gated by the control-API credentials.
func TestProxyTrafficNotAuthenticated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	s := authTestSnooper(t, upstream.URL)
	s.configureAPIAuth("admin:secret")

	handler := s.newRootHandler(false)

	// A normal proxied request with no credentials must succeed.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/eth/v1/x", nil))

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("proxied traffic was blocked by control-API auth (401)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("proxied request: got %d, want 200", rec.Code)
	}
}

// Without api-auth configured the control API stays open (unchanged behavior).
func TestControlAPIOpenWhenNoAuthConfigured(t *testing.T) {
	s := authTestSnooper(t, "http://127.0.0.1:1")

	handler := s.newRootHandler(false)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_snooper/status", nil))

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("status endpoint returned 401 with no auth configured")
	}
}
