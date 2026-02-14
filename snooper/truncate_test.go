package snooper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateHexValue(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		truncated bool
	}{
		{
			name:      "short hash (32 bytes) not truncated",
			input:     "0x" + strings.Repeat("ab", 32),
			truncated: false,
		},
		{
			name:      "address (20 bytes) not truncated",
			input:     "0x" + strings.Repeat("cd", 20),
			truncated: false,
		},
		{
			name:      "KZG commitment (48 bytes) not truncated",
			input:     "0x" + strings.Repeat("ef", 48),
			truncated: false,
		},
		{
			name:      "at threshold (127 bytes) not truncated",
			input:     "0x" + strings.Repeat("ab", 127),
			truncated: false,
		},
		{
			name:      "just over threshold truncated",
			input:     "0x" + strings.Repeat("ab", 200),
			truncated: true,
		},
		{
			name:      "blob data (131072 bytes) truncated",
			input:     "0x" + strings.Repeat("ff", 131072),
			truncated: true,
		},
		{
			name:      "non-hex string passes through",
			input:     "hello world this is a long string that exceeds the threshold easily " + strings.Repeat("x", 300),
			truncated: false,
		},
		{
			name:      "no 0x prefix passes through",
			input:     strings.Repeat("ab", 200),
			truncated: false,
		},
		{
			name:      "0x prefix but invalid hex passes through",
			input:     "0x" + strings.Repeat("zz", 200),
			truncated: false,
		},
		{
			name:      "empty string passes through",
			input:     "",
			truncated: false,
		},
		{
			name:      "just 0x passes through",
			input:     "0x",
			truncated: false,
		},
		{
			name:      "0X uppercase prefix also truncated",
			input:     "0X" + strings.Repeat("ab", 200),
			truncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateHexValue(tt.input)

			if tt.truncated {
				assert.NotEqual(t, tt.input, result)
				assert.Contains(t, result, "...")
				assert.Contains(t, result, "bytes>")
				// Must be shorter than original
				assert.Less(t, len(result), len(tt.input))
			} else {
				assert.Equal(t, tt.input, result)
			}
		})
	}
}

func TestTruncateHexValueFormat(t *testing.T) {
	// Verify the exact format of a truncated value.
	input := "0x" + strings.Repeat("ab", 200)
	result := truncateHexValue(input)

	// Should start with 0x + first 8 hex chars
	assert.True(t, strings.HasPrefix(result, "0xabababab"))
	// Should contain byte count
	assert.Contains(t, result, "<200 bytes>")
	// Should contain ellipsis
	assert.Contains(t, result, "...")
	// Should end with last 8 hex chars + byte count
	assert.True(t, strings.HasSuffix(result, "abababab <200 bytes>"))
}

func TestTruncateHexInTree(t *testing.T) {
	longHex := "0x" + strings.Repeat("ff", 200)
	shortHash := "0x" + strings.Repeat("ab", 32)

	tests := []struct {
		name  string
		input any
		check func(t *testing.T, result any)
	}{
		{
			name: "flat map with mixed values",
			input: map[string]any{
				"hash":     shortHash,
				"blobData": longHex,
				"number":   float64(42),
				"null":     nil,
				"flag":     true,
			},
			check: func(t *testing.T, result any) {
				t.Helper()

				m, ok := result.(map[string]any)
				require.True(t, ok)
				// Short hash preserved
				assert.Equal(t, shortHash, m["hash"])
				// Long hex truncated
				assert.NotEqual(t, longHex, m["blobData"])
				assert.Contains(t, m["blobData"], "bytes>")
				// Non-string values preserved
				assert.Equal(t, float64(42), m["number"])
				assert.Nil(t, m["null"])
				assert.Equal(t, true, m["flag"])
			},
		},
		{
			name: "array of hex values",
			input: []any{
				shortHash,
				longHex,
				"not hex",
			},
			check: func(t *testing.T, result any) {
				t.Helper()

				arr, ok := result.([]any)
				require.True(t, ok)
				require.Len(t, arr, 3)
				assert.Equal(t, shortHash, arr[0])
				assert.NotEqual(t, longHex, arr[1])
				assert.Contains(t, arr[1], "bytes>")
				assert.Equal(t, "not hex", arr[2])
			},
		},
		{
			name: "deeply nested structure",
			input: map[string]any{
				"result": map[string]any{
					"block": map[string]any{
						"transactions": []any{
							map[string]any{
								"input": longHex,
								"hash":  shortHash,
							},
						},
					},
				},
			},
			check: func(t *testing.T, result any) {
				t.Helper()

				r, ok := result.(map[string]any)
				require.True(t, ok)
				block, ok := r["result"].(map[string]any)
				require.True(t, ok)
				blk, ok := block["block"].(map[string]any)
				require.True(t, ok)
				txs, ok := blk["transactions"].([]any)
				require.True(t, ok)
				require.Len(t, txs, 1)
				tx, ok := txs[0].(map[string]any)
				require.True(t, ok)

				assert.Equal(t, shortHash, tx["hash"])
				assert.NotEqual(t, longHex, tx["input"])
				assert.Contains(t, tx["input"], "bytes>")
			},
		},
		{
			name:  "nil input",
			input: nil,
			check: func(t *testing.T, result any) {
				t.Helper()
				assert.Nil(t, result)
			},
		},
		{
			name:  "scalar string",
			input: longHex,
			check: func(t *testing.T, result any) {
				t.Helper()

				s, ok := result.(string)
				require.True(t, ok)
				assert.Contains(t, s, "bytes>")
			},
		},
		{
			name:  "scalar number",
			input: float64(123),
			check: func(t *testing.T, result any) {
				t.Helper()
				assert.Equal(t, float64(123), result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateHexInTree(tt.input)
			tt.check(t, result)
		})
	}
}

func TestTruncateHexInTreeDoesNotMutateInput(t *testing.T) {
	longHex := "0x" + strings.Repeat("ff", 200)

	input := map[string]any{
		"data": longHex,
	}

	_ = truncateHexInTree(input)

	// Original must be unchanged.
	assert.Equal(t, longHex, input["data"])
}
