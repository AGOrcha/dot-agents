package crgbehavior

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// releaseFixturePath is the checked-in capability fixture, from this package.
func releaseFixturePath() string {
	return filepath.Join("..", "..", DefaultReleasePath)
}

// The checked-in fixture IS the gate's baseline. If it ever describes another
// release, every downstream claim ("compared against 2.3.8") becomes false, so
// loading validates it against the pinned constants.
func TestReleaseFixtureDescribesThePinnedRelease(t *testing.T) {
	rel, err := LoadRelease(releaseFixturePath())
	if err != nil {
		t.Fatalf("load release fixture: %v", err)
	}
	if rel.Version != PinnedVersion || rel.SchemaVersion != PinnedSchemaVersion {
		t.Fatalf("fixture = %s schema v%d, want %s schema v%d",
			rel.Version, rel.SchemaVersion, PinnedVersion, PinnedSchemaVersion)
	}
	// The indexed-language set is what decides which commits are eligible and
	// which files carry symbols; a truncated map silently shrinks the corpus.
	for ext, want := range map[string]string{
		".go": "go", ".py": "python", ".ts": "typescript", ".tsx": "tsx",
		".rs": "rust", ".java": "java", ".rb": "ruby", ".cs": "csharp",
		".kt": "kotlin", ".swift": "swift", ".php": "php", ".c": "c", ".cpp": "cpp",
	} {
		if got := rel.Languages[ext]; got != want {
			t.Fatalf("release fixture maps %s to %q, want %q", ext, got, want)
		}
	}
	// The v9 edge-confidence columns are the schema delta this baseline adds;
	// dropping them from the fixture would silently retire a whole surface.
	edges, ok := rel.Table("edges")
	if !ok {
		t.Fatal("release fixture declares no edges table")
	}
	for _, column := range []string{"confidence", "confidence_tier"} {
		if !containsString(edges.Columns, column) {
			t.Fatalf("release fixture edges columns %v omit the schema-v9 %q column", edges.Columns, column)
		}
	}
}

// Every surface the corpus contract requires must be backed by a table the
// release fixture describes, or the contract could require something the probe
// can never report on.
func TestReleaseFixtureBacksEveryTableBackedSurface(t *testing.T) {
	rel, err := LoadRelease(releaseFixturePath())
	if err != nil {
		t.Fatalf("load release fixture: %v", err)
	}
	declared := map[string]bool{}
	for _, table := range rel.Tables {
		for _, surface := range table.Surfaces {
			declared[surface] = true
		}
	}
	for _, surface := range []string{
		SurfaceFlows, SurfaceFlowMetrics, SurfaceFlowSnapshots, SurfaceCommunities,
		SurfaceCommunitySummaries, SurfaceRiskIndex, SurfaceRiskDetail,
		SurfaceFTSIndex, SurfaceFTSSearch, SurfaceEdgeConfidence,
	} {
		if !declared[surface] {
			t.Fatalf("no release table backs surface %q, so the probe can never attribute it", surface)
		}
	}
}

// A fixture describing anything other than the pinned release is a
// contradiction, not a knob.
func TestReleaseValidateRejectsOffBaselineFixtures(t *testing.T) {
	mutations := map[string]func(*Release){
		"package":        func(r *Release) { r.Package = "other" },
		"version":        func(r *Release) { r.Version = "2.2.0" },
		"tag":            func(r *Release) { r.Tag = "v2.2.0" },
		"commit":         func(r *Release) { r.Commit = "deadbeef" },
		"schema version": func(r *Release) { r.SchemaVersion = 8 },
		"no languages":   func(r *Release) { r.Languages = nil },
		"no tables":      func(r *Release) { r.Tables = nil },
		"no columns":     func(r *Release) { r.Tables[0].Columns = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			rel := testRelease()
			mutate(&rel)
			if err := rel.Validate(); err == nil {
				t.Fatalf("Validate accepted a fixture with a mutated %s", name)
			}
		})
	}
}

// Row counts run through a literal statement per table, so a fixture may only
// name tables the pinned release is known to write. That rejects an
// injection-shaped name AND a perfectly well-formed identifier the release
// never writes — the second case is the one a pure identifier check accepted,
// and it would have reached the probe as an unanswerable table.
func TestReleaseRejectsAnUnwrittenTableName(t *testing.T) {
	for _, name := range []string{"nodes; DROP TABLE nodes", "risk_scores", "Nodes"} {
		t.Run(name, func(t *testing.T) {
			rel := testRelease()
			rel.Tables = append(rel.Tables, TableSpec{Name: name, Columns: []string{"id"}})
			err := rel.Validate()
			if err == nil || !strings.Contains(err.Error(), "not known to write") {
				t.Fatalf("Validate error = %v, want %q rejected as unwritten", err, name)
			}
		})
	}
}

// Every table the checked-in fixture declares must have a row-count statement,
// so the closed set in schema.go cannot drift away from the fixture it serves.
func TestSchemaCountsEveryReleaseTable(t *testing.T) {
	rel, err := LoadRelease(releaseFixturePath())
	if err != nil {
		t.Fatalf("load the checked-in release fixture: %v", err)
	}
	for _, spec := range rel.Tables {
		if _, ok := rowCountSQL[spec.Name]; !ok {
			t.Errorf("release fixture declares %q with no row-count statement", spec.Name)
		}
	}
	for table := range rowCountSQL {
		if _, ok := rel.Table(table); !ok {
			t.Errorf("rowCountSQL declares %q, which the release fixture does not", table)
		}
	}
}

// An unidentified bridge cannot be a baseline, and an off-release one must fail
// loudly rather than quietly becoming the comparison target.
func TestReleaseVersionChecks(t *testing.T) {
	version, err := ParseCLIVersion("code-review-graph 2.3.8\n")
	if err != nil || version != PinnedVersion {
		t.Fatalf("ParseCLIVersion = %q, %v; want %q", version, err, PinnedVersion)
	}
	if _, err := ParseCLIVersion("usage: code-review-graph [-h]\n"); err == nil {
		t.Fatal("ParseCLIVersion accepted output carrying no version")
	}
	rel := testRelease()
	if err := rel.CheckVersion(PinnedVersion); err != nil {
		t.Fatalf("CheckVersion(pinned) = %v", err)
	}
	if err := rel.CheckVersion("2.2.0"); !errors.Is(err, ErrReleaseMismatch) {
		t.Fatalf("CheckVersion(2.2.0) = %v, want ErrReleaseMismatch", err)
	}
}

// LanguageOf is the eligibility rule for a changed file; it must be
// case-insensitive on the extension and silent about unindexed files.
func TestReleaseLanguageOf(t *testing.T) {
	rel := testRelease()
	if got := rel.LanguageOf("pkg/A.GO"); got != "go" {
		t.Fatalf("LanguageOf(.GO) = %q, want go", got)
	}
	if got := rel.LanguageOf("docs/README.md"); got != "" {
		t.Fatalf("LanguageOf(.md) = %q, want the empty label", got)
	}
	if got := rel.IndexedLanguages(); len(got) == 0 {
		t.Fatal("IndexedLanguages returned nothing")
	}
}

// containsString reports slice membership.
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// The capability fixture is the gate's only source for release-dependent facts
// — the indexed-language set, the schema contract, the per-table surfaces. A
// fixture the gate cannot read or cannot trust must fail the load rather than
// leave the zero Release quietly indexing nothing.
func TestCiTLoadReleaseRejectsUnusableFixtures(t *testing.T) {
	offBaseline := testRelease()
	offBaseline.Version = "2.2.0"
	cases := []ciTLoadCase{
		{name: "no such file", want: "read release fixture"},
		{name: "malformed json", body: "{\"tables\":", want: "parse release fixture"},
		{name: "another release", body: ciTJSON(t, offBaseline), want: `version "2.2.0"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRelease(ciTStagedPath(t, "release.json", tc))
			ciTWantError(t, "LoadRelease", err, tc.want)
		})
	}
}

// Table is how a probe finds a spec and the surfaces it feeds. An absent table
// must report absence: handing back the zero spec as if it were found would
// turn "the release has no such table" into "the table declares no columns".
func TestCiTReleaseTableLookupReportsAbsence(t *testing.T) {
	rel := testRelease()
	spec, ok := rel.Table(tableRiskIndex)
	if !ok || !containsString(spec.Surfaces, SurfaceRiskIndex) {
		t.Fatalf("Table(%s) = %+v, %v; want the risk surfaces attributed", tableRiskIndex, spec, ok)
	}
	missing, ok := rel.Table("no_such_table")
	if ok {
		t.Fatalf("Table(no_such_table) reported a hit: %+v", missing)
	}
	if !reflect.DeepEqual(missing, TableSpec{}) {
		t.Fatalf("Table(no_such_table) = %+v, want the zero spec", missing)
	}
}

// Indexes is the corpus eligibility rule: a file the release does not parse
// carries no graph symbol, and a file it does must not be discarded as
// docs-only just because the extension was capitalized.
func TestCiTReleaseIndexesOnlyParsedFiles(t *testing.T) {
	rel := testRelease()
	for file, want := range map[string]bool{
		"pkg/a.go":         true,
		"web/App.TS":       true,
		"src/lib.rs":       true,
		"docs/README.md":   false,
		"Makefile":         false,
		"pkg/a.go.tmpl":    false,
		"testdata/x.gotmp": false,
	} {
		if got := rel.Indexes(file); got != want {
			t.Fatalf("Indexes(%q) = %v, want %v", file, got, want)
		}
	}
}

// The bridge is identified by the pinned package's OWN --version line and
// nothing else. An unidentified bridge cannot be a release-conformance
// baseline, so a near-miss line must fail rather than yield a version.
func TestCiTParseCLIVersionReadsOnlyThePinnedPackageLine(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{name: "surrounding whitespace", out: "\n   code-review-graph   2.3.8  \n\n", want: "2.3.8"},
		{name: "among other lines", out: "warning: deprecated\ncode-review-graph 2.4.0\ndone\n", want: "2.4.0"},
		{name: "a different package", out: "code-review-graph-plugin 9.9.9\n"},
		{name: "the package with no version", out: "code-review-graph\n"},
		{name: "empty output", out: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCLIVersion(tc.out)
			if tc.want == "" {
				ciTWantError(t, "ParseCLIVersion", err, "cannot read a "+PackageName+" version")
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ParseCLIVersion(%q) = %q, %v; want %q", tc.out, got, err, tc.want)
			}
		})
	}
}
