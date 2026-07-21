package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// errIndexTestFault is the shared sentinel returned by fault-injection stubs in
// this file — a single literal keeps the error-branch tests DRY (S1192).
var errIndexTestFault = errors.New("iter-index test fault")

// iter1Record is the bare iteration-1 record filename, referenced by several
// archival assertions (single const clears the duplicated-literal gate, S1192).
const iter1Record = "iter-1.yaml"

// ── unit fixtures ──────────────────────────────────────────────────────────

// seedIterRecord writes a minimal iter-N.yaml carrying the given wave (== the
// owning plan id) so the wave-scan fallback and dashboard/scoring readers both
// see a well-formed record.
func seedIterRecord(t *testing.T, projectPath string, n int, wave string) {
	t.Helper()
	dir := IterationLogDir(projectPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: 2\niteration: " + strconv.Itoa(n) + "\nwave: \"" + wave + "\"\ntask_id: \"\"\ncommit: \"x\"\ntests_total_pass: true\n"
	if err := os.WriteFile(iterRecordPath(projectPath, n), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedSidecar writes an arbitrary sidecar file next to iter-N.yaml.
func seedSidecar(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readIndex(t *testing.T, projectPath string) []iterIndexEntry {
	t.Helper()
	entries, err := loadIterationIndex(projectPath)
	if err != nil {
		t.Fatalf("loadIterationIndex: %v", err)
	}
	return entries
}

func ns(entries []iterIndexEntry) []int {
	out := make([]int, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.N)
	}
	sort.Ints(out)
	return out
}

// seedIndex upserts a batch of index entries, failing on the first error.
func seedIndex(t *testing.T, proj string, entries ...iterIndexEntry) {
	t.Helper()
	for _, e := range entries {
		if err := upsertIterationIndexEntry(proj, e); err != nil {
			t.Fatal(err)
		}
	}
}

// assertPathsExist fails for any dir/base that is missing.
func assertPathsExist(t *testing.T, dir string, bases ...string) {
	t.Helper()
	for _, b := range bases {
		if _, err := os.Stat(filepath.Join(dir, b)); err != nil {
			t.Errorf("expected %s under %s: %v", b, dir, err)
		}
	}
}

// assertContainsAll fails for any want substring absent from haystack.
func assertContainsAll(t *testing.T, haystack string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(haystack, w) {
			t.Errorf("missing %q in:\n%s", w, haystack)
		}
	}
}

// findIndexEntry returns the entry with the given N (zero value if absent).
func findIndexEntry(entries []iterIndexEntry, n int) iterIndexEntry {
	for _, e := range entries {
		if e.N == n {
			return e
		}
	}
	return iterIndexEntry{}
}

// ── load / parse ───────────────────────────────────────────────────────────

// A MISSING index is a legal no-op: it never errors, and the pure max-scan
// readers (NextIterationNumber / resolveActiveIterationN) are unaffected by its
// absence — the non-negotiable backward-compat invariant.
func TestIterationIndexMissingIsNoOp(t *testing.T) {
	proj := t.TempDir()
	entries, err := loadIterationIndex(proj)
	if err != nil || entries != nil {
		t.Fatalf("missing index must be (nil,nil), got (%v,%v)", entries, err)
	}
	// next-N + active-N derive purely from iter-*.yaml, never the index.
	seedIterRecord(t, proj, 7, "p1")
	if got, _ := NextIterationNumber(IterationLogDir(proj)); got != 8 {
		t.Errorf("NextIterationNumber with no index = %d, want 8", got)
	}
	n, active, _ := resolveActiveIterationN(stdHookOutcomeDeps{}, proj)
	if !active || n != 7 {
		t.Errorf("resolveActiveIterationN = (%d,%v), want (7,true)", n, active)
	}
}

func TestIterationIndexParseSkipsBlankLines(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(IterationLogDir(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "\n{\"n\":1,\"plan_id\":\"p1\"}\n\n{\"n\":2,\"plan_id\":\"p1\"}\n"
	if err := os.WriteFile(iterationIndexPath(proj), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ns(readIndex(t, proj)); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("parsed Ns = %v, want [1 2]", got)
	}
}

func TestIterationIndexParseMalformedLineErrors(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(IterationLogDir(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iterationIndexPath(proj), []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIterationIndex(proj); err == nil {
		t.Fatal("expected parse error on malformed JSONL line")
	}
}

// A single line larger than the scanner's max token size surfaces as a scan
// error rather than silent truncation.
func TestIterationIndexParseHugeLineScanError(t *testing.T) {
	huge := "{\"plan_id\":\"" + strings.Repeat("a", 2*1024*1024) + "\"}"
	if _, err := parseIterationIndex([]byte(huge+"\n"), "x"); err == nil {
		t.Fatal("expected scan error on oversized line")
	}
}

// A read error other than not-exist (here: the index path is a directory)
// propagates rather than being swallowed as absence.
func TestLoadIterationIndexReadErrorPropagates(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(iterationIndexPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIterationIndex(proj); err == nil {
		t.Fatal("expected read error when index path is a directory")
	}
}

// ── upsert / rewrite ───────────────────────────────────────────────────────

func TestUpsertAppendsThenReplacesByN(t *testing.T) {
	proj := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(upsertIterationIndexEntry(proj, iterIndexEntry{N: 1, PlanID: "p1", TaskID: "t1", CreatedAt: "c1"}))
	must(upsertIterationIndexEntry(proj, iterIndexEntry{N: 2, PlanID: "p2", TaskID: "t2", CreatedAt: "c2"}))
	if got := ns(readIndex(t, proj)); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("after two appends Ns = %v, want [1 2]", got)
	}
	// Re-upsert n=1 (cross-role re-entry): one line for n=1, plan/task refreshed,
	// original created_at preserved (first write wins).
	must(upsertIterationIndexEntry(proj, iterIndexEntry{N: 1, PlanID: "p1b", TaskID: "t1b", CreatedAt: "c1-late"}))
	entries := readIndex(t, proj)
	if len(entries) != 2 {
		t.Fatalf("upsert must not duplicate n=1: got %d entries", len(entries))
	}
	var one iterIndexEntry
	for _, e := range entries {
		if e.N == 1 {
			one = e
		}
	}
	if one.PlanID != "p1b" || one.TaskID != "t1b" {
		t.Errorf("plan/task not refreshed on re-upsert: %+v", one)
	}
	if one.CreatedAt != "c1" {
		t.Errorf("created_at must be preserved (first write wins): got %q want c1", one.CreatedAt)
	}
}

func TestUpsertLoadErrorPropagates(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(iterationIndexPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := upsertIterationIndexEntry(proj, iterIndexEntry{N: 1}); err == nil {
		t.Fatal("expected upsert to propagate the load error")
	}
}

func TestRewriteIterationIndexErrorBranches(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(IterationLogDir(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	one := []iterIndexEntry{{N: 1, PlanID: "p1"}}

	t.Run("marshal", func(t *testing.T) {
		prev := jsonMarshal
		jsonMarshal = func(any) ([]byte, error) { return nil, errIndexTestFault }
		t.Cleanup(func() { jsonMarshal = prev })
		if err := rewriteIterationIndex(proj, one); err == nil {
			t.Fatal("expected marshal error")
		}
	})
	t.Run("mkdir", func(t *testing.T) {
		prev := osMkdirAll
		osMkdirAll = func(string, os.FileMode) error { return errIndexTestFault }
		t.Cleanup(func() { osMkdirAll = prev })
		if err := rewriteIterationIndex(proj, one); err == nil {
			t.Fatal("expected mkdir error")
		}
	})
	t.Run("write", func(t *testing.T) {
		prev := osWriteFile
		osWriteFile = func(string, []byte, os.FileMode) error { return errIndexTestFault }
		t.Cleanup(func() { osWriteFile = prev })
		if err := rewriteIterationIndex(proj, one); err == nil {
			t.Fatal("expected write error")
		}
	})
}

// ── query ──────────────────────────────────────────────────────────────────

func TestEntriesForPlan(t *testing.T) {
	entries := []iterIndexEntry{{N: 1, PlanID: "p1"}, {N: 2, PlanID: "p2"}, {N: 3, PlanID: "p1"}}
	if got := ns(entriesForPlan(entries, "p1")); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("forPlan p1 = %v, want [1 3]", got)
	}
	if got := entriesForPlan(entries, ""); got != nil {
		t.Errorf("empty planID must match nothing, got %v", got)
	}
}

func TestIterationsForPlanIndexFirst(t *testing.T) {
	proj := t.TempDir()
	for _, e := range []iterIndexEntry{{N: 1, PlanID: "p1"}, {N: 2, PlanID: "p2"}, {N: 3, PlanID: "p1"}} {
		if err := upsertIterationIndexEntry(proj, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := iterationsForPlan(proj, "p1")
	if err != nil || !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("iterationsForPlan(p1) = (%v,%v), want [1 3]", got, err)
	}
	if got, _ := iterationsForPlan(proj, ""); got != nil {
		t.Errorf("empty planID must resolve to nil, got %v", got)
	}
}

func TestIterationsForPlanWaveScanFallback(t *testing.T) {
	proj := t.TempDir()
	// No index at all → wave-scan over iter-*.yaml.
	seedIterRecord(t, proj, 1, "p1")
	seedIterRecord(t, proj, 2, "p2")
	seedIterRecord(t, proj, 3, "p1")
	// A pathological overflow N is skipped, not fatal.
	if err := os.WriteFile(filepath.Join(IterationLogDir(proj), "iter-99999999999999999999.yaml"),
		[]byte("wave: \"p1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A name-matching directory is skipped (never parsed as a record).
	if err := os.Mkdir(filepath.Join(IterationLogDir(proj), "iter-5.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := iterationsForPlan(proj, "p1")
	if err != nil || !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("wave-scan fallback = (%v,%v), want [1 3]", got, err)
	}
}

func TestIterationsForPlanByWaveScanErrors(t *testing.T) {
	// Missing dir → nil, nil.
	proj := t.TempDir()
	if got, err := iterationsForPlanByWaveScan(proj, "p1"); got != nil || err != nil {
		t.Fatalf("missing dir = (%v,%v), want (nil,nil)", got, err)
	}
	// iteration-log path is a FILE → ReadDir errors.
	proj2 := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(IterationLogDir(proj2)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IterationLogDir(proj2), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := iterationsForPlanByWaveScan(proj2, "p1"); err == nil {
		t.Fatal("expected ReadDir error when iteration-log is a file")
	}
}

func TestReadIterRecordWave(t *testing.T) {
	proj := t.TempDir()
	seedIterRecord(t, proj, 1, "plan-x")
	if got := readIterRecordWave(iterRecordPath(proj, 1)); got != "plan-x" {
		t.Errorf("wave = %q, want plan-x", got)
	}
	if got := readIterRecordWave(filepath.Join(proj, "nope.yaml")); got != "" {
		t.Errorf("missing file must yield empty wave, got %q", got)
	}
	bad := filepath.Join(proj, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n  - ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readIterRecordWave(bad); got != "" {
		t.Errorf("malformed yaml must yield empty wave, got %q", got)
	}
}

func TestRepoRelSlash(t *testing.T) {
	proj := "/base/proj"
	abs := filepath.Join(proj, "a", "b.yaml")
	if got := repoRelSlash(proj, abs); got != "a/b.yaml" {
		t.Errorf("repoRelSlash = %q, want a/b.yaml", got)
	}
	// Rel fails when the target cannot be made relative to an absolute base.
	if got := repoRelSlash("/base", "relative/path"); got != "relative/path" {
		t.Errorf("rel-error fallback = %q, want relative/path", got)
	}
}

// ── archival relocation (non-git unit) ──────────────────────────────────────

func TestArchivePlanIterationsNoIterations(t *testing.T) {
	proj := t.TempDir()
	inc, err := archivePlanIterations(proj, "p1", false)
	if err != nil || inc != nil {
		t.Fatalf("no iterations must be a no-op, got (%v,%v)", inc, err)
	}
}

func TestArchivePlanIterationsDryRunMovesNothing(t *testing.T) {
	proj := t.TempDir()
	seedIterRecord(t, proj, 1, "p1")
	if err := upsertIterationIndexEntry(proj, iterIndexEntry{N: 1, PlanID: "p1"}); err != nil {
		t.Fatal(err)
	}
	inc, err := archivePlanIterations(proj, "p1", true)
	if err != nil || inc != nil {
		t.Fatalf("dry-run must return (nil,nil), got (%v,%v)", inc, err)
	}
	if _, err := os.Stat(iterRecordPath(proj, 1)); err != nil {
		t.Error("dry-run must not move the active record")
	}
	if got := ns(readIndex(t, proj)); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("dry-run must not mutate the index, got %v", got)
	}
}

func TestArchivePlanIterationsRelocatesAndUpdatesIndex(t *testing.T) {
	proj := t.TempDir()
	seedIterRecord(t, proj, 1, "p1")
	seedSidecar(t, hookOutcomeSidecarPath(proj, 1))
	seedSidecar(t, scoreSidecarPath(proj, 1))
	seedIterRecord(t, proj, 2, "p1")
	seedIterRecord(t, proj, 3, "p2") // non-archived plan
	seedIndex(t, proj,
		iterIndexEntry{N: 1, PlanID: "p1", TaskID: "t1", CreatedAt: "c1"},
		iterIndexEntry{N: 2, PlanID: "p1"},
		iterIndexEntry{N: 3, PlanID: "p2"},
	)

	inc, err := archivePlanIterations(proj, "p1", false)
	if err != nil {
		t.Fatalf("archivePlanIterations: %v", err)
	}

	histDir := filepath.Join(historyBaseDir(proj), "p1", historyIterLogSubdir)
	assertPathsExist(t, histDir, iter1Record, "iter-1.hook-outcomes.yaml", "iter-1.score.yaml", "iter-2.yaml")
	// Active p1 records gone; p2 record stays put.
	if _, err := os.Stat(iterRecordPath(proj, 1)); !os.IsNotExist(err) {
		t.Error("active iter-1.yaml should be relocated")
	}
	assertPathsExist(t, IterationLogDir(proj), "iter-3.yaml") // non-archived plan stays
	// Active index now only lists n=3 (p2).
	if got := ns(readIndex(t, proj)); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("active index after archive = %v, want [3]", got)
	}
	// History index preserves the moved entries with metadata.
	hist, err := loadIterationIndexAt(filepath.Join(histDir, iterationIndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ns(hist), []int{1, 2}) {
		t.Errorf("history index = %v, want [1 2]", ns(hist))
	}
	if h1 := findIndexEntry(hist, 1); h1.PlanID != "p1" || h1.TaskID != "t1" || h1.CreatedAt != "c1" {
		t.Errorf("history index lost metadata: %+v", h1)
	}
	// Includes name both source deletions and the active + history indexes.
	assertContainsAll(t, strings.Join(inc, "\n"),
		".agents/active/iteration-log/iter-1.yaml",
		".agents/active/iteration-log/"+iterationIndexFileName,
		".agents/history/p1/iteration-log/iter-1.yaml",
		".agents/history/p1/iteration-log/"+iterationIndexFileName,
	)
}

// Main-requested robustness path: index ABSENT → wave-scan fallback still
// relocates the plan's iterations, and archival does NOT materialize an active
// index where none existed.
func TestArchivePlanIterationsWaveScanFallbackNoIndex(t *testing.T) {
	proj := t.TempDir()
	seedIterRecord(t, proj, 1, "p1")
	seedIterRecord(t, proj, 2, "p2")

	if _, err := archivePlanIterations(proj, "p1", false); err != nil {
		t.Fatalf("archivePlanIterations: %v", err)
	}
	histDir := filepath.Join(historyBaseDir(proj), "p1", historyIterLogSubdir)
	if _, err := os.Stat(filepath.Join(histDir, iter1Record)); err != nil {
		t.Errorf("wave-scan archival must relocate iter-1.yaml: %v", err)
	}
	if _, err := os.Stat(iterRecordPath(proj, 1)); !os.IsNotExist(err) {
		t.Error("active iter-1.yaml should be relocated")
	}
	if _, err := os.Stat(iterRecordPath(proj, 2)); err != nil {
		t.Error("p2's iter-2.yaml must stay put")
	}
	// No active index was created.
	if _, err := os.Stat(iterationIndexPath(proj)); !os.IsNotExist(err) {
		t.Error("archival must not materialize an active index where none existed")
	}
	// History index synthesized from N + planID.
	hist, err := loadIterationIndexAt(filepath.Join(histDir, iterationIndexFileName))
	if err != nil || len(hist) != 1 || hist[0].N != 1 || hist[0].PlanID != "p1" {
		t.Errorf("synthesized history index wrong: %+v (err %v)", hist, err)
	}
}

// A corrupt active index is tolerated: ns comes from the wave-scan fallback,
// the moves proceed, and the corrupt active index is left untouched (no crash).
func TestArchivePlanIterationsCorruptActiveIndexTolerated(t *testing.T) {
	proj := t.TempDir()
	seedIterRecord(t, proj, 1, "p1")
	if err := os.WriteFile(iterationIndexPath(proj), []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := archivePlanIterations(proj, "p1", false); err != nil {
		t.Fatalf("corrupt index must be tolerated, got %v", err)
	}
	histDir := filepath.Join(historyBaseDir(proj), "p1", historyIterLogSubdir)
	if _, err := os.Stat(filepath.Join(histDir, iter1Record)); err != nil {
		t.Errorf("relocation must still happen with a corrupt index: %v", err)
	}
}

func TestArchivePlanIterationsResolveErrorPropagates(t *testing.T) {
	// iteration-log path is a file → both index load and wave-scan error out.
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(IterationLogDir(proj)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IterationLogDir(proj), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := archivePlanIterations(proj, "p1", false); err == nil {
		t.Fatal("expected resolve error to propagate")
	}
}

// archiveSinglePlan surfaces an iteration-log relocation failure with a
// contextual wrap (here the active iteration-log path is a file, so resolving
// the plan's iterations errors) — the plan-dir merge/stamp already ran, but the
// commit is never reached.
func TestArchiveSinglePlanIterLogErrorPropagates(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	if err := os.MkdirAll(filepath.Dir(IterationLogDir(proj)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IterationLogDir(proj), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := archiveSinglePlan(proj, "myplan", false, false, true)
	if err == nil || !strings.Contains(err.Error(), "archive iteration log") {
		t.Fatalf("expected wrapped iteration-log error, got %v", err)
	}
}

func TestArchivePlanIterationsMoveErrorBranches(t *testing.T) {
	seed := func(t *testing.T) string {
		proj := t.TempDir()
		seedIterRecord(t, proj, 1, "p1")
		return proj
	}
	t.Run("mkdir", func(t *testing.T) {
		proj := seed(t)
		prev := osMkdirAll
		osMkdirAll = func(string, os.FileMode) error { return errIndexTestFault }
		t.Cleanup(func() { osMkdirAll = prev })
		if _, err := archivePlanIterations(proj, "p1", false); err == nil {
			t.Fatal("expected mkdir error")
		}
	})
	t.Run("rename", func(t *testing.T) {
		proj := seed(t)
		prev := osRename
		osRename = func(string, string) error { return errIndexTestFault }
		t.Cleanup(func() { osRename = prev })
		if _, err := archivePlanIterations(proj, "p1", false); err == nil {
			t.Fatal("expected rename error")
		}
	})
	t.Run("active-index-rewrite", func(t *testing.T) {
		proj := seed(t)
		if err := upsertIterationIndexEntry(proj, iterIndexEntry{N: 1, PlanID: "p1"}); err != nil {
			t.Fatal(err)
		}
		prev := osWriteFile
		osWriteFile = func(string, []byte, os.FileMode) error { return errIndexTestFault }
		t.Cleanup(func() { osWriteFile = prev })
		if _, err := archivePlanIterations(proj, "p1", false); err == nil {
			t.Fatal("expected active-index rewrite error")
		}
	})
	t.Run("history-index-rewrite", func(t *testing.T) {
		proj := seed(t) // no index → active rewrite skipped, only history write happens
		prev := osWriteFile
		osWriteFile = func(string, []byte, os.FileMode) error { return errIndexTestFault }
		t.Cleanup(func() { osWriteFile = prev })
		if _, err := archivePlanIterations(proj, "p1", false); err == nil {
			t.Fatal("expected history-index rewrite error")
		}
	})
}

// ── creation-path integration ───────────────────────────────────────────────

// Creating iteration N appends an index entry capturing the active
// delegation's plan/task, and a second checkpoint for the same N upserts
// (never duplicates) the line.
func TestCheckpointLogToIterAppendsIndexEntry(t *testing.T) {
	repo := initWorkflowTestRepoWithCommit(t)
	t.Setenv("AGENTS_HOME", t.TempDir())

	delegDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delegDir, 0o755); err != nil {
		t.Fatal(err)
	}
	active := "schema_version: 1\nid: del-z-active\nparent_plan_id: plan-z\nparent_task_id: z-active\n" +
		"title: active\nwrite_scope: []\nstatus: active\ncreated_at: \"2026-04-18T00:00:00Z\"\nupdated_at: \"2026-04-18T00:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(delegDir, "z-active.yaml"), []byte(active), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "6"); err != nil {
		t.Fatalf("checkpoint --log-to-iter 6: %v", err)
	}
	entries := readIndex(t, repo)
	if len(entries) != 1 {
		t.Fatalf("expected one index entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.N != 6 || e.PlanID != "plan-z" || e.TaskID != "z-active" {
		t.Errorf("index entry = %+v, want n=6 plan=plan-z task=z-active", e)
	}
	if e.CreatedAt == "" {
		t.Error("index entry created_at should be populated")
	}

	// Re-checkpoint the same N (cross-role re-entry) upserts, not duplicates.
	if err := executeWorkflowCommand(t, repo, "checkpoint", "--log-to-iter", "6"); err != nil {
		t.Fatalf("second checkpoint --log-to-iter 6: %v", err)
	}
	if entries := readIndex(t, repo); len(entries) != 1 || entries[0].CreatedAt != e.CreatedAt {
		t.Errorf("re-checkpoint must upsert one line preserving created_at, got %+v", entries)
	}
}

// ── archive commit (real git, both backends) ────────────────────────────────

func seedArchiveIterLog(t *testing.T, repo string) {
	t.Helper()
	seedIterRecord(t, repo, 1, "myplan")
	seedSidecar(t, scoreSidecarPath(repo, 1))
	seedIterRecord(t, repo, 2, "otherplan") // a non-archived plan's iteration
	seedIndex(t, repo, iterIndexEntry{N: 1, PlanID: "myplan"}, iterIndexEntry{N: 2, PlanID: "otherplan"})
}

// assertArchiveFoldsIterLog drives one real-git archive under the given backend
// gate and asserts the moves + index landed in ONE archive commit.
func assertArchiveFoldsIterLog(t *testing.T, skip bool) {
	t.Helper()
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)

	priorSkip := planStateSkipped
	planStateSkipped = func() bool { return skip }
	t.Cleanup(func() { planStateSkipped = priorSkip })

	setupArchivePlan(t, dir, "myplan", "completed")
	seedArchiveIterLog(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed plan + iter-log")
	headBefore := gitOut(t, dir, "rev-parse", "HEAD")

	if err := archiveSinglePlan(dir, "myplan", false, false, false); err != nil {
		t.Fatalf("archiveSinglePlan: %v", err)
	}

	if gitOut(t, dir, "rev-parse", "HEAD") == headBefore {
		t.Error("expected a new archive commit; HEAD did not advance")
	}
	if status := gitOut(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("tree not clean after archive (moves/index unstaged?):\n%s", status)
	}

	tree := gitOut(t, dir, "ls-tree", "-r", "HEAD", "--name-only")
	assertContainsAll(t, tree,
		".agents/history/myplan/iteration-log/iter-1.yaml",
		".agents/history/myplan/iteration-log/iter-1.score.yaml",
		".agents/history/myplan/iteration-log/"+iterationIndexFileName,
		".agents/active/iteration-log/iter-2.yaml", // non-archived plan stays
	)
	if strings.Contains(tree, ".agents/active/iteration-log/iter-1.yaml") {
		t.Error("active iter-1.yaml should be deleted from the committed tree")
	}
	// Active index now lists only the non-archived plan's iteration.
	if got := ns(readIndex(t, dir)); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("active index after archive = %v, want [2]", got)
	}
	assertPathsExist(t, IterationLogDir(dir), "iter-2.yaml")
}

// The archive relocates the plan's iterations into history, drops them from the
// active index, and folds the moves + index into ONE archive commit under BOTH
// the local and git-ref backends; a non-archived plan's iterations stay put.
func TestArchiveSinglePlanFoldsIterLogIntoCommit(t *testing.T) {
	for _, tc := range []struct {
		name string
		skip bool
	}{{"local", false}, {"git-ref", true}} {
		t.Run(tc.name, func(t *testing.T) { assertArchiveFoldsIterLog(t, tc.skip) })
	}
}
