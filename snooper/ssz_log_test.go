package snooper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestFormatSSZForLog verifies that SSZ bodies are rendered as hex (so the
// on-wire content is visible) and tagged with the resolved Engine API method.
func TestFormatSSZForLog(t *testing.T) {
	s := &Snooper{logger: logrus.New()}

	payloadsReq := httptest.NewRequest("POST", "/engine/v1/payloads", http.NoBody)
	payloadsReq.Header.Set("Eth-Execution-Version", "amsterdam")

	t.Run("known route: hex body + method-tagged type", func(t *testing.T) {
		fields := logrus.Fields{}
		s.formatSSZForLog([]byte{0x03, 0x00, 0x00, 0x00, 0xab}, payloadsReq, fields)

		assert.Equal(t, "ssz (engine_newPayload[amsterdam])", fields["type"])
		assert.Equal(t, "0x03000000ab", fields["body"])
	})

	t.Run("unknown route: still hex body", func(t *testing.T) {
		fields := logrus.Fields{}
		s.formatSSZForLog([]byte{0xde, 0xad, 0xbe, 0xef}, httptest.NewRequest("POST", "/engine/v9/whatever", http.NoBody), fields)

		assert.Equal(t, "ssz", fields["type"])
		assert.Equal(t, "0xdeadbeef", fields["body"])
	})

	t.Run("truncation previews large bodies with byte count", func(t *testing.T) {
		st := &Snooper{logger: logrus.New(), logTruncationEnabled: true}
		fields := logrus.Fields{}
		st.formatSSZForLog(make([]byte, 4096), payloadsReq, fields)

		body, ok := fields["body"].(string)
		assert.True(t, ok)
		assert.True(t, strings.HasPrefix(body, "0x"))
		assert.Contains(t, body, "<4096 bytes>")
	})
}
