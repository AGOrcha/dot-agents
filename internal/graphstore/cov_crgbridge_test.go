// Package graphstore — behaviour coverage for the three release-facing seams
// that have no in-process fake: the installed-release accessor
// (crg_release.go), the version-control probes (crg_vcs.go) and the tool
// bridge's transport contract (crg_tool_bridge.go).
//
// The release itself is deliberately NOT required here. Every branch below is
// a property of OUR code — how `--version` output is parsed, what a missing
// graph reports, what `svn info` text means, what document the bridge sends
// and how it reacts to a malformed reply — so each is driven by a stub
// executable on disk. That keeps the assertions deterministic and makes them
// fail on a plausible bug rather than on an absent install. The integration
// leg that proves the real release answers faithfully lives in
// crg_tool_bridge_test.go and stays there.
package graphstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// ── shared fixtures ──────────────────────────────────────────────────────────

// covBrShellScript wraps body in a POSIX shell script header.
func covBrShellScript(body string) string {
	return "#!/bin/sh\n" + body
}

// covBrEmitScript returns a script whose whole job is to print text verbatim
// on stdout. A quoted heredoc is used so the payload needs no shell escaping.
func covBrEmitScript(text string) string {
	return covBrShellScript("cat <<'CRGEOF'\n" + text + "\nCRGEOF\n")
}

// covBrVersionBridge returns a CRGBridge whose `code-review-graph` executable
// is the supplied stub script.
func covBrVersionBridge(t *testing.T, script string) *CRGBridge {
	t.Helper()
	repo, bin := makeFakeCRGEnv(t, script, covBrShellScript("exit 0\n"))
	return &CRGBridge{RepoRoot: repo, Bin: bin}
}

// covBrSeedMetadata writes a graph database at CRGDBPath(repoRoot) carrying
// exactly the supplied metadata rows.
func covBrSeedMetadata(t *testing.T, repoRoot string, rows map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(CRGDBPath(repoRoot)), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", CRGDBPath(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for key, value := range rows {
		if _, err := db.Exec(`INSERT INTO metadata (key,value) VALUES (?,?)`, key, value); err != nil {
			t.Fatal(err)
		}
	}
}

// ── crg_release.go: parseCRGVersion ──────────────────────────────────────────

// The parser has to survive every shape `--version` has taken across
// releases: bare, program-name prefixed, v-prefixed, and pre-release
// suffixed — and must report "unknown" rather than guess when no field looks
// like a version at all.
func TestCovBrParseCRGVersionReadsEveryOutputShape(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"bare version", "2.3.8\n", "2.3.8"},
		{"program name prefix", "code-review-graph, version 2.3.8\n", "2.3.8"},
		{"v prefix is stripped", "code-review-graph v2.3.8\n", "2.3.8"},
		{"pre-release suffix is preserved", "version 10.0.0rc1\n", "10.0.0rc1"},
		{"a bare v field is skipped", "version 2.3.8 v\n", "2.3.8"},
		{"later lines are searched", "\nWARNING: noisy\nversion 2.3.8\n", "2.3.8"},
		{"no numeric field", "crg version dev build\n", ""},
		{"empty output", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCRGVersion(tc.out); got != tc.want {
				t.Errorf("parseCRGVersion(%q) = %q, want %q", tc.out, got, tc.want)
			}
		})
	}
}

// The first line that yields a version wins, and within a line the RIGHTMOST
// numeric field does — that is what makes "code-review-graph, version X"
// resolve to X rather than to a digit inside the program name.
func TestCovBrParseCRGVersionPrefersTheRightmostField(t *testing.T) {
	if got := parseCRGVersion("crg2 build 7 2.3.8\n"); got != "2.3.8" {
		t.Errorf("parseCRGVersion picked %q, want the rightmost field 2.3.8", got)
	}
}

// ── crg_release.go: Release ──────────────────────────────────────────────────

// Release must report the identity a caller can act on: the version the
// executable printed, the executable it came from, whether it is the pinned
// release, and the schema version of the graph beside it.
func TestCovBrReleaseReportsPinnedIdentity(t *testing.T) {
	bridge := covBrVersionBridge(t,
		covBrEmitScript("code-review-graph, version "+crgrelease.Version))
	covBrSeedMetadata(t, bridge.RepoRoot, map[string]string{"schema_version": "7"})

	release, err := bridge.Release()
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if release.Version != crgrelease.Version {
		t.Errorf("Version = %q, want %q", release.Version, crgrelease.Version)
	}
	if release.Bin != bridge.Bin {
		t.Errorf("Bin = %q, want %q", release.Bin, bridge.Bin)
	}
	if !release.MatchesPin {
		t.Errorf("the pinned version %q must report MatchesPin", crgrelease.Version)
	}
	if release.SchemaVersion != 7 {
		t.Errorf("SchemaVersion = %d, want 7", release.SchemaVersion)
	}
}

// An installed release that is NOT the pinned one must be reported as such
// rather than rejected: callers decide what to do about the drift.
func TestCovBrReleaseFlagsAnUnpinnedInstall(t *testing.T) {
	bridge := covBrVersionBridge(t, covBrEmitScript("code-review-graph, version 1.0.0"))

	release, err := bridge.Release()
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if release.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", release.Version)
	}
	if release.MatchesPin {
		t.Error("1.0.0 must not report MatchesPin against the pinned release")
	}
	if release.SchemaVersion != 0 {
		t.Errorf("SchemaVersion = %d, want 0 when no graph exists", release.SchemaVersion)
	}
}

// Unparsable `--version` output is an error that quotes what was actually
// printed — the only thing that makes the failure diagnosable.
func TestCovBrReleaseRejectsUnparsableVersionOutput(t *testing.T) {
	bridge := covBrVersionBridge(t, covBrEmitScript("  no numbers here  "))

	release, err := bridge.Release()
	if err == nil {
		t.Fatalf("expected an error, got %+v", release)
	}
	want := `unrecognized code-review-graph version output: "no numbers here"`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// A failed probe must surface the executable's own stderr, wrapped so the
// caller knows which step failed.
func TestCovBrReleasePropagatesProbeFailure(t *testing.T) {
	bridge := covBrVersionBridge(t,
		covBrShellScript("echo 'probe exploded' >&2\nexit 1\n"))

	if _, err := bridge.Release(); err == nil {
		t.Fatal("expected an error from a failing --version probe")
	} else if got := err.Error(); !strings.HasPrefix(got, "read code-review-graph version:") ||
		!strings.Contains(got, "probe exploded") {
		t.Errorf("error = %q, want the wrapped stderr of the probe", got)
	}
}

// ── crg_release.go: graphSchemaVersion ───────────────────────────────────────

// "Installed but the graph cannot answer" must read back as schema version 0,
// not as a Release error: the caller asked which release is installed, and a
// missing or unusable graph is a valid answer to that question.
func TestCovBrReleaseSchemaVersionDegradesToZero(t *testing.T) {
	cases := []struct {
		name string
		rows map[string]string
	}{
		{"no graph database at all", nil},
		{"metadata without a schema_version row", map[string]string{"last_updated": "2026-01-01"}},
		{"a schema_version that is not a number", map[string]string{"schema_version": "two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bridge := covBrVersionBridge(t, covBrEmitScript("version 2.3.8"))
			if tc.rows != nil {
				covBrSeedMetadata(t, bridge.RepoRoot, tc.rows)
			}
			release, err := bridge.Release()
			if err != nil {
				t.Fatalf("Release: %v", err)
			}
			if release.SchemaVersion != 0 {
				t.Errorf("SchemaVersion = %d, want 0", release.SchemaVersion)
			}
		})
	}
}

// A whitespace-padded numeric value is still a schema version: upstream
// writes the row as text and trailing newlines have shown up in practice.
func TestCovBrReleaseSchemaVersionToleratesPadding(t *testing.T) {
	bridge := covBrVersionBridge(t, covBrEmitScript("version 2.3.8"))
	covBrSeedMetadata(t, bridge.RepoRoot, map[string]string{"schema_version": " 12\n"})

	release, err := bridge.Release()
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if release.SchemaVersion != 12 {
		t.Errorf("SchemaVersion = %d, want 12", release.SchemaVersion)
	}
}

// ── crg_vcs.go: gitTimeoutFromEnv ────────────────────────────────────────────

// Only a positive integer second count overrides the 30s default; anything
// else must fall back rather than produce a zero or negative deadline, which
// would make every probe fail instantly.
func TestCovBrGitTimeoutFromEnv(t *testing.T) {
	const fallback = 30 * time.Second
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", fallback},
		{"45", 45 * time.Second},
		{"1", time.Second},
		{"0", fallback},
		{"-3", fallback},
		{"abc", fallback},
		{" 7 ", fallback},
	}
	for _, tc := range cases {
		t.Run("CRG_GIT_TIMEOUT="+tc.raw, func(t *testing.T) {
			t.Setenv("CRG_GIT_TIMEOUT", tc.raw)
			if got := gitTimeoutFromEnv(); got != tc.want {
				t.Errorf("gitTimeoutFromEnv() = %s, want %s", got, tc.want)
			}
		})
	}
}

// ── crg_vcs.go: DetectVCS ────────────────────────────────────────────────────

// DetectVCS labels the root it was handed and nothing else: it recognises a
// worktree's .git FILE as well as a directory, prefers git when both markers
// are present, and deliberately does not walk upwards — a subdirectory of a
// git checkout is not itself a working copy root.
func TestCovBrDetectVCS(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		probe string
		want  string
	}{
		{"git directory", covBrMakeGitDir, "", VCSGit},
		{"git worktree file", covBrMakeGitFile, "", VCSGit},
		{"subversion working copy", covBrMakeSVNDir, "", VCSSVN},
		{"git wins over a stale .svn", covBrMakeGitAndSVN, "", VCSGit},
		{"unversioned directory", covBrMakeNothing, "", VCSNone},
		{"subdirectory of a git checkout", covBrMakeGitDir, "sub", VCSNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			probe := root
			if tc.probe != "" {
				probe = filepath.Join(root, tc.probe)
				if err := os.MkdirAll(probe, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := DetectVCS(probe); got != tc.want {
				t.Errorf("DetectVCS = %q, want %q", got, tc.want)
			}
		})
	}
}

func covBrMakeGitDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func covBrMakeGitFile(t *testing.T, root string) {
	t.Helper()
	body := []byte("gitdir: /elsewhere/.git/worktrees/wt\n")
	if err := os.WriteFile(filepath.Join(root, ".git"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func covBrMakeSVNDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".svn"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func covBrMakeGitAndSVN(t *testing.T, root string) {
	t.Helper()
	covBrMakeGitDir(t, root)
	covBrMakeSVNDir(t, root)
}

func covBrMakeNothing(*testing.T, string) {}

// ── crg_vcs.go: SVNInfo ──────────────────────────────────────────────────────

// covBrStubSVN puts a stub `svn` ahead of the real one on PATH, printing body
// for any invocation, and returns a working-copy root to probe.
func covBrStubSVN(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub shell executables are POSIX-only")
	}
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "svn")
	if err := os.WriteFile(stub, []byte(covBrEmitScript(body)), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepended, not replaced: the stub still needs the shell utilities it
	// is written in.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	covBrMakeSVNDir(t, root)
	return root
}

// SVNInfo must pull the branch and revision out of `svn info` text, ignoring
// every other field the command prints.
func TestCovBrSVNInfoParsesBranchAndRevision(t *testing.T) {
	root := covBrStubSVN(t, strings.Join([]string{
		"Path: .",
		"Working Copy Root Path: /wc",
		"URL: https://svn.example.com/repo/branches/feature-x",
		"Relative URL: ^/branches/feature-x",
		"Revision: 4211",
		"Node Kind: directory",
	}, "\n"))

	branch, revision := SVNInfo(root)
	if branch != "branches/feature-x" {
		t.Errorf("branch = %q, want branches/feature-x", branch)
	}
	if revision != "4211" {
		t.Errorf("revision = %q, want 4211", revision)
	}
}

// A working copy whose `svn info` carries neither field is reported as
// unknown rather than as a partially-filled label.
func TestCovBrSVNInfoIsEmptyWithoutTheFields(t *testing.T) {
	root := covBrStubSVN(t, "Path: .\nNode Kind: directory")

	branch, revision := SVNInfo(root)
	if branch != "" || revision != "" {
		t.Errorf("SVNInfo = (%q, %q), want empty", branch, revision)
	}
}

// No svn binary is not an error: the probes only label a graph, so an
// unavailable client reports unknown and the build carries on.
func TestCovBrSVNInfoIsEmptyWithoutAnSvnClient(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	covBrMakeSVNDir(t, root)

	branch, revision := SVNInfo(root)
	if branch != "" || revision != "" {
		t.Errorf("SVNInfo = (%q, %q), want empty when svn is missing", branch, revision)
	}
}

// ── crg_vcs.go: svnBranchFromURL ─────────────────────────────────────────────

// The branch label is the repository-layout segment of the URL when there is
// one, and the URL's last path element otherwise.
func TestCovBrSVNBranchFromURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"trunk", "https://svn.example.com/repo/trunk", "trunk"},
		{"path below trunk", "https://svn.example.com/repo/trunk/sub", "trunk/sub"},
		{"named branch", "https://svn.example.com/repo/branches/feature-x", "branches/feature-x"},
		{"named tag", "https://svn.example.com/repo/tags/v1.2.0", "tags/v1.2.0"},
		{"branches wins over a later trunk segment",
			"https://svn.example.com/repo/branches/trunk-rework", "branches/trunk-rework"},
		{"no recognisable layout", "https://svn.example.com/repo/project", "project"},
		{"trailing slash is ignored", "https://svn.example.com/repo/project/", "project"},
		{"empty url", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := svnBranchFromURL(tc.url); got != tc.want {
				t.Errorf("svnBranchFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// ── crg_tool_bridge.go: discovery and availability ───────────────────────────

// covBrToolBridge stages a stub release whose interpreter is pyScript and
// returns the bridge plus the repository root it is rooted at.
func covBrToolBridge(t *testing.T, pyScript string) (*CRGToolBridge, string) {
	t.Helper()
	repo, _ := makeFakeCRGEnv(t, covBrShellScript("exit 0\n"), pyScript)
	bridge := NewCRGToolBridge(repo)
	if !bridge.Available() {
		t.Fatalf("a staged release must be discoverable: %s", bridge.Unavailable())
	}
	return bridge, repo
}

// When no release can be discovered the bridge must refuse every call with
// the SAME actionable message it reports from Unavailable — one explanation,
// not two vocabularies.
func TestCovBrToolBridgeRefusesCallsWithTheDiscoveryFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	bridge := NewCRGToolBridge(t.TempDir())

	if bridge.Available() {
		t.Fatal("no release was staged, so the bridge must be unavailable")
	}
	reason := bridge.Unavailable()
	if !strings.Contains(reason, "code-review-graph not found") {
		t.Errorf("Unavailable() = %q, want the discovery failure", reason)
	}
	_, err := bridge.CallTool("list_graph_stats_tool", nil)
	if err == nil {
		t.Fatal("expected an error from an unavailable bridge")
	}
	if err.Error() != reason {
		t.Errorf("CallTool error = %q, want the Unavailable() reason %q", err.Error(), reason)
	}
}

// pythonBin falls back to a BARE interpreter name when the virtualenv beside
// the executable has none, so discovery has to resolve that name itself. If it
// did not, a release whose virtualenv lost its interpreter would report the
// bridge as AVAILABLE and then fail on the first call with a raw exec error
// instead of naming the missing interpreter.
func TestCovBrToolBridgeRejectsAReleaseWithNoInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub shell executables are POSIX-only")
	}
	repo := t.TempDir()
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	crgPath := filepath.Join(binDir, crgBinName)
	if err := os.WriteFile(crgPath, []byte(covBrShellScript("exit 0\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	// An empty PATH removes the fallback interpreter as well, so nothing can
	// resolve the bare name pythonBin returns.
	t.Setenv("PATH", t.TempDir())

	bridge := NewCRGToolBridge(repo)

	if bridge.Available() {
		t.Fatal("a release with no interpreter must not report itself available")
	}
	reason := bridge.Unavailable()
	if !strings.Contains(reason, crgPath) ||
		!strings.Contains(reason, "no usable Python interpreter beside it") {
		t.Errorf("Unavailable() = %q, want it to name %q and the missing interpreter",
			reason, crgPath)
	}
}

// Unavailable distinguishes the three ways a bridge can be unusable, and says
// nothing at all once one is usable.
func TestCovBrToolBridgeUnavailableExplanations(t *testing.T) {
	var absent *CRGToolBridge
	usable, _ := covBrToolBridge(t, covBrShellScript("exit 0\n"))
	cases := []struct {
		name   string
		bridge *CRGToolBridge
		want   string
	}{
		{"no bridge at all", absent, "no bridge configured"},
		{"discovered nothing", &CRGToolBridge{},
			"no Python interpreter found for the code-review-graph release"},
		{"usable", usable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.bridge.Unavailable(); got != tc.want {
				t.Errorf("Unavailable() = %q, want %q", got, tc.want)
			}
			if wantAvailable := tc.want == ""; tc.bridge.Available() != wantAvailable {
				t.Errorf("Available() = %v, want %v", tc.bridge.Available(), wantAvailable)
			}
		})
	}
}

// ── crg_tool_bridge.go: CallTool transport ───────────────────────────────────

// covBrRequest is the request document the bridge is contracted to write on
// the interpreter's stdin.
type covBrRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	RepoRoot  string         `json:"repo_root"`
}

// covBrEnvelopeStub records what the bridge sent — the request on stdin, and
// CRG_REPO_ROOT from the environment — then prints one result document.
//
// The recordings use RELATIVE paths on purpose: they can only land in the
// repository root if the bridge ran the interpreter with that directory as
// its working directory, which is the other half of the contract.
func covBrEnvelopeStub(isError bool) string {
	return covBrShellScript(fmt.Sprintf(`cat > request.json
printf '%%s' "$CRG_REPO_ROOT" > repo_root.txt
cat <<'CRGEOF'
{"is_error":%t,"content":[{"type":"text","text":"envelope text"}],
 "structured_content":{"status":"ok","count":3}}
CRGEOF
`, isError))
}

// A bridge-routed call must hand the release a fully-formed request and return
// its envelope unchanged — including the error flag, which a client uses to
// decide whether the text block is a result or a diagnosis.
func TestCovBrToolBridgeRoundTripsTheEnvelope(t *testing.T) {
	for _, isError := range []bool{false, true} {
		t.Run(fmt.Sprintf("is_error=%t", isError), func(t *testing.T) {
			bridge, repo := covBrToolBridge(t, covBrEnvelopeStub(isError))
			args := map[string]any{"limit": 5, "name": "pkg::Fn"}

			result, err := bridge.CallTool("get_hub_nodes_tool", args)
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			covBrCheckEnvelope(t, result, isError)
			covBrCheckRequest(t, repo, args)
		})
	}
}

func covBrCheckEnvelope(t *testing.T, result crgrelease.CallResult, wantIsError bool) {
	t.Helper()
	if result.IsError != wantIsError {
		t.Errorf("IsError = %v, want %v", result.IsError, wantIsError)
	}
	want := []crgrelease.ContentBlock{{Type: "text", Text: "envelope text"}}
	if len(result.Content) != 1 || result.Content[0] != want[0] {
		t.Errorf("Content = %#v, want %#v", result.Content, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.StructuredContent, &payload); err != nil {
		t.Fatalf("structured content is not an object: %v", err)
	}
	if payload["status"] != "ok" || payload["count"] != float64(3) {
		t.Errorf("structured content = %v, want the release's payload verbatim", payload)
	}
}

func covBrCheckRequest(t *testing.T, repo string, args map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, "request.json"))
	if err != nil {
		t.Fatalf("the bridge wrote no request in the repository root: %v", err)
	}
	var got covBrRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("request is not JSON: %v (%s)", err, raw)
	}
	if got.Tool != "get_hub_nodes_tool" {
		t.Errorf("request tool = %q, want get_hub_nodes_tool", got.Tool)
	}
	if got.RepoRoot != repo {
		t.Errorf("request repo_root = %q, want %q", got.RepoRoot, repo)
	}
	if fmt.Sprint(got.Arguments["name"]) != fmt.Sprint(args["name"]) ||
		got.Arguments["limit"] != float64(5) {
		t.Errorf("request arguments = %v, want %v", got.Arguments, args)
	}
	env, err := os.ReadFile(filepath.Join(repo, "repo_root.txt"))
	if err != nil {
		t.Fatalf("reading the recorded environment: %v", err)
	}
	if string(env) != repo {
		t.Errorf("CRG_REPO_ROOT = %q, want %q", env, repo)
	}
}

// An argument set that cannot be encoded is rejected before any process is
// started, and the caller is told what was wrong with it.
func TestCovBrToolBridgeRejectsUnencodableArguments(t *testing.T) {
	bridge, repo := covBrToolBridge(t, covBrEnvelopeStub(false))

	_, err := bridge.CallTool("get_hub_nodes_tool", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected an error for an unencodable argument")
	}
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) {
		t.Errorf("error = %v, want a json.UnsupportedTypeError", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "request.json")); !os.IsNotExist(statErr) {
		t.Error("the interpreter must not be started for an unencodable argument set")
	}
}

// A reply that is not a result document is a transport failure, and the
// diagnosis has to carry the interpreter's stderr — that is where the real
// cause (a traceback, a wrong interpreter) is printed.
func TestCovBrToolBridgeReportsAnUnreadableResponse(t *testing.T) {
	bridge, _ := covBrToolBridge(t,
		covBrShellScript("echo 'ModuleNotFoundError: crg' >&2\necho not-json\n"))

	_, err := bridge.CallTool("get_hub_nodes_tool", nil)
	if err == nil {
		t.Fatal("expected an error for a non-JSON reply")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "unreadable bridge response:") ||
		!strings.Contains(got, "ModuleNotFoundError: crg") {
		t.Errorf("error = %q, want the decode failure plus the interpreter's stderr", got)
	}
}

// A result document that reports a fatal error is surfaced as the call's
// error, with the release's own wording and nothing added to it.
func TestCovBrToolBridgeSurfacesAFatalErrorDocument(t *testing.T) {
	bridge, _ := covBrToolBridge(t,
		covBrEmitScript(`{"fatal_error":"code-review-graph import failed: no module"}`))

	_, err := bridge.CallTool("get_hub_nodes_tool", nil)
	if err == nil {
		t.Fatal("expected an error for a fatal_error document")
	}
	if err.Error() != "code-review-graph import failed: no module" {
		t.Errorf("error = %q, want the fatal_error verbatim", err.Error())
	}
}

// An interpreter that dies without printing a document is reported with both
// its exit status and its stderr; guessing at a result would be worse than
// failing.
func TestCovBrToolBridgeReportsInterpreterFailure(t *testing.T) {
	bridge, _ := covBrToolBridge(t,
		covBrShellScript("echo 'segfault imminent' >&2\nexit 4\n"))

	_, err := bridge.CallTool("get_hub_nodes_tool", nil)
	if err == nil {
		t.Fatal("expected an error when the interpreter exits without output")
	}
	got := err.Error()
	if !strings.Contains(got, "exit status 4") || !strings.Contains(got, "segfault imminent") {
		t.Errorf("error = %q, want the exit status and stderr", got)
	}
}

// A non-zero exit that still printed a usable document is NOT a failure: the
// release exits non-zero on some tool errors while emitting a well-formed
// error envelope, and dropping it would turn a diagnosable tool error into an
// opaque transport error.
func TestCovBrToolBridgePrefersAPrintedDocumentOverExitStatus(t *testing.T) {
	bridge, _ := covBrToolBridge(t, covBrShellScript(
		"echo 'noise' >&2\n"+
			`echo '{"is_error":true,"content":[{"type":"text","text":"tool failed"}]}'`+"\n"+
			"exit 1\n"))

	result, err := bridge.CallTool("get_hub_nodes_tool", nil)
	if err != nil {
		t.Fatalf("a printed document must win over the exit status: %v", err)
	}
	if !result.IsError || len(result.Content) != 1 || result.Content[0].Text != "tool failed" {
		t.Errorf("result = %#v, want the printed error envelope", result)
	}
}
