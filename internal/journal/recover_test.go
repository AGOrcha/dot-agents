package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// recTestRepo points the journal at a fresh temp XDG state dir, freezes the clock,
// and returns a non-git repo path. The journal home (events.log / snapshot.json)
// lives under RepoDir(repo); fixtures below write into it directly.
func recTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origNow := now
	now = func() time.Time { return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = origNow })
	return t.TempDir()
}

// writeEventsFixture marshals envelopes to NDJSON and writes events.log.
func writeEventsFixture(t *testing.T, repo string, envs ...Envelope) {
	t.Helper()
	if err := os.MkdirAll(RepoDir(repo), 0o700); err != nil {
		t.Fatalf("mkdir journal home: %v", err)
	}
	var buf bytes.Buffer
	for _, e := range envs {
		line, err := e.MarshalLine()
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		buf.Write(line)
	}
	if err := os.WriteFile(EventsLogPath(repo), buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write events.log: %v", err)
	}
}

// writeRawEvents writes arbitrary raw bytes as events.log (for malformed-line tests).
func writeRawEvents(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.MkdirAll(RepoDir(repo), 0o700); err != nil {
		t.Fatalf("mkdir journal home: %v", err)
	}
	if err := os.WriteFile(EventsLogPath(repo), []byte(content), 0o600); err != nil {
		t.Fatalf("write events.log: %v", err)
	}
}

// writeSnapshotFixture persists a SnapshotState as snapshot.json.
func writeSnapshotFixture(t *testing.T, repo string, snap SnapshotState) {
	t.Helper()
	if err := os.MkdirAll(RepoDir(repo), 0o700); err != nil {
		t.Fatalf("mkdir journal home: %v", err)
	}
	data, err := marshalSnapshot(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := os.WriteFile(SnapshotPath(repo), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write snapshot.json: %v", err)
	}
}

// event builds a typed envelope with an explicit ts/seq (NewEvent omits those;
// Emit normally stamps them, but tests set them directly to control replay order
// and the snapshot watermark).
func event(t *testing.T, cmd, ts string, seq int64, in, obs any) Envelope {
	t.Helper()
	e, err := NewEvent(cmd, ActorMain, in, obs)
	if err != nil {
		t.Fatalf("NewEvent(%s): %v", cmd, err)
	}
	e.Schema = Schema
	e.Version = Version
	e.TS = ts
	e.Seq = seq
	return e
}

// snapWith builds a one-plan snapshot keyed to the repo's own identity, captured at
// the frozen clock, with the given tasks.
func snapWith(repo, plan string, tasks ...TaskState) SnapshotState {
	return SnapshotState{
		Schema:     SnapshotSchema,
		Version:    SnapshotVersion,
		CapturedAt: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC).Format(timeFormat),
		Identity:   ResolveIdentity(repo),
		Plans:      []PlanState{{ID: plan, Status: "active", Tasks: tasks}},
	}
}

// fakeSource is an injectable VerificationSource keyed by "plan/task".
type fakeSource struct {
	name   string
	auth   bool
	checks map[string]RealityCheck
	errs   map[string]error
}

func (f fakeSource) Name() string        { return f.name }
func (f fakeSource) Authoritative() bool { return f.auth }
func (f fakeSource) VerifyTask(key ItemKey, _ ItemState) (RealityCheck, error) {
	k := key.Plan + "/" + key.Task
	if f.errs != nil {
		if e, ok := f.errs[k]; ok {
			return RealityCheck{}, e
		}
	}
	if c, ok := f.checks[k]; ok {
		return c, nil
	}
	return RealityCheck{}, ErrSourceUnavailable
}

func itemByTask(items []RecoveredItem, task string) (RecoveredItem, bool) {
	for _, it := range items {
		if it.Key.Task == task {
			return it, true
		}
	}
	return RecoveredItem{}, false
}

// TestRecoveryReplayOverSnapshot proves events after the watermark move the
// reconstructed state past the snapshot, while events at/before it are ignored.
func TestRecoveryReplayOverSnapshot(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "t1", Status: statusPending},
	))
	// One pre-watermark event (must be ignored) and one post-watermark advance.
	writeEventsFixture(t, repo,
		event(t, CmdAdvance, "2026-06-29T11:00:00Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "blocked"}),
		event(t, CmdAdvance, "2026-06-29T13:00:00Z", 2,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "in_progress"}),
	)

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, ok := itemByTask(res.Items, "t1")
	if !ok {
		t.Fatalf("t1 not in view: %+v", res.Items)
	}
	if it.Reconstructed.Status != "in_progress" {
		t.Fatalf("t1 status = %q, want in_progress (post-watermark replay)", it.Reconstructed.Status)
	}
	// No sources → unverified at low trust (never injected as fact).
	if it.Status != StatusUnverified || it.Trust != TrustLow {
		t.Fatalf("t1 unverified/low expected, got %s/%s", it.Status, it.Trust)
	}
}

// TestRecoveryVerifiedChangedMissing covers the three core tags + delta strings.
func TestRecoveryVerifiedChangedMissing(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "tv", Status: "in_progress"},
		TaskState{ID: "tc", Status: "in_progress"},
		TaskState{ID: "tm", Status: "in_progress"},
	))
	src := fakeSource{name: "gh", auth: true, checks: map[string]RealityCheck{
		"alpha/tv": {Exists: true, Status: "in_progress"},
		"alpha/tc": {Exists: true, Status: "completed"},
		"alpha/tm": {Exists: false},
	}}

	res, err := RecoveryView(repo, Deps{Sources: []VerificationSource{src}})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}

	tv, _ := itemByTask(res.Items, "tv")
	if tv.Status != StatusVerified || tv.Trust != TrustHigh || tv.VerifiedBy != "gh" {
		t.Fatalf("tv = %+v, want verified/high/gh", tv)
	}
	if tv.Delta != "" {
		t.Fatalf("verified item should carry no delta, got %q", tv.Delta)
	}

	tc, _ := itemByTask(res.Items, "tc")
	if tc.Status != StatusChanged || tc.Reality == nil || tc.Reality.Status != "completed" {
		t.Fatalf("tc = %+v, want changed with reality completed", tc)
	}
	if tc.Delta == "" {
		t.Fatalf("changed item must carry an explicit delta")
	}

	tm, _ := itemByTask(res.Items, "tm")
	if tm.Status != StatusMissing || tm.Delta == "" {
		t.Fatalf("tm = %+v, want missing with delta", tm)
	}
}

// TestRecoveryTrustGradientAndFallback proves R7: an authoritative source yields
// high trust; when it is unavailable the local fallback yields medium trust; with
// no source the item is low.
func TestRecoveryTrustGradientAndFallback(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "thigh", Status: "completed"},
		TaskState{ID: "tmed", Status: "completed"},
		TaskState{ID: "tlow", Status: "completed"},
	))
	// Remote answers thigh, is unavailable for tmed and tlow. Local answers tmed.
	remote := fakeSource{name: "gh", auth: true, checks: map[string]RealityCheck{
		"alpha/thigh": {Exists: true, Status: "completed"},
	}}
	local := fakeSource{name: "git", auth: false, checks: map[string]RealityCheck{
		"alpha/tmed": {Exists: true, Status: "completed"},
	}}

	res, err := RecoveryView(repo, Deps{Sources: []VerificationSource{remote, local}})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	thigh, _ := itemByTask(res.Items, "thigh")
	if thigh.Trust != TrustHigh || thigh.VerifiedBy != "gh" {
		t.Fatalf("thigh = %+v, want high/gh", thigh)
	}
	tmed, _ := itemByTask(res.Items, "tmed")
	if tmed.Trust != TrustMedium || tmed.VerifiedBy != "git" || tmed.Status != StatusVerified {
		t.Fatalf("tmed = %+v, want medium/git/verified (remote-unavailable fallback)", tmed)
	}
	tlow, _ := itemByTask(res.Items, "tlow")
	if tlow.Trust != TrustLow || tlow.Status != StatusUnverified {
		t.Fatalf("tlow = %+v, want low/unverified", tlow)
	}
}

// TestRecoveryLocusEnrichmentNoConflation proves R8: a placeholder in-PR locus is
// enriched with the real PR number when still open (verified), and a merged item
// (in-PR → canonical) is tagged changed, never conflated as still-in-PR.
func TestRecoveryLocusEnrichmentNoConflation(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "topen", Status: statusAwaitingOwnerReview,
			Locus: &Locus{InOpenPR: &InOpenPRRef{Status: statusAwaitingOwnerReview}}},
		TaskState{ID: "tmerged", Status: statusAwaitingOwnerReview,
			Locus: &Locus{InOpenPR: &InOpenPRRef{Status: statusAwaitingOwnerReview}}},
		TaskState{ID: "tcanon", Status: statusCompleted,
			Locus: &Locus{Canonical: &CanonicalRef{Ref: canonicalLocusRef}}},
	))
	src := fakeSource{name: "gh", auth: true, checks: map[string]RealityCheck{
		// still open in PR #90 → verified + enrich PR number.
		"alpha/topen": {Exists: true, Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 90, Status: statusAwaitingOwnerReview}}},
		// merged → reality is canonical → changed (no conflation).
		"alpha/tmerged": {Exists: true, Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@abc123"}}},
		// landed → enrich the placeholder canonical ref with the real merge sha.
		"alpha/tcanon": {Exists: true, Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@def456"}}},
	}}

	res, err := RecoveryView(repo, Deps{Sources: []VerificationSource{src}})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}

	topen, _ := itemByTask(res.Items, "topen")
	if topen.Status != StatusVerified {
		t.Fatalf("topen status = %s, want verified", topen.Status)
	}
	if topen.Reconstructed.Locus.InOpenPR == nil || topen.Reconstructed.Locus.InOpenPR.PR != 90 {
		t.Fatalf("topen PR not enriched: %+v", topen.Reconstructed.Locus.InOpenPR)
	}

	tmerged, _ := itemByTask(res.Items, "tmerged")
	if tmerged.Status != StatusChanged {
		t.Fatalf("tmerged status = %s, want changed (merged out of PR)", tmerged.Status)
	}
	if tmerged.Delta == "" {
		t.Fatalf("tmerged must carry a delta recording the in-PR→merged transition")
	}
	// Not silently conflated: it is tagged changed WITH a delta. And the item is
	// moved to current truth (canonical), never left asserting the stale in-PR state.
	if locusArm(tmerged.Reconstructed.Locus) != armCanonical {
		t.Fatalf("tmerged reconstructed arm = %s, want canonical (adopted current truth)", locusArm(tmerged.Reconstructed.Locus))
	}
	if tmerged.Reality == nil || locusArm(tmerged.Reality.Locus) != armCanonical {
		t.Fatalf("tmerged reality arm not canonical: %+v", tmerged.Reality)
	}

	tcanon, _ := itemByTask(res.Items, "tcanon")
	if tcanon.Status != StatusVerified || tcanon.Reconstructed.Locus.Canonical.Ref != "master@def456" {
		t.Fatalf("tcanon = %+v, want verified + enriched canonical ref", tcanon.Reconstructed.Locus.Canonical)
	}
}

// TestRecoveryMergeBackLocusEnrich proves a merge-back event (task-only input) is
// resolved to the single owning plan and applies its locus.
func TestRecoveryMergeBackResolvesSinglePlan(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "t1", Status: "in_progress"},
	))
	writeEventsFixture(t, repo,
		event(t, CmdMergeBack, "2026-06-29T13:00:00Z", 1,
			&MergeBackInput{Task: "t1"},
			&MergeBackObserved{Verdict: "pass", Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 7, Status: statusAwaitingOwnerReview}}}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Locus == nil || it.Reconstructed.Locus.InOpenPR == nil || it.Reconstructed.Locus.InOpenPR.PR != 7 {
		t.Fatalf("merge-back locus not applied to alpha/t1: %+v", it.Reconstructed.Locus)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}
}

// TestRecoveryIdentityCollisionQuarantine proves D8: a merge-back whose task id is
// present in multiple plans cannot be disambiguated and is quarantined as a
// surfaced conflict, never silently merged onto a guessed plan.
func TestRecoveryIdentityCollisionQuarantine(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t2", Status: "in_progress"})
	snap.Plans = append(snap.Plans, PlanState{ID: "bravo", Status: "active",
		Tasks: []TaskState{{ID: "t2", Status: "in_progress"}}})
	writeSnapshotFixture(t, repo, snap)
	writeEventsFixture(t, repo,
		event(t, CmdMergeBack, "2026-06-29T13:00:00Z", 1,
			&MergeBackInput{Task: "t2"},
			&MergeBackObserved{Verdict: "pass", Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@x"}}}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Task != "t2" {
		t.Fatalf("want one t2 conflict, got %+v", res.Conflicts)
	}
	if len(res.Conflicts[0].CandidatePlans) != 2 {
		t.Fatalf("conflict candidates = %v, want alpha+bravo", res.Conflicts[0].CandidatePlans)
	}
	// The ambiguous merge-back must NOT have flipped either item's locus.
	for _, it := range res.Items {
		if it.Reconstructed.Locus != nil {
			t.Fatalf("ambiguous merge-back silently merged onto %s/%s: %+v", it.Key.Plan, it.Key.Task, it.Reconstructed.Locus)
		}
	}
}

// TestRecoveryIdentityMismatchQuarantine proves D8 bundle quarantine when the
// snapshot was captured against a different repo identity than the resuming session.
func TestRecoveryIdentityMismatchQuarantine(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: "completed"})
	snap.Identity = Identity{Fingerprint: "deadbeefdeadbeef"} // foreign identity
	writeSnapshotFixture(t, repo, snap)

	res, err := RecoveryView(repo, Deps{LastReasonedWrite: parseTS(snap.CapturedAt)})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if !res.Quarantined || res.QuarantineReason == "" {
		t.Fatalf("want bundle quarantined on identity mismatch, got %+v", res)
	}
}

// TestRecoveryFreshnessGradient covers D10 fresh / stale / orphaned.
func TestRecoveryFreshnessGradient(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: "completed"})
	writeSnapshotFixture(t, repo, snap)
	snapAt := parseTS(snap.CapturedAt)

	cases := []struct {
		name     string
		reasoned time.Time
		want     FreshnessLabel
		quarant  bool
	}{
		{"fresh", snapAt.Add(-2 * time.Minute), FreshnessFresh, false},
		{"stale", snapAt.Add(-30 * time.Minute), FreshnessStale, false},
		{"orphaned-gap", snapAt.Add(-3 * time.Hour), FreshnessOrphaned, true},
		{"orphaned-absent", time.Time{}, FreshnessOrphaned, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := RecoveryView(repo, Deps{LastReasonedWrite: c.reasoned})
			if err != nil {
				t.Fatalf("RecoveryView: %v", err)
			}
			if res.Freshness.Label != c.want {
				t.Fatalf("freshness = %s, want %s", res.Freshness.Label, c.want)
			}
			if res.Quarantined != c.quarant {
				t.Fatalf("quarantined = %v, want %v", res.Quarantined, c.quarant)
			}
		})
	}
}

// TestRecoveryReplayOnlyNoSnapshot proves a missing snapshot degrades to a
// replay-only view with a note rather than failing.
func TestRecoveryReplayOnlyNoSnapshot(t *testing.T) {
	repo := recTestRepo(t)
	writeEventsFixture(t, repo,
		event(t, CmdStartTask, "2026-06-29T13:00:00Z", 1,
			&StartTaskInput{Plan: "alpha", Task: "t9"}, &StartTaskObserved{ToStatus: "in_progress"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if it, ok := itemByTask(res.Items, "t9"); !ok || it.Reconstructed.Status != "in_progress" {
		t.Fatalf("t9 not reconstructed from events: %+v", res.Items)
	}
	if len(res.Notes) == 0 {
		t.Fatalf("expected a degraded-mode note for the missing snapshot")
	}
	// No snapshot baseline → orphaned freshness → quarantined.
	if res.Freshness.Label != FreshnessOrphaned {
		t.Fatalf("freshness = %s, want orphaned (no snapshot)", res.Freshness.Label)
	}
}

// TestRecoveryMalformedLineSkipped proves a torn/garbage line is skipped and noted,
// while valid surrounding lines still replay.
func TestRecoveryMalformedLineSkipped(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))
	good := event(t, CmdAdvance, "2026-06-29T13:00:00Z", 1,
		&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "in_progress"})
	line, _ := good.MarshalLine()
	writeRawEvents(t, repo, "{not valid json\n"+string(line)+"\n   \n")

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != "in_progress" {
		t.Fatalf("valid line did not replay: %+v", it)
	}
	foundNote := false
	for _, n := range res.Notes {
		if bytes.Contains([]byte(n), []byte("malformed")) {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatalf("expected a malformed-line note, got %+v", res.Notes)
	}
}

// TestRecoveryFailedEventIgnored proves a failed event never mutates reconstructed
// state (R1: a failure carries no observed delta).
func TestRecoveryFailedEventIgnored(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))
	failed := event(t, CmdAdvance, "2026-06-29T13:00:00Z", 1,
		&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "completed"})
	failed.EventType = EventFailed
	failed.Observed = nil
	writeEventsFixture(t, repo, failed)

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != statusPending {
		t.Fatalf("failed event mutated state: status = %q", it.Reconstructed.Status)
	}
}

// TestRecoveryUnregisteredAndNonTransitionIgnored proves a journaled non-transition
// command (checkpoint) and an unregistered command leave task state untouched.
func TestRecoveryNonTransitionIgnored(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))
	writeEventsFixture(t, repo,
		event(t, CmdCheckpoint, "2026-06-29T13:00:00Z", 1,
			&CheckpointInput{Message: "wip"}, &CheckpointObserved{CheckpointID: "ck1"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != statusPending {
		t.Fatalf("non-transition command moved state: %q", it.Reconstructed.Status)
	}
}

// TestRecoveryEventReadError proves an unreadable event log is a hard error.
func TestRecoveryEventReadError(t *testing.T) {
	repo := recTestRepo(t)
	orig := readEventsLog
	readEventsLog = func(string) ([]byte, error) { return nil, errors.New("read boom") }
	defer func() { readEventsLog = orig }()

	if _, err := RecoveryView(repo, Deps{}); err == nil || err.Error() == "" {
		t.Fatalf("want read error, got %v", err)
	}
}

// TestRecoverySourceErrorFallsThrough proves a non-unavailable source error also
// falls through to the next source (robust to any probe failure).
func TestRecoverySourceErrorFallsThrough(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: "completed"}))
	boom := fakeSource{name: "gh", auth: true, errs: map[string]error{"alpha/t1": errors.New("api 500")}}
	local := fakeSource{name: "git", auth: false, checks: map[string]RealityCheck{
		"alpha/t1": {Exists: true, Status: "completed"},
	}}
	res, err := RecoveryView(repo, Deps{Sources: []VerificationSource{boom, local}})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.VerifiedBy != "git" || it.Trust != TrustMedium {
		t.Fatalf("did not fall through to local on a source error: %+v", it)
	}
}

// TestRecoverySessionIdentityOverride proves an explicit session identity is used
// for the D8 mismatch check (and matching identity does not quarantine).
func TestRecoverySessionIdentityMatchNoQuarantine(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: "completed"})
	snap.Identity = Identity{Fingerprint: "shared-fp"}
	writeSnapshotFixture(t, repo, snap)

	res, err := RecoveryView(repo, Deps{
		SessionIdentity:   Identity{Fingerprint: "shared-fp"},
		LastReasonedWrite: parseTS(snap.CapturedAt),
	})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if res.Quarantined {
		t.Fatalf("matching identity should not quarantine: %+v", res)
	}
}

// TestRecoveryWatermarkChronologicalNotLexical is the BUG 1 regression: the
// snapshot is captured at a whole-second instant ("…:00Z"), and an event fires
// 0.1s later. RFC3339Nano is variable-width, so the event ts ("…:00.1Z") sorts
// LEXICALLY BEFORE the watermark ('.' < 'Z') even though it is chronologically
// AFTER. A lexical watermark compare would DROP it from replay; the chronological
// compare must apply it.
func TestRecoveryWatermarkChronologicalNotLexical(t *testing.T) {
	repo := recTestRepo(t)
	// Snapshot captured at exactly 12:00:00 (no fraction), task pending.
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending})
	snap.CapturedAt = "2026-06-29T12:00:00Z"
	writeSnapshotFixture(t, repo, snap)
	writeEventsFixture(t, repo,
		// 0.1s after the snapshot — lexically "12:00:00.1Z" < "12:00:00Z".
		event(t, CmdAdvance, "2026-06-29T12:00:00.1Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "in_progress"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != "in_progress" {
		t.Fatalf("sub-second just-after-snapshot event dropped: status = %q, want in_progress", it.Reconstructed.Status)
	}
}

// TestRecoveryWatermarkBoundaryEqualTSApplied proves an event at EXACTLY the
// snapshot timestamp is applied (not lost), and one strictly before is ignored.
func TestRecoveryWatermarkBoundaryEqualTSApplied(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending})
	snap.CapturedAt = "2026-06-29T12:00:00Z"
	writeSnapshotFixture(t, repo, snap)
	writeEventsFixture(t, repo,
		event(t, CmdAdvance, "2026-06-29T11:59:59Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "before"}),
		event(t, CmdAdvance, "2026-06-29T12:00:00Z", 2,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "at_boundary"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != "at_boundary" {
		t.Fatalf("equal-ts boundary event not applied: status = %q, want at_boundary", it.Reconstructed.Status)
	}
}

// TestRecoveryChronologicalReplayOrder is the BUG 1 (sort half) regression: two
// events whose timestamps differ only in sub-second width — A at "…:00Z" and B at
// "…:00.1Z" (B is chronologically newer but sorts LEXICALLY before A, '.' < 'Z') —
// must replay in chronological order regardless of input/append order, so the
// NEWER event B wins the last-writer assignment. A lexical sort would let the older
// A overwrite B.
func TestRecoveryChronologicalReplayOrder(t *testing.T) {
	older := func(t *testing.T) Envelope {
		return event(t, CmdAdvance, "2026-06-29T12:00:00Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "older"})
	}
	newer := func(t *testing.T) Envelope {
		return event(t, CmdAdvance, "2026-06-29T12:00:00.1Z", 2,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "newer"})
	}

	for _, order := range []string{"older-first", "newer-first"} {
		t.Run(order, func(t *testing.T) {
			repo := recTestRepo(t)
			writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))
			if order == "older-first" {
				writeEventsFixture(t, repo, older(t), newer(t))
			} else {
				writeEventsFixture(t, repo, newer(t), older(t))
			}
			res, err := RecoveryView(repo, Deps{})
			if err != nil {
				t.Fatalf("RecoveryView: %v", err)
			}
			it, _ := itemByTask(res.Items, "t1")
			if it.Reconstructed.Status != "newer" {
				t.Fatalf("input %s: final status = %q, want newer (chronological last-writer)", order, it.Reconstructed.Status)
			}
		})
	}
}

// TestLessEventTiebreaks proves the deterministic ordering rules directly: equal
// parsed times tiebreak by Seq, and an unparseable ts sorts last (fail-safe).
func TestLessEventTiebreaks(t *testing.T) {
	a := Envelope{TS: "2026-06-29T12:00:00Z", Seq: 1}
	b := Envelope{TS: "2026-06-29T12:00:00Z", Seq: 2}
	if !lessEvent(a, b) || lessEvent(b, a) {
		t.Fatalf("equal-ts events must order by seq")
	}
	good := Envelope{TS: "2026-06-29T12:00:00Z", Seq: 9}
	bad := Envelope{TS: "garbage", Seq: 1}
	if !lessEvent(good, bad) || lessEvent(bad, good) {
		t.Fatalf("unparseable ts must sort after a well-formed one")
	}
	// two unparseable timestamps fall back to seq.
	bad2 := Envelope{TS: "also-bad", Seq: 5}
	if !lessEvent(bad, bad2) {
		t.Fatalf("two unparseable ts should tiebreak by seq")
	}
}

// TestRecoveryStaleCoordinateIsChange is the BUG 2 regression: a same-arm locus
// whose COORDINATE differs (PR #7→#8, or canonical master@old→master@new) must be
// tagged changed and UPDATED to the source's current value, never kept stale and
// labelled verified.
func TestRecoveryStaleCoordinateIsChange(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha",
		TaskState{ID: "tpr", Status: statusAwaitingOwnerReview,
			Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 7, Status: statusAwaitingOwnerReview}}},
		TaskState{ID: "tsha", Status: statusCompleted,
			Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@old"}}},
	))
	src := fakeSource{name: "gh", auth: true, checks: map[string]RealityCheck{
		// same arm, different PR number → changed + adopt #8.
		"alpha/tpr": {Exists: true, Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 8, Status: statusAwaitingOwnerReview}}},
		// same arm, different merge sha → changed + adopt master@new.
		"alpha/tsha": {Exists: true, Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@new"}}},
	}}
	res, err := RecoveryView(repo, Deps{Sources: []VerificationSource{src}})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}

	tpr, _ := itemByTask(res.Items, "tpr")
	if tpr.Status != StatusChanged || tpr.Delta == "" {
		t.Fatalf("tpr = %+v, want changed with delta (stale PR #7 vs #8)", tpr)
	}
	if tpr.Reconstructed.Locus.InOpenPR.PR != 8 {
		t.Fatalf("tpr kept stale PR %d, want updated to 8", tpr.Reconstructed.Locus.InOpenPR.PR)
	}

	tsha, _ := itemByTask(res.Items, "tsha")
	if tsha.Status != StatusChanged || tsha.Delta == "" {
		t.Fatalf("tsha = %+v, want changed with delta (stale sha)", tsha)
	}
	if tsha.Reconstructed.Locus.Canonical.Ref != "master@new" {
		t.Fatalf("tsha kept stale ref %q, want master@new", tsha.Reconstructed.Locus.Canonical.Ref)
	}
}

// TestRecoveryDuplicateSnapshotKeyQuarantined is the BUG 3 regression: a malformed
// snapshot carrying two tasks under one (plan, task) identity must surface a
// conflict and present NEITHER item as a fact, rather than silently overwriting one.
func TestRecoveryDuplicateSnapshotKeyQuarantined(t *testing.T) {
	repo := recTestRepo(t)
	snap := SnapshotState{
		Schema:     SnapshotSchema,
		Version:    SnapshotVersion,
		CapturedAt: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC).Format(timeFormat),
		Identity:   ResolveIdentity(repo),
		Plans: []PlanState{{ID: "alpha", Status: "active", Tasks: []TaskState{
			{ID: "dup", Status: "completed"},
			{ID: "dup", Status: "pending"}, // duplicate identity
			{ID: "ok", Status: "in_progress"},
		}}},
	}
	writeSnapshotFixture(t, repo, snap)

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if _, present := itemByTask(res.Items, "dup"); present {
		t.Fatalf("duplicate-identity item was presented as a fact (should be quarantined): %+v", res.Items)
	}
	if _, present := itemByTask(res.Items, "ok"); !present {
		t.Fatalf("the non-colliding item must survive: %+v", res.Items)
	}
	found := false
	for _, c := range res.Conflicts {
		if c.Plan == "alpha" && c.Task == "dup" {
			found = true
		}
	}
	if !found {
		t.Fatalf("duplicate key not surfaced as a conflict: %+v", res.Conflicts)
	}
}

// TestRecoveryQuarantinedKeyNotResurrectedByEvent proves an event targeting a
// quarantined (duplicate-identity) key does not silently bring it back as a fact.
func TestRecoveryQuarantinedKeyNotResurrectedByEvent(t *testing.T) {
	repo := recTestRepo(t)
	snap := SnapshotState{
		Schema: SnapshotSchema, Version: SnapshotVersion,
		CapturedAt: "2026-06-29T12:00:00Z",
		Identity:   ResolveIdentity(repo),
		Plans: []PlanState{{ID: "alpha", Status: "active", Tasks: []TaskState{
			{ID: "dup", Status: "completed"},
			{ID: "dup", Status: "pending"},
		}}},
	}
	writeSnapshotFixture(t, repo, snap)
	writeEventsFixture(t, repo,
		event(t, CmdAdvance, "2026-06-29T13:00:00Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "dup"}, &AdvanceObserved{ToStatus: "in_progress"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if _, present := itemByTask(res.Items, "dup"); present {
		t.Fatalf("quarantined key resurrected by a replayed event: %+v", res.Items)
	}
}

// TestRecoveryTripleDuplicateKeyQuarantinedOnce proves a third task under an
// already-quarantined key is dropped without a second conflict (idempotent
// quarantine).
func TestRecoveryTripleDuplicateKeyQuarantinedOnce(t *testing.T) {
	repo := recTestRepo(t)
	snap := SnapshotState{
		Schema: SnapshotSchema, Version: SnapshotVersion,
		CapturedAt: "2026-06-29T12:00:00Z", Identity: ResolveIdentity(repo),
		Plans: []PlanState{{ID: "alpha", Status: "active", Tasks: []TaskState{
			{ID: "dup", Status: "completed"},
			{ID: "dup", Status: "pending"},
			{ID: "dup", Status: "in_progress"},
		}}},
	}
	writeSnapshotFixture(t, repo, snap)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if _, present := itemByTask(res.Items, "dup"); present {
		t.Fatalf("triple-duplicate item presented as fact: %+v", res.Items)
	}
	n := 0
	for _, c := range res.Conflicts {
		if c.Task == "dup" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("triple duplicate recorded %d conflicts, want exactly 1", n)
	}
}

// TestRecoveryUnparseableEventTSFailSafe proves an event whose ts cannot be parsed
// is APPLIED (fail-safe) rather than silently dropped by the watermark filter.
func TestRecoveryUnparseableEventTSFailSafe(t *testing.T) {
	repo := recTestRepo(t)
	snap := snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending})
	snap.CapturedAt = "2026-06-29T12:00:00Z"
	writeSnapshotFixture(t, repo, snap)
	bad := event(t, CmdAdvance, "not-a-timestamp", 1,
		&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "applied"})
	writeEventsFixture(t, repo, bad)

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != "applied" {
		t.Fatalf("unparseable-ts event dropped: status = %q, want applied", it.Reconstructed.Status)
	}
}

func TestPRMatches(t *testing.T) {
	// placeholder PR (0) matches anything; empty reality field is no opinion.
	if !prMatches(&InOpenPRRef{PR: 0}, &InOpenPRRef{PR: 9, Status: "x"}) {
		t.Fatalf("placeholder PR should match")
	}
	// same concrete PR, different status → mismatch.
	if prMatches(&InOpenPRRef{PR: 5, Status: "open"}, &InOpenPRRef{PR: 5, Status: "merged"}) {
		t.Fatalf("differing in-PR status should be a change")
	}
	// same PR, same status → match.
	if !prMatches(&InOpenPRRef{PR: 5, Status: "open"}, &InOpenPRRef{PR: 5, Status: "open"}) {
		t.Fatalf("identical coords should match")
	}
}

// --- focused unit tests over the small helpers (branch coverage) -------------

func TestLocusArmAndDescribe(t *testing.T) {
	if locusArm(nil) != armNone || locusArm(&Locus{}) != armNone {
		t.Fatalf("nil/empty locus arm")
	}
	if locusArm(&Locus{Canonical: &CanonicalRef{}}) != armCanonical {
		t.Fatalf("canonical arm")
	}
	if locusArm(&Locus{InOpenPR: &InOpenPRRef{}}) != armInOpenPR {
		t.Fatalf("in-pr arm")
	}
	if describe(ItemState{}) != "(no state)" {
		t.Fatalf("empty describe")
	}
	if got := describe(ItemState{Status: "x", Locus: &Locus{Canonical: &CanonicalRef{Ref: "r"}}}); got != "status=x canonical r" {
		t.Fatalf("describe canonical = %q", got)
	}
	if got := describe(ItemState{Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 5}}}); got != "in_open_pr #5" {
		t.Fatalf("describe in-pr = %q", got)
	}
	if describeLocus(&Locus{}) != "" {
		t.Fatalf("empty locus describe should be blank")
	}
}

func TestEnrichLocusGuards(t *testing.T) {
	// nil reality / nil reconstructed locus → no-op, no panic.
	s := ItemState{Locus: &Locus{Canonical: &CanonicalRef{Ref: canonicalLocusRef}}}
	enrichLocus(&s, nil)
	if s.Locus.Canonical.Ref != canonicalLocusRef {
		t.Fatalf("nil reality mutated locus")
	}
	none := ItemState{}
	enrichLocus(&none, &Locus{Canonical: &CanonicalRef{Ref: "r"}})
	if none.Locus != nil {
		t.Fatalf("nil reconstructed locus mutated")
	}
	// non-placeholder canonical ref is preserved (not overwritten).
	keep := ItemState{Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@real"}}}
	enrichLocus(&keep, &Locus{Canonical: &CanonicalRef{Ref: "master@other"}})
	if keep.Locus.Canonical.Ref != "master@real" {
		t.Fatalf("non-placeholder ref overwritten: %q", keep.Locus.Canonical.Ref)
	}
}

func TestParseTSAndDistinct(t *testing.T) {
	if !parseTS("not-a-time").IsZero() {
		t.Fatalf("bad ts should be zero")
	}
	if parseTS("2026-06-29T12:00:00Z").IsZero() {
		t.Fatalf("good ts should parse")
	}
	got := distinct([]string{"a", "a", "b", "a", "c"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("distinct = %v", got)
	}
}

// TestRecoveryUnregisteredAndMalformedTyped proves replay ignores an unregistered
// command and a registered command with undecodable typed payloads, leaving the
// snapshot-seeded state untouched (and adding no spurious item).
func TestRecoveryUnregisteredAndMalformedTyped(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))

	unregistered := Envelope{
		Schema: Schema, Version: Version, TS: "2026-06-29T13:00:00Z", Seq: 1,
		Actor: ActorMain, Command: "definitely not journaled", EventType: EventDurableDelta,
		Input: json.RawMessage(`{"x":1}`),
	}
	// Registered command, but the typed input cannot decode (plan must be a string).
	badInput := Envelope{
		Schema: Schema, Version: Version, TS: "2026-06-29T13:00:01Z", Seq: 2,
		Actor: ActorMain, Command: CmdAdvance, EventType: EventDurableDelta,
		Input:    json.RawMessage(`{"plan":123,"task":"t1"}`),
		Observed: json.RawMessage(`{"to_status":"completed"}`),
	}
	// Registered command, decodable input but undecodable observed.
	badObserved := Envelope{
		Schema: Schema, Version: Version, TS: "2026-06-29T13:00:02Z", Seq: 3,
		Actor: ActorMain, Command: CmdAdvance, EventType: EventDurableDelta,
		Input:    json.RawMessage(`{"plan":"alpha","task":"t1"}`),
		Observed: json.RawMessage(`{"to_status":[]}`),
	}
	writeEventsFixture(t, repo, unregistered, badInput, badObserved)

	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("want only the snapshot task, got %+v", res.Items)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != statusPending {
		t.Fatalf("undecodable events mutated state: %q", it.Reconstructed.Status)
	}
}

// TestRecoveryCloseTaskAndBareMergeBack covers the close-task transition arm and a
// merge-back for a task absent from every plan (resolved to a bare, plan-less item).
func TestRecoveryCloseTaskAndBareMergeBack(t *testing.T) {
	repo := recTestRepo(t)
	// No snapshot at all → both items come purely from events.
	writeEventsFixture(t, repo,
		event(t, CmdCloseTask, "2026-06-29T13:00:00Z", 1,
			&CloseTaskInput{Plan: "alpha", Task: "t1"},
			&CloseTaskObserved{ToStatus: statusCompleted, Locus: &Locus{Canonical: &CanonicalRef{Ref: "master@z"}}}),
		event(t, CmdMergeBack, "2026-06-29T13:00:01Z", 2,
			&MergeBackInput{Task: "orphan"},
			&MergeBackObserved{Verdict: "pass", Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 3}}}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	closed, _ := itemByTask(res.Items, "t1")
	if closed.Reconstructed.Status != statusCompleted || locusArm(closed.Reconstructed.Locus) != armCanonical {
		t.Fatalf("close-task not applied: %+v", closed.Reconstructed)
	}
	bare, ok := itemByTask(res.Items, "orphan")
	if !ok || bare.Key.Plan != "" {
		t.Fatalf("bare merge-back not placed as plan-less item: %+v", bare)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("a single (bare) target is not a conflict: %+v", res.Conflicts)
	}
}

// TestRecoverySeqTiebreaker proves two events sharing a timestamp are ordered by
// seq, so the higher-seq transition wins even when written out of order.
func TestRecoverySeqTiebreaker(t *testing.T) {
	repo := recTestRepo(t)
	writeSnapshotFixture(t, repo, snapWith(repo, "alpha", TaskState{ID: "t1", Status: statusPending}))
	// Written seq-2 first, seq-1 second; the stable sort must order by seq.
	writeEventsFixture(t, repo,
		event(t, CmdAdvance, "2026-06-29T13:00:00Z", 2,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "final"}),
		event(t, CmdAdvance, "2026-06-29T13:00:00Z", 1,
			&AdvanceInput{Plan: "alpha", Task: "t1"}, &AdvanceObserved{ToStatus: "earlier"}),
	)
	res, err := RecoveryView(repo, Deps{})
	if err != nil {
		t.Fatalf("RecoveryView: %v", err)
	}
	it, _ := itemByTask(res.Items, "t1")
	if it.Reconstructed.Status != "final" {
		t.Fatalf("seq tiebreaker wrong: status = %q, want final", it.Reconstructed.Status)
	}
}

func TestEnrichLocusInOpenPREmptyStatus(t *testing.T) {
	s := ItemState{Locus: &Locus{InOpenPR: &InOpenPRRef{}}} // PR 0, status ""
	enrichLocus(&s, &Locus{InOpenPR: &InOpenPRRef{PR: 42, Status: statusAwaitingOwnerReview}})
	if s.Locus.InOpenPR.PR != 42 || s.Locus.InOpenPR.Status != statusAwaitingOwnerReview {
		t.Fatalf("in-pr placeholder not enriched: %+v", s.Locus.InOpenPR)
	}
}

func TestLessKeyOrdersByKindThenPlanThenTask(t *testing.T) {
	if !lessKey(ItemKey{Kind: "a"}, ItemKey{Kind: "b"}) {
		t.Fatalf("kind ordering")
	}
	if !lessKey(ItemKey{Kind: KindTask, Plan: "a"}, ItemKey{Kind: KindTask, Plan: "b"}) {
		t.Fatalf("plan ordering")
	}
	if !lessKey(ItemKey{Kind: KindTask, Plan: "p", Task: "a"}, ItemKey{Kind: KindTask, Plan: "p", Task: "b"}) {
		t.Fatalf("task ordering")
	}
}

func TestComputeFreshnessBoundaries(t *testing.T) {
	base := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// snapshot zero → orphaned regardless of reasoned time.
	if computeFreshness(time.Time{}, base, time.Minute, time.Hour).Label != FreshnessOrphaned {
		t.Fatalf("zero snapshot should be orphaned")
	}
	// exactly on the stale boundary counts as fresh; future reasoned write (abs gap).
	if l := computeFreshness(base, base.Add(10*time.Minute), defaultStaleAfter, defaultOrphanAfter).Label; l != FreshnessFresh {
		t.Fatalf("boundary gap should be fresh, got %s", l)
	}
}
