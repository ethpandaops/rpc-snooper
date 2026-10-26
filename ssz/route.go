package ssz

import (
	"regexp"
)

// EndpointInfo holds the resolved Engine API method name for a
// matched REST URL path.
type EndpointInfo struct {
	Method string
}

type routePattern struct {
	re         *regexp.Regexp
	method     string
	forkScoped bool
}

var routes = []routePattern{
	{re: regexp.MustCompile(`^/engine/v1/payloads$`), method: "engine_newPayload", forkScoped: true},
	{re: regexp.MustCompile(`^/engine/v1/payloads/[0-9a-fA-Fx]+$`), method: "engine_getPayload", forkScoped: true},
	{re: regexp.MustCompile(`^/engine/v1/forkchoice$`), method: "engine_forkchoiceUpdated", forkScoped: true},
	{re: regexp.MustCompile(`^/engine/v1/bodies/hash$`), method: "engine_getPayloadBodiesByHash", forkScoped: true},
	{re: regexp.MustCompile(`^/engine/v1/bodies$`), method: "engine_getPayloadBodiesByRange", forkScoped: true},
	{re: regexp.MustCompile(`^/engine/v1/blobs/v([1-9]\d*)$`), method: "engine_getBlobsV"},
	{re: regexp.MustCompile(`^/engine/v1/identity$`), method: "engine_getClientVersionV1"},
	{re: regexp.MustCompile(`^/engine/v1/capabilities$`), method: "engine_exchangeCapabilities"},
}

// MatchRoute resolves a REST+SSZ Engine API URL path and its
// Eth-Execution-Version header value to an Engine API method name.
// Returns nil if the path does not match any known endpoint.
func MatchRoute(path, fork string) *EndpointInfo {
	for i := range routes {
		matches := routes[i].re.FindStringSubmatch(path)
		if matches == nil {
			continue
		}

		method := routes[i].method
		if len(matches) > 1 {
			method += matches[1]
		}

		if routes[i].forkScoped && fork != "" {
			method += "[" + fork + "]"
		}

		return &EndpointInfo{Method: method}
	}

	return nil
}
