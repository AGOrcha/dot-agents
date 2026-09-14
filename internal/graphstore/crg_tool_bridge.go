package graphstore

import (
	"bytes"
	"context"
	// Blank: the //go:embed directive below needs the embed package linked
	// in, but this file references no identifier from it.
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/execabs"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// crgToolCallScript runs one tool against the retained release IN PROCESS,
// through the release's own MCP server object, and prints exactly one JSON
// document.
//
// Going through the release's own server — rather than re-deriving each
// tool's behaviour from its CLI equivalent — is what makes bridge-routed
// calls byte-faithful: argument handling, the provenance block, truncation
// metadata and error text are all produced by the release itself.
//
//go:embed crg_tool_call.py
var crgToolCallScript string

// crgToolCallTimeout bounds one bridge-routed tool call. The release's own
// long-running tools (build, embed, wiki) are the reason this is generous;
// without any bound a wedged subprocess would hang the MCP session.
const crgToolCallTimeout = 10 * time.Minute

// CRGToolBridge serves tool calls the kg-native backend cannot answer exactly
// by delegating to the retained Python release.
//
// It is the Phase-A rollback path made concrete: the native cutover is the
// default, but every capability the native backend does not reproduce is
// still answered by the release itself rather than approximated.
type CRGToolBridge struct {
	repoRoot string
	// python is the interpreter of the virtualenv the release is installed
	// in, resolved once.
	python string
	// discoverErr records why no interpreter was found.
	discoverErr error
}

var _ ToolBridge = (*CRGToolBridge)(nil)

// NewCRGToolBridge resolves the release's interpreter for repoRoot.
func NewCRGToolBridge(repoRoot string) *CRGToolBridge {
	bridge := &CRGToolBridge{repoRoot: repoRoot}
	crgBin, err := DiscoverCRGBin(repoRoot)
	if err != nil {
		bridge.discoverErr = err
		return bridge
	}
	base := &CRGBridge{Bin: crgBin, RepoRoot: repoRoot}
	// pythonBin falls back to a BARE interpreter name when the virtualenv
	// beside the executable has none, so it never reports absence itself.
	// Resolving that name here is what makes Available() truthful: without
	// it a discovered shim with no interpreter would claim the bridge is
	// usable and then fail on the first call with a raw exec error instead
	// of this actionable one.
	python, err := execabs.LookPath(base.pythonBin())
	if err != nil {
		bridge.discoverErr = fmt.Errorf(
			"found %s but no usable Python interpreter beside it: %w", crgBin, err)
		return bridge
	}
	bridge.python = python
	return bridge
}

// Available reports whether a release interpreter was found.
func (b *CRGToolBridge) Available() bool {
	return b != nil && b.python != ""
}

// Unavailable explains why Available is false.
func (b *CRGToolBridge) Unavailable() string {
	if b == nil {
		return "no bridge configured"
	}
	if b.discoverErr != nil {
		return b.discoverErr.Error()
	}
	if b.python == "" {
		return "no Python interpreter found for the code-review-graph release"
	}
	return ""
}

// bridgeCallResult is the helper script's output contract.
type bridgeCallResult struct {
	IsError           bool                      `json:"is_error"`
	Content           []crgrelease.ContentBlock `json:"content"`
	StructuredContent json.RawMessage           `json:"structured_content"`
	FatalError        string                    `json:"fatal_error"`
}

// CallTool forwards a validated, defaulted argument set to the release and
// returns its MCP result unchanged.
func (b *CRGToolBridge) CallTool(name string, args map[string]any) (crgrelease.CallResult, error) {
	if !b.Available() {
		return crgrelease.CallResult{}, fmt.Errorf("%s", b.Unavailable())
	}
	payload, err := json.Marshal(map[string]any{
		"tool":      name,
		"arguments": args,
		"repo_root": b.repoRoot,
	})
	if err != nil {
		return crgrelease.CallResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), crgToolCallTimeout)
	defer cancel()

	cmd := execabs.CommandContext(ctx, b.python, "-c", crgToolCallScript)
	cmd.Dir = b.repoRoot
	cmd.Stdin = bytes.NewReader(payload)
	// The release logs to stderr; only stdout carries the result document.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Keep the release's own environment but pin the repository so a tool
	// that resolves its root from the process cwd agrees with the caller.
	cmd.Env = append(os.Environ(), "CRG_REPO_ROOT="+b.repoRoot)

	runErr := cmd.Run()
	if ctx.Err() != nil {
		return crgrelease.CallResult{}, fmt.Errorf(
			"%s timed out after %s", name, crgToolCallTimeout)
	}
	if runErr != nil && stdout.Len() == 0 {
		return crgrelease.CallResult{}, fmt.Errorf(
			"%w: %s", runErr, strings.TrimSpace(stderr.String()))
	}

	var decoded bridgeCallResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &decoded); err != nil {
		return crgrelease.CallResult{}, fmt.Errorf(
			"unreadable bridge response: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	if decoded.FatalError != "" {
		return crgrelease.CallResult{}, fmt.Errorf("%s", decoded.FatalError)
	}
	return crgrelease.CallResult{
		Content:           decoded.Content,
		StructuredContent: decoded.StructuredContent,
		IsError:           decoded.IsError,
	}, nil
}
