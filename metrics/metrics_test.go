package metrics

import "testing"

func TestRouteFromPath(t *testing.T) {
	tests := map[string]string{
		"/":                         "/",
		"/eth/v1/node/version":      "/eth/v1/node/version",
		"/eth/v2/beacon/blocks/123": "/eth/v2/beacon/blocks/{id}",
		"/eth/v1/beacon/states/head/validators/0xabc": "/eth/v1/beacon/states/head/validators/{id}",
		"/eth/v1/validator/duties/attester/42":        "/eth/v1/validator/duties/attester/{id}",
		"/eth/v2/validator/aggregate_attestation":     "/eth/v2/validator/aggregate_attestation",
	}

	for path, want := range tests {
		if got := routeFromPath(path); got != want {
			t.Errorf("routeFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}
