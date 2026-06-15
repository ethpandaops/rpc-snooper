package ssz

import (
	"fmt"
	"regexp"
	"strconv"
)

// EndpointInfo holds the resolved Engine API method name for a
// matched REST URL path.
type EndpointInfo struct {
	Method string
}

type routePattern struct {
	re      *regexp.Regexp
	builder func(version int, matches []string) *EndpointInfo
}

var routes []routePattern

func init() {
	routes = []routePattern{
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/payloads/bodies/by-hash$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_getPayloadBodiesByHashV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/payloads/bodies/by-range$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_getPayloadBodiesByRangeV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/payloads/([0-9a-fA-Fx]+)$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_getPayloadV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(`^/engine/v(\d+)/payloads$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_newPayloadV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(`^/engine/v(\d+)/forkchoice$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_forkchoiceUpdatedV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(`^/engine/v(\d+)/blobs$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_getBlobsV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/client/version$`),
			builder: func(v int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: fmt.Sprintf(
						"engine_getClientVersionV%d", v),
				}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/capabilities$`),
			builder: func(_ int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: "engine_exchangeCapabilities",
				}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/transition-configuration$`),
			builder: func(_ int, _ []string) *EndpointInfo {
				return &EndpointInfo{
					Method: "engine_exchangeTransitionConfigurationV1",
				}
			},
		},

		// execution-apis #793 fork-scoped REST endpoints
		// (e.g. /engine/v2/amsterdam/payloads). Group 1 is the REST API
		// version, group 2 is the fork name. Listed after the version-only
		// routes above so the older #764 paths keep precedence.
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/([a-z0-9]+)/payloads/[0-9a-fA-Fx]+$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_getPayload[%s]", m[2])}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/([a-z0-9]+)/payloads$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_newPayload[%s]", m[2])}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/([a-z0-9]+)/forkchoice$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_forkchoiceUpdated[%s]", m[2])}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/([a-z0-9]+)/bodies/by-hash$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_getPayloadBodiesByHash[%s]", m[2])}
			},
		},
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/([a-z0-9]+)/bodies$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_getPayloadBodiesByRange[%s]", m[2])}
			},
		},
		// #793 independently-versioned blobs: /engine/v2/blobs/v1 .. v4.
		// The only capture group is the blobs version.
		{
			re: regexp.MustCompile(
				`^/engine/v\d+/blobs/v(\d+)$`),
			builder: func(_ int, m []string) *EndpointInfo {
				return &EndpointInfo{Method: fmt.Sprintf("engine_getBlobsV%s", m[1])}
			},
		},
		// #793 unscoped identity endpoint.
		{
			re: regexp.MustCompile(
				`^/engine/v(\d+)/identity$`),
			builder: func(_ int, _ []string) *EndpointInfo {
				return &EndpointInfo{Method: "engine_getClientVersionV1"}
			},
		},
	}
}

// MatchRoute resolves a URL path to its Engine API method name.
// Returns nil if the path does not match any known SSZ REST endpoint.
func MatchRoute(path string) *EndpointInfo {
	for i := range routes {
		matches := routes[i].re.FindStringSubmatch(path)
		if matches == nil {
			continue
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		return routes[i].builder(version, matches)
	}

	return nil
}
