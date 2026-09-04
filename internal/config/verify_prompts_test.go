package config

import (
	"os"
	"strings"
	"testing"
)

// seedPromptUnitLock writes a lock whose units section carries the given prompt
// units (plus one sibling layer unit, so the filter is exercised) and returns the
// project path.
func seedPromptUnitLock(t *testing.T, units map[string]LockedUnit) string {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	all := map[string]LockedUnit{"team:team/base.json": {Kind: UnitKindLayer, Digest: "deadbeef"}}
	for key, unit := range units {
		all[key] = unit
	}
	if err := WriteUnitsLock(repo, UnitsLock{Units: all}); err != nil {
		t.Fatal(err)
	}
	return repo
}

// cachePromptBytes writes bytes into the prompt cache at the given digest.
func cachePromptBytes(t *testing.T, ref PromptUnitRef, digest string, body []byte) {
	t.Helper()
	if err := writeCachedUnit(promptTarget(ref), digest, body); err != nil {
		t.Fatal(err)
	}
}

// onlyStatus verifies the project's prompt units, asserts exactly one status was
// reported, and returns it.
func onlyStatus(t *testing.T, projectPath string) PromptUnitStatus {
	t.Helper()
	statuses, err := VerifyPromptUnits(projectPath)
	if err != nil {
		t.Fatalf("VerifyPromptUnits: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v, want exactly one prompt unit", statuses)
	}
	return statuses[0]
}

// TestVerifyPromptUnitsCachedAndMissing covers the two headline outcomes: a
// pinned prompt whose bytes are present verifies, and one whose cache entry was
// pruned reports the actionable sync hint.
func TestVerifyPromptUnitsCachedAndMissing(t *testing.T) {
	ref := PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	body := []byte("# ts-lint\n")
	digest := contentHash(body)

	repo := seedPromptUnitLock(t, map[string]LockedUnit{
		ref.Key(): {Kind: UnitKindPrompt, Digest: digest},
	})
	cachePromptBytes(t, ref, digest, body)

	st := onlyStatus(t, repo)
	if !st.OK() || !st.Cached {
		t.Fatalf("cached prompt must verify, got %#v", st)
	}
	if st.SourceID != "team" || st.Path != "verifiers/ts-lint.md" {
		t.Fatalf("status did not carry source/path: %#v", st)
	}
	if st.CachePath == "" {
		t.Fatal("status must report the cache path it checked")
	}

	if err := os.RemoveAll(CachedPromptPath(ref, digest)); err != nil {
		t.Fatal(err)
	}
	st = onlyStatus(t, repo)
	assertPromptProblem(t, st, "missing")
}

// TestVerifyPromptUnitsProblemCases covers the remaining fail-closed branches
// from the lock side: a digest-less pin, an unparseable key, and cached bytes
// that no longer hash to a content-addressed digest.
func TestVerifyPromptUnitsProblemCases(t *testing.T) {
	ref := PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	cases := []struct {
		name  string
		key   string
		unit  LockedUnit
		seed  func(t *testing.T)
		want  string
		cache bool
	}{
		{
			name: "no digest",
			key:  ref.Key(),
			unit: LockedUnit{Kind: UnitKindPrompt},
			want: "pinned without a digest",
		},
		{
			name: "unparseable key",
			key:  "no-source-prefix",
			unit: LockedUnit{Kind: UnitKindPrompt, Digest: "abc"},
			want: "invalid prompt unit key",
		},
		{
			name:  "content digest mismatch",
			key:   ref.Key(),
			unit:  LockedUnit{Kind: UnitKindPrompt, Digest: contentHash([]byte("# ts-lint\n"))},
			seed:  func(t *testing.T) { cachePromptBytes(t, ref, contentHash([]byte("# ts-lint\n")), []byte("tampered")) },
			want:  "no longer match the locked digest",
			cache: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := seedPromptUnitLock(t, map[string]LockedUnit{tc.key: tc.unit})
			if tc.seed != nil {
				tc.seed(t)
			}
			st := onlyStatus(t, repo)
			assertPromptProblem(t, st, tc.want)
			if st.Cached != tc.cache {
				t.Fatalf("cached = %t, want %t (%#v)", st.Cached, tc.cache, st)
			}
		})
	}
}

// assertPromptProblem asserts a status is a problem whose text contains want.
func assertPromptProblem(t *testing.T, st PromptUnitStatus, want string) {
	t.Helper()
	if st.OK() {
		t.Fatalf("expected a problem containing %q, got a clean status %#v", want, st)
	}
	if !strings.Contains(st.Problem, want) {
		t.Fatalf("problem = %q, want it to mention %q", st.Problem, want)
	}
}

// TestVerifyPromptUnitsEmptyAndNonContentDigests covers the no-prompt-units case
// and the digests VerifyPromptUnits deliberately does NOT re-hash: a 40-hex git
// commit and a prefixed OCI digest verify on presence alone.
func TestVerifyPromptUnitsEmptyAndNonContentDigests(t *testing.T) {
	empty := seedPromptUnitLock(t, nil)
	statuses, err := VerifyPromptUnits(empty)
	if err != nil {
		t.Fatalf("VerifyPromptUnits: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("a lock with no prompt units must report nothing, got %#v", statuses)
	}

	ref := PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	for _, digest := range []string{"abc0000000000000000000000000000000000001", "sha256:" + contentHash([]byte("x"))} {
		repo := seedPromptUnitLock(t, map[string]LockedUnit{ref.Key(): {Kind: UnitKindPrompt, Digest: digest}})
		cachePromptBytes(t, ref, digest, []byte("# unrelated bytes\n"))
		st := onlyStatus(t, repo)
		if !st.OK() {
			t.Fatalf("digest %q must verify on presence alone, got %#v", digest, st)
		}
	}
}

// TestVerifyPromptUnitsSurfacesLockError proves a corrupt lock is an error, not a
// silent "nothing pinned".
func TestVerifyPromptUnitsSurfacesLockError(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(AgentsLockPath(repo), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPromptUnits(repo); err == nil {
		t.Fatal("expected a corrupt lock to surface an error")
	}
}

// TestIsBareSha256Hex covers the digest-shape gate directly, including the
// non-hex character branch a full-resolve test cannot easily reach.
func TestIsBareSha256Hex(t *testing.T) {
	cases := map[string]bool{
		contentHash([]byte("x")):                   true,
		"sha256:" + contentHash([]byte("x")):       false,
		"abc0000000000000000000000000000000000001": false,
		"":                                 false,
		"z" + contentHash([]byte("x"))[1:]: false,
	}
	for digest, want := range cases {
		if got := isBareSha256Hex(digest); got != want {
			t.Fatalf("isBareSha256Hex(%q) = %t, want %t", digest, got, want)
		}
	}
}
