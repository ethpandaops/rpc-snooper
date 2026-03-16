package ssz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchRoute(t *testing.T) {
	tests := []struct {
		path       string
		wantMethod string
		wantNil    bool
	}{
		{"/engine/v3/payloads", "engine_newPayloadV3", false},
		{"/engine/v5/payloads", "engine_newPayloadV5", false},
		{"/engine/v6/payloads/0x1234567890abcdef",
			"engine_getPayloadV6", false},
		{"/engine/v3/forkchoice",
			"engine_forkchoiceUpdatedV3", false},
		{"/engine/v2/blobs", "engine_getBlobsV2", false},
		{"/engine/v1/client/version",
			"engine_getClientVersionV1", false},
		{"/engine/v1/capabilities",
			"engine_exchangeCapabilities", false},
		{"/engine/v1/payloads/bodies/by-hash",
			"engine_getPayloadBodiesByHashV1", false},
		{"/engine/v1/payloads/bodies/by-range",
			"engine_getPayloadBodiesByRangeV1", false},
		{"/engine/v1/transition-configuration",
			"engine_exchangeTransitionConfigurationV1", false},
		{"/", "", true},
		{"/eth/v1/events", "", true},
		{"/engine/invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			info := MatchRoute(tt.path)
			if tt.wantNil {
				assert.Nil(t, info)
			} else {
				require.NotNil(t, info)
				assert.Equal(t, tt.wantMethod, info.Method)
			}
		})
	}
}
