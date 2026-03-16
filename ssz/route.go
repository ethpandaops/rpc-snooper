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
