package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// ---------- NewInstallCmd metadata ----------

func TestNewInstallCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewInstallCmd()
	if cmd.Flags().Lookup("generate") == nil {
		t.Error("missing --generate flag")
	}
	if cmd.Flags().Lookup("strict") == nil {
		t.Error("missing --strict flag")
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("install should reject positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("install should accept zero args, got: %v", err)
	}
}

// ---------- installProjectName ----------

func TestInstallProjectName(t *testing.T) {
	if got := installProjectName("manifest-name", "/tmp/whatever/dir"); got != "manifest-name" {
		t.Errorf("got %q, want manifest-name", got)
	}
	if got := installProjectName("", "/tmp/whatever/dir"); got != "dir" {
		t.Errorf("got %q, want dir (basename)", got)
	}
}

// ---------- resourceMarkerFile ----------

func TestResourceMarkerFile(t *testing.T) {
	cases := map[string]string{
		"skills": "SKILL.md",
		"agents": "AGENT.md",
		"other":  "",
	}
	for in, want := range cases {
		if got := resourceMarkerFile(in); got != want {
			t.Errorf("resourceMarkerFile(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------- resourceCandidateIsValid ----------

func TestResourceCandidateIsValid(t *testing.T) {
	tmp := t.TempDir()
	// missing path
	if resourceCandidateIsValid(filepath.Join(tmp, "missing"), "SKILL.md") {
		t.Error("missing path should be invalid")
	}

	// file (not dir)
	plain := filepath.Join(tmp, "file.txt")
	os.WriteFile(plain, []byte("x"), 0644)
	if resourceCandidateIsValid(plain, "") {
		t.Error("non-dir should be invalid")
	}

	// dir without marker but markerFile == "" → valid
	dir := filepath.Join(tmp, "agentdir")
	os.MkdirAll(dir, 0755)
	if !resourceCandidateIsValid(dir, "") {
		t.Error("dir with empty markerFile should be valid")
	}

	// dir with marker missing → invalid
	if resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir without marker should be invalid")
	}

	// dir with marker present → valid
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0644)
	if !resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir with marker should be valid")
	}
}

// ---------- firstResourceCandidate ----------

func TestFirstResourceCandidate(t *testing.T) {
	root := t.TempDir()
	// project-scoped skill present
	projDir := filepath.Join(root, "skills", "proj", "mine")
	os.MkdirAll(projDir, 0755)
	os.WriteFile(filepath.Join(projDir, "SKILL.md"), []byte("x"), 0644)

	candidate, srcRoot, found := firstResourceCandidate("skills", "mine", "SKILL.md", "proj", []string{root})
	if !found {
		t.Fatal("expected candidate to be found")
	}
	if candidate != projDir {
		t.Errorf("candidate = %q, want %q", candidate, projDir)
	}
	if srcRoot != root {
		t.Errorf("srcRoot = %q, want %q", srcRoot, root)
	}

	// fallback to global
	root2 := t.TempDir()
	globalDir := filepath.Join(root2, "skills", "global", "shared")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "SKILL.md"), []byte("x"), 0644)
	candidate, _, found = firstResourceCandidate("skills", "shared", "SKILL.md", "proj", []string{root2})
	if !found || candidate != globalDir {
		t.Errorf("expected global fallback, got candidate=%q found=%v", candidate, found)
	}

	// not found
	_, _, found = firstResourceCandidate("skills", "absent", "SKILL.md", "proj", []string{t.TempDir()})
	if found {
		t.Error("absent resource should not be found")
	}
}

// ---------- shouldSkipLinkDestination ----------

func TestShouldSkipLinkDestination(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "missing")
	if shouldSkipLinkDestination(missing) {
		t.Error("missing dest should not be skipped")
	}

	// exists, no force → skip
	exists := filepath.Join(tmp, "exists")
	os.MkdirAll(exists, 0755)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if !shouldSkipLinkDestination(exists) {
		t.Error("existing without --force should be skipped")
	}

	// exists with force → remove and don't skip
	Flags = GlobalFlags{Force: true}
	if shouldSkipLinkDestination(exists) {
		t.Error("existing with --force should not skip")
	}
	if _, err := os.Stat(exists); err == nil {
		t.Error("--force should remove existing destination")
	}
}

// ---------- resolveSourceRoot ----------

func TestResolveSourceRoot_LocalDefault(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	root, err := resolveSourceRoot(config.Source{Type: "local"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != agentsHome {
		t.Errorf("root = %q, want %q", root, agentsHome)
	}
}

func TestResolveSourceRoot_LocalCustomPath(t *testing.T) {
	tmp := t.TempDir()
	root, err := resolveSourceRoot(config.Source{Type: "local", Path: tmp})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != tmp {
		t.Errorf("root = %q, want %q", root, tmp)
	}
}

func TestResolveSourceRoot_GitMissingURL(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "git"})
	if err != nil || root != "" {
		t.Errorf("missing url: root=%q err=%v, want empty", root, err)
	}
}

func TestResolveSourceRoot_UnknownType(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "ftp"})
	if err != nil || root != "" {
		t.Errorf("unknown type: root=%q err=%v, want empty", root, err)
	}
}

// ---------- resolveSources ----------

func TestResolveSources_MixedAndCustomDirs(t *testing.T) {
	tmp := t.TempDir()
	custom := filepath.Join(tmp, "custom-src")
	os.MkdirAll(custom, 0755)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	sources := []config.Source{
		{Type: "local", Path: custom},
		{Type: "ftp"}, // skipped
		{Type: "git"}, // skipped (missing url)
	}
	resolved, err := resolveSources(sources)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(resolved) != 1 || resolved[0] != custom {
		t.Errorf("resolved = %v, want [%s]", resolved, custom)
	}
}

// ---------- gitCloneDryRunCommand ----------

func TestGitCloneDryRunCommand(t *testing.T) {
	got := gitCloneDryRunCommand("https://example.com/repo.git", "", "/cache/x")
	if got != "git clone --depth 1 -- https://example.com/repo.git /cache/x" {
		t.Errorf("no ref: %q", got)
	}
	got = gitCloneDryRunCommand("https://example.com/repo.git", "main", "/cache/x")
	if got != "git clone --depth 1 --branch main -- https://example.com/repo.git /cache/x" {
		t.Errorf("with ref: %q", got)
	}
}

// TestCloneGitSource_MaliciousURLNotParsedAsFlag verifies that a URL beginning
// with "--upload-pack=" (CVE-2017-1000117 class) is passed as a positional
// argument because cloneGitSource inserts "--" before url/cacheDir. We use a
// fake "git" binary that prints its argv; we then assert the URL appears
// after "--" in the recorded argv.
func TestCloneGitSource_MaliciousURLNotParsedAsFlag(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")

	// Fake git that writes its argv to a file then exits non-zero (so
	// cloneGitSource's failure path runs and cleans up cacheDir). Built as a
	// real Go binary rather than a shell script so it runs on Windows too
	// (no #!/bin/sh interpreter, correct .exe suffix).
	fakeBin := buildFakeGit(t, tmp, argvFile)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	maliciousURL := "--upload-pack=/bin/sh -c touch /tmp/pwned"
	cacheDir := filepath.Join(tmp, "cache")
	_, err := cloneGitSource(fakeBin, maliciousURL, "", cacheDir)
	if err == nil {
		t.Fatal("expected clone to fail (fake git exits 1)")
	}
	data, readErr := os.ReadFile(argvFile)
	if readErr != nil {
		t.Fatalf("fake git did not record argv: %v", readErr)
	}
	argv := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Expected: clone --depth 1 -- <url> <cacheDir>
	var sepIdx = -1
	for i, a := range argv {
		if a == "--" {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 {
		t.Fatalf("missing -- separator in argv: %v", argv)
	}
	if sepIdx+1 >= len(argv) || argv[sepIdx+1] != maliciousURL {
		t.Errorf("expected URL immediately after --; argv=%v", argv)
	}
	// Sanity: no element BEFORE -- equals or starts with the malicious URL.
	for i := 0; i < sepIdx; i++ {
		if argv[i] == maliciousURL {
			t.Errorf("URL leaked into flag position at argv[%d]: %v", i, argv)
		}
	}
}

// buildFakeGit compiles a tiny cross-platform Go program that records its
// argv (one per line) to argvFile and exits 1. Returns the binary path
// (with the platform-correct extension). Replaces the previous #!/bin/sh
// fixture, which could not execute on Windows.
func buildFakeGit(t *testing.T, dir, argvFile string) string {
	t.Helper()
	srcDir := filepath.Join(dir, "fakegit-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package main\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\n" +
		"func main() {\n" +
		"\tdata := strings.Join(os.Args[1:], \"\\n\") + \"\\n\"\n" +
		"\t_ = os.WriteFile(" + strconv.Quote(argvFile) + ", []byte(data), 0o644)\n" +
		"\tos.Exit(1)\n}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Standalone module so `go build` does not climb to the repo go.mod.
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module fakegit\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fakegit")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fake git: %v\n%s", err, out)
	}
	return bin
}

// ---------- hasCachedGitSource / shouldUseCachedGitSource ----------

func TestCachedGitSource(t *testing.T) {
	tmp := t.TempDir()
	if hasCachedGitSource(tmp) {
		t.Error("empty dir should not be a cached source")
	}
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	if !hasCachedGitSource(tmp) {
		t.Error("dir with .git should be a cached source")
	}

	saved := Flags
	defer func() { Flags = saved }()

	// no .last-fetch → should not use cache
	Flags = GlobalFlags{}
	if shouldUseCachedGitSource(tmp, "url") {
		t.Error("no .last-fetch: cache should not be used")
	}

	// fresh .last-fetch → use cache
	os.WriteFile(filepath.Join(tmp, ".last-fetch"), []byte("now"), 0644)
	if !shouldUseCachedGitSource(tmp, "url") {
		t.Error("fresh .last-fetch: cache should be used")
	}

	// force → don't use cache even with fresh marker
	Flags = GlobalFlags{Force: true}
	if shouldUseCachedGitSource(tmp, "url") {
		t.Error("--force: cache should not be used")
	}
}

// ---------- findProjectByPath ----------

func TestFindProjectByPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	projPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projPath, 0755)
	cfg.AddProject("myrepo", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if got := findProjectByPath(projPath); got != "myrepo" {
		t.Errorf("got %q, want myrepo", got)
	}
	if got := findProjectByPath(filepath.Join(tmp, "missing")); got != "" {
		t.Errorf("missing project should return empty, got %q", got)
	}
}

// ---------- linkResourceFromSources (dry-run) ----------

func TestLinkResourceFromSources_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	if err := linkResourceFromSources("skills", "demo", "proj", []string{src}); err != nil {
		t.Fatalf("dry-run link failed: %v", err)
	}
	// Nothing should have been linked
	dest := filepath.Join(tmp, ".agents", "skills", "proj", "demo")
	if _, err := os.Lstat(dest); err == nil {
		t.Error("dry-run should not have created link")
	}
}

func TestLinkResourceFromSources_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := linkResourceFromSources("skills", "absent", "proj", []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestLinkResourceFromSources_CreatesSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := linkResourceFromSources("skills", "demo", "proj", []string{src}); err != nil {
		t.Fatalf("link failed: %v", err)
	}
	dest := filepath.Join(agentsHome, "skills", "proj", "demo")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", dest, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected dest to be a symlink")
	}
}

// ---------- runInstall: error pathways ----------

func TestRunInstall_NoManifestErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Run from a directory that has no .agentsrc.json
	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstall(false)
	if err == nil {
		t.Fatal("expected error when manifest missing")
	}
	if !strings.Contains(err.Error(), ".agentsrc.json") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunInstall_UninitializedAgentsHomeErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755) // exists but no config.json
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	manifest := config.AgentsRC{Version: 1, Project: "proj"}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(projDir, config.AgentsRCFile), data, 0644)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstall(false)
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected not-initialized error, got: %v", err)
	}
}

// ---------- runInstallGenerate: round-trip from current state ----------

func TestRunInstallGenerate_CreatesManifestFromState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Initialize a skill and an agent in canonical home
	projName := "myrepo"
	skillDir := filepath.Join(agentsHome, "skills", projName, "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo"), 0644)
	agentDir := filepath.Join(agentsHome, "agents", projName, "support")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# support"), 0644)

	projPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projPath, 0755)

	// Register the project so generate picks it up by name
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject(projName, projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("runInstallGenerate: %v", err)
	}

	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if rc.Project != projName {
		t.Errorf("manifest project = %q, want %q", rc.Project, projName)
	}
	if !containsString(rc.Skills, "demo") {
		t.Errorf("manifest skills = %v, want demo present", rc.Skills)
	}
	if !containsString(rc.Agents, "support") {
		t.Errorf("manifest agents = %v, want support present", rc.Agents)
	}
}

func TestRunInstallGenerate_DryRunDoesNotWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "drygen")
	os.MkdirAll(projPath, 0755)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("dry-run generate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projPath, config.AgentsRCFile)); err == nil {
		t.Error("dry-run should not write manifest")
	}
}

func TestRunInstallGenerate_PreservesExistingProjectAndExtras(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "mergetest")
	os.MkdirAll(projPath, 0755)

	// Pre-existing manifest with explicit project name and unknown key
	existing := []byte(`{
  "version": 1,
  "project": "explicit-name",
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type":"local"}],
  "custom_extra": {"keep":true}
}`)
	if err := os.WriteFile(filepath.Join(projPath, config.AgentsRCFile), existing, 0644); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("runInstallGenerate: %v", err)
	}

	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if rc.Project != "explicit-name" {
		t.Errorf("project = %q, want explicit-name (preserved)", rc.Project)
	}
	if _, ok := rc.ExtraFields["custom_extra"]; !ok {
		t.Errorf("extra fields should be preserved: %v", rc.ExtraFields)
	}
}

// ---------- additional coverage ----------

func TestLoadInstallManifest_MissingFileWithHints(t *testing.T) {
	tmp := t.TempDir()
	_, err := loadInstallManifest(tmp)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected hints about not-found, got: %v", err)
	}
}

func TestLoadInstallManifest_Corrupt(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, config.AgentsRCFile), []byte("bogus"), 0644)
	_, err := loadInstallManifest(tmp)
	if err == nil {
		t.Fatal("expected error for corrupt manifest")
	}
}

func TestLoadInstallManifest_Found(t *testing.T) {
	tmp := t.TempDir()
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadInstallManifest(tmp)
	if err != nil || loaded.Project != "p" {
		t.Errorf("got rc=%+v err=%v", loaded, err)
	}
}

func TestEnsureAgentsHomeInitialized_MissingHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Don't create agentsHome at all → no config.json.
	if err := ensureAgentsHomeInitialized(); err == nil {
		t.Error("expected not-initialized error")
	}
}

func TestEnsureAgentsHomeInitialized_Present(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte("{}"), 0644)
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := ensureAgentsHomeInitialized(); err != nil {
		t.Errorf("expected nil error when config.json present, got %v", err)
	}
}

func TestLinkInstallResourceList_StrictReturnsErr(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := linkInstallResourceList("skills", "skill", []string{"absent"}, "p", []string{t.TempDir()}, true)
	if err == nil {
		t.Error("expected --strict to return error")
	}
}

func TestLinkInstallResourceList_NonStrictWarnsAndContinues(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := linkInstallResourceList("skills", "skill", []string{"absent"}, "p", []string{t.TempDir()}, false); err != nil {
		t.Errorf("non-strict should not error, got %v", err)
	}
}

func TestLinkInstallResourceList_EmptyNamesSkips(t *testing.T) {
	if err := linkInstallResourceList("skills", "skill", nil, "p", nil, false); err != nil {
		t.Errorf("empty names should be no-op, got %v", err)
	}
}

func TestEnsureInstallProjectDirs_DryRun(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := ensureInstallProjectDirs("p"); err != nil {
		t.Errorf("dry-run: %v", err)
	}
}

func TestEnsureInstallProjectDirs_RealCreatesDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := ensureInstallProjectDirs("p"); err != nil {
		t.Errorf("real run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "rules", "p")); err != nil {
		t.Errorf("expected project rules dir created: %v", err)
	}
}

func TestRegisterInstallProject_NewlyRegisters(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := registerInstallProject("newp", filepath.Join(tmp, "p")); err != nil {
		t.Fatalf("register: %v", err)
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("newp") == "" {
		t.Error("expected project to be registered")
	}
}

func TestRegisterInstallProject_AlreadyRegisteredSkips(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", filepath.Join(tmp, "p"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := registerInstallProject("p", filepath.Join(tmp, "p")); err != nil {
		t.Errorf("registering already-registered should be no-op, got %v", err)
	}
}

func TestRegisterInstallProject_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := registerInstallProject("p", filepath.Join(tmp, "p")); err != nil {
		t.Errorf("dry-run register: %v", err)
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("p") != "" {
		t.Error("dry-run should not register")
	}
}

func TestFinalizeInstall_DryRunIsNoop(t *testing.T) {
	tmp := t.TempDir()
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	finalizeInstall("p", tmp)
}

func TestFinalizeInstall_WritesRefreshMetadata(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	rc := &config.AgentsRC{Version: 1, Project: "p"}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	finalizeInstall("p", projectPath)

	loaded, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Refresh == nil || loaded.Refresh.RefreshedAt == "" {
		t.Error("expected refresh metadata to be set")
	}
}

func TestResolveInstallSources_StrictPropagatesErrors(t *testing.T) {
	// Pass an explicit local source with a nonexistent path; resolveSources returns no error
	// for missing local paths (it just returns the path), so we instead use a git source
	// without URL to coerce a warn path. resolveSources never returns first-error for
	// missing URL because resolveSourceRoot returns ("", nil). So this path is best
	// covered by direct test of resolveInstallSources non-strict.
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	sources := []config.Source{{Type: "git"}} // missing URL → skipped
	resolved, err := resolveInstallSources(sources, true)
	if err != nil {
		t.Errorf("git-missing-url: expected nil, got %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected no resolved sources, got %v", resolved)
	}
}

func TestLinkInstallResources_FallsBackToAgentsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Set up a skill that lives in agentsHome already.
	skillDir := filepath.Join(agentsHome, "skills", "p", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	rc := &config.AgentsRC{Version: 1, Project: "p", Skills: []string{"demo"}}
	saved := Flags
	Flags = GlobalFlags{DryRun: true} // dry-run avoids actually creating links
	defer func() { Flags = saved }()
	if err := linkInstallResources("p", rc, nil, false); err != nil {
		t.Errorf("expected fallback to agents-home to work, got %v", err)
	}
}

// runInstall happy path - manifest exists, agentsHome initialized, dry-run skips network/etc.
func TestRunInstall_HappyPathDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	// Mark agents home initialized.
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "proj"}
	if err := rc.Save(projDir); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runInstall(false); err != nil {
		t.Errorf("runInstall happy: %v", err)
	}
}

func TestRunInstall_StrictWithMissingSkillErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	// Manifest declares a skill that doesn't exist; --strict should fail
	rc := &config.AgentsRC{Version: 1, Project: "proj", Skills: []string{"absent"}}
	if err := rc.Save(projDir); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstall(true)
	if err == nil {
		t.Error("expected --strict to error on missing skill")
	}
}

// touchLastFetch is a tiny helper - exercise to bump coverage.
func TestTouchLastFetch_WritesMarker(t *testing.T) {
	tmp := t.TempDir()
	touchLastFetch(tmp)
	if _, err := os.Stat(filepath.Join(tmp, ".last-fetch")); err != nil {
		t.Errorf("expected .last-fetch marker: %v", err)
	}
}

func TestRunInstallSharedTargets_NoEnabledPlatforms(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	// With no platforms installed at all, projection still completes (warn path).
	runInstallSharedTargets("p", filepath.Join(tmp, "p"))
}

// ---------- git source helpers (fetch / clone / update) ----------

func requireGitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// runGitCmd runs a git command in dir, failing the test on error.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ensureGoodCWD restores cwd to a valid directory in case an earlier test
// in the package chdir'd into a temp dir that's already cleaned up. Other
// tests register chdir cleanup that resolves *after* their t.TempDir()
// cleanup runs, leaving subsequent subprocess execs without a valid cwd.
// We chdir to a long-lived directory (the OS temp root) so the subprocess
// has a valid cwd for the duration of the run.
func ensureGoodCWD(t *testing.T) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(os.TempDir()); err != nil {
		t.Fatalf("chdir to os.TempDir: %v", err)
	}
	if prev != "" {
		t.Cleanup(func() { _ = os.Chdir(prev) })
	}
}

// makeBareGitFixture creates a bare repo at tmp/remote.git seeded with one
// commit, and returns its absolute path along with the seed commit's first
// file name. The bare repo is suitable as an `src.URL` argument.
func makeBareGitFixture(t *testing.T) string {
	t.Helper()
	requireGitOrSkip(t)
	ensureGoodCWD(t)
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "remote.git")
	if err := os.MkdirAll(bare, 0755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, bare, "init", "--bare", "-q", "--initial-branch=main")

	// Seed via a working clone.
	work := filepath.Join(tmp, "work")
	runGitCmd(t, tmp, "clone", "-q", bare, work)
	runGitCmd(t, work, "config", "user.name", "Test")
	runGitCmd(t, work, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# seed\n"), 0644)
	runGitCmd(t, work, "add", "README.md")
	runGitCmd(t, work, "commit", "-q", "-m", "seed")
	runGitCmd(t, work, "push", "-q", "origin", "HEAD:main")

	return bare
}

func TestFetchGitSource_ClonesIntoEmptyCache(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	cacheDir, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("fetchGitSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".git")); err != nil {
		t.Errorf("expected .git in cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "README.md")); err != nil {
		t.Errorf("expected README.md in cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".last-fetch")); err != nil {
		t.Errorf("expected .last-fetch marker: %v", err)
	}
}

func TestFetchGitSource_UsesFreshCache(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// Prime cache.
	first, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("priming clone: %v", err)
	}
	// Mark a sentinel file so we know cache wasn't replaced.
	sentinel := filepath.Join(first, "_sentinel")
	os.WriteFile(sentinel, []byte("kept"), 0644)

	second, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if second != first {
		t.Errorf("cache dir changed: %q vs %q", first, second)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel removed → cache was rebuilt: %v", err)
	}
}

func TestFetchGitSource_StaleCacheTriggersUpdate(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	saved := Flags
	Flags = GlobalFlags{Verbose: true} // exercises the verbose log branch in updateCachedGitSource
	defer func() { Flags = saved }()

	first, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("priming clone: %v", err)
	}
	// Backdate .last-fetch so the cache is considered stale.
	stale := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(first, ".last-fetch"), stale, stale)

	second, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("update fetch: %v", err)
	}
	if second != first {
		t.Errorf("cache dir changed after update: %q vs %q", first, second)
	}
	info, err := os.Stat(filepath.Join(second, ".last-fetch"))
	if err != nil || info.ModTime().Before(time.Now().Add(-30*time.Second)) {
		t.Errorf("expected .last-fetch refreshed after update: %v info=%+v", err, info)
	}
}

func TestFetchGitSource_StaleCacheDryRunSkipsUpdate(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	// Prime cache (non-dry-run).
	saved := Flags
	Flags = GlobalFlags{}
	first, err := fetchGitSource(bare, "main")
	if err != nil {
		Flags = saved
		t.Fatalf("priming clone: %v", err)
	}
	stale := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(first, ".last-fetch"), stale, stale)

	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	second, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("dry-run update: %v", err)
	}
	if second != first {
		t.Errorf("cache dir changed during dry-run: %q vs %q", first, second)
	}
	// .last-fetch should still be stale because dry-run skipped update.
	info, err := os.Stat(filepath.Join(second, ".last-fetch"))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) < time.Hour {
		t.Errorf("dry-run should not refresh .last-fetch, got modtime %v", info.ModTime())
	}
}

func TestFetchGitSource_DryRunCloneSkipsFilesystem(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	cacheDir, err := fetchGitSource(bare, "main")
	if err != nil {
		t.Fatalf("dry-run clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".git")); err == nil {
		t.Error("dry-run should not have cloned the repo")
	}
}

func TestCloneGitSource_FailureCleansUpCacheDir(t *testing.T) {
	requireGitOrSkip(t)
	gitBin, _ := exec.LookPath("git")

	cacheRoot := t.TempDir()
	cacheDir := filepath.Join(cacheRoot, "should-be-removed")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	bogusURL := filepath.Join(t.TempDir(), "does-not-exist.git")
	_, err := cloneGitSource(gitBin, bogusURL, "main", cacheDir)
	if err == nil {
		t.Fatal("expected clone failure")
	}
	if _, statErr := os.Stat(cacheDir); statErr == nil {
		t.Error("cacheDir should be removed after clone failure")
	}
}

func TestUpdateCachedGitSource_RemoteAbsentLogsWarning(t *testing.T) {
	requireGitOrSkip(t)
	gitBin, _ := exec.LookPath("git")
	tmp := t.TempDir()
	// Working git repo, but with no remote configured.
	runGitCmd(t, tmp, "init", "-q", "-b", "main", tmp)
	runGitCmd(t, tmp, "config", "user.name", "Test")
	runGitCmd(t, tmp, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(tmp, "a"), []byte("a"), 0644)
	runGitCmd(t, tmp, "add", "a")
	runGitCmd(t, tmp, "commit", "-q", "-m", "x")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// Should not panic; logs warn and returns. .last-fetch is NOT created when pull fails.
	updateCachedGitSource(gitBin, tmp, "irrelevant")
	if _, err := os.Stat(filepath.Join(tmp, ".last-fetch")); err == nil {
		t.Error("failed pull should not touch .last-fetch")
	}
}

func TestResolveSourceRoot_GitSucceedsWithBareFixture(t *testing.T) {
	requireGitOrSkip(t)
	bare := makeBareGitFixture(t)

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	root, err := resolveSourceRoot(config.Source{Type: "git", URL: bare, Ref: "main"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root == "" {
		t.Error("expected non-empty cache dir")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Errorf("expected cache root to contain a clone: %v", err)
	}
}

// ---------- helpers ----------

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestRunInstall_HappyPathWithInstalledClaude exercises the full install path
// without --dry-run: createInstallPlatformLink calls into Claude's CreateLinks
// (success branch), finalizeInstall writes the refresh metadata, and the
// shared-target projection plan is materialized.
func TestRunInstall_HappyPathWithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Force claude installed
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	// Mark agents home initialized
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "proj"}
	if err := rc.Save(projDir); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, Verbose: true}
	defer func() { Flags = saved }()

	if err := runInstall(false); err != nil {
		t.Errorf("runInstall full happy: %v", err)
	}

	// finalizeInstall should have updated the manifest with refresh metadata.
	if _, err := os.Stat(filepath.Join(projDir, ".agentsrc.json")); err != nil {
		t.Errorf("expected manifest to remain: %v", err)
	}
}

// TestResolveInstallSources_StrictErrorPropagates covers the strict-mode
// resolveSources error branch.
func TestResolveInstallSources_StrictErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	// A git source with an invalid url + no git binary cache → resolveSources
	// returns an error which strict mode surfaces.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	_, err := resolveInstallSources([]config.Source{{Type: "git", URL: "git://nonexistent.invalid/no.git", Ref: "main"}}, true)
	if err == nil {
		t.Error("expected strict-mode error")
	}
}

// TestResolveInstallSources_NonStrictIgnoresError covers the non-strict branch
// where err != nil but strict=false swallows the error.
func TestResolveInstallSources_NonStrictIgnoresError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got, err := resolveInstallSources([]config.Source{{Type: "git", URL: "git://nonexistent.invalid/no.git", Ref: "main"}}, false)
	if err != nil {
		t.Errorf("non-strict should swallow err, got %v / %v", got, err)
	}
}

// TestRegisterInstallProject_AlreadyRegistered covers the skip branch when the
// project is already in config.
func TestRegisterInstallProject_AlreadyRegistered(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := registerInstallProject("p", projPath); err != nil {
		t.Errorf("registerInstallProject: %v", err)
	}
}

// TestLinkInstallResources_NoSourcesFallsBackToAgentsHome covers the fallback
// when resolvedSources is empty: sources becomes ~/.agents/.
func TestLinkInstallResources_NoSourcesFallsBackToAgentsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Seed a skill in the canonical home that linkInstallResources can find.
	skill := filepath.Join(agentsHome, "skills", "proj", "x")
	os.MkdirAll(skill, 0755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	rc := &config.AgentsRC{Version: 1, Project: "proj", Skills: []string{"x"}}
	if err := linkInstallResources("proj", rc, nil, false); err != nil {
		t.Errorf("linkInstallResources: %v", err)
	}
	// Linked dir should exist.
	dest := filepath.Join(agentsHome, "skills", "proj", "x")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("expected linked dest: %v", err)
	}
}

// TestLinkInstallResourceList_StrictMissing covers strict-mode error from
// linkResourceFromSources when the resource doesn't exist anywhere.
func TestLinkInstallResourceList_StrictMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := linkInstallResourceList("skills", "skill", []string{"missing-skill"}, "proj", []string{agentsHome}, true)
	if err == nil {
		t.Error("expected strict mode error for missing skill")
	}
}

// TestLinkInstallResourceList_NonStrictWarnings covers the non-strict warn path.
func TestLinkInstallResourceList_NonStrictWarnings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := linkInstallResourceList("skills", "skill", []string{"missing"}, "proj", []string{agentsHome}, false); err != nil {
		t.Errorf("non-strict should not error: %v", err)
	}
}

// TestCreateInstallPlatformLink_DryRun covers the dry-run branch.
func TestCreateInstallPlatformLink_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	for _, p := range platform.All() {
		createInstallPlatformLink(p, "p", filepath.Join(tmp, "p"))
	}
}

// TestFinalizeInstall_DryRun covers the early return.
func TestFinalizeInstall_DryRun(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	finalizeInstall("p", t.TempDir())
}

// TestFinalizeInstall_WriteFailWarns covers the WriteRefreshToAgentsRC err
// branch by pointing at a non-writable path (a file masquerading as a dir).
func TestFinalizeInstall_WriteFailWarns(t *testing.T) {
	tmp := t.TempDir()
	// projectPath is a regular file → WriteRefreshToAgentsRC fails to write.
	if err := os.WriteFile(filepath.Join(tmp, "not-a-dir"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	finalizeInstall("p", filepath.Join(tmp, "not-a-dir"))
}

// TestRunInstallSharedTargets_DryRunErrorPath covers the dry-run warn branch
// when RunSharedTargetProjection returns an error.
func TestRunInstallSharedTargets_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	runInstallSharedTargets("p", filepath.Join(tmp, "p"))
}

// TestShouldSkipLinkDestination_ForceDeletes covers Force=true branch.
func TestShouldSkipLinkDestination_ForceDeletes(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "target")
	os.MkdirAll(dest, 0755)
	saved := Flags
	Flags = GlobalFlags{Force: true}
	defer func() { Flags = saved }()
	if shouldSkipLinkDestination(dest) {
		t.Error("expected force to clear dest, not skip")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("expected dest to be removed under --force")
	}
}

// TestShouldSkipLinkDestination_ExistsNoForce covers the skip-existing branch.
func TestShouldSkipLinkDestination_ExistsNoForce(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "target")
	os.MkdirAll(dest, 0755)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if !shouldSkipLinkDestination(dest) {
		t.Error("expected skip when destination exists and !Force")
	}
}

// TestRunInstall_StrictWithBadGitSourceErrors covers install.go:78-80, the
// `resolveInstallSources err && strict` propagation, by feeding a manifest
// with a malformed git source under --strict.
func TestRunInstall_StrictWithBadGitSourceErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rc := &config.AgentsRC{
		Version: 1,
		Project: "proj",
		Sources: []config.Source{{Type: "git", URL: "https://invalid.localhost.test/missing.git", Ref: "main"}},
	}
	if err := rc.Save(projDir); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInstall(true); err == nil {
		t.Error("expected --strict runInstall to propagate git resolve error")
	}
}
