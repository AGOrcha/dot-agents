package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// seedCachedLayerFile writes a fake cached layer.json under the resolved cache
// root so verifyLayerLocks sees a remote layer's downloaded assets as present.
func seedCachedLayerFile(t *testing.T, sourceID, layerPath, sha string) {
	t.Helper()
	dir := filepath.Join(cfg.AgentsHome(), "cache", "config", sourceID, filepath.FromSlash(layerPath), sha)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "layer.json"), []byte(`{"skills":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// okProbe / failProbe are injectable code-review-graph readiness stubs so the
// binary check is deterministic regardless of what is installed on the host.
func okProbe(string) error   { return nil }
func failProbe(string) error { return os.ErrNotExist }

func mustVerifyOptions(project string, json bool, probe func(string) error) *runVerifyOptions {
	return &runVerifyOptions{
		jsonOut:  json,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		cwd:      project,
		crgProbe: probe,
	}
}

func findCheck(checks []VerifyCheck, name string) (VerifyCheck, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return VerifyCheck{}, false
}

// ---------- buildVerifyReport ----------

func TestBuildVerifyReport_ManifestMissingShortCircuits(t *testing.T) {
	project := withRepoLayer(t, "", "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false for missing manifest")
	}
	if len(report.Checks) != 1 || report.Checks[0].Name != "manifest" || report.Checks[0].Status != verifyFail {
		t.Fatalf("expected single failed manifest check, got %+v", report.Checks)
	}
}

func TestBuildVerifyReport_ManifestUnparseable(t *testing.T) {
	project := withRepoLayer(t, "{not-json", "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false for unparseable manifest")
	}
	if c := report.Checks[0]; c.Status != verifyFail {
		t.Fatalf("manifest check should fail, got %+v", c)
	}
}

func TestBuildVerifyReport_CleanManifest_ProbeBranches(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")

	rOK := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if !rOK.OK {
		t.Fatalf("expected OK=true, got %+v", rOK)
	}
	if c, _ := findCheck(rOK.Checks, "binary:code-review-graph"); c.Status != verifyPass {
		t.Fatalf("probe ok should pass, got %+v", c)
	}
	if c, _ := findCheck(rOK.Checks, "manifest"); c.Status != verifyPass {
		t.Fatalf("manifest should pass, got %+v", c)
	}

	rWarn := buildVerifyReport(mustVerifyOptions(project, false, failProbe))
	if !rWarn.OK {
		t.Fatalf("a binary warning must not flip OK, got %+v", rWarn)
	}
	if c, _ := findCheck(rWarn.Checks, "binary:code-review-graph"); c.Status != verifyWarn {
		t.Fatalf("probe failure should warn, got %+v", c)
	}
}

func TestBuildVerifyReport_LocalPathMissingFails(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local","path":"no/such/layer"}]}`, "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false when a declared local layer path is missing")
	}
}

// ---------- verifySources ----------

func TestVerifySources(t *testing.T) {
	cwd := t.TempDir()
	// A present relative path and a present absolute path.
	if err := os.MkdirAll(filepath.Join(cwd, "present"), 0o755); err != nil {
		t.Fatal(err)
	}
	absPresent := filepath.Join(cwd, "abs-present")
	if err := os.WriteFile(absPresent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		sources    any
		wantName   string
		wantStatus string
	}{
		{"no sources key", nil, "config-layers", verifyPass},
		{"empty list", []any{}, "config-layers", verifyPass},
		{"local no path", []any{map[string]any{"type": "local"}}, "source[0](local)", verifyPass},
		{"local rel present", []any{map[string]any{"type": "local", "path": "present"}}, "source[0](local)", verifyPass},
		{"local abs present", []any{map[string]any{"type": "local", "path": absPresent}}, "source[0](local)", verifyPass},
		{"local missing", []any{map[string]any{"type": "local", "path": "nope"}}, "source[0](local)", verifyFail},
		{"git remote", []any{map[string]any{"type": "git", "url": "u"}}, "source[0](git)", verifyPass},
		{"http remote", []any{map[string]any{"type": "http", "url": "u"}}, "source[0](http)", verifyPass},
		{"oci remote", []any{map[string]any{"type": "oci", "url": "u"}}, "source[0](oci)", verifyPass},
		{"missing type", []any{map[string]any{"url": "u"}}, "source[0](?)", verifyFail},
		{"unknown type", []any{map[string]any{"type": "ftp"}}, "source[0](ftp)", verifyWarn},
		{"not an object", []any{"oops"}, "source[0]", verifyFail},
		{"id label", []any{map[string]any{"type": "git", "id": "acme", "url": "u"}}, "source:acme", verifyPass},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := map[string]any{}
			if tc.sources != nil {
				repo["sources"] = tc.sources
			}
			snap := &snapshot{layers: map[string]map[string]any{layerRepoLocal: repo}}
			checks := verifySources(cwd, snap)
			c, ok := findCheck(checks, tc.wantName)
			if !ok {
				t.Fatalf("missing check %q in %+v", tc.wantName, checks)
			}
			if c.Status != tc.wantStatus {
				t.Fatalf("check %q status = %q, want %q (%+v)", tc.wantName, c.Status, tc.wantStatus, c)
			}
		})
	}
}

// ---------- sourceLabel ----------

func TestSourceLabel(t *testing.T) {
	cases := []struct {
		name string
		i    int
		src  map[string]any
		want string
	}{
		{"id wins", 2, map[string]any{"id": "acme", "type": "git"}, "source:acme"},
		{"type fallback", 1, map[string]any{"type": "git"}, "source[1](git)"},
		{"empty id falls through", 0, map[string]any{"id": "", "type": "local"}, "source[0](local)"},
		{"no type", 3, map[string]any{}, "source[3](?)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceLabel(tc.i, tc.src); got != tc.want {
				t.Fatalf("sourceLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------- runVerify ----------

func TestRunVerify_HumanOK(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	opts := mustVerifyOptions(project, false, okProbe)
	if err := runVerify(opts, testDeps()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Summary:") || !strings.Contains(out, "OK") {
		t.Fatalf("human output missing summary/OK: %s", out)
	}
}

func TestRunVerify_JSONOK(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	opts := mustVerifyOptions(project, true, okProbe)
	if err := runVerify(opts, testDeps()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	var report VerifyReport
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &report); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected OK=true, got %+v", report)
	}
}

func TestRunVerify_FailReturnsError(t *testing.T) {
	project := withRepoLayer(t, "", "") // missing manifest
	opts := mustVerifyOptions(project, false, okProbe)
	err := runVerify(opts, testDeps())
	if err == nil {
		t.Fatalf("expected error for failing verify")
	}
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, "failed checks") {
		t.Fatalf("expected hintError about failed checks, got %v", err)
	}
}

func TestRunVerify_FailJSON(t *testing.T) {
	project := withRepoLayer(t, "", "")
	opts := mustVerifyOptions(project, true, okProbe)
	if err := runVerify(opts, testDeps()); err == nil {
		t.Fatalf("expected error for failing verify")
	}
	var report VerifyReport
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &report); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if report.OK {
		t.Fatalf("expected OK=false, got %+v", report)
	}
}

// ---------- command wiring + small pure helpers ----------

func TestNewVerifyCmd_Execute(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cmd := newVerifyCmd(testDeps())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "Config verify") {
		t.Fatalf("expected verify output, got %s", out.String())
	}
}

func TestDefaultCRGProbe_DoesNotPanic(t *testing.T) {
	// Result depends on host PATH/.venv; we only exercise the line for coverage
	// and require it to return (nil or error) without panicking.
	_ = defaultCRGProbe(t.TempDir())
}

func TestExtendsRefSourceIDs(t *testing.T) {
	cases := []struct {
		name    string
		repo    map[string]any
		wantHas []string
		wantNot []string
	}{
		{"string + object refs", map[string]any{"extends": []any{
			"acme:org/base", map[string]any{"ref": "acme:org/opt", "optional": true}, "beta:x/y",
		}}, []string{"acme", "beta"}, []string{"missing"}},
		{"no-colon ref ignored", map[string]any{"extends": []any{"bogus"}}, nil, []string{"bogus"}},
		{"no extends key", map[string]any{}, nil, []string{"acme"}},
		{"extends not array", map[string]any{"extends": "nope"}, nil, []string{"nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extendsRefSourceIDs(tc.repo)
			for _, id := range tc.wantHas {
				if _, ok := got[id]; !ok {
					t.Fatalf("want id %q present in %v", id, got)
				}
			}
			for _, id := range tc.wantNot {
				if _, ok := got[id]; ok {
					t.Fatalf("id %q should be absent in %v", id, got)
				}
			}
		})
	}
}

func TestVerifySources_RemoteReferencedVsUnused(t *testing.T) {
	cwd := t.TempDir()
	gitSrc := map[string]any{"type": "git", "id": "acme", "url": "u"}

	// referenced: an extends ref uses the source id
	used := &snapshot{layers: map[string]map[string]any{layerRepoLocal: {
		"sources": []any{gitSrc},
		"extends": []any{"acme:org/base"},
	}}}
	c, _ := findCheck(verifySources(cwd, used), "source:acme")
	if c.Status != verifyPass || !strings.Contains(c.Detail, "verified in the locked-layers check") {
		t.Fatalf("referenced remote should point to locked-layers, got %+v", c)
	}

	// unused: no extends references it
	unused := &snapshot{layers: map[string]map[string]any{layerRepoLocal: {
		"sources": []any{gitSrc},
	}}}
	c, _ = findCheck(verifySources(cwd, unused), "source:acme")
	if c.Status != verifyPass || !strings.Contains(c.Detail, "unused") {
		t.Fatalf("unreferenced remote should be flagged unused, got %+v", c)
	}
}

func TestVerifyLayerLocks_RendersStatuses(t *testing.T) {
	manifest := `{
	  "sources": [
	    {"type":"git","id":"acme","url":"https://example.com/a.git"},
	    {"type":"local","id":"loc","path":"./layers"}
	  ],
	  "extends": [
	    "acme:org/base",
	    "acme:org/gone",
	    "loc:team/x",
	    {"ref":"acme:org/opt","optional":true}
	  ]
	}`
	project := withRepoLayer(t, manifest, "")
	if err := cfg.WriteConfigLock(project, map[string]cfg.LockedLayer{
		"acme:org/base": {ResolvedSHA: "abcdef0123456789", FetchedAt: "2026-06-02T00:00:00Z"},
		"acme:org/gone": {ResolvedSHA: "deadbeefcafef00d", FetchedAt: "2026-06-02T00:00:00Z"},
		"loc:team/x":    {ResolvedSHA: "11112222", FetchedAt: "2026-06-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	seedCachedLayerFile(t, "acme", "org/base", "abcdef0123456789") // base cached; gone is not
	seedCachedLayerFile(t, "loc", "team/x", "11112222")            // local layers are cached like remote

	checks := verifyLayerLocks(project)
	want := map[string]string{
		"layer:acme:org/base": verifyPass, // remote, locked + cached
		"layer:acme:org/gone": verifyFail, // remote, locked but cache missing
		"layer:loc:team/x":    verifyPass, // local, locked
		"layer:acme:org/opt":  verifyWarn, // optional, unlocked
	}
	for name, status := range want {
		c, ok := findCheck(checks, name)
		if !ok {
			t.Fatalf("missing check %q in %+v", name, checks)
		}
		if c.Status != status {
			t.Fatalf("check %q = %q, want %q (%+v)", name, c.Status, status, c)
		}
	}
	// the cached remote layer's detail should carry the abbreviated SHA
	c, _ := findCheck(checks, "layer:acme:org/base")
	if !strings.Contains(c.Detail, "abcdef012345") {
		t.Fatalf("base detail should show abbreviated sha, got %q", c.Detail)
	}
}

func TestVerifyLayerLocks_NoExtendsReturnsNil(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	if got := verifyLayerLocks(project); got != nil {
		t.Fatalf("expected nil for no extends, got %+v", got)
	}
}

func TestVerifyLayerLocks_CorruptLockfileWarns(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"git","id":"a","url":"u"}],"extends":["a:org/b"]}`, "")
	if err := os.WriteFile(cfg.AgentsLockPath(project), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ok := findCheck(verifyLayerLocks(project), "locked-layers")
	if !ok || c.Status != verifyWarn {
		t.Fatalf("expected a locked-layers warn on corrupt lockfile, got %+v", verifyLayerLocks(project))
	}
}

func TestAbbrevSHA(t *testing.T) {
	if abbrevSHA("abcdef0123456789") != "abcdef012345" {
		t.Fatalf("long sha not truncated: %q", abbrevSHA("abcdef0123456789"))
	}
	if abbrevSHA("short") != "short" {
		t.Fatalf("short sha changed: %q", abbrevSHA("short"))
	}
}

func TestVerifyMarkAndOutcome(t *testing.T) {
	for status, want := range map[string]string{
		verifyPass: "ok ",
		verifyWarn: "warn",
		verifyFail: "FAIL",
		"bogus":    "?  ",
	} {
		if got := verifyMark(status); got != want {
			t.Fatalf("verifyMark(%q) = %q, want %q", status, got, want)
		}
	}
	if verifyOutcome(true) != "OK" || verifyOutcome(false) != "FAILED" {
		t.Fatalf("verifyOutcome mismatch")
	}
}
