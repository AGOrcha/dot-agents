package crgbehavior

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// pinnedInfo is the release a recording claims to come from.
func pinnedInfo() ReleaseInfo {
	return ReleaseInfo{Package: PackageName, Version: PinnedVersion, SchemaVersion: PinnedSchemaVersion}
}

// recordedRows is a small two-surface recording.
func recordedRows() map[string][]string {
	return map[string][]string{
		SurfaceFlows:          {"entry=b path=b", "entry=a path=a > b"},
		SurfaceRiskIndex:      {"a risk_score=0.300000"},
		SurfaceEdgeConfidence: {"CALLS a -> b confidence=1.000000 tier=EXTRACTED"},
	}
}

// A recording round-trips through disk and normalizes row order, so a fixture
// diff is a behavior diff rather than an ordering artifact.
func TestFixtureRoundTripsAndSortsRows(t *testing.T) {
	dir := t.TempDir()
	fx := NewFixture(pinnedInfo(), "1111111111111111", recordedRows(), LifecycleObservation{Observed: true})
	if err := fx.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadFixture(dir, "1111111111111111")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"entry=a path=a > b", "entry=b path=b"}
	if !reflect.DeepEqual(loaded.Rows[SurfaceFlows], want) {
		t.Fatalf("recorded flows = %v, want them sorted %v", loaded.Rows[SurfaceFlows], want)
	}
	if !loaded.Lifecycle.Observed {
		t.Fatal("the lifecycle observation was not recorded")
	}
}

// With no recorded baseline there is nothing proving the bridge still behaves
// like the release the gate certifies against — and the error has to say how to
// produce one.
func TestLoadFixtureReportsAMissingRecording(t *testing.T) {
	_, err := LoadFixture(t.TempDir(), "1111111111111111")
	if !errors.Is(err, ErrFixtureMissing) {
		t.Fatalf("LoadFixture error = %v, want ErrFixtureMissing", err)
	}
	if !strings.Contains(err.Error(), "-record") {
		t.Fatalf("error %q does not name the recording command", err)
	}
}

// A fixture recorded from another release, another schema or another commit is
// not this run's baseline.
func TestFixtureValidateRejectsAnOffBaselineRecording(t *testing.T) {
	base := func() UpstreamFixture {
		return NewFixture(pinnedInfo(), "1111111111111111", recordedRows(), LifecycleObservation{})
	}
	cases := map[string]func(*UpstreamFixture){
		"schema version": func(f *UpstreamFixture) { f.SchemaVersion = 99 },
		"release":        func(f *UpstreamFixture) { f.Release.Version = "2.2.0" },
		"graph schema":   func(f *UpstreamFixture) { f.Release.SchemaVersion = 8 },
		"commit":         func(f *UpstreamFixture) { f.Commit = "2222222222222222" },
		"no rows":        func(f *UpstreamFixture) { f.Rows = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fx := base()
			mutate(&fx)
			if err := fx.Validate("1111111111111111"); err == nil {
				t.Fatalf("Validate accepted a recording with a mutated %s", name)
			}
		})
	}
}

// Conformance is judged by the same exact oracle every other surface uses, and
// the diff names the surface that drifted.
func TestFixtureConformDetectsUpstreamDrift(t *testing.T) {
	fx := NewFixture(pinnedInfo(), "1111111111111111", recordedRows(), LifecycleObservation{})
	if s := fx.Conform(recordedRows()); s.Status != StatusAgree {
		t.Fatalf("identical rows = %s (%v), want agreement", s.Status, s.Detail)
	}
	drifted := recordedRows()
	drifted[SurfaceRiskIndex] = []string{"a risk_score=0.900000"}
	s := fx.Conform(drifted)
	if s.Status != StatusDiverge {
		t.Fatalf("drifted rows = %s, want a divergence", s.Status)
	}
	joined := strings.Join(s.Detail, "\n")
	if !strings.Contains(joined, SurfaceRiskIndex) ||
		!strings.Contains(joined, "only in LIVE") || !strings.Contains(joined, "only in FIXTURE") {
		t.Fatalf("conformance diff does not name the surface and both sides: %v", s.Detail)
	}
}

// A surface the live run did not produce at all is a conformance divergence,
// not a silent match against the recorded rows.
func TestFixtureConformFlagsAMissingLiveSurface(t *testing.T) {
	fx := NewFixture(pinnedInfo(), "1111111111111111", recordedRows(), LifecycleObservation{})
	live := recordedRows()
	delete(live, SurfaceFlows)
	if s := fx.Conform(live); s.Status != StatusDiverge {
		t.Fatalf("a missing live surface = %s, want a divergence", s.Status)
	}
}

// The recorded baseline is what proves the live bridge still behaves like the
// release the gate certifies against. Every way of failing to read one has to
// name itself: only an ABSENT recording may report ErrFixtureMissing, because
// that is the one case `crgbehaviorgate -record` fixes.
func TestCiTLoadFixtureRejectsUnusableRecordings(t *testing.T) {
	const commit = "1111111111111111"
	offRelease := NewFixture(
		ReleaseInfo{Package: PackageName, Version: "2.2.0", SchemaVersion: PinnedSchemaVersion},
		commit, recordedRows(), LifecycleObservation{})
	cases := []struct {
		name  string
		stage func(t *testing.T, path string)
		want  string
	}{
		{
			name: "the fixture path is a directory",
			stage: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("stage a directory at the fixture path: %v", err)
				}
			},
			want: "read upstream fixture",
		},
		{
			name:  "malformed json",
			stage: func(t *testing.T, path string) { ciTStage(t, path, "{\"rows\":") },
			want:  "parse upstream fixture",
		},
		{
			name:  "recorded from another release",
			stage: func(t *testing.T, path string) { ciTStage(t, path, ciTJSON(t, offRelease)) },
			want:  "was recorded from",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.stage(t, FixturePath(dir, commit))
			_, err := LoadFixture(dir, commit)
			ciTWantError(t, "LoadFixture", err, tc.want)
			if errors.Is(err, ErrFixtureMissing) {
				t.Fatalf("a present but unusable recording reported ErrFixtureMissing: %v", err)
			}
		})
	}
}
