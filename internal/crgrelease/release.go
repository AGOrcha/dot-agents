// Package crgrelease is the VERSIONED product contract of the
// `code-review-graph` release the kg-native code-graph backend must be
// behaviour- and API-compatible with.
//
// It exists because "parity with the latest CRG" is only checkable if the
// release's surface is data rather than prose: the MCP tool inventory (names,
// descriptions, JSON schemas, defaults, required arguments), the argument
// validation and error semantics, the tools/call response envelope, and the
// per-tool native/bridge capability routing all come from a fixture GENERATED
// from the pinned upstream release (see
// tools/crgrelease/generate_release_contract.py and
// testdata/crg-release/v2.3.8).
//
// The package deliberately depends on nothing in this repository: both the
// native backend (internal/codegraph) and the retained Python bridge
// (internal/graphstore) are measured against it, so it cannot import either.
package crgrelease

import (
	"encoding/json"
	"fmt"
)

// Version is the pinned upstream release this contract describes.
//
// It is not a preference: every fixture under testdata/crg-release/<Version>
// was produced by running that exact release, and the generator refuses to run
// against any other version. Moving the product to a newer CRG means
// regenerating the fixtures and re-verifying the contract tests, not editing
// this constant.
const Version = "2.3.8"

// TagCommit is the upstream git commit `v2.3.8` resolves to. Recording it
// makes the oracle reproducible even if the tag is ever moved.
const TagCommit = "2c6dae32643572ee528eb9b77dbcc17f58f3a8c9"

// SchemaVersion is the graph database schema version the pinned release
// migrates to (`metadata.schema_version`). v9 is the edge-confidence
// migration; a native store that reports anything else is not compatible.
const SchemaVersion = 9

// ServerName / Instructions / ProtocolVersion are the MCP identity the pinned
// release advertises in its `initialize` response. Clients key capability
// decisions off them, so a native server that renames itself is not a drop-in.
const (
	ServerName      = "code-review-graph"
	ProtocolVersion = "2025-06-18"
	Instructions    = "Persistent incremental knowledge graph for token-efficient, " +
		"context-aware code reviews. Parses your codebase with Tree-sitter, " +
		"builds a structural graph, and provides smart impact analysis."
)

// releaseMetadata is the generated companion to the constants above. Keeping
// it as data lets a test prove the constants and the fixtures agree instead of
// trusting that someone updated both.
type releaseMetadata struct {
	Version          string   `json:"version"`
	TagCommit        string   `json:"tag_commit"`
	SchemaVersion    int      `json:"schema_version"`
	FastMCPVersion   string   `json:"fastmcp_version"`
	ProtocolVersion  string   `json:"protocol_version"`
	ToolCount        int      `json:"tool_count"`
	BaseSHA          string   `json:"base_sha"`
	HeadSHA          string   `json:"head_sha"`
	UnexercisedTools []string `json:"unexercised_tools"`
}

// Metadata returns the generated release descriptor: the fixture's own record
// of which upstream release, schema version and MCP protocol version it was
// captured from.
func Metadata() (Release, error) {
	var meta releaseMetadata
	if err := json.Unmarshal(releaseJSON, &meta); err != nil {
		return Release{}, fmt.Errorf("crgrelease: decode release metadata: %w", err)
	}
	return Release{
		Version:         meta.Version,
		TagCommit:       meta.TagCommit,
		SchemaVersion:   meta.SchemaVersion,
		FastMCPVersion:  meta.FastMCPVersion,
		ProtocolVersion: meta.ProtocolVersion,
		ToolCount:       meta.ToolCount,
	}, nil
}

// Release is the public view of the pinned release's identity.
type Release struct {
	Version         string
	TagCommit       string
	SchemaVersion   int
	FastMCPVersion  string
	ProtocolVersion string
	ToolCount       int
}
