package metrics

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/prometheus/client_golang/prometheus"
)

// The bounded set must pass known values and values under the cap, and collapse
// everything beyond the cap to the overflow placeholder.
func TestBoundedLabelSetCapsDistinctValues(t *testing.T) {
	set := newBoundedLabelSet()

	for i := 0; i < maxDistinctLabelValues; i++ {
		v := fmt.Sprintf("v%d", i)
		if got := set.value(v); got != v {
			t.Fatalf("value %q under the cap returned %q", v, got)
		}
	}

	// A new value beyond the cap overflows.
	if got := set.value("overflow-me"); got != overflowLabel {
		t.Fatalf("expected %q past the cap, got %q", overflowLabel, got)
	}

	// An already-seen value still passes after the cap is reached.
	if got := set.value("v0"); got != "v0" {
		t.Fatalf("known value should still pass, got %q", got)
	}
}

// distinctURIValues counts how many distinct uri label values exist across the
// request vectors in the default registry.
func distinctURIValues(t *testing.T) int {
	t.Helper()

	fams, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	seen := map[string]struct{}{}

	for _, f := range fams {
		if f.GetName() != "ngx_request_count" {
			continue
		}

		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "uri" {
					seen[lp.GetValue()] = struct{}{}
				}
			}
		}
	}

	return len(seen)
}

// Registering far more unique URIs than the cap must not create an unbounded
// number of uri label values: the total distinct values stays within the bound.
func TestPrometheusMetricsRegisterBoundsURICardinality(t *testing.T) {
	for i := 0; i < 5000; i++ {
		PrometheusMetricsRegister(&MetricsEntry{
			Server: "t", Scheme: "http", Method: "POST", Hostname: "h", Status: "200",
			URI:        fmt.Sprintf("/path-%d", i),
			JRPCMethod: "eth_call",
		})
	}

	distinct := distinctURIValues(t)
	t.Logf("registered 5000 unique URIs; distinct uri label values retained: %d (cap %d + overflow)", distinct, maxDistinctLabelValues)

	if distinct > maxDistinctLabelValues+1 {
		t.Fatalf("uri cardinality unbounded: %d distinct values exceed cap %d+1", distinct, maxDistinctLabelValues)
	}
}

// The uri label must not include the raw query string.
func TestCreateMetricsEntryDropsQueryString(t *testing.T) {
	target, _ := url.Parse("http://upstream:8551")
	reqURL, _ := url.Parse("http://proxy/some/path?evil=1&more=2")

	entry := CreateMetricsEntryFromContexts(target, &types.RequestContext{
		Method: "POST",
		URL:    reqURL,
	}, nil)

	if entry.URI != "/some/path" {
		t.Fatalf("expected uri without query string, got %q", entry.URI)
	}
}
