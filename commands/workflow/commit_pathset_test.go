package workflow

import (
	"reflect"
	"testing"
)

// A workflow-managed-root path that was modified must end up in the staged
// set. This is the most basic positive case.
func TestDerivePathSetManagedRootIncluded(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{XY: ".M", Path: ".agents/workflow/plans/x/PLAN.yaml"},
		{XY: ".M", Path: ".agents/history/y/PLAN.yaml"},
	}, nil)
	want := []string{
		".agents/history/y/PLAN.yaml",
		".agents/workflow/plans/x/PLAN.yaml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Paths outside the managed roots stay out unless the caller explicitly named
// them in the mutation surface (e.g. session-touched iter-N.yaml under
// .agents/active/). This is the "never -A" rule in action.
func TestDerivePathSetNonManagedExcludedByDefault(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{XY: ".M", Path: "src/main.go"},
		{XY: ".M", Path: "README.md"},
		{XY: ".M", Path: ".agents/workflow/plans/x/PLAN.yaml"},
	}, nil)
	want := []string{".agents/workflow/plans/x/PLAN.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("non-managed path leaked: got %v, want %v", got, want)
	}
}

// When the caller declares a path in the mutation surface, that path is
// included even outside the managed roots (e.g. iter-log files written
// under .agents/active/).
func TestDerivePathSetMutationSurfaceOptsIn(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{XY: ".M", Path: ".agents/active/iteration-log/iter-7.yaml"},
		{XY: ".M", Path: "src/main.go"}, // never declared, excluded
	}, []string{".agents/active/iteration-log/iter-7.yaml"})
	want := []string{".agents/active/iteration-log/iter-7.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Untracked entries OUTSIDE managed roots are excluded unless the mutation
// surface claims them. This is the "pre-existing-untracked dirs" guard:
// a stray node_modules/ or tmp/ that another tool created never sneaks in.
func TestDerivePathSetUntrackedOutsideManagedExcluded(t *testing.T) {
	status := []StatusEntry{
		{XY: "??", Path: "tmp/notes.md", Untracked: true},
		{XY: "??", Path: "node_modules/junk", Untracked: true},
	}
	got := DerivePathSet(status, nil)
	if len(got) != 0 {
		t.Errorf("untracked-outside-managed leaked: %v", got)
	}
}

// Untracked entries UNDER a managed root ARE included — a fresh PLAN.yaml
// from `da workflow plan create` is an untracked file on its first commit
// pass, and excluding it would orphan every new plan from the very flow
// this command exists to support.
func TestDerivePathSetUntrackedUnderManagedIncluded(t *testing.T) {
	status := []StatusEntry{
		{XY: "??", Path: ".agents/workflow/plans/new-plan/PLAN.yaml", Untracked: true},
		{XY: "??", Path: ".agents/history/old-plan/PLAN.yaml", Untracked: true},
	}
	got := DerivePathSet(status, nil)
	want := []string{
		".agents/history/old-plan/PLAN.yaml",
		".agents/workflow/plans/new-plan/PLAN.yaml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// An untracked file the mutation surface declared (e.g. a freshly-written
// iter-N.yaml under .agents/active/) IS included even when it falls outside
// the managed roots — the surface is authoritative for non-managed paths.
func TestDerivePathSetMutationSurfaceClaimsUntrackedOutsideManaged(t *testing.T) {
	status := []StatusEntry{
		{XY: "??", Path: ".agents/active/iteration-log/iter-7.yaml", Untracked: true},
	}
	got := DerivePathSet(status, []string{".agents/active/iteration-log/iter-7.yaml"})
	want := []string{".agents/active/iteration-log/iter-7.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Submodule-pointer entries must never be staged by this command — even if
// they fall under a managed root (which would be unusual, but the rule
// stands). A submodule bump is a separate kind of commit.
func TestDerivePathSetSubmodulePointersExcluded(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{XY: ".M", Path: "vendor/some-sub", Submodule: true},
		{XY: ".M", Path: ".agents/workflow/plans/x/sub-link", Submodule: true},
		{XY: ".M", Path: ".agents/workflow/plans/x/PLAN.yaml"},
	}, nil)
	want := []string{".agents/workflow/plans/x/PLAN.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("submodule entry leaked: got %v, want %v", got, want)
	}
}

// Rename entries stage both the destination and the source. Without the
// source, git would show the new file as untracked-added and the old file
// as still-tracked, producing two unrelated commits instead of a clean
// rename.
func TestDerivePathSetRenameStagesBothSides(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{
			XY:       "R.",
			Path:     ".agents/history/plans/old/PLAN.yaml",
			OrigPath: ".agents/workflow/plans/old/PLAN.yaml",
		},
	}, nil)
	want := []string{
		".agents/history/plans/old/PLAN.yaml",
		".agents/workflow/plans/old/PLAN.yaml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rename should stage both sides: got %v, want %v", got, want)
	}
}

// Determinism: same (status, surface) ⇒ same output, sorted, deduplicated.
// Run the function with intentionally-shuffled and duplicated inputs and
// assert the result is stable.
func TestDerivePathSetDeterministicSortedDeduplicated(t *testing.T) {
	status := []StatusEntry{
		{XY: ".M", Path: ".agents/workflow/plans/b/PLAN.yaml"},
		{XY: ".M", Path: ".agents/workflow/plans/a/PLAN.yaml"},
		{XY: ".M", Path: ".agents/workflow/plans/b/PLAN.yaml"}, // dup
		{XY: ".M", Path: ".agents/history/c/PLAN.yaml"},
	}
	got1 := DerivePathSet(status, nil)
	got2 := DerivePathSet(status, nil)
	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("non-deterministic output: %v vs %v", got1, got2)
	}
	want := []string{
		".agents/history/c/PLAN.yaml",
		".agents/workflow/plans/a/PLAN.yaml",
		".agents/workflow/plans/b/PLAN.yaml",
	}
	if !reflect.DeepEqual(got1, want) {
		t.Errorf("got %v, want %v", got1, want)
	}
}

// .agents/workflow_old/... must not match .agents/workflow/ — the prefix
// anchor uses a trailing slash exactly to prevent this kind of accidental
// directory-name collision.
func TestDerivePathSetPrefixAnchored(t *testing.T) {
	got := DerivePathSet([]StatusEntry{
		{XY: ".M", Path: ".agents/workflow_old/x"},
		{XY: ".M", Path: ".agents/historyish/y"},
	}, nil)
	if len(got) != 0 {
		t.Errorf("anchor broken: %v", got)
	}
}

// --- ParseStatus tests ---------------------------------------------------

// An ordinary v2 record: "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>".
// Smoke-check the parser pulls the right XY and path.
func TestParseStatusOrdinary(t *testing.T) {
	raw := []byte("1 .M N... 100644 100644 100644 abc abc .agents/workflow/plans/x/PLAN.yaml\x00")
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].XY != ".M" {
		t.Errorf("XY = %q, want .M", got[0].XY)
	}
	if got[0].Path != ".agents/workflow/plans/x/PLAN.yaml" {
		t.Errorf("Path = %q", got[0].Path)
	}
	if got[0].Submodule {
		t.Error("non-submodule entry flagged as submodule")
	}
}

// Untracked "?" record produces XY="??" and Untracked=true.
func TestParseStatusUntracked(t *testing.T) {
	raw := []byte("? scratch.tmp\x00")
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].XY != "??" || !got[0].Untracked || got[0].Path != "scratch.tmp" {
		t.Errorf("got %+v, want untracked scratch.tmp", got[0])
	}
}

// Submodule sub-state marker: field 3 starts with 'S' instead of 'N'. The
// parser flips Submodule=true so DerivePathSet can exclude it.
func TestParseStatusSubmoduleSubState(t *testing.T) {
	raw := []byte("1 .M S.M. 160000 160000 160000 abc abc vendor/some-sub\x00")
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if !got[0].Submodule {
		t.Error("Submodule = false, want true (sub field begins with S)")
	}
}

// Rename record: "2 R. ... <path>\x00<origPath>". The parser recovers both
// the new path and the original path so DerivePathSet can stage the rename
// from both sides.
func TestParseStatusRename(t *testing.T) {
	raw := []byte(
		"2 R. N... 100644 100644 100644 abc abc R100 " +
			".agents/history/plans/old/PLAN.yaml\x00" +
			".agents/workflow/plans/old/PLAN.yaml\x00",
	)
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Path != ".agents/history/plans/old/PLAN.yaml" {
		t.Errorf("Path = %q", got[0].Path)
	}
	if got[0].OrigPath != ".agents/workflow/plans/old/PLAN.yaml" {
		t.Errorf("OrigPath = %q", got[0].OrigPath)
	}
}

// Unmerged ("u") record: 11 fields, path is last.
func TestParseStatusUnmerged(t *testing.T) {
	raw := []byte("u UU N... 100644 100644 100644 100644 abc def ghi some/path\x00")
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 || got[0].XY != "UU" || got[0].Path != "some/path" {
		t.Errorf("got %+v", got)
	}
}

// Headers (# branch.oid …) and ignored (!) entries are skipped — they are
// not staging candidates.
func TestParseStatusSkipsHeadersAndIgnored(t *testing.T) {
	raw := []byte(
		"# branch.oid abc\x00" +
			"# branch.head main\x00" +
			"! .gitignore-noise\x00" +
			"1 .M N... 100644 100644 100644 abc abc real/file\x00",
	)
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1 (headers + ignored should be skipped)", len(got))
	}
	if got[0].Path != "real/file" {
		t.Errorf("Path = %q", got[0].Path)
	}
}

// Empty input is not an error.
func TestParseStatusEmpty(t *testing.T) {
	got, err := ParseStatus(nil)
	if err != nil {
		t.Fatalf("ParseStatus(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("entries = %d, want 0", len(got))
	}
}

// A malformed record (e.g. truncated v2 line) returns an error rather than
// silently dropping the entry — silent drops are how submodules sneak into
// staging in the first place.
func TestParseStatusMalformedReturnsError(t *testing.T) {
	raw := []byte("1 .M N... missing-fields\x00")
	if _, err := ParseStatus(raw); err == nil {
		t.Fatal("expected error for malformed record, got nil")
	}
}

// Truncated rename: too few space-separated fields before the path tail.
func TestParseStatusMalformedRenameReturnsError(t *testing.T) {
	raw := []byte("2 R. N... 100644 100644 abc def newpath\x00oldpath\x00")
	if _, err := ParseStatus(raw); err == nil {
		t.Fatal("expected error for truncated rename, got nil")
	}
}

// Truncated unmerged: too few fields before the path.
func TestParseStatusMalformedUnmergedReturnsError(t *testing.T) {
	raw := []byte("u UU N... 100644 100644 100644 abc def some/path\x00")
	if _, err := ParseStatus(raw); err == nil {
		t.Fatal("expected error for truncated unmerged record, got nil")
	}
}

// An unrecognized leading byte surfaces an error — porcelain v2 has a closed
// set of record kinds and an unknown one means the caller fed us something
// that is not v2 output (e.g. v1, plain text), which we must not silently
// pretend to parse.
func TestParseStatusUnknownRecordKindReturnsError(t *testing.T) {
	raw := []byte("x what is this\x00")
	if _, err := ParseStatus(raw); err == nil {
		t.Fatal("expected error for unknown record kind, got nil")
	}
}

// Direct call into parseV2Rename with a constructed slice whose tail field
// has no NUL separator — the case splitNUL normally prevents by always
// pairing rename records with a trailing NUL+origPath chunk. Confirms the
// defensive guard still trips if a caller short-circuits splitNUL.
func TestParseV2RenameMissingOrigPathSeparator(t *testing.T) {
	rec := []byte("2 R. N... 100644 100644 100644 abc abc R100 path-without-nul")
	if _, err := parseV2Rename(rec); err == nil {
		t.Fatal("expected error for missing origPath separator, got nil")
	}
}

// Direct call exercising splitNUL's "rename record is the final chunk with
// no trailing data" branch: the splicer would produce a record with empty
// origPath instead of swallowing the truncation silently. Asserts the
// resulting StatusEntry has an empty OrigPath rather than crashing.
func TestSplitNULTrailingRenameWithoutOrig(t *testing.T) {
	raw := []byte("2 R. N... 100644 100644 100644 abc abc R100 only-path\x00")
	chunks := splitNUL(raw)
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(chunks))
	}
	// The chunk should end in NUL (the splicer appended one) followed by
	// empty origPath. ParseStatus will then see Path="only-path", OrigPath="".
	got, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if len(got) != 1 || got[0].Path != "only-path" || got[0].OrigPath != "" {
		t.Errorf("got %+v, want Path=only-path OrigPath=\"\"", got)
	}
}

// End-to-end: parser feeds derivation. Confirms the two halves compose for
// the spec's headline case — "deliberately dirty worktree + submodule
// pointer fixture" — where the submodule must be filtered out and only the
// managed-root paths survive.
func TestParseAndDeriveDirtyWorktreeWithSubmodule(t *testing.T) {
	raw := []byte(
		"# branch.oid abc\x00" +
			"1 .M N... 100644 100644 100644 a a .agents/workflow/plans/x/PLAN.yaml\x00" +
			"1 .M N... 100644 100644 100644 a a src/code.go\x00" +
			"1 .M S.M. 160000 160000 160000 a a vendor/some-sub\x00" +
			"? scratch.tmp\x00",
	)
	entries, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	got := DerivePathSet(entries, nil)
	want := []string{".agents/workflow/plans/x/PLAN.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (submodule + code + untracked must all be excluded)", got, want)
	}
}
