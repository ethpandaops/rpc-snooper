package snooper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ethpandaops/rpc-snooper/xatu"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

const (
	// refreshInterval is how often to refresh execution metadata.
	refreshInterval = 60 * time.Second

	// fetchTimeout is the timeout for fetching execution metadata.
	fetchTimeout = 5 * time.Second

	// initialRetryDelay is the initial delay between retries.
	initialRetryDelay = 2 * time.Second

	// maxRetryDelay is the maximum delay between retries.
	maxRetryDelay = 30 * time.Second
)

// ExecutionMetadataFetcher manages fetching and caching execution client metadata.
type ExecutionMetadataFetcher struct {
	targetURL  *url.URL
	jwtSecret  []byte
	log        logrus.FieldLogger
	httpClient *http.Client

	mu       sync.RWMutex
	metadata *xatu.ExecutionMetadata

	// ready signals when initial metadata has been fetched
	ready     chan struct{}
	readyOnce sync.Once

	// done signals shutdown
	done chan struct{}
	wg   sync.WaitGroup
}

// NewExecutionMetadataFetcher creates a new ExecutionMetadataFetcher.
func NewExecutionMetadataFetcher(targetURL *url.URL, jwtSecret string, log logrus.FieldLogger) *ExecutionMetadataFetcher {
	secret := ParseJWTSecret(jwtSecret, log)

	return &ExecutionMetadataFetcher{
		targetURL: targetURL,
		jwtSecret: secret,
		log:       log.WithField("component", "execution_metadata"),
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// createJWTToken creates a JWT token for Engine API authentication.
func (f *ExecutionMetadataFetcher) createJWTToken() (string, error) {
	if len(f.jwtSecret) == 0 {
		return "", fmt.Errorf("no JWT secret configured")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(f.jwtSecret)
}

// Start begins fetching execution metadata. It blocks until initial metadata
// is successfully fetched (with retries) or the context is cancelled.
func (f *ExecutionMetadataFetcher) Start(ctx context.Context) error {
	// Fetch initial metadata with retries
	if err := f.fetchWithRetries(ctx); err != nil {
		return fmt.Errorf("failed to fetch initial execution metadata: %w", err)
	}

	// Signal ready
	f.readyOnce.Do(func() {
		close(f.ready)
	})

	// Start background refresh goroutine
	f.wg.Add(1)

	go f.refreshLoop(ctx)

	return nil
}

// Stop gracefully shuts down the fetcher.
func (f *ExecutionMetadataFetcher) Stop() {
	close(f.done)
	f.wg.Wait()
}

// Ready returns a channel that is closed when initial metadata is available.
func (f *ExecutionMetadataFetcher) Ready() <-chan struct{} {
	return f.ready
}

// Get returns the current execution metadata.
func (f *ExecutionMetadataFetcher) Get() *xatu.ExecutionMetadata {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return f.metadata
}

// Update updates the cached metadata from an observed engine_getClientVersionV1 response.
// This is used for passive observation when the CL calls this method.
func (f *ExecutionMetadataFetcher) Update(versions []xatu.ClientVersionV1) {
	if len(versions) == 0 {
		return
	}

	// Use the first client version (in case of multiplexer, we just take the first)
	cv := versions[0]
	metadata := f.parseClientVersion(cv)

	f.mu.Lock()
	f.metadata = metadata
	f.mu.Unlock()

	f.log.WithFields(logrus.Fields{
		"implementation": metadata.Implementation,
		"version":        metadata.Version,
	}).Debug("updated execution metadata from observed response")
}

// fetchWithRetries attempts to fetch metadata with retries indefinitely.
// Uses exponential backoff up to maxRetryDelay.
func (f *ExecutionMetadataFetcher) fetchWithRetries(ctx context.Context) error {
	delay := initialRetryDelay
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.done:
			return fmt.Errorf("fetcher stopped")
		default:
		}

		attempt++

		if err := f.fetch(ctx); err != nil {
			f.log.WithError(err).WithFields(logrus.Fields{
				"attempt":    attempt,
				"next_retry": delay,
			}).Warn("failed to fetch execution metadata, retrying...")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.done:
				return fmt.Errorf("fetcher stopped")
			case <-time.After(delay):
			}

			// Exponential backoff with cap
			delay *= 2
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}

			continue
		}

		return nil
	}
}

// fetch performs a single fetch of execution metadata.
func (f *ExecutionMetadataFetcher) fetch(ctx context.Context) error {
	// Build JSON-RPC request for engine_getClientVersionV1
	// The method takes a ClientVersionV1 param identifying the caller
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "engine_getClientVersionV1",
		"params": []any{
			xatu.ClientVersionV1{
				Code:    "RS", // rpc-snooper
				Name:    "rpc-snooper",
				Version: "v0.0.0",
				Commit:  "00000000",
			},
		},
		"id": 1,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.targetURL.String(), bytes.NewReader(reqBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add JWT auth header if we have a secret
	if len(f.jwtSecret) > 0 {
		token, err := f.createJWTToken()
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse JSON-RPC response
	var rpcResp struct {
		Result []xatu.ClientVersionV1 `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if len(rpcResp.Result) == 0 {
		return fmt.Errorf("empty result from engine_getClientVersionV1")
	}

	// Parse and store metadata
	cv := rpcResp.Result[0]
	metadata := f.parseClientVersion(cv)

	f.mu.Lock()
	f.metadata = metadata
	f.mu.Unlock()

	f.log.WithFields(logrus.Fields{
		"implementation": metadata.Implementation,
		"version":        metadata.Version,
	}).Info("fetched execution metadata")

	return nil
}

// parseClientVersion converts a ClientVersionV1 to ExecutionMetadata.
func (f *ExecutionMetadataFetcher) parseClientVersion(cv xatu.ClientVersionV1) *xatu.ExecutionMetadata {
	// Parse version string (e.g., "v1.14.0" or "1.14.0")
	version := cv.Version
	versionMajor, versionMinor, versionPatch := parseVersion(version)

	return &xatu.ExecutionMetadata{
		Implementation: cv.Name,
		Version:        version,
		VersionMajor:   versionMajor,
		VersionMinor:   versionMinor,
		VersionPatch:   versionPatch,
	}
}

// parseVersion parses a version string into major, minor, patch components.
func parseVersion(version string) (major, minor, patch string) {
	if version == "" {
		return "", "", ""
	}

	// Remove 'v' prefix if present
	if version[0] == 'v' {
		version = version[1:]
	}

	// Split by '-' or '+' to get core version
	coreVersion := version

	for i, c := range version {
		if c == '-' || c == '+' {
			coreVersion = version[:i]

			break
		}
	}

	// Split by '.' to get major.minor.patch
	parts := splitBy(coreVersion, '.')
	if len(parts) > 0 {
		major = parts[0]
	}

	if len(parts) > 1 {
		minor = parts[1]
	}

	if len(parts) > 2 {
		patch = parts[2]
	}

	return major, minor, patch
}

// splitBy splits a string by a delimiter.
func splitBy(s string, delim rune) []string {
	var parts []string

	start := 0

	for i, c := range s {
		if c == delim {
			if i > start {
				parts = append(parts, s[start:i])
			}

			start = i + 1
		}
	}

	if start < len(s) {
		parts = append(parts, s[start:])
	}

	return parts
}

// refreshLoop periodically refreshes execution metadata.
func (f *ExecutionMetadataFetcher) refreshLoop(ctx context.Context) {
	defer f.wg.Done()

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-ticker.C:
			if err := f.fetch(ctx); err != nil {
				f.log.WithError(err).Warn("failed to refresh execution metadata")
			}
		}
	}
}
