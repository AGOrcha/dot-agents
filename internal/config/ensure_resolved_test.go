package config

import (
	"errors"
	"testing"
)

// fakeEnsureResolver is a hermetic EnsureResolverSeam: it records which method
// EnsureResolved drove and returns a sentinel snapshot, so no test contacts the
// network or a real resolver. An optional error simulates a resolution failure.
type fakeEnsureResolver struct {
	resolveCalls       int
	resolveLockedCalls int
	resolveErr         error
	resolveLockedErr   error
}

func (f *fakeEnsureResolver) Resolve(string) (*Snapshot, error) {
	f.resolveCalls++
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &Snapshot{Effective: AgentsRC{Project: "resolved"}}, nil
}

func (f *fakeEnsureResolver) ResolveLocked(string) (*Snapshot, error) {
	f.resolveLockedCalls++
	if f.resolveLockedErr != nil {
		return nil, f.resolveLockedErr
	}
	return &Snapshot{Effective: AgentsRC{Project: "locked"}}, nil
}

// projResolved/projLocked are the snapshot labels the fake resolver tags so a
// test can assert which resolution path ran.
const (
	projResolved = "resolved"
	projLocked   = "locked"
	// repoManifestX is the shared repo-local manifest body the seam tests seed.
	repoManifestX = `{"version":2,"project":"x"}`
	// staleDigest is an inputs_digest that never matches the live scopes, forcing
	// the inputs-digest staleness driver event.
	staleDigest = "sha256:stale"
)

// ensureSeed writes a repo-local manifest plus a user-local manifest and returns
// the project dir + user-local path. The manifests are the local scopes
// inputs_digest hashes; a test seeds the lock with a matching or mismatched
// digest to drive fresh vs stale.
func ensureSeed(t *testing.T, repoManifest string) (repo, userPath string) {
	t.Helper()
	return stalenessSeed(t, repoManifest, `{"version":2}`)
}

// seedFreshLock writes a units lock whose inputs_digest matches the current
// local scopes and whose unit set matches the manifest's declared set, so a
// staleness check reports Fresh.
func seedFreshLock(t *testing.T, repo, userPath string, units map[string]LockedUnit) {
	t.Helper()
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	if err := WriteUnitsLock(repo, UnitsLock{Units: units, InputsDigest: digest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
}

func TestEnsureResolved_FrozenSkipsStalenessAndReadsLock(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	// No lock at all: Frozen must NOT run a staleness check (which would still
	// succeed here) and must NOT re-resolve — it reads the lock as-is.
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{Frozen: true, UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved frozen: %v", err)
	}
	if fake.resolveCalls != 0 {
		t.Errorf("frozen must not Resolve, got %d calls", fake.resolveCalls)
	}
	if fake.resolveLockedCalls != 1 {
		t.Errorf("frozen must ResolveLocked once, got %d", fake.resolveLockedCalls)
	}
	if res.ReResolved || res.Fresh {
		t.Errorf("frozen result must be neither ReResolved nor Fresh, got %+v", res)
	}
	if res.Snapshot.Effective.Project != projLocked {
		t.Errorf("expected locked snapshot, got %q", res.Snapshot.Effective.Project)
	}
}

func TestEnsureResolved_FreshDefaultIsNoOpReadOnly(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	seedFreshLock(t, repo, userPath, map[string]LockedUnit{})
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved fresh: %v", err)
	}
	if fake.resolveCalls != 0 {
		t.Errorf("fresh default must not Resolve, got %d", fake.resolveCalls)
	}
	if fake.resolveLockedCalls != 1 {
		t.Errorf("fresh default must ResolveLocked once, got %d", fake.resolveLockedCalls)
	}
	if !res.Fresh || res.ReResolved {
		t.Errorf("fresh default: want Fresh && !ReResolved, got %+v", res)
	}
	if len(res.Reasons) != 0 {
		t.Errorf("fresh result must carry no reasons, got %v", res.Reasons)
	}
}

func TestEnsureResolved_StaleDefaultReResolvesAndRewrites(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	// Lock records a stale inputs_digest → staleness fires ReasonInputsDigest.
	if err := WriteUnitsLock(repo, UnitsLock{InputsDigest: staleDigest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved stale: %v", err)
	}
	if fake.resolveCalls != 1 {
		t.Errorf("stale default must Resolve once, got %d", fake.resolveCalls)
	}
	if fake.resolveLockedCalls != 0 {
		t.Errorf("stale default must not ResolveLocked, got %d", fake.resolveLockedCalls)
	}
	if res.Fresh || !res.ReResolved {
		t.Errorf("stale default: want ReResolved && !Fresh, got %+v", res)
	}
	if res.Snapshot.Effective.Project != projResolved {
		t.Errorf("expected resolved snapshot, got %q", res.Snapshot.Effective.Project)
	}
	if !containsReason(res.Reasons, ReasonInputsDigest) {
		t.Errorf("expected inputs-digest reason, got %v", res.Reasons)
	}
}

func TestEnsureResolved_LockedFreshResolvesReadOnly(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	seedFreshLock(t, repo, userPath, map[string]LockedUnit{})
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{Locked: true, UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved locked-fresh: %v", err)
	}
	if fake.resolveCalls != 0 || fake.resolveLockedCalls != 1 {
		t.Errorf("locked-fresh: want read-only, got resolve=%d locked=%d", fake.resolveCalls, fake.resolveLockedCalls)
	}
	if !res.Fresh {
		t.Errorf("locked-fresh result must be Fresh, got %+v", res)
	}
}

func TestEnsureResolved_LockedStaleErrorsAndWritesNothing(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	if err := WriteUnitsLock(repo, UnitsLock{InputsDigest: staleDigest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{Locked: true, UserLocalPath: userPath, Resolver: fake})
	if !errors.Is(err, ErrLockWouldChange) {
		t.Fatalf("locked-stale: want ErrLockWouldChange, got err=%v res=%+v", err, res)
	}
	if res != nil {
		t.Errorf("locked-stale must return nil result, got %+v", res)
	}
	if fake.resolveCalls != 0 || fake.resolveLockedCalls != 0 {
		t.Errorf("locked-stale must write/resolve nothing, got resolve=%d locked=%d", fake.resolveCalls, fake.resolveLockedCalls)
	}
}

func TestEnsureResolved_OfflineNeverResolvesEvenWhenStale(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	if err := WriteUnitsLock(repo, UnitsLock{InputsDigest: staleDigest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{Offline: true, UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved offline: %v", err)
	}
	if fake.resolveCalls != 0 {
		t.Errorf("offline must never Resolve, got %d", fake.resolveCalls)
	}
	if fake.resolveLockedCalls != 1 {
		t.Errorf("offline must ResolveLocked once, got %d", fake.resolveLockedCalls)
	}
	if res.Fresh || res.ReResolved {
		t.Errorf("offline-stale: want !Fresh && !ReResolved, got %+v", res)
	}
	if !containsReason(res.Reasons, ReasonInputsDigest) {
		t.Errorf("offline must carry staleness reasons, got %v", res.Reasons)
	}
}

func TestEnsureResolved_NoSyncEchoedOnResult(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	seedFreshLock(t, repo, userPath, map[string]LockedUnit{})
	fake := &fakeEnsureResolver{}

	res, err := EnsureResolved(repo, EnsureOpts{NoSync: true, UserLocalPath: userPath, Resolver: fake})
	if err != nil {
		t.Fatalf("EnsureResolved no-sync: %v", err)
	}
	if !res.NoSync {
		t.Errorf("NoSync must be echoed on the result, got %+v", res)
	}
}

func TestEnsureResolved_DefaultsToLayeredResolver(t *testing.T) {
	// No Resolver injected: opts.resolver() must default to a real
	// LayeredResolver (exercising the default branch). A missing manifest makes
	// the staleness LoadAgentsRC fail loudly, proving the default path ran
	// without us touching the network.
	_, err := EnsureResolved(t.TempDir(), EnsureOpts{})
	if err == nil {
		t.Fatal("expected staleness error on a project with no manifest")
	}
}

func TestEnsureResolved_StalenessErrorPropagates(t *testing.T) {
	// A repo with no manifest makes Staleness fail (LoadAgentsRC error); the
	// error must surface and no resolution must run.
	fake := &fakeEnsureResolver{}
	_, err := EnsureResolved(t.TempDir(), EnsureOpts{Resolver: fake})
	if err == nil {
		t.Fatal("expected staleness error")
	}
	if fake.resolveCalls != 0 || fake.resolveLockedCalls != 0 {
		t.Errorf("no resolution should run on staleness error, got resolve=%d locked=%d", fake.resolveCalls, fake.resolveLockedCalls)
	}
}

func TestEnsureResolved_ResolveLockedErrorPropagates(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	sentinel := errors.New("cache gap")
	fake := &fakeEnsureResolver{resolveLockedErr: sentinel}

	_, err := EnsureResolved(repo, EnsureOpts{Frozen: true, UserLocalPath: userPath, Resolver: fake})
	if !errors.Is(err, sentinel) {
		t.Fatalf("frozen ResolveLocked error must propagate, got %v", err)
	}
}

func TestEnsureResolved_ResolveErrorPropagates(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	if err := WriteUnitsLock(repo, UnitsLock{InputsDigest: staleDigest}); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
	sentinel := errors.New("resolve boom")
	fake := &fakeEnsureResolver{resolveErr: sentinel}

	_, err := EnsureResolved(repo, EnsureOpts{UserLocalPath: userPath, Resolver: fake})
	if !errors.Is(err, sentinel) {
		t.Fatalf("stale Resolve error must propagate, got %v", err)
	}
}

func TestEnsureResolved_OfflineResolveLockedErrorPropagates(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	seedFreshLock(t, repo, userPath, map[string]LockedUnit{})
	sentinel := errors.New("offline cache gap")
	fake := &fakeEnsureResolver{resolveLockedErr: sentinel}

	_, err := EnsureResolved(repo, EnsureOpts{Offline: true, UserLocalPath: userPath, Resolver: fake})
	if !errors.Is(err, sentinel) {
		t.Fatalf("offline ResolveLocked error must propagate, got %v", err)
	}
}

// containsReason reports whether want appears in reasons.
func containsReason(reasons []StalenessReason, want StalenessReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func TestEnsureResolved_FreshResolveLockedErrorPropagates(t *testing.T) {
	repo, userPath := ensureSeed(t, repoManifestX)
	seedFreshLock(t, repo, userPath, map[string]LockedUnit{})
	sentinel := errors.New("fresh cache gap")
	fake := &fakeEnsureResolver{resolveLockedErr: sentinel}

	_, err := EnsureResolved(repo, EnsureOpts{UserLocalPath: userPath, Resolver: fake})
	if !errors.Is(err, sentinel) {
		t.Fatalf("fresh-default ResolveLocked error must propagate, got %v", err)
	}
}
