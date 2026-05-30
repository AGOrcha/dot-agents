package config

import (
	"testing"
	"time"
)

// writeManifest (shared with resolver_test.go) drops a .agentsrc.json into dir.

// withLockDriftClock pins lockDriftClock to a fixed instant for the duration of
// the test, so ttl-expired classification is deterministic.
func withLockDriftClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := lockDriftClock
	lockDriftClock = func() time.Time { return now }
	t.Cleanup(func() { lockDriftClock = prev })
}

func TestReadLockedLayers_WrapsResolverReader(t *testing.T) {
	repo := t.TempDir()
	layers := map[string]LockedLayer{
		"acme:org/base": {ResolvedSHA: "abc123", FetchedAt: "2026-05-01T00:00:00Z"},
	}
	if err := WriteConfigLock(repo, layers); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLockedLayers(repo)
	if err != nil {
		t.Fatalf("ReadLockedLayers: %v", err)
	}
	if entry, ok := got["acme:org/base"]; !ok || entry.ResolvedSHA != "abc123" {
		t.Fatalf("expected locked layer acme:org/base sha abc123, got %+v", got)
	}
}

func TestReadLockedLayers_AbsentLockReturnsEmpty(t *testing.T) {
	repo := t.TempDir()
	got, err := ReadLockedLayers(repo)
	if err != nil {
		t.Fatalf("ReadLockedLayers on missing lock: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map for absent lock, got %+v", got)
	}
}

func TestLockDrift_NoExtendsNotApplicable(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if res.HasExtends {
		t.Error("expected HasExtends=false for manifest with no extends")
	}
	if !res.IsClean() {
		t.Error("expected IsClean()=true when no extends declared")
	}
	if len(res.Layers) != 0 {
		t.Errorf("expected no layer records, got %+v", res.Layers)
	}
}

func TestLockDrift_CleanWhenAllDeclaredLockedAndFresh(t *testing.T) {
	repo := t.TempDir()
	withLockDriftClock(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	writeManifest(t, repo, `{"extends":["acme:org/base.json","acme:team/fe.json"]}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "2026-04-01T00:00:00Z", TTLExpiresAt: "2026-06-01T00:00:00Z"},
		"acme:team/fe.json":  {ResolvedSHA: "b2", FetchedAt: "2026-04-01T00:00:00Z"}, // no TTL → never re-check
	}); err != nil {
		t.Fatal(err)
	}

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if !res.HasExtends || !res.LockPresent {
		t.Fatalf("expected HasExtends && LockPresent, got %+v", res)
	}
	if !res.IsClean() {
		t.Fatalf("expected clean result, got problems %+v", res.Problems())
	}
	if len(res.Layers) != 2 {
		t.Fatalf("expected 2 layer records, got %d: %+v", len(res.Layers), res.Layers)
	}
	// Sorted by ref: base before team.
	if res.Layers[0].Ref != "acme:org/base.json" || res.Layers[1].Ref != "acme:team/fe.json" {
		t.Errorf("layers not sorted by ref: %+v", res.Layers)
	}
	for _, l := range res.Layers {
		if l.Status != LockStatusOK {
			t.Errorf("expected ok status, got %q for %s", l.Status, l.Ref)
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
	if probs[0].ResolvedSHA != "" {
		t.Errorf("missing-from-lock should carry no SHA, got %q", probs[0].ResolvedSHA)
	}
}

func TestLockDrift_ExtraInLock(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"extends":["acme:org/base.json"]}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"acme:org/base.json":  {ResolvedSHA: "a1", FetchedAt: "t"},
		"acme:org/stale.json": {ResolvedSHA: "st", FetchedAt: "t"}, // no longer declared
	}); err != nil {
		t.Fatal(err)
	}

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	probs := res.Problems()
	if len(probs) != 1 || probs[0].Status != LockStatusExtraInLock || probs[0].Ref != "acme:org/stale.json" {
		t.Fatalf("expected one extra-in-lock for stale ref, got %+v", probs)
	}
	if probs[0].ResolvedSHA != "st" {
		t.Errorf("extra-in-lock should carry the stale SHA, got %q", probs[0].ResolvedSHA)
	}
}

func TestLockDrift_TTLExpired(t *testing.T) {
	repo := t.TempDir()
	withLockDriftClock(t, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	writeManifest(t, repo, `{"extends":["acme:org/base.json"]}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "2026-04-01T00:00:00Z", TTLExpiresAt: "2026-05-01T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := LockDrift(repo)
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	probs := res.Problems()
	if len(probs) != 1 || probs[0].Status != LockStatusTTLExpired {
		t.Fatalf("expected ttl-expired, got %+v", probs)
	}
	if probs[0].TTLExpiresAt != "2026-05-01T00:00:00Z" {
		t.Errorf("expected TTL surfaced, got %q", probs[0].TTLExpiresAt)
	}
}

func TestLockDrift_MissingManifestErrors(t *testing.T) {
	repo := t.TempDir()
	// No .agentsrc.json at all.
	if _, err := LockDrift(repo); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestClassifyLockedLayer(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ttl  string
		want LockDriftStatus
	}{
		{"empty ttl is ok", "", LockStatusOK},
		{"future ttl is ok", "2026-06-01T00:00:00Z", LockStatusOK},
		{"past ttl expired", "2026-05-01T00:00:00Z", LockStatusTTLExpired},
		{"unparseable ttl is ok", "not-a-timestamp", LockStatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLockedLayer(LockedLayer{TTLExpiresAt: tc.ttl}, now)
			if got != tc.want {
				t.Errorf("classifyLockedLayer(%q) = %q, want %q", tc.ttl, got, tc.want)
			}
		})
	}
}

func TestLockDriftResult_ProblemsEmptyWhenClean(t *testing.T) {
	res := LockDriftResult{
		HasExtends: true,
		Layers:     []LockLayerDrift{{Ref: "x", Status: LockStatusOK}},
	}
	if !res.IsClean() {
		t.Error("expected clean")
	}
	if len(res.Problems()) != 0 {
		t.Errorf("expected no problems, got %+v", res.Problems())
	}
}
