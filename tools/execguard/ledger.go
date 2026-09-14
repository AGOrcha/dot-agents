package main

import (
	"fmt"
	"sort"
	"strings"
)

// This file is the single source of truth for sanctioned process execution in
// shipping Go code. execguard is DEFAULT DENY: a policed site with no record
// here fails CI.
//
// A record is exact. It pins the file, the enclosing function, the resolved
// executable, and the number of sites that key may carry, so code motion
// inside a function is tolerated while a NEW spawn is not. `Purpose` has to
// say why no in-process API exists — "it was already there" is not a purpose.
//
// Two kinds, and the difference is the whole point:
//
//	KindBoundary — a genuine native-unavailable boundary: another runtime's
//	  entry point, an OS-defined helper ABI, a platform tool with no library
//	  equivalent, harness-authored commands, or a re-exec of this binary.
//	  These are permanent and need no follow-up.
//
//	KindDebt — an in-process alternative EXISTS and this site has not migrated
//	  yet. Requires `Followup`, the canonical record that owns the migration.
//	  A debt record is tracked debt, NOT an approval.
//
// GIT IS CATEGORICALLY UNALLOWABLE as a boundary. go-git v6 is vendored and
// already carries this module's worktree, remote, commit, and (since the
// submodule-blindness fix) code-graph Git reads. validateLedger REJECTS a
// KindBoundary record for git, and reconcile rejects ANY git site inside the
// cutover-locked packages below — that is the mechanical half of the
// internal/graphstore native cutover.

// recordKind distinguishes a permanent boundary from tracked migration debt.
type recordKind int

const (
	// KindBoundary is a permanent native-unavailable process boundary.
	KindBoundary recordKind = iota
	// KindDebt is a site with an in-process alternative, tracked to Followup.
	KindDebt
)

func (k recordKind) String() string {
	if k == KindDebt {
		return "debt"
	}
	return "boundary"
}

// recordKey is the exact identity of a sanctioned site group: one module
// relative file, one enclosing function, one resolved executable.
type recordKey struct {
	File string
	Func string
	Exe  string
}

// record is one sanctioned site group.
type record struct {
	recordKey
	// Kind is boundary (permanent) or debt (migration pending).
	Kind recordKind
	// Sites is the exact number of policed sites this key may carry.
	Sites int
	// Purpose states why the process boundary exists, or for debt, what the
	// in-process replacement is.
	Purpose string
	// Followup names the canonical record owning a debt migration. Required
	// for KindDebt, and meaningless for KindBoundary.
	Followup string
}

// lockedPackages are packages whose Git access has been migrated to go-git and
// must STAY migrated. A git site in any of them is a violation that no ledger
// record can sanction; the value is the cutover it would regress.
var lockedPackages = map[string]string{
	modulePath + "/internal/graphstore": "go-git index/merge-base cutover (kg-code-graph-submodule-blindness)",
	modulePath + "/internal/gitwt":      "go-git worktree manager",
	modulePath + "/internal/gitremote":  "go-git remote resolution",
}

// testScaffoldPackages are non-_test.go packages whose entire purpose is
// building fixtures for tests. The gate's scope is shipping code, and a
// fixture that constructs a REAL git repository is deliberate: it is the only
// way to test behaviour against git's own on-disk formats. They are listed
// here by exact path rather than by a pattern so a new package cannot become
// exempt by being named "…util".
var testScaffoldPackages = map[string]bool{
	modulePath + "/internal/testutil": true,
}

// nativeMigrationFollowup is the canonical record that owns migrating this
// module's remaining CLI-process Git/GitHub reads to in-process APIs. Every
// KindDebt record points at it.
const nativeMigrationFollowup = "fold-back production-exec-native-alternatives " +
	"(plan graph-backend-adapter-contract, task t6-bridge-decommission)"

// boundary builds a permanent native-unavailable record.
func boundary(file, fn, exe string, sites int, purpose string) record {
	return record{
		recordKey: recordKey{File: file, Func: fn, Exe: exe},
		Kind:      KindBoundary,
		Sites:     sites,
		Purpose:   purpose,
	}
}

// debt builds a migration-debt record against nativeMigrationFollowup.
func debt(file, fn, exe string, sites int, purpose string) record {
	return record{
		recordKey: recordKey{File: file, Func: fn, Exe: exe},
		Kind:      KindDebt,
		Sites:     sites,
		Purpose:   purpose,
		Followup:  nativeMigrationFollowup,
	}
}

// gitDebt is the shared purpose for a pre-existing git shell-out: the in-process
// replacement is named so the migration reads as work, not as an exemption.
func gitDebt(file, fn string, sites int, replacement string) record {
	return debt(file, fn, gitExe, sites, "pre-existing git CLI read/write; "+
		"in-process replacement: "+replacement)
}

// ledger is the sanctioned-site list, sorted by file then function so a review
// diff reads as one line per boundary.
var ledger = []record{
	// ── da re-exec ───────────────────────────────────────────────────────────
	// Both sites run THIS binary (os.Executable) as a child so the KG bridge
	// query runs with its own working directory and environment. There is no
	// in-process API for "run my own CLI surface"; calling the command
	// function directly would share this process's cobra/global state.
	boundary("commands/workflow/graph.go", "runWorkflowGraphQueryViaKGBridge", exeDynamic, 1,
		"re-exec of this da binary (os.Executable) to run `kg bridge query` with its own cwd/env"),
	boundary("commands/workflow/plan_task.go", "deriveScopeKGBridgeQuery", exeDynamic, 1,
		"re-exec of this da binary (os.Executable) to derive a task write-scope from `kg bridge query`"),

	// ── GitHub CLI ───────────────────────────────────────────────────────────
	boundary("commands/workflow/journal.go", "ghJSON", "gh", 1,
		"gh CLI is the single seam for the merged-PR read; it resolves the host, "+
			"auth token, and enterprise endpoint that a raw REST client would have to "+
			"reimplement. Tests override ghJSON rather than invoking gh"),

	// ── git: pre-existing shell-outs, tracked for migration ──────────────────
	// None of these is an approval. Git is never a native-unavailable
	// boundary (validateRecord rejects that outright) and never permitted in
	// the cutover-locked packages; these records exist so the gate can ship
	// against the current tree while the migration is owned by a follow-up.
	gitDebt("commands/internal/lifecycle/init_from.go", "gitCloneHomeSource", 1,
		"git.PlainClone"),
	gitDebt("commands/internal/lifecycle/init_from.go", "untrackStagedMachineLocal", 1,
		"Worktree.Remove with Cached"),
	gitDebt("commands/internal/lifecycle/install.go", "fetchGitSource", 1,
		"go-git needs no binary probe, so the LookPath disappears with the clone/pull migration"),
	gitDebt("commands/internal/lifecycle/install.go", "updateCachedGitSource", 1,
		"Worktree.Pull"),
	gitDebt("commands/internal/lifecycle/install.go", "CloneGitSource", 1,
		"git.PlainClone with Depth/ReferenceName"),
	gitDebt("commands/internal/lifecycle/status.go", "probeAgentsHomeGit", 1,
		"Repository.Head with reference-name shortening"),
	gitDebt("commands/kg/sync_code_warm_link.go", "runKGSyncIO", 1,
		"Worktree.Pull / Repository.Push (credential parity is the migration's real work)"),
	gitDebt("commands/sync/commit.go", "runSyncCommit", 2,
		"Worktree.AddWithOptions(All) then Worktree.Commit"),
	gitDebt("commands/sync/helpers.go", "countPorcelainStatus", 1,
		"Worktree.Status"),
	gitDebt("commands/sync/helpers.go", "printAheadBehind", 1,
		"Repository.Log from each side of the merge base"),
	gitDebt("commands/sync/helpers.go", "printBranchStatus", 1,
		"Repository.Head with reference-name shortening"),
	gitDebt("commands/sync/init.go", "initSyncRepo", 3,
		"git.PlainInit then Worktree.Add / Worktree.Commit"),
	gitDebt("commands/sync/init.go", "reportExistingSyncRepo", 1,
		"Repository.Remotes"),
	gitDebt("commands/sync/init.go", "untrackMachineLocalState", 1,
		"Worktree.Remove with Cached"),
	gitDebt("commands/sync/log.go", "newLogCmd", 1,
		"Repository.Log plus reference decoration"),
	gitDebt("commands/sync/pull.go", "newPullCmd", 1,
		"Worktree.Pull"),
	gitDebt("commands/sync/push.go", "printPendingPushCommits", 1,
		"Repository.Log bounded by the upstream reference"),
	gitDebt("commands/sync/push.go", "runSyncPush", 1,
		"Repository.Push"),
	gitDebt("commands/sync/push.go", "stageAndCommit", 2,
		"Worktree.AddWithOptions(All) then Worktree.Commit"),
	gitDebt("commands/workflow/delegation.go", "gitDiffChangedFiles", 1,
		"Worktree.Status, or a HEAD-tree to worktree diff"),
	gitDebt("commands/workflow/iter_log.go", "gitIterDiffStat", 1,
		"Repository.ResolveRevision(HEAD~1)"),
	gitDebt("commands/workflow/plan_task.go", "checkScopeGitDiffFiles", 2,
		"Worktree.Status, classifying staged and unstaged explicitly"),
	gitDebt("commands/workflow/plan_task.go", "gitStateExec", 1,
		"generic git wrapper: it has to be decomposed per operation, not wrapped again"),
	gitDebt("commands/workflow/plan_task.go", "readFileFromCanonicalRef", 1,
		"ResolveRevision then CommitObject.File contents"),
	gitDebt("commands/workflow/state.go", "gitOutput", 1,
		"generic git wrapper: it has to be decomposed per operation, not wrapped again"),
	gitDebt("commands/workflow/state.go", "isGitRepo", 1,
		"git.PlainOpenWithOptions with DetectDotGit"),
	gitDebt("internal/scoring/signal_backfill.go", "commitTime", 1,
		"CommitObject(hash).Committer.When"),
	gitDebt("internal/scoring/signal_git.go", "runGit", 1,
		"generic git wrapper: it has to be decomposed per operation, not wrapped again"),

	// ── OCI credential helpers ───────────────────────────────────────────────
	boundary("internal/config/oci_auth.go", "runOCICredentialHelper", exeDynamic, 1,
		"docker-credential-<store> helpers are defined by a PROCESS ABI (stdin request, "+
			"stdout JSON); the helper binary is operator-configured, so there is nothing to link against"),

	// ── OS keychain ──────────────────────────────────────────────────────────
	boundary("internal/credstore/keyring_darwin.go", "darwinKeyring.Get", exeDynamic, 1,
		"macOS Keychain access via /usr/bin/security; the Security.framework "+
			"alternative needs cgo, which this module does not use"),
	boundary("internal/credstore/keyring_darwin.go", "darwinKeyring.Set", exeDynamic, 1,
		"macOS Keychain write via /usr/bin/security; see darwinKeyring.Get"),

	// ── evaluation harness ───────────────────────────────────────────────────
	boundary("internal/eval/runner/runner.go", "realExec", exeDynamic, 1,
		"running the task under evaluation IS the product: the argv comes from a TaskSpec "+
			"and must execute in a separate process to be timed, bounded, and killed"),
	boundary("internal/eval/verifier/engine.go", "NewBase", exeDynamic, 1,
		"exec.LookPath as the injected toolchain resolver: a verifier reports "+
			"toolchain-missing, and tests substitute a deterministic resolver here"),
	boundary("internal/eval/verifier/engine.go", "runProcess", exeDynamic, 1,
		"running a language toolchain (build/test) for verification; the command comes "+
			"from the harness-authored TaskSpec"),

	// ── event sources ────────────────────────────────────────────────────────
	boundary("internal/events/producer.go", "DefaultFetcher.fetchExec", exeDynamic, 1,
		"an `exec` event source is a user-declared command whose stdout IS the event "+
			"payload; there is no in-process equivalent of an arbitrary declared command"),

	// ── Windows filesystem fallback ──────────────────────────────────────────
	// Build-tagged windows-only. The records are judged only on the runs that
	// compile the file (see unmatchedRecords).
	boundary("internal/fsops/fsops_windows.go", "RemoveAll", exeDynamic, 1,
		"PowerShell fallback for a Windows path the Go syscall cannot handle (long paths, "+
			"locked handles) — the reason internal/fsops exists"),
	boundary("internal/fsops/fsops_windows.go", "Remove", exeDynamic, 1,
		"PowerShell fallback for a Windows path the Go syscall cannot handle"),
	boundary("internal/fsops/fsops_windows.go", "WriteFile", exeDynamic, 1,
		"PowerShell fallback for a Windows path the Go syscall cannot handle"),

	// ── code-review-graph Python bridge ──────────────────────────────────────
	// The bridge is another RUNTIME, not another library: the graph builder is
	// a Python package. These sites retire with the native-adapter cutover
	// (plan graph-backend-adapter-contract, task t6-bridge-decommission), not
	// by being rewritten in Go here. They are emphatically NOT git: the Git
	// reads this package needs are already in process (gitnative.go), and
	// internal/graphstore is cutover-locked so they cannot come back.
	boundary("internal/graphstore/crg.go", "CRGBridge.commandWithSQLiteAutocommit", exeDynamic, 2,
		"code-review-graph entrypoint, optionally wrapped in its own python interpreter "+
			"to force SQLite autocommit"),
	boundary("internal/graphstore/crg.go", "CRGBridge.run", exeDynamic, 1,
		"code-review-graph CLI invocation (captured stdout/stderr)"),
	boundary("internal/graphstore/crg.go", "CRGBridge.runPyQuery", exeDynamic, 1,
		"python -c query against the CRG library for data its CLI does not expose"),
	boundary("internal/graphstore/crg.go", "CRGBridge.runStreamed", exeDynamic, 1,
		"code-review-graph CLI invocation with streamed output for long builds"),
	boundary("internal/graphstore/crg.go", "DiscoverCRGBin", "code-review-graph", 1,
		"locating the CRG entrypoint on PATH, applying the platform's PATHEXT rules"),

	// ── target CLI probes ────────────────────────────────────────────────────
	// Reporting whether another vendor's CLI is installed, and at what
	// version, is inherently a question about an external process.
	boundary("internal/platform/cliprobe.go", "probeInstalled", exeDynamic, 1,
		"presence probe for a target platform's CLI"),
	boundary("internal/platform/cliprobe.go", "probeVersion", exeDynamic, 1,
		"resolving a target platform's CLI on PATH before probing its version"),
	boundary("internal/platform/cliprobe.go", "probeVersionAtPath", exeDynamic, 1,
		"reading a target platform's CLI version from `<bin> --version`"),
	boundary("internal/platform/cursor.go", "firstCLIPeekVersion", exeDynamic, 1,
		"resolving the Cursor CLI (`agent`, then `cursor`) on PATH"),
	boundary("internal/platform/cursor.go", "macOSCursorAppShortVersion", "defaults", 1,
		"macOS `defaults read` of Cursor.app's Info.plist: the plist domain lookup is a "+
			"platform tool, and this path is already guarded by a darwin check"),
}

// indexLedger keys the ledger for lookup. It is built per run rather than held
// in a package var so `ledger` is the ONLY source of truth — a second copy
// would let one of them go stale, which is the failure mode a ratchet cannot
// afford.
func indexLedger() map[recordKey]record {
	out := make(map[recordKey]record, len(ledger))
	for _, r := range ledger {
		out[r.recordKey] = r
	}
	return out
}

// lockedPackage returns the cutover a package is locked by, or "".
func lockedPackage(pkgPath string) string { return lockedPackages[pkgPath] }

// unmatchedRecords returns the keys of records whose file WAS scanned yet
// produced no matching site, sorted for stable output. A stale record is a
// violation: it means a boundary moved or was removed and the ledger was not
// updated with it.
//
// Records for files the current build did not compile (a _windows.go or
// _darwin.go boundary on another OS) are not judged: the gate runs on every
// OS in the matrix, and reporting them as stale would make each platform's run
// demand a different ledger.
func unmatchedRecords(observed map[recordKey][]site, scanned map[string]bool) []recordKey {
	var stale []recordKey
	for _, r := range ledger {
		if _, ok := observed[r.recordKey]; ok || !scanned[r.File] {
			continue
		}
		stale = append(stale, r.recordKey)
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].File != stale[j].File {
			return stale[i].File < stale[j].File
		}
		return stale[i].Func < stale[j].Func
	})
	return stale
}

// validateLedger enforces the ledger's own invariants. A violation here is a
// POLICY error (exit 2), not a code finding: the gate itself is misconfigured.
func validateLedger() error {
	seen := make(map[recordKey]bool, len(ledger))
	for _, r := range ledger {
		if err := validateRecord(r); err != nil {
			return err
		}
		if seen[r.recordKey] {
			return fmt.Errorf("duplicate record for %s / %s / %s", r.File, r.Func, r.Exe)
		}
		seen[r.recordKey] = true
	}
	return nil
}

// validateRecord checks one record, including the git prohibition.
func validateRecord(r record) error {
	switch {
	case r.File == "" || r.Func == "" || r.Exe == "":
		return fmt.Errorf("record %+v: file, func, and exe are all required", r.recordKey)
	case r.Sites < 1:
		return fmt.Errorf("record %s / %s: sites must be at least 1", r.File, r.Func)
	case strings.TrimSpace(r.Purpose) == "":
		return fmt.Errorf("record %s / %s: purpose is required", r.File, r.Func)
	case r.Exe == gitExe && r.Kind == KindBoundary:
		return fmt.Errorf("record %s / %s: git is never a native-unavailable boundary — "+
			"use go-git, or record KindDebt with a follow-up", r.File, r.Func)
	case r.Kind == KindDebt && strings.TrimSpace(r.Followup) == "":
		return fmt.Errorf("record %s / %s: debt requires a follow-up record", r.File, r.Func)
	}
	return nil
}
