package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/journal"
)

// --- test doubles ------------------------------------------------------------

type fakeOpenLister struct {
	prs []openPR
	err error
}

func (f fakeOpenLister) ListOpenPRs(string) ([]openPR, error) { return f.prs, f.err }

type fakeMergedLister struct {
	prs []mergedPR
	err error
}

func (f fakeMergedLister) ListMergedPRs(string) ([]mergedPR, error) { return f.prs, f.err }

type fakeVSource struct {
	name   string
	auth   bool
	checks map[string]journal.RealityCheck
	errs   map[string]error
}

func (f *fakeVSource) Name() string        { return f.name }
func (f *fakeVSource) Authoritative() bool { return f.auth }
func (f *fakeVSource) VerifyTask(key journal.ItemKey, _ journal.ItemState) (journal.RealityCheck, error) {
	if e, ok := f.errs[key.Task]; ok {
		return journal.RealityCheck{}, e
	}
	if c, ok := f.checks[key.Task]; ok {
		return c, nil
	}
	return journal.RealityCheck{}, journal.ErrSourceUnavailable
}

// setupJournalTestRepo creates a git-backed workflow project with the wave-2 plan
// fixture (t1 in_progress, t2 pending, t3 completed) and an isolated journal home.
func setupJournalTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	return repo
}

// chdirProject chdirs into repo and returns the project path the runners resolve
// via currentWorkflowProject (os.Getwd), so test seeding and the runner under test
// agree on the journal home fingerprint even when /tmp is symlinked.
func chdirProject(t *testing.T, repo string) string {
	t.Helper()
	chdirForCov(t, repo)
	p, err := currentWorkflowProject()
	if err != nil {
		t.Fatal(err)
	}
	return p.Path
}

func withJSON(t *testing.T, on bool) {
	t.Helper()
	prev := workflowTestJSON
	workflowTestJSON = on
	t.Cleanup(func() { workflowTestJSON = prev })
}

// --- snapshot ----------------------------------------------------------------

func TestRunWorkflowJournalSnapshot(t *testing.T) {
	repo := setupJournalTestRepo(t)
	chdirForCov(t, repo)

	var buf bytes.Buffer
	if err := runWorkflowJournalSnapshot(&buf); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"journal snapshot captured", "plans:       1", "task(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot text missing %q in:\n%s", want, out)
		}
	}

	withJSON(t, true)
	buf.Reset()
	if err := runWorkflowJournalSnapshot(&buf); err != nil {
		t.Fatalf("snapshot json: %v", err)
	}
	var snap journal.SnapshotState
	if err := json.Unmarshal(buf.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot json: %v\n%s", err, buf.String())
	}
	if len(snap.Plans) != 1 || snap.Plans[0].ID != "wave-2" {
		t.Errorf("unexpected snapshot plans: %+v", snap.Plans)
	}
}

func TestRunWorkflowJournalSnapshotError(t *testing.T) {
	repo := setupJournalTestRepo(t)
	chdirForCov(t, repo)
	prev := journalSnapshot
	journalSnapshot = func(string) (journal.SnapshotState, error) {
		return journal.SnapshotState{}, errors.New("disk full")
	}
	t.Cleanup(func() { journalSnapshot = prev })

	if err := runWorkflowJournalSnapshot(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("want snapshot error, got %v", err)
	}
}

// --- recover -----------------------------------------------------------------

func TestRunWorkflowJournalRecover(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	if _, err := journal.Snapshot(proj); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	emitTestEvent(t, proj) // keep the bundle fresh (D10), not orphaned

	prev := journalVerificationSources
	journalVerificationSources = func(string) []journal.VerificationSource {
		return []journal.VerificationSource{&fakeVSource{
			name: "fake",
			auth: true,
			checks: map[string]journal.RealityCheck{
				"t3": {Exists: true, Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: "deadbeef"}}},
				"t1": {Exists: false},
			},
			errs: map[string]error{"t2": journal.ErrSourceUnavailable},
		}}
	}
	t.Cleanup(func() { journalVerificationSources = prev })

	var buf bytes.Buffer
	if err := runWorkflowJournalRecover(&buf); err != nil {
		t.Fatalf("recover: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"recovery view", "verified (1)", "missing (1)", "unverified (1)", "deadbeef"} {
		if !strings.Contains(out, want) {
			t.Errorf("recover text missing %q in:\n%s", want, out)
		}
	}

	withJSON(t, true)
	buf.Reset()
	if err := runWorkflowJournalRecover(&buf); err != nil {
		t.Fatalf("recover json: %v", err)
	}
	var res journal.RecoveryResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decode recover json: %v\n%s", err, buf.String())
	}
	if len(res.Items) != 3 {
		t.Fatalf("want 3 items, got %d", len(res.Items))
	}
}

func TestRunWorkflowJournalRecoverError(t *testing.T) {
	repo := setupJournalTestRepo(t)
	chdirForCov(t, repo)
	prev := journalRecoveryView
	journalRecoveryView = func(string, journal.Deps) (journal.RecoveryResult, error) {
		return journal.RecoveryResult{}, errors.New("log torn")
	}
	t.Cleanup(func() { journalRecoveryView = prev })
	if err := runWorkflowJournalRecover(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "log torn") {
		t.Fatalf("want recover error, got %v", err)
	}
}

func TestRenderRecover(t *testing.T) {
	res := journal.RecoveryResult{
		Identity:         journal.Identity{Fingerprint: "fp"},
		SnapshotAt:       "2026-06-29T00:00:00Z",
		Freshness:        journal.BundleFreshness{Label: journal.FreshnessStale},
		Quarantined:      true,
		QuarantineReason: "identity mismatch",
		Items: []journal.RecoveredItem{
			{Key: journal.ItemKey{Plan: "p", Task: "a"}, Status: journal.StatusVerified, Trust: journal.TrustHigh, VerifiedBy: "gh", CoordVerified: true,
				Reconstructed: journal.ItemState{Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: "sha"}}}},
			{Key: journal.ItemKey{Plan: "p", Task: "b"}, Status: journal.StatusChanged, Trust: journal.TrustHigh, VerifiedBy: "gh", Delta: "x→y", CoordVerified: true,
				Reconstructed: journal.ItemState{Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{PR: 9}}}},
			{Key: journal.ItemKey{Plan: "p", Task: "c"}, Status: journal.StatusMissing, Trust: journal.TrustMedium},
			{Key: journal.ItemKey{Plan: "p", Task: "d"}, Status: journal.StatusUnverified, Trust: journal.TrustLow,
				Reconstructed: journal.ItemState{Locus: &journal.Locus{}}},
		},
		Conflicts: []journal.IdentityConflict{{Task: "z", Reason: "ambiguous"}},
		Notes:     []string{"snapshot unavailable"},
	}
	var buf bytes.Buffer
	renderRecover(&buf, res)
	out := buf.String()
	for _, want := range []string{
		"recovery view for fp", "snapshot:  2026-06-29", "freshness: stale",
		"QUARANTINED: identity mismatch", "verified (1)", "changed (1)", "missing (1)",
		"unverified (1)", "canonical sha", "in_open_pr #9", "delta: x→y",
		"quarantined conflicts (1)", "task z: ambiguous", "note: snapshot unavailable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderRecover missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderRecoverEmptyAndNoSnapshot(t *testing.T) {
	var buf bytes.Buffer
	renderRecover(&buf, journal.RecoveryResult{})
	out := buf.String()
	if !strings.Contains(out, "replay-only, degraded") || !strings.Contains(out, "no recovered items") {
		t.Errorf("empty recover render missing markers:\n%s", out)
	}
}

func TestDefaultJournalVerificationSources(t *testing.T) {
	repo := setupJournalTestRepo(t)

	prevLister := defaultPRSourceLister
	prevGH := ghJSON
	t.Cleanup(func() { defaultPRSourceLister = prevLister; ghJSON = prevGH })

	// gh available: both sources present, gh first.
	defaultPRSourceLister = fakeOpenLister{}
	ghJSON = func(string, ...string) ([]byte, error) { return []byte("[]"), nil }
	sources := defaultJournalVerificationSources(repo)
	if len(sources) != 2 || sources[0].Name() != journalSourceGH || !sources[0].Authoritative() {
		t.Fatalf("want [gh, local], got %v", sourceNames(sources))
	}
	if sources[1].Name() != journalSourceLocal || sources[1].Authoritative() {
		t.Fatalf("want local non-authoritative last, got %v", sourceNames(sources))
	}

	// gh unavailable: only the local fallback remains.
	defaultPRSourceLister = fakeOpenLister{err: errors.New("gh down")}
	sources = defaultJournalVerificationSources(repo)
	if len(sources) != 1 || sources[0].Name() != journalSourceLocal {
		t.Fatalf("want [local] when gh down, got %v", sourceNames(sources))
	}
}

func sourceNames(s []journal.VerificationSource) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = v.Name()
	}
	return out
}

// --- gh source ---------------------------------------------------------------

func TestNewGHSourceErrors(t *testing.T) {
	if _, err := newGHSource("r", fakeOpenLister{err: errors.New("x")}, fakeMergedLister{}); err == nil {
		t.Error("want error when open list fails")
	}
	if _, err := newGHSource("r", fakeOpenLister{}, fakeMergedLister{err: errors.New("y")}); err == nil {
		t.Error("want error when merged list fails")
	}
	if _, err := newGHSource("r", fakeOpenLister{}, fakeMergedLister{}); err != nil {
		t.Errorf("want ok, got %v", err)
	}
}

func TestGHSourceVerifyTask(t *testing.T) {
	g := &ghSource{
		open:   []openPR{{Number: 42, Branch: "feature/t1"}},
		merged: []mergedPR{{Number: 7, Branch: "feature/t2", MergeSHA: "abc123"}},
	}
	if g.Name() != journalSourceGH || !g.Authoritative() {
		t.Fatalf("identity wrong: %s auth=%v", g.Name(), g.Authoritative())
	}

	inPR := func() journal.ItemState {
		return journal.ItemState{Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}}
	}
	canon := func() journal.ItemState {
		return journal.ItemState{Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: "canonical"}}}
	}
	key := func(task string) journal.ItemKey { return journal.ItemKey{Kind: journal.KindTask, Task: task} }

	// in_open_pr matching an open PR → enriched PR number.
	rc, err := g.VerifyTask(key("t1"), inPR())
	if err != nil || !rc.Exists || rc.Locus.InOpenPR == nil || rc.Locus.InOpenPR.PR != 42 {
		t.Errorf("in_open_pr open match: %+v err=%v", rc, err)
	}
	// in_open_pr that has since merged → canonical merge sha (a change).
	rc, _ = g.VerifyTask(key("t2"), inPR())
	if !rc.Exists || rc.Locus.Canonical == nil || rc.Locus.Canonical.Ref != "abc123" {
		t.Errorf("in_open_pr merged: %+v", rc)
	}
	// in_open_pr with no matching PR at all → gone.
	rc, _ = g.VerifyTask(key("gone"), inPR())
	if rc.Exists {
		t.Errorf("expected missing, got %+v", rc)
	}
	// canonical matching a merged PR → enriched merge sha.
	rc, _ = g.VerifyTask(key("t2"), canon())
	if !rc.Exists || rc.Locus.Canonical.Ref != "abc123" {
		t.Errorf("canonical merged: %+v", rc)
	}
	// canonical with no merged PR in window → defer to local.
	if _, err := g.VerifyTask(key("t1"), canon()); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("canonical no-merge want unavailable, got %v", err)
	}
	// no locus → defer to local.
	if _, err := g.VerifyTask(key("t1"), journal.ItemState{}); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("nil locus want unavailable, got %v", err)
	}
	// locus with neither arm → defer to local.
	if _, err := g.VerifyTask(key("t1"), journal.ItemState{Locus: &journal.Locus{}}); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("empty locus want unavailable, got %v", err)
	}
}

// TestGHSourceStrictMatchAndAmbiguity covers BUG 1: a task id must match a branch
// only as a complete segment (task-002 must NOT bind feature/task-002-extra), and
// when more than one PR resolves to the identity the source must NOT guess — it
// defers (ErrSourceUnavailable) so recovery never asserts a wrong PR/sha.
func TestGHSourceStrictMatchAndAmbiguity(t *testing.T) {
	// strictBranchMatch: complete-segment only.
	if strictBranchMatch("feature/task-002-extra", "", "task-002") {
		t.Error("task-002 must NOT match feature/task-002-extra (prefix of a longer token)")
	}
	if !strictBranchMatch("feature/task-002", "", "task-002") {
		t.Error("task-002 must match feature/task-002 (complete trailing segment)")
	}
	// full-identity qualified form: <plan>-<task> embedded in the branch.
	if !strictBranchMatch("impl/session-handoff-journal-p6", "session-handoff-journal", "p6") {
		t.Error("qualified <plan>-<task> must match")
	}
	// a bare token in the interior with a trailing '-' must not match.
	if strictBranchMatch("impl/p6-followup", "", "p6") {
		t.Error("p6 must NOT match impl/p6-followup (interior, trailing '-')")
	}
	// Repeated token where no occurrence is a complete segment (loop runs off the end).
	if strictBranchMatch("t1t1", "", "t1") {
		t.Error("t1 must NOT match t1t1 (no bounded segment)")
	}

	// Ambiguity: two distinct open PRs both resolve to t1 → no enrichment.
	amb := &ghSource{open: []openPR{{Number: 1, Branch: "a/t1"}, {Number: 2, Branch: "b/t1"}}}
	if _, err := amb.VerifyTask(
		journal.ItemKey{Task: "t1"},
		journal.ItemState{Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}},
	); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("ambiguous open match must defer, got %v", err)
	}
	// Ambiguity on the merged side, canonical arm → defer.
	ambM := &ghSource{merged: []mergedPR{{Number: 3, Branch: "a/t1", MergeSHA: "x"}, {Number: 4, Branch: "b/t1", MergeSHA: "y"}}}
	if _, err := ambM.VerifyTask(
		journal.ItemKey{Task: "t1"},
		journal.ItemState{Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: "canonical"}}},
	); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("ambiguous merged match must defer, got %v", err)
	}
	// in_open_pr item, no open match, but the merged side is ambiguous → defer.
	ambMixed := &ghSource{merged: []mergedPR{{Number: 5, Branch: "a/t1", MergeSHA: "p"}, {Number: 6, Branch: "b/t1", MergeSHA: "q"}}}
	if _, err := ambMixed.VerifyTask(
		journal.ItemKey{Task: "t1"},
		journal.ItemState{Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}},
	); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("in_open_pr with ambiguous merged fallback must defer, got %v", err)
	}
	// Same PR number listed twice is NOT ambiguous (one logical PR).
	dup := &ghSource{open: []openPR{{Number: 9, Branch: "a/t1"}, {Number: 9, Branch: "a/t1"}}}
	rc, err := dup.VerifyTask(journal.ItemKey{Task: "t1"}, journal.ItemState{Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}})
	if err != nil || rc.Locus.InOpenPR.PR != 9 {
		t.Errorf("duplicate same-number PR should resolve to #9, got %+v err=%v", rc, err)
	}
}

func TestParseMergedPRs(t *testing.T) {
	data := []byte(`[{"number":3,"headRefName":"feat/x","mergeCommit":{"oid":"sha1"}}]`)
	prs, err := parseMergedPRs(data)
	if err != nil || len(prs) != 1 || prs[0].MergeSHA != "sha1" || prs[0].Branch != "feat/x" || prs[0].Number != 3 {
		t.Fatalf("parse merged: %+v err=%v", prs, err)
	}
	if _, err := parseMergedPRs([]byte("not json")); err == nil {
		t.Error("want decode error")
	}
}

func TestGHMergedPRLister(t *testing.T) {
	prev := ghJSON
	t.Cleanup(func() { ghJSON = prev })

	ghJSON = func(_ string, args ...string) ([]byte, error) {
		if len(args) == 0 || args[0] != "pr" {
			t.Errorf("unexpected gh args: %v", args)
		}
		return []byte(`[{"number":9,"headRefName":"b","mergeCommit":{"oid":"s"}}]`), nil
	}
	prs, err := ghMergedPRLister{}.ListMergedPRs("r")
	if err != nil || len(prs) != 1 || prs[0].MergeSHA != "s" {
		t.Fatalf("merged lister: %+v err=%v", prs, err)
	}

	ghJSON = func(string, ...string) ([]byte, error) { return nil, errors.New("gh missing") }
	if _, err := (ghMergedPRLister{}).ListMergedPRs("r"); err == nil {
		t.Error("want gh error propagated")
	}
}

// --- local source ------------------------------------------------------------

func TestLocalSourceVerifyTask(t *testing.T) {
	tasks := &CanonicalTaskFile{PlanID: "p", Tasks: []CanonicalTask{
		{ID: "done", Status: TaskStatusCompleted},
		{ID: "review", Status: TaskStatusAwaitingOwnerReview},
		{ID: "pend", Status: TaskStatusPending},
	}}
	s := &localSource{repoPath: "r", load: func(string, string) (*CanonicalTaskFile, error) { return tasks, nil }}
	if s.Name() != journalSourceLocal || s.Authoritative() {
		t.Fatalf("identity wrong: %s auth=%v", s.Name(), s.Authoritative())
	}

	cases := []struct {
		task   string
		exists bool
		status string
		arm    string
	}{
		{"done", true, TaskStatusCompleted, "canonical"},
		{"review", true, TaskStatusAwaitingOwnerReview, "in_open_pr"},
		{"pend", true, TaskStatusPending, "none"},
		{"ghost", false, "", "none"},
	}
	for _, c := range cases {
		rc, err := s.VerifyTask(journal.ItemKey{Plan: "p", Task: c.task}, journal.ItemState{})
		if err != nil {
			t.Fatalf("%s: %v", c.task, err)
		}
		if rc.Exists != c.exists || rc.Status != c.status || locusArmName(rc.Locus) != c.arm {
			t.Errorf("%s: got exists=%v status=%q arm=%q", c.task, rc.Exists, rc.Status, locusArmName(rc.Locus))
		}
	}
}

func TestLocalSourceLoadErrors(t *testing.T) {
	// not-exist → task missing (plan gone).
	s := &localSource{repoPath: "r", load: func(string, string) (*CanonicalTaskFile, error) { return nil, os.ErrNotExist }}
	rc, err := s.VerifyTask(journal.ItemKey{Plan: "p", Task: "t"}, journal.ItemState{})
	if err != nil || rc.Exists {
		t.Errorf("not-exist want missing, got %+v err=%v", rc, err)
	}
	// other error → unavailable.
	s.load = func(string, string) (*CanonicalTaskFile, error) { return nil, errors.New("parse boom") }
	if _, err := s.VerifyTask(journal.ItemKey{Plan: "p", Task: "t"}, journal.ItemState{}); !errors.Is(err, journal.ErrSourceUnavailable) {
		t.Errorf("parse error want unavailable, got %v", err)
	}
}

func locusArmName(l *journal.Locus) string {
	switch {
	case l == nil:
		return "none"
	case l.Canonical != nil:
		return "canonical"
	case l.InOpenPR != nil:
		return "in_open_pr"
	default:
		return "none"
	}
}

// --- show --------------------------------------------------------------------

func TestRunWorkflowJournalShow(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	if _, err := journal.Snapshot(proj); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	emitTestEvent(t, proj)

	var buf bytes.Buffer
	if err := runWorkflowJournalShow(&buf, 20, false); err != nil {
		t.Fatalf("show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "snapshot ") || !strings.Contains(out, "events (1") || !strings.Contains(out, "workflow advance") {
		t.Errorf("show text missing markers:\n%s", out)
	}

	withJSON(t, true)
	buf.Reset()
	if err := runWorkflowJournalShow(&buf, 20, true); err != nil {
		t.Fatalf("show json: %v", err)
	}
	var res journalShowResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decode show json: %v\n%s", err, buf.String())
	}
	if res.Snapshot == nil || res.EventCount != 1 {
		t.Errorf("unexpected show result: snapshot=%v count=%d", res.Snapshot != nil, res.EventCount)
	}
}

func TestRunWorkflowJournalShowEmpty(t *testing.T) {
	repo := setupJournalTestRepo(t)
	chdirForCov(t, repo)
	var buf bytes.Buffer
	if err := runWorkflowJournalShow(&buf, 20, false); err != nil {
		t.Fatalf("show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "none captured yet") || !strings.Contains(out, "no events") {
		t.Errorf("empty show missing markers:\n%s", out)
	}
}

// --- prune -------------------------------------------------------------------

func TestPruneJournal(t *testing.T) {
	repo := setupJournalTestRepo(t)
	for i := 0; i < 5; i++ {
		emitTestEvent(t, repo)
	}

	// dry-run: computes but does not write.
	res, err := pruneJournal(repo, 2, true)
	if err != nil {
		t.Fatalf("dry prune: %v", err)
	}
	if res.Total != 5 || res.Kept != 2 || res.Removed != 3 || !res.DryRun {
		t.Fatalf("dry result: %+v", res)
	}
	if events, _ := readJournalEvents(repo); len(events) != 5 {
		t.Fatalf("dry-run must not write, have %d", len(events))
	}

	// real prune: keep the newest 2.
	res, err = pruneJournal(repo, 2, false)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Removed != 3 {
		t.Fatalf("want removed 3, got %+v", res)
	}
	if events, _ := readJournalEvents(repo); len(events) != 2 {
		t.Fatalf("want 2 events after prune, got %d", len(events))
	}

	// keep >= total: no-op.
	res, _ = pruneJournal(repo, 100, false)
	if res.Removed != 0 {
		t.Fatalf("want no removal, got %+v", res)
	}
}

func TestRunWorkflowJournalPrune(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	for i := 0; i < 3; i++ {
		emitTestEvent(t, proj)
	}

	if err := runWorkflowJournalPrune(&bytes.Buffer{}, -1); err == nil {
		t.Fatal("want error for negative keep")
	}

	var buf bytes.Buffer
	if err := runWorkflowJournalPrune(&buf, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !strings.Contains(buf.String(), "journal prune:") {
		t.Errorf("prune text missing summary:\n%s", buf.String())
	}

	withJSON(t, true)
	buf.Reset()
	if err := runWorkflowJournalPrune(&buf, 0); err != nil {
		t.Fatalf("prune json: %v", err)
	}
	var res journalPruneResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decode prune json: %v", err)
	}
	if res.Kept != 0 {
		t.Errorf("want kept 0, got %+v", res)
	}
}

func TestRenderJournalPruneDryRun(t *testing.T) {
	var buf bytes.Buffer
	renderJournalPrune(&buf, journalPruneResult{Total: 5, Kept: 2, Removed: 3, DryRun: true, Path: "p"})
	if !strings.Contains(buf.String(), "would remove 3") {
		t.Errorf("dry-run verb missing:\n%s", buf.String())
	}
}

// --- append ------------------------------------------------------------------

func TestRunWorkflowJournalAppend(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)

	var buf bytes.Buffer
	in := journalAppendInput{Command: "workflow advance", Actor: "main", EventType: "durable_delta", Input: `{"plan":"p"}`}
	if err := runWorkflowJournalAppend(&buf, in); err != nil {
		t.Fatalf("append: %v", err)
	}
	if !strings.Contains(buf.String(), "recorded") {
		t.Errorf("append text missing confirmation:\n%s", buf.String())
	}
	if events, _ := readJournalEvents(proj); len(events) != 1 || events[0].Command != "workflow advance" {
		t.Fatalf("append did not write the event: %+v", events)
	}

	withJSON(t, true)
	buf.Reset()
	if err := runWorkflowJournalAppend(&buf, journalAppendInput{Command: "workflow commit", Actor: "main", EventType: "durable_delta"}); err != nil {
		t.Fatalf("append json: %v", err)
	}
	var res journalAppendResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decode append json: %v", err)
	}
	if !res.Appended || res.Command != "workflow commit" {
		t.Errorf("unexpected append result: %+v", res)
	}
}

func TestRunWorkflowJournalAppendEmitError(t *testing.T) {
	repo := setupJournalTestRepo(t)
	chdirForCov(t, repo)
	prev := journalEmit
	journalEmit = func(string, journal.Envelope) error { return errors.New("lock held") }
	t.Cleanup(func() { journalEmit = prev })
	err := runWorkflowJournalAppend(&bytes.Buffer{}, journalAppendInput{Command: "workflow advance", Actor: "main", EventType: "durable_delta"})
	if err == nil || !strings.Contains(err.Error(), "lock held") {
		t.Fatalf("want emit error, got %v", err)
	}
}

func TestBuildAppendEnvelope(t *testing.T) {
	ok, err := buildAppendEnvelope(journalAppendInput{
		Command: "workflow advance", Actor: "orchestrator", EventType: "failed",
		Input: `{"a":1}`, Observed: `{"b":2}`,
	})
	if err != nil {
		t.Fatalf("valid build: %v", err)
	}
	if ok.Actor != journal.ActorOrchestrator || ok.EventType != journal.EventFailed || ok.Input == nil || ok.Observed == nil {
		t.Errorf("unexpected envelope: %+v", ok)
	}

	bad := []journalAppendInput{
		{Command: "  "},
		{Command: "c", Actor: "nope"},
		{Command: "c", Actor: "main", EventType: "weird"},
		{Command: "c", Actor: "main", EventType: "failed", Input: "{bad"},
		{Command: "c", Actor: "main", EventType: "failed", Observed: "{bad"},
	}
	for i, in := range bad {
		if _, err := buildAppendEnvelope(in); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func TestReadJournalEventsAndChrono(t *testing.T) {
	repo := setupJournalTestRepo(t)

	// missing log → empty, no error.
	if events, err := readJournalEvents(repo); err != nil || len(events) != 0 {
		t.Fatalf("missing log: %v %d", err, len(events))
	}

	// write a log with a blank line and a malformed line (skipped).
	path := journal.EventsLogPath(repo)
	if err := os.MkdirAll(journal.RepoDir(repo), 0o700); err != nil {
		t.Fatal(err)
	}
	lines := "" +
		`{"ts":"2026-06-29T00:00:02Z","seq":2,"command":"b"}` + "\n" +
		"\n" +
		"not-json\n" +
		`{"ts":"2026-06-29T00:00:01Z","seq":1,"command":"a"}` + "\n" +
		`{"ts":"","seq":9,"command":"noTS9"}` + "\n" +
		`{"ts":"","seq":5,"command":"noTS5"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := readJournalEvents(repo)
	if err != nil || len(events) != 4 {
		t.Fatalf("want 4 well-formed events, got %d err=%v", len(events), err)
	}
	sortEventsChrono(events)
	// parseable timestamps first (a before b), then unparseable ones ordered by Seq.
	if events[0].Command != "a" || events[1].Command != "b" ||
		events[2].Command != "noTS5" || events[3].Command != "noTS9" {
		t.Errorf("chrono order wrong: %v", []string{events[0].Command, events[1].Command, events[2].Command, events[3].Command})
	}

	// latest TS is the newest parseable event.
	if got := latestJournalEventTS(repo); got.IsZero() {
		t.Error("want non-zero latest ts")
	}
}

func TestReadJournalEventsReadError(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	// Make events.log a directory so ReadFile fails with a non-not-exist error.
	if err := os.MkdirAll(journal.EventsLogPath(proj), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := readJournalEvents(proj); err == nil {
		t.Error("want read error for directory path")
	}
	if got := latestJournalEventTS(proj); !got.IsZero() {
		t.Error("want zero ts on read error")
	}
	// prune surfaces the same read error (runner + helper, both prune paths).
	if _, err := pruneJournal(proj, 1, true); err == nil {
		t.Error("pruneJournal dry-run want read error")
	}
	if _, err := pruneJournal(proj, 1, false); err == nil {
		t.Error("pruneJournal want read error")
	}
	if err := runWorkflowJournalPrune(&bytes.Buffer{}, 1); err == nil {
		t.Error("runWorkflowJournalPrune want read error")
	}
}

func TestParseEventTS(t *testing.T) {
	if !parseEventTS("not-a-time").IsZero() {
		t.Error("bad ts must parse to zero time")
	}
	if parseEventTS("2026-06-29T00:00:00Z").IsZero() {
		t.Error("good ts must parse")
	}
}

func TestTailEvents(t *testing.T) {
	ev := []journal.Envelope{{Seq: 1}, {Seq: 2}, {Seq: 3}}
	if got := tailEvents(ev, 0); len(got) != 0 {
		t.Errorf("zero limit: %d", len(got))
	}
	if got := tailEvents(ev, 10); len(got) != 3 {
		t.Errorf("over limit: %d", len(got))
	}
	if got := tailEvents(ev, 2); len(got) != 2 || got[0].Seq != 2 {
		t.Errorf("tail: %+v", got)
	}
}

func TestLocalLocusForStatusAndMisc(t *testing.T) {
	if localLocusForStatus(TaskStatusCompleted).Canonical == nil {
		t.Error("completed → canonical")
	}
	if localLocusForStatus(TaskStatusAwaitingOwnerReview).InOpenPR == nil {
		t.Error("awaiting → in_open_pr")
	}
	if localLocusForStatus("in_progress") != nil {
		t.Error("other → nil")
	}
	if describeRecoveredLocus(nil, false) != "" || describeRecoveredLocus(&journal.Locus{}, false) != "" {
		t.Error("nil/empty locus should describe as empty")
	}
	if safeDryRun() { // deps.Flags.DryRun is unset in tests → false
		t.Error("safeDryRun must tolerate an unset DryRun seam")
	}
}

// TestRecoverCoordVerifiedOnlyAuthoritative covers BUG 3: a concrete locus coord
// (a reconstructed in_open_pr #7) is rendered as a confirmed fact ONLY when an
// AUTHORITATIVE source confirmed it. A non-authoritative (local) verification that
// agrees on the arm but has no coord opinion must render it as "PR unconfirmed".
func TestRecoverCoordVerifiedOnlyAuthoritative(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	if _, err := journal.Snapshot(proj); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	// Replay an event that reconstructs wave-2/t2 as in_open_pr carrying a CONCRETE
	// PR #7 (the snapshot itself only records a placeholder PR 0).
	ev, err := journal.NewEvent("workflow advance", journal.ActorMain,
		&journal.AdvanceInput{Plan: "wave-2", Task: "t2"},
		&journal.AdvanceObserved{ToStatus: TaskStatusAwaitingOwnerReview,
			Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{PR: 7, Status: TaskStatusAwaitingOwnerReview}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Emit(proj, ev); err != nil {
		t.Fatal(err)
	}

	findT2 := func(res journal.RecoveryResult) journal.RecoveredItem {
		for _, it := range res.Items {
			if it.Key.Task == "t2" {
				return it
			}
		}
		t.Fatalf("t2 not in recovery items: %+v", res.Items)
		return journal.RecoveredItem{}
	}

	// (a) gh unavailable → local fallback confirms status + arm, NOT the coord.
	local := &fakeVSource{name: journalSourceLocal, auth: false, checks: map[string]journal.RealityCheck{
		"t2": {Exists: true, Status: TaskStatusAwaitingOwnerReview, Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}},
	}}
	res, err := journal.RecoveryView(proj, journal.Deps{Sources: []journal.VerificationSource{local}, LastReasonedWrite: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if it := findT2(res); it.Status != journal.StatusVerified || it.CoordVerified {
		t.Errorf("local-verified must NOT confirm coord: status=%s coordVerified=%v", it.Status, it.CoordVerified)
	}
	var buf bytes.Buffer
	renderRecover(&buf, res)
	if !strings.Contains(buf.String(), "in_open_pr (PR unconfirmed)") || strings.Contains(buf.String(), "#7") {
		t.Errorf("stale #7 must render as unconfirmed, not a fact:\n%s", buf.String())
	}

	// (b) authoritative gh confirms PR #7 → CoordVerified, rendered as the fact.
	gh := &fakeVSource{name: journalSourceGH, auth: true, checks: map[string]journal.RealityCheck{
		"t2": {Exists: true, Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{PR: 7}}},
	}}
	res2, err := journal.RecoveryView(proj, journal.Deps{Sources: []journal.VerificationSource{gh}, LastReasonedWrite: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if it := findT2(res2); !it.CoordVerified {
		t.Errorf("authoritative match must confirm coord: %+v", it)
	}
	buf.Reset()
	renderRecover(&buf, res2)
	if !strings.Contains(buf.String(), "in_open_pr #7") {
		t.Errorf("authoritative coord must render as #7:\n%s", buf.String())
	}
}

// TestPruneLockFirstNoLostUpdate covers BUG 2: an append that races a prune is not
// lost. prunePersist now holds the advisory lock across the whole read→rewrite, so
// a concurrent append blocks on the same lock until after the rewrite and lands on
// the pruned log. The hook fires after the locked read but before the rewrite; a
// goroutine started there appends a marker that must survive.
func TestPruneLockFirstNoLostUpdate(t *testing.T) {
	repo := setupJournalTestRepo(t)
	proj := chdirProject(t, repo)
	for i := 0; i < 4; i++ {
		emitTestEvent(t, proj)
	}

	appendStarted := make(chan struct{})
	appendDone := make(chan error, 1)
	prev := pruneAfterReadHook
	pruneAfterReadHook = func() {
		go func() {
			close(appendStarted)
			// This Emit must block on the advisory lock prunePersist holds, and only
			// complete after the rewrite — proving no lost update.
			ev, _ := journal.NewEvent("workflow commit", journal.ActorMain,
				&journal.CommitInput{}, &journal.CommitObserved{Noop: true})
			appendDone <- journal.Emit(proj, ev)
		}()
		<-appendStarted
		time.Sleep(30 * time.Millisecond) // let the goroutine reach the blocking lock
		pruneAfterReadHook = prev         // fire once
	}
	t.Cleanup(func() { pruneAfterReadHook = prev })

	res, err := pruneJournal(proj, 1, false)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Removed != 3 {
		t.Fatalf("want removed 3 from the read-snapshot, got %+v", res)
	}
	if err := <-appendDone; err != nil {
		t.Fatalf("concurrent append: %v", err)
	}

	events, err := readJournalEvents(proj)
	if err != nil {
		t.Fatal(err)
	}
	// The pruned log keeps 1 of the original 4, plus the concurrent append = 2; the
	// marker append is NOT lost.
	if len(events) != 2 {
		t.Fatalf("want 2 events (1 kept + 1 raced append), got %d", len(events))
	}
	hasMarker := false
	for _, e := range events {
		if e.Command == "workflow commit" {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Errorf("raced append was lost: %+v", events)
	}
}

// --- cobra wiring ------------------------------------------------------------

func TestJournalCobraWiring(t *testing.T) {
	repo := setupJournalTestRepo(t)
	// Keep recover hermetic (no real gh) by injecting a no-op source list.
	prev := journalVerificationSources
	journalVerificationSources = func(string) []journal.VerificationSource { return nil }
	t.Cleanup(func() { journalVerificationSources = prev })

	for _, args := range [][]string{
		{"journal", "snapshot"},
		{"journal", "show"},
		{"journal", "recover"},
		{"journal", "prune", "--keep", "5"},
		{"journal", "append", "--command", "workflow advance"},
	} {
		if err := executeWorkflowCommand(t, repo, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// emitTestEvent appends one well-formed event to the repo's journal.
func emitTestEvent(t *testing.T, repo string) {
	t.Helper()
	ev, err := journal.NewEvent("workflow advance", journal.ActorMain,
		&journal.AdvanceInput{Plan: "wave-2", Task: "t1"},
		&journal.AdvanceObserved{FromStatus: "pending", ToStatus: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Emit(repo, ev); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond) // distinct nanosecond timestamps for ordering
}
