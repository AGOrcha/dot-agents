package crgbehavior

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FixtureSchemaVersion is the pinned upstream-fixture format version.
const FixtureSchemaVersion = 1

// DefaultFixtureDir is the repo-relative directory holding the release-pinned
// upstream behavior fixtures, one per corpus commit.
const DefaultFixtureDir = "testdata/crg-behavior/fixtures"

// ErrFixtureMissing reports that no release-pinned upstream fixture exists for
// a corpus commit. It is a gate failure, not a skip: without a recorded
// baseline there is nothing proving the live bridge still behaves like the
// release the gate claims to certify against.
var ErrFixtureMissing = errors.New("crgbehavior: no release-pinned upstream fixture for this commit")

// UpstreamFixture is the RECORDED behavior of the pinned release at one corpus
// commit — the release-conformance baseline.
//
// It is produced by `crgbehaviorgate -record` driving the real pinned release
// against a materialized worktree; it is never hand-authored. Recording rather
// than hand-writing is what makes it evidence: a hand-written "expected" file
// asserts what someone believed, a recorded one asserts what the release did.
//
// Rows hold the SAME canonical row renderings the live comparison produces, so
// the fixture is diffed by the one exact oracle every other surface uses, and a
// reviewer reads an upstream-behavior change as a plain text diff.
type UpstreamFixture struct {
	SchemaVersion int `json:"schema_version"`
	// Release is the bridge the fixture was recorded from.
	Release ReleaseInfo `json:"release"`
	// Commit is the corpus commit the worktree was materialized at.
	Commit string `json:"commit"`
	// RecordedAt is when the recording ran (provenance only; never compared).
	RecordedAt string `json:"recorded_at"`
	// Rows maps a surface name to its canonical row rendering.
	Rows map[string][]string `json:"rows"`
	// Lifecycle is the release's observed build/postprocess staleness contract.
	Lifecycle LifecycleObservation `json:"lifecycle"`
}

// NewFixture records one commit's upstream behavior.
func NewFixture(info ReleaseInfo, commit string, rows map[string][]string,
	lifecycle LifecycleObservation) UpstreamFixture {
	normalized := make(map[string][]string, len(rows))
	for surface, values := range rows {
		sorted := append([]string(nil), values...)
		sort.Strings(sorted)
		normalized[surface] = sorted
	}
	return UpstreamFixture{
		SchemaVersion: FixtureSchemaVersion,
		Release:       info,
		Commit:        commit,
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
		Rows:          normalized,
		Lifecycle:     lifecycle,
	}
}

// FixturePath is the on-disk location of one commit's fixture.
func FixturePath(dir, commit string) string {
	return filepath.Join(dir, short(commit)+".json")
}

// LoadFixture reads and validates one commit's recorded upstream behavior.
func LoadFixture(dir, commit string) (UpstreamFixture, error) {
	path := FixturePath(dir, commit)
	data, err := os.ReadFile(path) //nolint:gosec // pinned test fixture path
	if os.IsNotExist(err) {
		return UpstreamFixture{}, fmt.Errorf("%w: %s (record it with `crgbehaviorgate -record` against %s %s)",
			ErrFixtureMissing, path, PackageName, PinnedVersion)
	}
	if err != nil {
		return UpstreamFixture{}, fmt.Errorf("crgbehavior: read upstream fixture: %w", err)
	}
	var fx UpstreamFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return UpstreamFixture{}, fmt.Errorf("crgbehavior: parse upstream fixture %s: %w", path, err)
	}
	if err := fx.Validate(commit); err != nil {
		return UpstreamFixture{}, err
	}
	return fx, nil
}

// Validate rejects a fixture that cannot serve as this commit's baseline.
func (f UpstreamFixture) Validate(commit string) error {
	switch {
	case f.SchemaVersion != FixtureSchemaVersion:
		return fmt.Errorf("crgbehavior: upstream fixture schema_version %d, want %d",
			f.SchemaVersion, FixtureSchemaVersion)
	case f.Release.Version != PinnedVersion:
		return fmt.Errorf("%w: fixture for %s was recorded from %s %s",
			ErrReleaseMismatch, short(commit), PackageName, f.Release.Version)
	case f.Release.SchemaVersion != PinnedSchemaVersion:
		return fmt.Errorf("%w: fixture for %s records graph schema v%d, want v%d",
			ErrSchemaIncompatible, short(commit), f.Release.SchemaVersion, PinnedSchemaVersion)
	case commit != "" && f.Commit != commit:
		return fmt.Errorf("crgbehavior: upstream fixture records commit %s, want %s",
			short(f.Commit), short(commit))
	case len(f.Rows) == 0:
		return fmt.Errorf("crgbehavior: upstream fixture for %s records no surface", short(commit))
	}
	return nil
}

// Save writes the fixture as pretty-printed JSON.
func (f UpstreamFixture) Save(dir string) error {
	return writeJSON(FixturePath(dir, f.Commit), f)
}

// Conform compares the live bridge's observed rows against the recorded
// release-pinned baseline. Every recorded surface is compared EXACTLY: the
// fixture already holds only normalized values, so any difference is a real
// upstream behavior change (or a graph that was not materialized at the pinned
// commit), never formatting noise.
func (f UpstreamFixture) Conform(observed map[string][]string) Surface {
	var detail []string
	surfaces := make([]string, 0, len(f.Rows))
	for surface := range f.Rows {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)
	changed := 0
	for _, surface := range surfaces {
		onlyLive, onlyRecorded := symmetricDifference(observed[surface], f.Rows[surface])
		if len(onlyLive) == 0 && len(onlyRecorded) == 0 {
			continue
		}
		changed++
		detail = append(detail, prefixed(surface+" only in LIVE: ", onlyLive)...)
		detail = append(detail, prefixed(surface+" only in FIXTURE: ", onlyRecorded)...)
	}
	metric := fmt.Sprintf("%d of %d recorded surface(s) match %s %s",
		len(surfaces)-changed, len(surfaces), PackageName, f.Release.Version)
	if changed == 0 {
		return agree(SurfaceUpstreamFixture, metric)
	}
	return diverge(SurfaceUpstreamFixture, metric, detail)
}

// short abbreviates a commit SHA for fixture names and messages.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
