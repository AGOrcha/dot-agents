package kg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

// ── Command registration ──────────────────────────────────────────────────────

func commandJSON(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	v, err := cmd.Flags().GetBool("json")
	return err == nil && v
}

const warmStoreOpenErrFmt = "open warm store: %w"

// ── Phase 6C: kg sync ─────────────────────────────────────────────────────────

// runKGSync is a thin wrapper: git pull (or push) followed by kg lint.
// It does not implement a custom sync protocol — git provides the transport.
//
// Cobra wires this via `RunE: runKGSync` (no Deps); the body delegates to
// runKGSyncIO so tests can drive the post-pull lint-error branch with a
// fakeKGIO that fault-injects the lint's underlying ReadDir.
func runKGSync(cmd *cobra.Command, args []string) error {
	return runKGSyncIO(stdKGIO{}, cmd, args)
}

// runKGSyncIO is the threaded implementation of runKGSync. Production wires
// stdKGIO{} via runKGSync; tests construct a fakeKGIO and call this directly.
func runKGSyncIO(io kgIO, cmd *cobra.Command, _ []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized at %s: run 'da kg setup' first", home)
	}

	push, _ := cmd.Flags().GetBool("push")

	// kg sync moves a git remote and is not locally snapshot-recoverable, so it
	// is journaled fully (KGSyncObserved). ok flips true the moment the git op
	// lands; a post-pull lint error is reported to the operator but does not undo
	// the pull, so it still records success (mirrors p3a's ok-after-mutation).
	repoPath := crgRepoRoot()
	input := &journal.KGSyncInput{Push: push}
	observed := &journal.KGSyncObserved{}
	ok := false
	defer func() { journalKG(repoPath, journal.CmdKGSync, input, observed, ok) }()

	var gitArgs []string
	if push {
		gitArgs = []string{"-C", home, "push"}
	} else {
		gitArgs = []string{"-C", home, "pull"}
	}

	op := "pull"
	if push {
		op = "push"
	}

	ui.Info(fmt.Sprintf("Running git %s in %s ...", op, home))
	gitCmd := execabs.Command("git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w", op, err)
	}

	if push {
		observed.PushStatus = "ok"
		ok = true
		ui.Success("Graph pushed.")
		return nil
	}
	observed.PullStatus = "ok"
	ok = true

	// After pull, run lint to surface any content drift
	ui.Info("Running kg lint after pull ...")
	report, err := runGraphLint(io, home)
	if err != nil {
		return fmt.Errorf("lint after sync: %w", err)
	}

	if report.ErrorCount > 0 || report.WarnCount > 0 {
		ui.InfoBox(
			fmt.Sprintf("Sync complete — lint found issues (%d errors, %d warnings)", report.ErrorCount, report.WarnCount),
			"Run 'da kg lint' for details",
		)
	} else {
		ui.Success(fmt.Sprintf("Sync complete — graph is clean (%d notes)", len(report.Results)+report.InfoCount))
	}
	return nil
}

// ── Phase B: CRG code-graph commands ─────────────────────────────────────────

// crgRepoRoot returns the nearest git repo root above the cwd, falling back to cwd.
func crgRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return dir
}

func runKGBuild(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	skipFlows, _ := cmd.Flags().GetBool("skip-flows")
	skipPost, _ := cmd.Flags().GetBool("skip-postprocess")

	// KG decision event: record the build outcome + resulting graph counts
	// (KGDecisionObserved), never node/edge bodies (D4). repoPath is the graphed
	// repo itself.
	input := &journal.KGDecisionInput{Repo: root}
	observed := &journal.KGDecisionObserved{}
	ok := false
	defer func() { journalKG(root, journal.CmdKGBuild, input, observed, ok) }()

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	if !commandJSON(cmd) {
		ui.Info(fmt.Sprintf("Building code graph for %s ...", root))
	}
	report, err := bridge.BuildReport(graphstore.BuildOptions{
		SkipFlows:       skipFlows,
		SkipPostprocess: skipPost,
	})
	if err != nil {
		return err
	}
	observed.Outcome = report.Outcome
	setDecisionGraphCounts(observed, report.Status)
	ok = true
	if commandJSON(cmd) {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	switch report.Outcome {
	case string(graphstore.CRGReadinessReady):
		ui.SuccessBox(report.Summary)
	case string(graphstore.CRGReadinessUnbuilt):
		ui.InfoBox("Code graph remains unbuilt", report.Summary)
	case string(graphstore.CRGReadinessBusyOrLocked):
		ui.WarnBox("Code graph is busy or locked", report.Summary)
	default:
		ui.InfoBox("Code graph build status", report.Summary)
	}
	return nil
}

// setDecisionGraphCounts copies the post-operation node/edge/file counts from a
// CRG status onto a KG decision-event observed payload (pointer fields so an
// absent status omits them). It records counts only — never node/edge bodies (D4).
func setDecisionGraphCounts(observed *journal.KGDecisionObserved, status *graphstore.CRGStatus) {
	if status == nil {
		return
	}
	nodes, edges, files := status.Nodes, status.Edges, status.Files
	observed.Nodes = &nodes
	observed.Edges = &edges
	observed.Files = &files
}

func runKGUpdate(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	// code-review-graph is an optional dependency. When it isn't installed,
	// degrade gracefully (exit 0) instead of erroring — the graph-update
	// post_tool_use hook runs on every edit and must not fail the session for
	// users without the tool.
	if _, err := graphstore.DiscoverCRGBin(root); err != nil {
		if !commandJSON(cmd) {
			ui.Info("code-review-graph not installed; skipping code graph update")
		}
		return nil
	}
	base, _ := cmd.Flags().GetString("base")
	skipFlows, _ := cmd.Flags().GetBool("skip-flows")
	skipPost, _ := cmd.Flags().GetBool("skip-postprocess")

	// Journal only once we know the tool is present (the graceful no-op above
	// mutates nothing). Decision event: outcome + graph counts, never bodies (D4).
	input := &journal.KGDecisionInput{Repo: root, Base: base}
	observed := &journal.KGDecisionObserved{}
	ok := false
	defer func() { journalKG(root, journal.CmdKGUpdate, input, observed, ok) }()

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	if !commandJSON(cmd) {
		ui.Info(fmt.Sprintf("Updating code graph for %s ...", root))
	}
	report, err := bridge.UpdateReport(graphstore.UpdateOptions{
		Base:            base,
		SkipFlows:       skipFlows,
		SkipPostprocess: skipPost,
	})
	if err != nil {
		return err
	}
	observed.Outcome = report.Outcome
	setDecisionGraphCounts(observed, report.Status)
	ok = true
	if commandJSON(cmd) {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	switch report.Outcome {
	case "no_diff":
		ui.InfoBox("No code diff to update", report.Summary)
	case "no_mutation":
		ui.SuccessBox(report.Summary)
	case "updated":
		ui.SuccessBox(report.Summary)
	default:
		ui.InfoBox("Code graph update status", report.Summary)
	}
	return nil
}

func runKGCodeStatus(deps Deps, cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	status, err := (&graphstore.CRGBridge{RepoRoot: root}).Status()
	if err != nil {
		return err
	}
	if commandJSON(cmd) {
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Code Graph Status  [%s]", strings.ToUpper(status.State)))
	ui.Info(fmt.Sprintf("  Nodes:        %d", status.Nodes))
	ui.Info(fmt.Sprintf("  Edges:        %d", status.Edges))
	ui.Info(fmt.Sprintf("  Files:        %d", status.Files))
	ui.Info(fmt.Sprintf("  Languages:    %s", status.Languages))
	ui.Info(fmt.Sprintf("  Last updated: %s", status.LastUpdated))
	if status.Message != "" {
		ui.Info(fmt.Sprintf("  State:        %s", status.Message))
	}
	return nil
}

// crgStatusState calls Status() on a CRGBridge and returns the state string.
// If Status() fails (e.g. CRG not installed), "unknown" is returned.
func crgStatusState(root string) string {
	status, err := (&graphstore.CRGBridge{RepoRoot: root}).Status()
	if err != nil || status == nil {
		return "unknown"
	}
	return status.State
}

// crgBridgeStatus is a seam over CRGBridge.Status for tests to inject a
// real-error response (production Status() never returns a non-nil error
// today, but callers must not silently swallow one if that changes).
var crgBridgeStatus = func(root string) (*graphstore.CRGStatus, error) {
	return (&graphstore.CRGBridge{RepoRoot: root}).Status()
}

// checkCRGReadiness calls Status() and emits warnings for unbuilt/busy states.
// When requireGraph is true and the graph is not ready, an error is returned.
func checkCRGReadiness(root string, requireGraph bool) error {
	status, err := crgBridgeStatus(root)
	if err != nil {
		// Status() failed — this is a REAL error (permission/db/I-O fault), not
		// "CRG not installed" (that case is reported via status.State/Message
		// with a nil error, handled below). Warn, and — critically — do not let
		// --require-graph silently pass when readiness could not be determined.
		ui.WarnBox("Code graph status unavailable", fmt.Sprintf("could not determine code graph readiness: %v", err))
		if requireGraph {
			return fmt.Errorf("code graph readiness unknown: %w", err)
		}
		return nil
	}
	switch status.State {
	case graphstore.CRGReadinessUnbuilt:
		ui.WarnBox("Code graph not built", "Run 'kg build' first — results will be empty or incomplete.")
		if requireGraph {
			return fmt.Errorf("code graph is not built")
		}
	case graphstore.CRGReadinessBusyOrLocked:
		ui.WarnBox("Code graph is busy or locked", "Wait for concurrent operation to finish and retry.")
		if requireGraph {
			return fmt.Errorf("code graph is busy or locked")
		}
	}
	return nil
}

// kgImpactJSONOutput is the JSON wrapper for kg impact output, adding graph_state.
type kgImpactJSONOutput struct {
	GraphState string `json:"graph_state"`
	*graphstore.CRGImpactResult
}

// kgChangesJSONOutput is the JSON wrapper for kg changes output, adding graph_state.
type kgChangesJSONOutput struct {
	GraphState string `json:"graph_state"`
	*graphstore.CRGChangeReport
}

func runKGImpact(deps Deps, cmd *cobra.Command, args []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	base, _ := cmd.Flags().GetString("base")
	maxDepth, _ := cmd.Flags().GetInt("depth")
	maxResults, _ := cmd.Flags().GetInt("limit")
	requireGraph, _ := cmd.Flags().GetBool("require-graph")

	if err := checkCRGReadiness(root, requireGraph); err != nil {
		return err
	}

	var files []string
	if len(args) > 0 {
		files = args
	}

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.GetImpactRadius(graphstore.ImpactOptions{
		ChangedFiles: files,
		MaxDepth:     maxDepth,
		MaxResults:   maxResults,
		Base:         base,
	})
	if err != nil {
		return err
	}
	if commandJSON(cmd) {
		data, _ := json.MarshalIndent(kgImpactJSONOutput{
			GraphState:      crgStatusState(root),
			CRGImpactResult: result,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	renderImpactResultText(result)
	return nil
}

// renderImpactResultText prints the human-readable impact-radius report.
func renderImpactResultText(result *graphstore.CRGImpactResult) {
	ui.Header("Impact Radius")
	ui.Info(result.Summary)
	renderImpactNodeSection("Changed nodes", "warn", result.ChangedNodes)
	renderImpactNodeSection("Impacted nodes", "found", result.ImpactedNodes)
	if len(result.ImpactedFiles) > 0 {
		ui.Section("Impacted files")
		for _, f := range result.ImpactedFiles {
			ui.Bullet("found", f)
		}
	}
	if result.Truncated {
		ui.Info(fmt.Sprintf("  (results truncated — %d total impacted)", result.TotalImpacted))
	}
	if len(result.ChangedNodes) == 0 && len(result.ImpactedNodes) == 0 {
		ui.Info("Note: run 'kg code-status' to verify the code graph is current.")
	}
}

// renderImpactNodeSection prints a section header followed by one bullet
// per non-file node, using the supplied bullet glyph. The section is
// suppressed entirely when nodes is empty.
func renderImpactNodeSection(header, glyph string, nodes []graphstore.ImpactNode) {
	if len(nodes) == 0 {
		return
	}
	ui.Section(header)
	for _, n := range nodes {
		if n.Kind == "File" {
			continue // file-level nodes are noisy
		}
		ui.Bullet(glyph, fmt.Sprintf("[%s] %s", n.Kind, n.Name))
	}
}

func runKGFlows(deps Deps, cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	limit, _ := cmd.Flags().GetInt("limit")
	sortBy, _ := cmd.Flags().GetString("sort")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.ListFlows(limit, sortBy)
	if err != nil {
		return err
	}
	if commandJSON(cmd) {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Execution Flows  [%s]", result.Summary))
	if len(result.Flows) == 0 {
		ui.Info("No flows detected. Run 'da kg postprocess' to detect flows.")
		return nil
	}
	for _, f := range result.Flows {
		ui.Bullet("found", fmt.Sprintf("[%s] %s (steps=%d, criticality=%.2f)", f.Kind, f.Name, f.StepCount, f.Criticality))
		if f.EntryPoint != "" {
			ui.Info(fmt.Sprintf("        entry: %s", f.EntryPoint))
		}
	}
	return nil
}

func runKGCommunities(deps Deps, cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	minSize, _ := cmd.Flags().GetInt("min-size")
	sortBy, _ := cmd.Flags().GetString("sort")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.ListCommunities(minSize, sortBy)
	if err != nil {
		return err
	}
	if commandJSON(cmd) {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Code Communities  [%s]", result.Summary))
	for _, c := range result.Communities {
		ui.Bullet("found", fmt.Sprintf("[%s] %s (size=%d, cohesion=%.2f)", c.DominantLanguage, c.Name, c.Size, c.Cohesion))
		if c.Description != "" {
			ui.Info(fmt.Sprintf("        %s", c.Description))
		}
	}
	return nil
}

func runKGPostprocess(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	noFlows, _ := cmd.Flags().GetBool("no-flows")
	noCommunities, _ := cmd.Flags().GetBool("no-communities")
	noFTS, _ := cmd.Flags().GetBool("no-fts")

	// Decision event: postprocess rebuilds derived data; record the outcome, not
	// the rebuilt flow/community bodies (D4).
	input := &journal.KGDecisionInput{Repo: root}
	observed := &journal.KGDecisionObserved{Outcome: "postprocessed"}
	ok := false
	defer func() { journalKG(root, journal.CmdKGPostprocess, input, observed, ok) }()

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	ui.Info(fmt.Sprintf("Running post-processing on %s ...", root))
	if err := bridge.Postprocess(graphstore.PostprocessOptions{
		NoFlows:       noFlows,
		NoCommunities: noCommunities,
		NoFTS:         noFTS,
	}); err != nil {
		return err
	}
	// Postprocess returns no report, so read the resulting graph status to record
	// the same node/edge/file counts build/update journal (the KGDecisionObserved
	// contract). Status is best-effort: if it fails the postprocess still landed,
	// so we keep Outcome and just omit the counts rather than fail the command.
	if status, serr := bridge.Status(); serr == nil {
		setDecisionGraphCounts(observed, status)
	}
	ok = true
	return nil
}

func runKGChanges(deps Deps, cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	base, _ := cmd.Flags().GetString("base")
	brief, _ := cmd.Flags().GetBool("brief")
	requireGraph, _ := cmd.Flags().GetBool("require-graph")

	if err := checkCRGReadiness(root, requireGraph); err != nil {
		return err
	}

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	report, err := bridge.DetectChanges(graphstore.DetectChangesOptions{
		Base:  base,
		Brief: brief,
	})
	if err != nil {
		return err
	}
	if commandJSON(cmd) {
		data, _ := json.MarshalIndent(kgChangesJSONOutput{
			GraphState:      crgStatusState(root),
			CRGChangeReport: report,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Change Impact")
	ui.Info(report.Summary)
	if len(report.ChangedFunctions) > 0 {
		ui.Section("Changed symbols")
		for _, n := range report.ChangedFunctions {
			ui.Bullet("warn", fmt.Sprintf("[risk=%.2f] %s", n.RiskScore, n.QualifiedName))
		}
	}
	if len(report.TestGaps) > 0 {
		ui.Section("Test gaps")
		for _, g := range report.TestGaps {
			ui.Bullet("error", g.QualifiedName)
		}
	}
	if len(report.ReviewPriorities) > 0 {
		ui.Section("Review priorities")
		for _, p := range report.ReviewPriorities {
			ui.Bullet("found", fmt.Sprintf("[risk=%.2f] %s — %s", p.RiskScore, p.QualifiedName, p.Reason))
		}
	}
	if len(report.ChangedFunctions) == 0 {
		ui.Info("Note: run 'kg code-status' to verify the code graph is current.")
	}
	return nil
}

// ── Phase D: Hot/cold note lifecycle ─────────────────────────────────────────

// graphstoreDBPath returns the path to the SQLite warm-layer database.
func graphstoreDBPath(kgHomeDir string) string {
	return filepath.Join(kgHomeDir, "ops", "graphstore.db")
}

// openKGStore opens (or creates) the warm-layer SQLite database. It returns
// the contract-typed graphstore.Store handle (gcc3 binding — callers depend
// on the published contract, never on the concrete backend). The provider
// behind the returned Store owns pooling/serialization per CONTRACT.md
// guarantee #3; lifecycle stays explicit (callers Close when done) per #4.
func openKGStore(kgHomeDir string) (graphstore.Store, error) {
	return graphstore.OpenSQLite(graphstoreDBPath(kgHomeDir))
}

// noteToKGNote converts a GraphNote from the hot filesystem layer to a
// graphstore.KGNote for the warm database layer.
func noteToKGNote(note *GraphNote, filePath string) graphstore.KGNote {
	archivedAt := ""
	if note.Status == "archived" || note.Status == "superseded" {
		archivedAt = note.UpdatedAt
	}
	return graphstore.KGNote{
		ID:         note.ID,
		Title:      note.Title,
		NoteType:   note.Type,
		Status:     note.Status,
		Summary:    note.Summary,
		FilePath:   filePath,
		Version:    note.Version,
		ArchivedAt: archivedAt,
	}
}

// runKGWarmCodeImport imports CRG code nodes and edges into the warm SQLite layer.
// It reads directly from .code-review-graph/graph.db in the repo root. The
// store parameter is the published contract type (gcc3 binding) — this
// caller exercises both the Writer and a Closer-adjacent lifetime via its
// outer scope, so it depends on the whole-store Store rather than a narrower
// role.
func runKGWarmCodeImport(store graphstore.Store, repoRoot string) (nodesImported, edgesImported int, err error) {
	bridge, berr := graphstore.NewCRGBridge(repoRoot)
	if berr != nil {
		return 0, 0, fmt.Errorf("CRG not available: %w", berr)
	}
	nodes, nerr := bridge.ReadNodes(0)
	if nerr != nil {
		return 0, 0, fmt.Errorf("read CRG nodes: %w", nerr)
	}
	for _, n := range nodes {
		info := graphstore.NodeInfo{
			Kind:       n.Kind,
			Name:       n.Name,
			FilePath:   n.FilePath,
			LineStart:  n.LineStart,
			LineEnd:    n.LineEnd,
			Language:   n.Language,
			ParentName: n.ParentName,
			Params:     n.Params,
			ReturnType: n.ReturnType,
			IsTest:     n.IsTest,
			Extra:      n.Extra,
		}
		if _, uerr := store.UpsertNode(info, n.FileHash); uerr == nil {
			nodesImported++
		}
	}
	edges, eerr := bridge.ReadEdges(0)
	if eerr != nil {
		return nodesImported, 0, fmt.Errorf("read CRG edges: %w", eerr)
	}
	for _, e := range edges {
		info := graphstore.EdgeInfo{
			Kind:     e.Kind,
			Source:   e.SourceQualified,
			Target:   e.TargetQualified,
			FilePath: e.FilePath,
			Line:     e.Line,
			Extra:    e.Extra,
		}
		if _, uerr := store.UpsertEdge(info); uerr == nil {
			edgesImported++
		}
	}
	return nodesImported, edgesImported, nil
}

// runKGWarm syncs all hot filesystem notes into the warm SQLite layer.
//
// IO note: runKGWarm is wired into Cobra via the RunE: runKGWarm pointer (no
// Deps), and its tests invoke it as runKGWarm(cmd, nil) without a fake. It
// uses stdKGIO{} for the underlying warmNotesInDir walks; the per-operation
// fault-injection tests call warmNotesInDir directly with a fakeKGIO.
func runKGWarm(cmd *cobra.Command, _ []string) error {
	io := stdKGIO{}
	home := kgHome()
	noteTypeFilter, _ := cmd.Flags().GetString("type")
	includeCode, _ := cmd.Flags().GetBool("include-code")

	// Content-delta event: record how many notes were indexed/skipped (counts
	// only, never note bodies — D4). The optional type filter is the only target.
	repoPath := crgRepoRoot()
	input := &journal.KGContentDeltaInput{Operation: "warm"}
	if noteTypeFilter != "" {
		input.Targets = []string{noteTypeFilter}
	}
	observed := &journal.KGContentDeltaObserved{}
	ok := false
	defer func() { journalKG(repoPath, journal.CmdKGWarm, input, observed, ok) }()

	store, err := openKGStore(home)
	if err != nil {
		return fmt.Errorf(warmStoreOpenErrFmt, err)
	}
	defer store.Close()

	subdirs, err := warmNoteSubdirs(noteTypeFilter)
	if err != nil {
		return err
	}

	indexed, skipped := warmActiveNotes(io, store, home, subdirs)
	archIndexed, archSkipped := warmArchivedNotes(io, store, home)
	indexed += archIndexed
	skipped += archSkipped

	_ = store.SetMetadata("last_warm_sync", time.Now().UTC().Format(time.RFC3339))

	// Build the observed counts AFTER the code lane so the event reflects the
	// full mutation: --include-code upserts code nodes/edges into the warm store,
	// and even a partial import (nodes before an edge-read error) is a real
	// change that must be journaled, not just the note lane (D4: counts, no bodies).
	counts := map[string]int{"indexed": indexed, "skipped": skipped}
	codeMsg := ""
	if includeCode {
		codeNodes, codeEdges, msg := warmCodeLane(store)
		codeMsg = msg
		counts["code_nodes"] = codeNodes
		counts["code_edges"] = codeEdges
	}
	observed.Counts = counts
	ok = true

	lines := []string{
		"da kg link add <note-id> <symbol> — link a note to a code symbol",
		"da kg link list <note-id>         — list all symbol links for a note",
	}
	summary := fmt.Sprintf("Warm sync complete: %d notes indexed, %d skipped", indexed, skipped)
	if codeMsg != "" {
		summary += "\n" + codeMsg
	}
	ui.SuccessBox(summary, lines...)
	return nil
}

// warmNoteSubdirs translates a note-type filter (or empty for all types)
// into the matching notes/<sub> subdirectory list. Returns an error when
// the explicit type filter is unrecognized.
func warmNoteSubdirs(noteTypeFilter string) ([]string, error) {
	allTypes := []string{"source", "entity", "concept", "synthesis", "decision", "repo", "session"}
	typeList := allTypes
	if noteTypeFilter != "" {
		if !isValidNoteType(noteTypeFilter) {
			return nil, fmt.Errorf("invalid note type %q: valid values are %s", noteTypeFilter, strings.Join(allTypes, ", "))
		}
		typeList = []string{noteTypeFilter}
	}
	subdirs := make([]string, len(typeList))
	for i, t := range typeList {
		subdirs[i] = noteSubdir(t)
	}
	return subdirs, nil
}

// warmActiveNotes upserts each note found under the given notes/ subdirs
// into store and returns the indexed/skipped counters. The store parameter
// is the published contract (gcc3 binding); concretely this caller uses the
// KGNoteStore role, but we keep the whole-store type to share the same
// handle across the warm/code-import passes in runKGWarm.
func warmActiveNotes(io kgIO, store graphstore.Store, home string, subdirs []string) (indexed, skipped int) {
	for _, sub := range subdirs {
		i, s := warmNotesInDir(io, store, filepath.Join(home, "notes", sub), nil)
		indexed += i
		skipped += s
	}
	return indexed, skipped
}

// warmArchivedNotes upserts the contents of notes/_archived, marking each
// resulting KGNote as archived when the source frontmatter omits the
// archive timestamp. store is the published contract (gcc3 binding).
func warmArchivedNotes(io kgIO, store graphstore.Store, home string) (indexed, skipped int) {
	return warmNotesInDir(io, store, filepath.Join(home, "notes", "_archived"), func(kn *graphstore.KGNote, note *GraphNote) {
		if kn.ArchivedAt == "" {
			kn.ArchivedAt = note.UpdatedAt
		}
	})
}

// warmNotesInDir reads every top-level .md file under dir, parses it as a
// GraphNote, applies the optional adjust callback, and upserts the
// resulting KGNote into store. Returns the indexed/skipped counters.
// Missing directories are not counted as skips. store is the published
// contract (gcc3 binding); only the KGNoteStore role is exercised here.
func warmNotesInDir(io kgIO, store graphstore.Store, dir string, adjust func(*graphstore.KGNote, *GraphNote)) (indexed, skipped int) {
	entries, err := io.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		fpath := filepath.Join(dir, e.Name())
		data, err := io.ReadFile(fpath)
		if err != nil {
			skipped++
			continue
		}
		note, _, err := parseGraphNote(data)
		if err != nil || note.ID == "" {
			skipped++
			continue
		}
		kn := noteToKGNote(note, fpath)
		if adjust != nil {
			adjust(&kn, note)
		}
		if err := store.UpsertKGNote(kn); err != nil {
			skipped++
			continue
		}
		indexed++
	}
	return indexed, skipped
}

// warmCodeLane runs the optional code-lane import from CRG and returns the
// number of code nodes/edges actually upserted plus a summary line for the
// SuccessBox body (empty on failure). store is the published contract (gcc3
// binding).
//
// The counts are returned even when the import errors: runKGWarmCodeImport
// upserts nodes before reading edges, so an edge-read failure still leaves real
// node mutations behind. The caller journals these so the warm event reflects
// the FULL mutation (note lane + code lane), not just the note lane.
func warmCodeLane(store graphstore.Store) (nodes, edges int, summary string) {
	repoRoot, _ := os.Getwd()
	nodesIn, edgesIn, cerr := runKGWarmCodeImport(store, repoRoot)
	if cerr != nil {
		ui.Warn(fmt.Sprintf("code-lane import skipped: %v", cerr))
		return nodesIn, edgesIn, ""
	}
	_ = store.SetMetadata("last_code_import", time.Now().UTC().Format(time.RFC3339))
	return nodesIn, edgesIn, fmt.Sprintf("  code-lane: %d nodes, %d edges imported from CRG", nodesIn, edgesIn)
}

// runKGLinkAdd creates a note→symbol link.
func runKGLinkAdd(deps Deps, cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return kgUsageError(deps, "kg link add expects 2 arguments",
			"Usage: da kg link add <note-id> <qualified-name>.")
	}
	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "mentions"
	}
	validLinkKinds := map[string]bool{
		"mentions": true, "implements": true, "documents": true,
		"decides": true, "references": true,
	}
	if !validLinkKinds[kind] {
		return kgUsageError(deps,
			fmt.Sprintf("invalid link kind %q: valid values are mentions, implements, documents, decides, references", kind),
			"Pass one of: mentions, implements, documents, decides, references.")
	}

	// Content-delta event: record the link add as a count + the affected ids
	// (note id, symbol, link id) — never bodies (D4). Armed after flag validation
	// so a usage error is a pre-mutation rejection, not a journaled attempt.
	repoPath := crgRepoRoot()
	input := &journal.KGContentDeltaInput{Operation: "link add", Targets: []string{args[0], args[1]}}
	observed := &journal.KGContentDeltaObserved{}
	ok := false
	defer func() { journalKG(repoPath, journal.CmdKGLinkAdd, input, observed, ok) }()

	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf(warmStoreOpenErrFmt, err)
	}
	defer store.Close()

	link := graphstore.NoteSymbolLink{
		NoteID:        args[0],
		QualifiedName: args[1],
		LinkKind:      kind,
	}
	id, err := store.UpsertNoteSymbolLink(link)
	if err != nil {
		return fmt.Errorf("create link: %w", err)
	}
	observed.Counts = map[string]int{"links_added": 1}
	observed.IDs = []string{fmt.Sprintf("%d", id)}
	ok = true
	ui.Success(fmt.Sprintf("Link created (id=%d): %s -[%s]-> %s", id, args[0], kind, args[1]))
	return nil
}

// runKGLinkList shows all symbol links for a note.
func runKGLinkList(deps Deps, _ *cobra.Command, args []string) error {
	if len(args) < 1 {
		return kgUsageError(deps, "kg link list expects 1 argument",
			"Usage: da kg link list <note-id>.")
	}
	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf(warmStoreOpenErrFmt, err)
	}
	defer store.Close()

	links, err := store.GetLinksForNote(args[0])
	if err != nil {
		return fmt.Errorf("get links: %w", err)
	}
	if len(links) == 0 {
		ui.Info(fmt.Sprintf("No symbol links for note %q. Run 'kg warm' first if notes are not yet indexed.", args[0]))
		return nil
	}
	for _, l := range links {
		fmt.Printf("  [%d] %s -[%s]-> %s\n", l.ID, l.NoteID, l.LinkKind, l.QualifiedName)
	}
	return nil
}

// runKGLinkRemove deletes a note→symbol link by ID.
func runKGLinkRemove(deps Deps, _ *cobra.Command, args []string) error {
	if len(args) < 1 {
		return kgUsageError(deps, "kg link remove expects 1 argument",
			"Usage: da kg link remove <link-id>.")
	}
	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
		return kgUsageError(deps,
			fmt.Sprintf("invalid link ID %q: expected an integer", args[0]),
			"Pass the numeric link ID shown by `da kg link list`.")
	}
	// Content-delta event: record the link removal as a count + the removed link
	// id — never bodies (D4). Armed after the id-parse validation.
	repoPath := crgRepoRoot()
	input := &journal.KGContentDeltaInput{Operation: "link remove", Targets: []string{args[0]}}
	observed := &journal.KGContentDeltaObserved{}
	ok := false
	defer func() { journalKG(repoPath, journal.CmdKGLinkRemove, input, observed, ok) }()

	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf(warmStoreOpenErrFmt, err)
	}
	defer store.Close()

	if err := store.DeleteNoteSymbolLink(id); err != nil {
		return fmt.Errorf("remove link: %w", err)
	}
	observed.Counts = map[string]int{"links_removed": 1}
	observed.IDs = []string{args[0]}
	ok = true
	ui.Success(fmt.Sprintf("Link %d removed", id))
	return nil
}

// runKGWarmStats shows warm layer stats without doing a sync.
func runKGWarmStats(_ *cobra.Command, _ []string) error {
	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf(warmStoreOpenErrFmt, err)
	}
	defer store.Close()

	stats, err := store.GetStats()
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}
	lastSync, _ := store.GetMetadata("last_warm_sync")
	if lastSync == "" {
		lastSync = "never"
	}
	ui.InfoBox("Warm Layer Stats",
		fmt.Sprintf("Notes indexed:    %d", stats.NotesCount),
		fmt.Sprintf("Symbol links:     %d", stats.LinksCount),
		fmt.Sprintf("Code nodes:       %d", stats.TotalNodes),
		fmt.Sprintf("Code edges:       %d", stats.TotalEdges),
		fmt.Sprintf("Last warm sync:   %s", lastSync),
		fmt.Sprintf("DB path:          %s", graphstoreDBPath(kgHome())),
	)
	return nil
}
