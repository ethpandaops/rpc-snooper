package snooper

import (
	"fmt"
	"strings"
)

const (
	// hexTruncateThreshold is the minimum length of a hex string before
	// truncation kicks in. Values at or below this length pass through
	// unchanged. This preserves hashes (66 chars), addresses (42 chars),
	// and KZG commitments/proofs (98 chars).
	hexTruncateThreshold = 256

	// hexTruncatePreviewLen is the number of hex characters shown at
	// each end of a truncated value (after the 0x prefix).
	hexTruncatePreviewLen = 8
)

// truncateHexValue truncates a single hex string if it exceeds the
// threshold. Short hex values (hashes, addresses, KZG proofs) pass
// through unchanged. Non-hex strings are returned as-is.
func truncateHexValue(s string) string {
	if len(s) <= hexTruncateThreshold {
		return s
	}

	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return s
	}

	// Spot-check the first 16 chars after 0x to confirm this looks
	// like hex data — avoids false positives on arbitrary strings.
	check := s[2:]
	if len(check) > 16 {
		check = check[:16]
	}

	for _, c := range check {
		if !isHexChar(c) {
			return s
		}
	}

	// 0x + preview...preview <N bytes>
	// Each pair of hex chars = 1 byte, so byte count = (len - 2) / 2.
	byteCount := (len(s) - 2) / 2
	prefix := s[2 : 2+hexTruncatePreviewLen]
	suffix := s[len(s)-hexTruncatePreviewLen:]

	return fmt.Sprintf("0x%s...%s <%d bytes>", prefix, suffix, byteCount)
}

// truncateHexInTree recursively walks a parsed JSON tree and replaces
// any hex string values that exceed the threshold with a truncated
// placeholder. The input is not modified; a new tree is returned.
func truncateHexInTree(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = truncateHexInTree(child)
		}

		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = truncateHexInTree(child)
		}

		return out
	case string:
		return truncateHexValue(val)
	default:
		return v
	}
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F')
}
