package config

import (
	"testing"
	"time"
)

// writeManifest (shared with resolver_test.go) drops a .agentsrc.json into dir.

// withReviewNudgeClock pins reviewNudgeClock to a fixed instant for the duration
// of the test, so "last re-checked N ago" durations are deterministic.
func withReviewNudgeClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := reviewNudgeClock
	reviewNudgeClock = func() time.Time { return now }
	t.Cleanup(func() { reviewNudgeClock = prev })
}

// seedUnits writes a units-model lockfile for the project under test.
func seedUnits(t *testing.T, repo string, digest string, units map[string]LockedUnit) {
	t.Helper()
	if err := WriteUnitsLock(repo, UnitsLock{Units: units, InputsDigest: digest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
}

func TestReadLockedUnits_ReadsUnitsSection(t *testing.T) {
	repo := t.TempDir()
	seedUnits(t, repo, "sha256:in", map[string]LockedUnit{
		"acme:org/base@v1": {Kind: UnitKindLayer, Digest: "sha256:d1", FetchedAt: "2026-05-01T00:00:00Z"},
	})

	got, err := ReadLockedUnits(repo)
	if err != nil {
		t.Fatalf("ReadLockedUnits: %v", err)
	}
	if entry, ok := got["acme:org/base@v1"]; !ok || entry.Digest != "sha256:d1" {
		t.Fatalf("expected unit acme:org/base@v1 digest sha256:d1, got %+v", got)
	}
}

func TestReadLockedUnits_MigratesLegacyLockfile(t *testing.T) {
	repo := t.TempDir()
	// A legacy config-section lockfile migrates to units on read.
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "abc123", FetchedAt: "2026-05-01T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLockedUnits(repo)
	if err != nil {
		t.Fatalf("ReadLockedUnits: %v", err)
	}
	entry, ok := got["acme:org/base.json"]
	if !ok || entry.Digest != "abc123" || entry.Kind != UnitKindLayer {
		t.Fatalf("expected migrated layer unit, got %+v", got)
	}
}

func TestReadLockedUnits_AbsentLockReturnsEmpty(t *testing.T) {
	repo := t.TempDir()
	got, err := ReadLockedUnits(repo)
	if err != nil {
		t.Fatalf("ReadLockedUnits on missing lock: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map for absent lock, got %+v", got)
	}
}

func TestLockDrift_NoDeclaredUnitsNotApplicable(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if res.HasDeclaredUnits {
		t.Error("expected HasDeclaredUnits=false for manifest with no extends/packages")
	}
	if !res.IsClean() {
		t.Error("expected IsClean()=true when no units declared")
	}
	if len(res.Units) != 0 {
		t.Errorf("expected no unit records, got %+v", res.Units)
	}
}

func TestLockDrift_CleanWhenAllDeclaredLocked(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"extends":["acme:org/base.json"],"packages":["acme:skill/x@^1"]}`)
	seedUnits(t, repo, "sha256:in", map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1", FetchedAt: "t"},
		"acme:skill/x@1.0.0":    {Kind: UnitKindArtifact, Digest: "sha256:d2", FetchedAt: "t"},
	})

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if !res.HasDeclaredUnits || !res.LockPresent {
		t.Fatalf("expected HasDeclaredUnits && LockPresent, got %+v", res)
	}
	if !res.IsClean() {
		t.Fatalf("expected clean result, got problems %+v", res.Problems())
	}
	if len(res.Units) != 2 {
		t.Fatalf("expected 2 unit records, got %d: %+v", len(res.Units), res.Units)
	}
	// Sorted by ref.
	if res.Units[0].Ref != "acme:org/base.json" || res.Units[1].Ref != "acme:skill/x" {
		t.Errorf("units not sorted by ref: %+v", res.Units)
	}
	for _, u := range res.Units {
		if u.Status != LockStatusOK {
			t.Errorf("expected ok status, got %q for %s", u.Status, u.Ref)
		}
	}
}

func TestLockDrift_MissingFromLock(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"extends":["acme:org/base.json"]}`)
	// No lockfile written at all.

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if res.LockPresent {
		t.Error("expected LockPresent=false when no lockfile exists")
	}
	if res.IsClean() {
		t.Fatal("expected drift (missing-from-lock), got clean")
	}
	probs := res.Problems()
	if len(probs) != 1 || probs[0].Status != LockStatusMissingFromLock {
		t.Fatalf("expected one missing-from-lock problem, got %+v", probs)
	}
	if probs[0].Digest != "" {
		t.Errorf("missing-from-lock should carry no digest, got %q", probs[0].Digest)
	}
}

func TestLockDrift_ExtraInLock(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"extends":["acme:org/base.json"]}`)
	seedUnits(t, repo, "sha256:in", map[string]LockedUnit{
		"acme:org/base.json@a1":  {Kind: UnitKindLayer, Digest: "sha256:d1", FetchedAt: "t"},
		"acme:org/stale.json@s1": {Kind: UnitKindLayer, Digest: "sha256:st", FetchedAt: "t"}, // no longer declared
	})

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	probs := res.Problems()
	if len(probs) != 1 || probs[0].Status != LockStatusExtraInLock || probs[0].Ref != "acme:org/stale.json" {
		t.Fatalf("expected one extra-in-lock for stale ref, got %+v", probs)
	}
	if probs[0].Digest != "sha256:st" {
		t.Errorf("extra-in-lock should carry the stale digest, got %q", probs[0].Digest)
	}
}

func TestLockDrift_MissingManifestErrors(t *testing.T) {
	repo := t.TempDir()
	// No .agentsrc.json at all.
	if _, err := LockDrift(repo); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestLockDriftResult_ProblemsEmptyWhenClean(t *testing.T) {
	res := LockDriftResult{
		HasDeclaredUnits: true,
		Units:            []LockUnitDrift{{Ref: "x", Status: LockStatusOK}},
	}
	if !res.IsClean() {
		t.Error("expected clean")
	}
	if len(res.Problems()) != 0 {
		t.Errorf("expected no problems, got %+v", res.Problems())
	}
}

func TestReviewNudges_UsesLastCheckedThenFetched(t *testing.T) {
	repo := t.TempDir()
	withReviewNudgeClock(t, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	seedUnits(t, repo, "sha256:in", map[string]LockedUnit{
		// Re-checked recently: last_checked_at wins over fetched_at.
		"acme:a@v1": {Kind: UnitKindLayer, FetchedAt: "2026-04-01T00:00:00Z", LastCheckedAt: "2026-05-10T00:00:00Z"},
		// Never re-checked: falls back to fetched_at.
		"acme:b@v1": {Kind: UnitKindLayer, FetchedAt: "2026-05-01T00:00:00Z"},
		// No timestamp basis at all: listed with zero duration.
		"acme:c@v1": {Kind: UnitKindArtifact},
	})

	nudges, err := ReviewNudges(repo)
	if err != nil {
		t.Fatalf("ReviewNudges: %v", err)
	}
	if len(nudges) != 3 {
		t.Fatalf("expected 3 nudges, got %d: %+v", len(nudges), nudges)
	}
	// Sorted by ref: a, b, c.
	if nudges[0].Ref != "acme:a@v1" || nudges[1].Ref != "acme:b@v1" || nudges[2].Ref != "acme:c@v1" {
		t.Fatalf("nudges not sorted by ref: %+v", nudges)
	}
	if nudges[0].LastCheckedAt != "2026-05-10T00:00:00Z" {
		t.Errorf("expected last_checked_at basis, got %q", nudges[0].LastCheckedAt)
	}
	if want := 5 * 24 * time.Hour; nudges[0].SinceLastCheck != want {
		t.Errorf("expected %v since last check, got %v", want, nudges[0].SinceLastCheck)
	}
	if nudges[1].LastCheckedAt != "2026-05-01T00:00:00Z" {
		t.Errorf("expected fetched_at fallback basis, got %q", nudges[1].LastCheckedAt)
	}
	if nudges[2].LastCheckedAt != "" || nudges[2].SinceLastCheck != 0 {
		t.Errorf("expected zero nudge for timestamp-less unit, got %+v", nudges[2])
	}
}

func TestReviewNudges_UnparseableBasisYieldsZero(t *testing.T) {
	repo := t.TempDir()
	withReviewNudgeClock(t, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	seedUnits(t, repo, "sha256:in", map[string]LockedUnit{
		"acme:a@v1": {Kind: UnitKindLayer, FetchedAt: "not-a-timestamp"},
	})

	nudges, err := ReviewNudges(repo)
	if err != nil {
		t.Fatalf("ReviewNudges: %v", err)
	}
	if len(nudges) != 1 || nudges[0].SinceLastCheck != 0 {
		t.Fatalf("expected single zero-duration nudge, got %+v", nudges)
	}
}

func TestReviewNudges_AbsentLockEmpty(t *testing.T) {
	repo := t.TempDir()
	nudges, err := ReviewNudges(repo)
	if err != nil {
		t.Fatalf("ReviewNudges: %v", err)
	}
	if len(nudges) != 0 {
		t.Errorf("expected no nudges for absent lock, got %+v", nudges)
	}
}
