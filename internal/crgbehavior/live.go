package crgbehavior

import (
	"fmt"
	"strings"

	"golang.org/x/sys/execabs"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// DefaultDepth is the impact-radius hop budget the review skills query at.
const DefaultDepth = 2

// DefaultMaxResults bounds the bridge's impact query.
const DefaultMaxResults = 2000

// BridgeImpact is the release's own answer to one review task's impact-radius
// query, normalized into the comparison id space.
type BridgeImpact struct {
	// ChangedIDs are the symbols the release resolved for the changed files.
	ChangedIDs []string
	// ImpactedIDs are the symbols the release reported as blast radius.
	ImpactedIDs []string
	// Truncated reports that the release capped its own result set.
	Truncated bool
}

// LiveBridge drives the pinned Python code-review-graph release for ONE
// materialized worktree: it builds the graph, reads the persisted views, issues
// the live impact query, runs the release's own FTS5 search, and probes the
// release's build/postprocess lifecycle contract.
type LiveBridge struct {
	cli     *graphstore.CRGBridge
	root    string
	version string
}

// NewLiveBridge binds the pinned release to graphRoot and verifies that the
// discovered CLI IS the pinned release before anything is built.
//
// Discovery failure returns ErrBridgeUnavailable so the caller can report an
// explicit inconclusive verdict; a CLI that reports a DIFFERENT version returns
// ErrReleaseMismatch, which is a hard failure — running the comparison against
// an unpinned build would certify nothing while looking like evidence.
func NewLiveBridge(graphRoot string, rel Release) (*LiveBridge, error) {
	cli, err := graphstore.NewCRGBridge(graphRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBridgeUnavailable, err)
	}
	version, err := cliVersion(cli.Bin)
	if err != nil {
		return nil, err
	}
	if err := rel.CheckVersion(version); err != nil {
		return nil, err
	}
	return &LiveBridge{cli: cli, root: graphRoot, version: version}, nil
}

// Version is the CLI's own `--version` answer, recorded in every report and
// artifact so a run names the release it was produced against.
func (l *LiveBridge) Version() string { return l.version }

// cliVersion asks the discovered binary which release it is.
func cliVersion(bin string) (string, error) {
	out, err := execabs.Command(bin, "--version").CombinedOutput() //nolint:gosec // bin is a discovered CRG executable
	if err != nil {
		return "", fmt.Errorf("%w: %s --version failed: %v: %s",
			ErrBridgeUnavailable, bin, err, strings.TrimSpace(string(out)))
	}
	return ParseCLIVersion(string(out))
}

// Build runs a FULL build. A full build is the only mode that also computes the
// release's summary tables (community_summaries, flow_snapshots, risk_index),
// so it is the state a review consumer actually reads.
func (l *LiveBridge) Build() error {
	if _, err := l.cli.BuildReport(graphstore.BuildOptions{}); err != nil {
		return fmt.Errorf("crgbehavior: build the pinned release graph at %s: %w", l.root, err)
	}
	return nil
}

// Postprocess runs the release's STANDALONE postprocess — the command whose
// documented behavior (flows/communities/FTS rebuilt, summary tables left
// stale) the lifecycle surface asserts.
func (l *LiveBridge) Postprocess() error {
	if err := l.cli.Postprocess(graphstore.PostprocessOptions{}); err != nil {
		return fmt.Errorf("crgbehavior: postprocess the pinned release graph at %s: %w", l.root, err)
	}
	return nil
}

// Open opens the built graph read-only and probes its schema capabilities.
func (l *LiveBridge) Open(rel Release) (*BridgeStore, error) {
	return OpenBridgeStore(l.root, graphstore.CRGDBPath(l.root), l.version, rel)
}

// ImpactRadius issues the release's own blast-radius query for a review task's
// changed files and normalizes its answer into the comparison id space.
func (l *LiveBridge) ImpactRadius(norm Normalizer, changedFiles []string, maxDepth, maxResults int) (BridgeImpact, error) {
	res, err := l.cli.GetImpactRadius(graphstore.ImpactOptions{
		ChangedFiles: changedFiles,
		MaxDepth:     maxDepth,
		MaxResults:   maxResults,
	})
	if err != nil {
		return BridgeImpact{}, classifyQueryError(err)
	}
	changed, err := nativeIDs(norm, res.ChangedNodes)
	if err != nil {
		return BridgeImpact{}, err
	}
	impacted, err := nativeIDs(norm, res.ImpactedNodes)
	if err != nil {
		return BridgeImpact{}, err
	}
	return BridgeImpact{ChangedIDs: changed, ImpactedIDs: impacted, Truncated: res.Truncated}, nil
}

// interpreterFailures are the signatures of a code-review-graph install whose
// interpreter cannot actually run the bridge — a CLI found on PATH whose
// sibling interpreter lacks the package. That is the SAME environment fact as
// "not installed": the gate cannot be driven, so it reports an inconclusive
// verdict rather than inventing a behavior divergence — and, because an
// unavailable bridge is not a green path, it still exits non-zero.
var interpreterFailures = []string{
	"ModuleNotFoundError",
	"No module named",
	"command not found",
	"executable file not found",
	"no such file or directory",
}

// classifyQueryError marks an unusable interpreter as an unavailable bridge;
// any other query failure stays a hard error.
func classifyQueryError(err error) error {
	msg := strings.ToLower(err.Error())
	for _, marker := range interpreterFailures {
		if strings.Contains(msg, strings.ToLower(marker)) {
			return fmt.Errorf("%w: %v", ErrBridgeUnavailable, err)
		}
	}
	return err
}

// nativeIDs maps the release's impact nodes onto the comparison id space so
// both sides are keyed identically.
func nativeIDs(norm Normalizer, nodes []graphstore.ImpactNode) ([]string, error) {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		qualified, err := norm.Qualified(n.QualifiedName)
		if err != nil {
			return nil, err
		}
		file, err := norm.Path(n.FilePath)
		if err != nil {
			return nil, err
		}
		out = append(out, crg.SymbolID(crg.Symbol{QualifiedName: qualified, FilePath: file}))
	}
	return out, nil
}
