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
		// execution-apis #793 fork-scoped + unscoped endpoints
		{"/engine/v2/amsterdam/payloads",
			"engine_newPayload[amsterdam]", false},
		{"/engine/v2/amsterdam/payloads/0x03af07f1bba00268",
			"engine_getPayload[amsterdam]", false},
		{"/engine/v2/amsterdam/forkchoice",
			"engine_forkchoiceUpdated[amsterdam]", false},
		{"/engine/v2/amsterdam/bodies/by-hash",
			"engine_getPayloadBodiesByHash[amsterdam]", false},
		{"/engine/v2/amsterdam/bodies",
			"engine_getPayloadBodiesByRange[amsterdam]", false},
		{"/engine/v2/blobs/v3", "engine_getBlobsV3", false},
		{"/engine/v2/identity", "engine_getClientVersionV1", false},
		// the older #764 paths must still take precedence
		{"/engine/v2/blobs", "engine_getBlobsV2", false},
		{"/engine/v6/payloads/0x1234", "engine_getPayloadV6", false},
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
