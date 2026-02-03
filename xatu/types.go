package xatu

import (
	"fmt"
	"strings"

	xatu "github.com/ethpandaops/xatu/pkg/proto/xatu"
)

// ExecutionMetadata holds cached execution client information.
type ExecutionMetadata struct {
	Implementation string
	Version        string
	VersionMajor   string
	VersionMinor   string
	VersionPatch   string
}

// ToProto converts the metadata to the xatu proto format.
func (m *ExecutionMetadata) ToProto() *xatu.ClientMeta_Ethereum_Execution {
	if m == nil {
		return nil
	}

	return &xatu.ClientMeta_Ethereum_Execution{
		Implementation: m.Implementation,
		Version:        m.Version,
		VersionMajor:   m.VersionMajor,
		VersionMinor:   m.VersionMinor,
		VersionPatch:   m.VersionPatch,
	}
}

// ClientVersionV1 represents the response from engine_getClientVersionV1.
// See: https://github.com/ethereum/execution-apis/blob/main/src/engine/identification.md
type ClientVersionV1 struct {
	Code    string `json:"code"`    // 2-letter client code (e.g., "GE" for Geth)
	Name    string `json:"name"`    // Human-readable name (e.g., "Geth")
	Version string `json:"version"` // Version string (e.g., "v1.14.0")
	Commit  string `json:"commit"`  // 4-byte commit hash
}

// String returns a web3_clientVersion-style string (Name/Version).
func (c ClientVersionV1) String() string {
	version := c.Version
	commitNorm := strings.TrimPrefix(c.Commit, "0x")

	if commitNorm != "" && !commitContainedInVersion(c.Version, commitNorm) {
		version = fmt.Sprintf("%s-%s", c.Version, c.Commit)
	}

	return fmt.Sprintf("%s/%s", c.Name, version)
}

// commitContainedInVersion checks if the commit (or a prefix) is already in the version.
func commitContainedInVersion(version, commit string) bool {
	if strings.Contains(version, commit) {
		return true
	}

	const minPrefixLen = 6
	if len(commit) >= minPrefixLen && strings.Contains(version, commit[:minPrefixLen]) {
		return true
	}

	return false
}
