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

// String returns a web3_clientVersion-style string for compatibility with parsers.
// The format is Name/Version, where Version includes the commit hash if not already present.
func (c ClientVersionV1) String() string {
	version := c.Version

	// Normalize commit by stripping 0x prefix for comparison
	commitNorm := strings.TrimPrefix(c.Commit, "0x")

	// If commit is not empty and not already embedded in version, append it
	if commitNorm != "" && !strings.Contains(c.Version, commitNorm) {
		version = fmt.Sprintf("%s-%s", c.Version, c.Commit)
	}

	return fmt.Sprintf("%s/%s", c.Name, version)
}
