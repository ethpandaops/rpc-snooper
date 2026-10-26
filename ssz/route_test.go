package ssz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const amsterdam = "amsterdam"

func TestMatchRoute(t *testing.T) {
	tests := []struct {
		path       string
		fork       string
		wantMethod string
		wantNil    bool
	}{
		{"/engine/v1/payloads", amsterdam, "engine_newPayload[amsterdam]", false},
		{"/engine/v1/payloads/0x03af07f1bba00268", amsterdam, "engine_getPayload[amsterdam]", false},
		{"/engine/v1/forkchoice", "osaka", "engine_forkchoiceUpdated[osaka]", false},
		{"/engine/v1/bodies/hash", "cancun", "engine_getPayloadBodiesByHash[cancun]", false},
		{"/engine/v1/bodies", amsterdam, "engine_getPayloadBodiesByRange[amsterdam]", false},
		{"/engine/v1/payloads", "", "engine_newPayload", false},
		{"/engine/v1/blobs/v1", "", "engine_getBlobsV1", false},
		{"/engine/v1/blobs/v4", amsterdam, "engine_getBlobsV4", false},
		{"/engine/v1/identity", amsterdam, "engine_getClientVersionV1", false},
		{"/engine/v1/capabilities", "", "engine_exchangeCapabilities", false},
		{"/", "", "", true},
		{"/eth/v1/events", "", "", true},
		{"/engine/invalid", "", "", true},
		{"/engine/v2/payloads", amsterdam, "", true},
		{"/engine/v1/amsterdam/payloads", "", "", true},
		{"/engine/v1/blobs", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"#"+tt.fork, func(t *testing.T) {
			info := MatchRoute(tt.path, tt.fork)
			if tt.wantNil {
				assert.Nil(t, info)
			} else {
				require.NotNil(t, info)
				assert.Equal(t, tt.wantMethod, info.Method)
			}
		})
	}
}
