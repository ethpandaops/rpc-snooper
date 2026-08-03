package snooper

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestParseAPIAuth_ValidEntries(t *testing.T) {
	auth, err := parseAPIAuth("admin:secret,operator:pass")
	if err != nil {
		t.Fatalf("parseAPIAuth: %v", err)
	}

	if auth["admin"] != "secret" || auth["operator"] != "pass" {
		t.Fatalf("unexpected credential map: %v", auth)
	}
}

func TestParseAPIAuth_MissingColonIsRejected(t *testing.T) {
	if _, err := parseAPIAuth("adminsecret"); err == nil {
		t.Fatal("expected an error for an entry missing the colon separator, got nil")
	}
}

func TestParseAPIAuth_OneBadEntryRejectsTheWholeConfig(t *testing.T) {
	// One well-formed entry alongside one malformed entry must not silently
	// keep only the good one: the whole config is rejected so the operator
	// finds out at startup, not by testing every user later.
	if _, err := parseAPIAuth("admin:secret,operatorpass"); err == nil {
		t.Fatal("expected an error when any entry in the list is malformed, got nil")
	}
}

func TestParseAPIAuth_EmptyUsernameIsRejected(t *testing.T) {
	if _, err := parseAPIAuth(":secret"); err == nil {
		t.Fatal("expected an error for an entry with an empty username, got nil")
	}
}

func quietTestSnooperAuth(t *testing.T, target string) *Snooper {
	lg := logrus.New()
	lg.SetOutput(io.Discard)
	lg.SetLevel(logrus.InfoLevel)

	s, err := NewSnooper(target, lg, nil, "")
	if err != nil {
		t.Fatalf("NewSnooper: %v", err)
	}

	return s
}

func freePortAuth(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed reserving a free port: %v", err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return port
}

func waitForServerAuth(t *testing.T, port int) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("API server on port %d never came up", port)
}

func TestStartAPIServer_MalformedAuthConfigFailsToStart(t *testing.T) {
	s := quietTestSnooperAuth(t, "http://127.0.0.1:1")
	port := freePortAuth(t)

	err := s.StartAPIServer("127.0.0.1", port, "adminsecret")
	if err == nil {
		t.Fatal("expected StartAPIServer to return an error for a malformed api-auth config")
	}

	// The server must never come up unauthenticated because of a typo.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Fatal("API server accepted connections despite a rejected auth config")
	}
}

func TestStartAPIServer_ValidAuthConfigStillEnforced(t *testing.T) {
	s := quietTestSnooperAuth(t, "http://127.0.0.1:1")
	port := freePortAuth(t)

	if err := s.StartAPIServer("127.0.0.1", port, "admin:secret"); err != nil {
		t.Fatalf("StartAPIServer: %v", err)
	}

	waitForServerAuth(t, port)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_snooper/status", port))
	if err != nil {
		t.Fatalf("GET /_snooper/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unauthenticated request against a valid auth config, got %d", resp.StatusCode)
	}
}
