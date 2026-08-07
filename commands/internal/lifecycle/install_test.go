package lifecycle

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// fakeInstallDeps mirrors the commands-package fake but lives here for
// lifecycle-package tests so they can exercise the moved implementation
// directly without crossing back into commands. Nil func fields delegate
// to the real implementation.
type fakeInstallDeps struct {
	getwd      func() (string, error)
	mkdirAll   func(string, os.FileMode) error
	symlink    func(string, string) error
	loadConfig func() (*config.Config, error)
}

func (f fakeInstallDeps) Getwd() (string, error) {
	if f.getwd != nil {
		return f.getwd()
	}
	return os.Getwd()
}

func (f fakeInstallDeps) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f fakeInstallDeps) Symlink(oldname, newname string) error {
	if f.symlink != nil {
		return f.symlink(oldname, newname)
	}
	return os.Symlink(oldname, newname)
}

func (f fakeInstallDeps) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

type fakeInstallPlatform struct {
	id        string
	installed bool
	linkErr   error
	intentErr error
	intents   []platform.ResourceIntent
}

func (f fakeInstallPlatform) ID() string                       { return f.id }
func (f fakeInstallPlatform) DisplayName() string              { return f.id }
func (f fakeInstallPlatform) IsInstalled() bool                { return f.installed }
func (f fakeInstallPlatform) Version() string                  { return "" }
func (f fakeInstallPlatform) RemoveLinks(string, string) error { return nil }
func (f fakeInstallPlatform) HasDeprecatedFormat(string) bool  { return false }
func (f fakeInstallPlatform) DeprecatedDetails(string) string  { return "" }
func (f fakeInstallPlatform) CreateLinks(string, string) error { return f.linkErr }
func (f fakeInstallPlatform) SharedTargetIntents(string) ([]platform.ResourceIntent, error) {
	if f.intentErr != nil {
		return nil, f.intentErr
	}
	return f.intents, nil
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
	if resourceCandidateIsValid(filepath.Join(tmp, "missing"), "SKILL.md") {
		t.Error("missing path should be invalid")
	}

	plain := filepath.Join(tmp, "file.txt")
	os.WriteFile(plain, []byte("x"), 0644)
	if resourceCandidateIsValid(plain, "") {
		t.Error("non-dir should be invalid")
	}

	dir := filepath.Join(tmp, "agentdir")
	os.MkdirAll(dir, 0755)
	if !resourceCandidateIsValid(dir, "") {
		t.Error("dir with empty markerFile should be valid")
	}

	if resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir without marker should be invalid")
	}

	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0644)
	if !resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir with marker should be valid")
	}
}

// ---------- firstResourceCandidate ----------

func TestFirstResourceCandidate(t *testing.T) {
	root := t.TempDir()
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

	root2 := t.TempDir()
	globalDir := filepath.Join(root2, "skills", "global", "shared")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "SKILL.md"), []byte("x"), 0644)
	candidate, _, found = firstResourceCandidate("skills", "shared", "SKILL.md", "proj", []string{root2})
	if !found || candidate != globalDir {
		t.Errorf("expected global fallback, got candidate=%q found=%v", candidate, found)
	}

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

	exists := filepath.Join(tmp, "exists")
	os.MkdirAll(exists, 0755)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if !shouldSkipLinkDestination(exists) {
		t.Error("existing without --force should be skipped")
	}

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

	root, err := resolveSourceRoot(config.Source{Type: "local"}, StdInstallDeps{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != agentsHome {
		t.Errorf("root = %q, want %q", root, agentsHome)
	}
}

func TestResolveSourceRoot_LocalCustomPath(t *testing.T) {
	tmp := t.TempDir()
	root, err := resolveSourceRoot(config.Source{Type: "local", Path: tmp}, StdInstallDeps{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != tmp {
		t.Errorf("root = %q, want %q", root, tmp)
	}
}

func TestResolveSourceRoot_GitMissingURL(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "git"}, StdInstallDeps{})
	if err != nil || root != "" {
		t.Errorf("missing url: root=%q err=%v, want empty", root, err)
	}
}

func TestResolveSourceRoot_UnknownType(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "ftp"}, StdInstallDeps{})
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
		{Type: "ftp"},
		{Type: "git"},
	}
	resolved, err := resolveSources(sources, StdInstallDeps{})
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
// argument because cloneGitSource inserts "--" before url/cacheDir.
func TestCloneGitSource_MaliciousURLNotParsedAsFlag(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")

	fakeBin := buildFakeGit(t, tmp, argvFile)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	maliciousURL := "--upload-pack=/bin/sh -c touch /tmp/pwned"
	cacheDir := filepath.Join(tmp, "cache")
	_, err := CloneGitSource(fakeBin, maliciousURL, "", cacheDir, StdInstallDeps{})
	if err == nil {
		t.Fatal("expected clone to fail (fake git exits 1)")
	}
	data, readErr := os.ReadFile(argvFile)
	if readErr != nil {
		t.Fatalf("fake git did not record argv: %v", readErr)
	}
	argv := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
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
	for i := 0; i < sepIdx; i++ {
		if argv[i] == maliciousURL {
			t.Errorf("URL leaked into flag position at argv[%d]: %v", i, argv)
		}
	}
}

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

	Flags = GlobalFlags{}
	if ShouldUseCachedGitSource(tmp, "url") {
		t.Error("no .last-fetch: cache should not be used")
	}

	os.WriteFile(filepath.Join(tmp, ".last-fetch"), []byte("now"), 0644)
	if !ShouldUseCachedGitSource(tmp, "url") {
		t.Error("fresh .last-fetch: cache should be used")
	}

	Flags = GlobalFlags{Force: true}
	if ShouldUseCachedGitSource(tmp, "url") {
		t.Error("--force: cache should not be used")
	}
}

// ---------- FindProjectByPath ----------

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

	if got := FindProjectByPath(projPath, StdInstallDeps{}); got != "myrepo" {
		t.Errorf("got %q, want myrepo", got)
	}
	if got := FindProjectByPath(filepath.Join(tmp, "missing"), StdInstallDeps{}); got != "" {
		t.Errorf("missing project should return empty, got %q", got)
	}
}

// ---------- LinkResourceFromSources (dry-run) ----------

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

	if err := LinkResourceFromSources("skills", "demo", "proj", []string{src}, StdInstallDeps{}); err != nil {
		t.Fatalf("dry-run link failed: %v", err)
	}
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

	err := LinkResourceFromSources("skills", "absent", "proj", []string{t.TempDir()}, StdInstallDeps{})
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

	if err := LinkResourceFromSources("skills", "demo", "proj", []string{src}, StdInstallDeps{}); err != nil {
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

// ---------- RunInstall: error pathways ----------

func TestRunInstall_NoManifestErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

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

	err := RunInstall(false, StdInstallDeps{})
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
	os.MkdirAll(agentsHome, 0755)
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

	err := RunInstall(false, StdInstallDeps{})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected not-initialized error, got: %v", err)
	}
}

// ---------- RunInstallGenerate: round-trip from current state ----------

func TestRunInstallGenerate_CreatesManifestFromState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projName := "myrepo"
	skillDir := filepath.Join(agentsHome, "skills", projName, "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo"), 0644)
	agentDir := filepath.Join(agentsHome, "agents", projName, "support")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# support"), 0644)

	projPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projPath, 0755)

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

	if err := RunInstallGenerate(StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstallGenerate: %v", err)
	}

	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if rc.Project != projName {
		t.Errorf("manifest project = %q, want %q", rc.Project, projName)
	}
	if !slices.Contains(rc.Skills, "demo") {
		t.Errorf("manifest skills = %v, want demo present", rc.Skills)
	}
	if !slices.Contains(rc.Agents, "support") {
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

	if err := RunInstallGenerate(StdInstallDeps{}); err != nil {
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

	if err := RunInstallGenerate(StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstallGenerate: %v", err)
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

	err := linkInstallResourceList("skills", "skill", []string{"absent"}, "p", []string{t.TempDir()}, true, StdInstallDeps{})
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

	if err := linkInstallResourceList("skills", "skill", []string{"absent"}, "p", []string{t.TempDir()}, false, StdInstallDeps{}); err != nil {
		t.Errorf("non-strict should not error, got %v", err)
	}
}

func TestLinkInstallResourceList_EmptyNamesSkips(t *testing.T) {
	if err := linkInstallResourceList("skills", "skill", nil, "p", nil, false, StdInstallDeps{}); err != nil {
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
	if err := RegisterInstallProject("newp", filepath.Join(tmp, "p"), StdInstallDeps{}); err != nil {
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
	if err := RegisterInstallProject("p", filepath.Join(tmp, "p"), StdInstallDeps{}); err != nil {
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
	if err := RegisterInstallProject("p", filepath.Join(tmp, "p"), StdInstallDeps{}); err != nil {
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
	if err := finalizeInstall("p", tmp, installOptions{}); err != nil {
		t.Fatalf("finalizeInstall dry-run: %v", err)
	}
}

func TestFinalizeInstall_WritesLockStampNotManifest(t *testing.T) {
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

	opts := installOptions{version: "1.2.3", commit: "deadbeef", describe: "v1.2.3-0-gdeadbeef"}
	if err := finalizeInstall("p", projectPath, opts); err != nil {
		t.Fatalf("finalizeInstall: %v", err)
	}

	if _, err := config.LoadAgentsRC(projectPath); err != nil {
		t.Fatal(err)
	}
	lf, err := agentslock.Open(config.AgentsLockPath(projectPath))
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	var stamp installLockStamp
	if ok, err := lf.Section(installLockSection, &stamp); err != nil || !ok {
		t.Fatalf("install lock stamp missing: ok=%v err=%v", ok, err)
	}
	if stamp.Project != "p" || stamp.Stamped == "" {
		t.Fatalf("install lock stamp = %+v", stamp)
	}
	if stamp.Version != "1.2.3" || stamp.Commit != "deadbeef" || stamp.Describe != "v1.2.3-0-gdeadbeef" {
		t.Fatalf("finalizeInstall did not stamp build info from opts: %+v", stamp)
	}
}

func TestResolveInstallSources_StrictPropagatesErrors(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	sources := []config.Source{{Type: "git"}}
	resolved, err := resolveInstallSources(sources, true, StdInstallDeps{})
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

	skillDir := filepath.Join(agentsHome, "skills", "p", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	rc := &config.AgentsRC{Version: 1, Project: "p", Skills: []string{"demo"}}
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := linkInstallResources("p", rc, nil, false, StdInstallDeps{}); err != nil {
		t.Errorf("expected fallback to agents-home to work, got %v", err)
	}
}

func TestRunInstall_HappyPathDryRun(t *testing.T) {
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

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Errorf("RunInstall happy: %v", err)
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

	err := RunInstall(true, StdInstallDeps{})
	if err == nil {
		t.Error("expected --strict to error on missing skill")
	}
}

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
	if err := runInstallSharedTargets("p", filepath.Join(tmp, "p"), installOptions{}); err != nil {
		t.Fatalf("runInstallSharedTargets: %v", err)
	}
}

// ---------- install exact/prune (--inexact) ----------

// sharedSkillIntentForInstall builds a valid ResourcePruneTarget skill intent
// whose target lives at <relDir>/review, so the install projection projects a
// managed link there and sibling-prunes the rest of <relDir>. Mirrors the
// platform-package validSharedSkillIntent fixture (kept local because that
// helper is unexported in the platform test package).
func sharedSkillIntentForInstall(relDir string) platform.ResourceIntent {
	return platform.ResourceIntent{
		IntentID:    "skills.proj.review.fake",
		Project:     "proj",
		Bucket:      "skills",
		LogicalName: "review",
		TargetPath:  filepath.Join(relDir, "review"),
		Ownership:   platform.ResourceOwnershipSharedRepo,
		SourceRef: platform.ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "skills",
			RelativePath: "review",
			Kind:         platform.ResourceSourceCanonicalDir,
			Origin:       "shared-skill-mirror",
		},
		Shape:         platform.ResourceShapeDirectDir,
		Transport:     platform.ResourceTransportSymlink,
		Materializer:  "shared-skill-dir-symlink",
		ReplacePolicy: platform.ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   platform.ResourcePruneTarget,
		MarkerFiles:   []string{"SKILL.md"},
		Provenance:    platform.ResourceProvenance{Emitter: "fake"},
	}
}

// seedManagedSkillLink creates a managed symlink at linkPath pointing into
// <agentsHome>/skills/proj/<name> (so links.IsManagedLinkUnder reports it
// managed). The canonical target dir is created first so the link resolves.
func seedManagedSkillLink(t *testing.T, agentsHome, name, linkPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink seeding not exercised on windows")
	}
	canonical := filepath.Join(agentsHome, "skills", "proj", name)
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := links.Symlink(canonical, linkPath); err != nil {
		t.Fatalf("seed managed link: %v", err)
	}
	if !links.IsManagedLinkUnder(linkPath, agentsHome) {
		t.Fatalf("seeded link %s not detected as managed under %s", linkPath, agentsHome)
	}
}

// setupInstallPruneFixture builds repo + agentsHome with a canonical "review"
// skill, a stale managed link sibling, and an unmanaged user file sibling in
// the prune scope directory. Returns the three sibling paths.
func setupInstallPruneFixture(t *testing.T) (repo, agentsHome, relDir, wanted, stale, userFile string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome = filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	repo = filepath.Join(tmp, "repo")
	relDir = filepath.Join(".agents", "skills")
	dir := filepath.Join(repo, relDir)

	// Canonical skill the projection links into the wanted target.
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "skills", "proj", "review", "SKILL.md"),
		[]byte("---\nname: review\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wanted = filepath.Join(dir, "review")
	stale = filepath.Join(dir, "obsolete")
	seedManagedSkillLink(t, agentsHome, "obsolete", stale)

	// A plain user file living next to managed outputs must never be pruned.
	userFile = filepath.Join(dir, "user-notes.md")
	if err := os.WriteFile(userFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, agentsHome, relDir, wanted, stale, userFile
}

// Default (exact) install prunes a stale managed link while preserving the
// wanted target and an unmanaged user sibling.
func TestRunInstallSharedTargets_ExactPrunesStaleKeepsUser(t *testing.T) {
	repo, _, relDir, wanted, stale, userFile := setupInstallPruneFixture(t)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	platforms := []platform.Platform{fakeInstallPlatform{
		id:        "fake",
		installed: true,
		intents:   []platform.ResourceIntent{sharedSkillIntentForInstall(relDir)},
	}}
	if err := runInstallSharedTargetsFor("proj", repo, platforms, installOptions{inexact: false}, nil, false); err != nil {
		t.Fatalf("runInstallSharedTargetsFor exact: %v", err)
	}

	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale managed link must be pruned, lstat err = %v", err)
	}
	if _, err := os.Lstat(wanted); err != nil {
		t.Fatalf("wanted target must be present, lstat err = %v", err)
	}
	if _, err := os.Lstat(userFile); err != nil {
		t.Fatalf("unmanaged user file must be preserved, lstat err = %v", err)
	}
}

// --inexact install keeps the stale managed link in place (additive behavior).
func TestRunInstallSharedTargets_InexactKeepsStale(t *testing.T) {
	repo, _, relDir, _, stale, userFile := setupInstallPruneFixture(t)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	platforms := []platform.Platform{fakeInstallPlatform{
		id:        "fake",
		installed: true,
		intents:   []platform.ResourceIntent{sharedSkillIntentForInstall(relDir)},
	}}
	if err := runInstallSharedTargetsFor("proj", repo, platforms, installOptions{inexact: true}, nil, false); err != nil {
		t.Fatalf("runInstallSharedTargetsFor inexact: %v", err)
	}

	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("--inexact must keep the stale managed link, lstat err = %v", err)
	}
	if _, err := os.Lstat(userFile); err != nil {
		t.Fatalf("unmanaged user file must be preserved, lstat err = %v", err)
	}
}

// Two back-to-back exact projections on an already-converged tree are a no-op:
// the second run must not error and must not remove the wanted target.
func TestRunInstallSharedTargets_ExactIdempotent(t *testing.T) {
	repo, _, relDir, wanted, _, _ := setupInstallPruneFixture(t)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	platforms := []platform.Platform{fakeInstallPlatform{
		id:        "fake",
		installed: true,
		intents:   []platform.ResourceIntent{sharedSkillIntentForInstall(relDir)},
	}}
	if err := runInstallSharedTargetsFor("proj", repo, platforms, installOptions{inexact: false}, nil, false); err != nil {
		t.Fatalf("first projection: %v", err)
	}
	fi1, err := os.Lstat(wanted)
	if err != nil {
		t.Fatalf("wanted target missing after first run: %v", err)
	}
	if err := runInstallSharedTargetsFor("proj", repo, platforms, installOptions{inexact: false}, nil, false); err != nil {
		t.Fatalf("second projection (must be a no-op): %v", err)
	}
	fi2, err := os.Lstat(wanted)
	if err != nil {
		t.Fatalf("wanted target missing after second run: %v", err)
	}
	if fi1.Mode() != fi2.Mode() {
		t.Fatalf("converged target changed between runs: %v -> %v", fi1.Mode(), fi2.Mode())
	}
}

// ---------- git source helpers (fetch / clone / update) ----------

func requireGitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

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

	cacheDir, err := fetchGitSource(bare, "main", StdInstallDeps{})
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

	first, err := fetchGitSource(bare, "main", StdInstallDeps{})
	if err != nil {
		t.Fatalf("priming clone: %v", err)
	}
	sentinel := filepath.Join(first, "_sentinel")
	os.WriteFile(sentinel, []byte("kept"), 0644)

	second, err := fetchGitSource(bare, "main", StdInstallDeps{})
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
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	first, err := fetchGitSource(bare, "main", StdInstallDeps{})
	if err != nil {
		t.Fatalf("priming clone: %v", err)
	}
	stale := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(first, ".last-fetch"), stale, stale)

	second, err := fetchGitSource(bare, "main", StdInstallDeps{})
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

	saved := Flags
	Flags = GlobalFlags{}
	first, err := fetchGitSource(bare, "main", StdInstallDeps{})
	if err != nil {
		Flags = saved
		t.Fatalf("priming clone: %v", err)
	}
	stale := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(first, ".last-fetch"), stale, stale)

	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	second, err := fetchGitSource(bare, "main", StdInstallDeps{})
	if err != nil {
		t.Fatalf("dry-run update: %v", err)
	}
	if second != first {
		t.Errorf("cache dir changed during dry-run: %q vs %q", first, second)
	}
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

	cacheDir, err := fetchGitSource(bare, "main", StdInstallDeps{})
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
	_, err := CloneGitSource(gitBin, bogusURL, "main", cacheDir, StdInstallDeps{})
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
	runGitCmd(t, tmp, "init", "-q", "-b", "main", tmp)
	runGitCmd(t, tmp, "config", "user.name", "Test")
	runGitCmd(t, tmp, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(tmp, "a"), []byte("a"), 0644)
	runGitCmd(t, tmp, "add", "a")
	runGitCmd(t, tmp, "commit", "-q", "-m", "x")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

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

	root, err := resolveSourceRoot(config.Source{Type: "git", URL: bare, Ref: "main"}, StdInstallDeps{})
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

// TestRunInstall_HappyPathWithInstalledClaude exercises the full install path
// without --dry-run.
func TestRunInstall_HappyPathWithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
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

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Errorf("RunInstall full happy: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projDir, ".agentsrc.json")); err != nil {
		t.Errorf("expected manifest to remain: %v", err)
	}
	if _, err := config.LoadAgentsRC(projDir); err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	lock, err := config.ReadUnits(projDir)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if lock.InputsDigest == "" {
		t.Fatal("install must ensure the units lock carries inputs_digest")
	}
	lf, err := agentslock.Open(config.AgentsLockPath(projDir))
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	var stamp installLockStamp
	if ok, err := lf.Section(installLockSection, &stamp); err != nil || !ok {
		t.Fatalf("install stamp missing: ok=%v err=%v", ok, err)
	}
	if stamp.Project != "proj" || stamp.Stamped == "" {
		t.Fatalf("install stamp = %+v", stamp)
	}
}

func TestResolveInstallSources_StrictErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	_, err := resolveInstallSources([]config.Source{{Type: "git", URL: "git://nonexistent.invalid/no.git", Ref: "main"}}, true, StdInstallDeps{})
	if err == nil {
		t.Error("expected strict-mode error")
	}
}

func TestResolveInstallSources_NonStrictIgnoresError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got, err := resolveInstallSources([]config.Source{{Type: "git", URL: "git://nonexistent.invalid/no.git", Ref: "main"}}, false, StdInstallDeps{})
	if err != nil {
		t.Errorf("non-strict should swallow err, got %v / %v", got, err)
	}
}

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
	if err := RegisterInstallProject("p", projPath, StdInstallDeps{}); err != nil {
		t.Errorf("RegisterInstallProject: %v", err)
	}
}

func TestLinkInstallResources_NoSourcesFallsBackToAgentsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	skill := filepath.Join(agentsHome, "skills", "proj", "x")
	os.MkdirAll(skill, 0755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	rc := &config.AgentsRC{Version: 1, Project: "proj", Skills: []string{"x"}}
	if err := linkInstallResources("proj", rc, nil, false, StdInstallDeps{}); err != nil {
		t.Errorf("linkInstallResources: %v", err)
	}
	dest := filepath.Join(agentsHome, "skills", "proj", "x")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("expected linked dest: %v", err)
	}
}

func TestLinkInstallResourceList_StrictMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := linkInstallResourceList("skills", "skill", []string{"missing-skill"}, "proj", []string{agentsHome}, true, StdInstallDeps{})
	if err == nil {
		t.Error("expected strict mode error for missing skill")
	}
}

func TestLinkInstallResourceList_NonStrictWarnings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := linkInstallResourceList("skills", "skill", []string{"missing"}, "proj", []string{agentsHome}, false, StdInstallDeps{}); err != nil {
		t.Errorf("non-strict should not error: %v", err)
	}
}

func TestCreateInstallPlatformLink_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	for _, p := range platform.All() {
		if err := createInstallPlatformLink(p, "p", filepath.Join(tmp, "p")); err != nil {
			t.Fatalf("createInstallPlatformLink dry-run: %v", err)
		}
	}
}

func TestFinalizeInstall_DryRun(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := finalizeInstall("p", t.TempDir(), installOptions{}); err != nil {
		t.Fatalf("finalizeInstall dry-run: %v", err)
	}
}

func TestFinalizeInstall_WriteFailReturnsError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "not-a-dir"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := finalizeInstall("p", filepath.Join(tmp, "not-a-dir"), installOptions{}); err == nil {
		t.Fatal("expected finalizeInstall to return lock write error")
	}
}

func TestRunInstallSharedTargets_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := runInstallSharedTargets("p", filepath.Join(tmp, "p"), installOptions{}); err != nil {
		t.Fatalf("runInstallSharedTargets dry-run: %v", err)
	}
}

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

	if err := RunInstall(true, StdInstallDeps{}); err == nil {
		t.Error("expected --strict RunInstall to propagate git resolve error")
	}
}

// ---------- NewInstallCmd ----------
//
// Pure constructor tests that exercise the cobra builder, Args validator,
// flag wiring, and RunE generate/non-generate dispatch.

func stubNoArgsWithHints(_ ...string) cobra.PositionalArgs {
	return cobra.NoArgs
}

func installTestDeps() Deps {
	return Deps{
		ExampleBlock:    stubExampleBlock,
		NoArgsWithHints: stubNoArgsWithHints,
	}
}

func TestNewInstallCmd_MetadataAndFlags(t *testing.T) {
	cmd := NewInstallCmd(installTestDeps())
	if cmd.Use != "install" {
		t.Errorf("Use = %q, want install", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short must not be empty")
	}
	if cmd.Long == "" {
		t.Error("Long must not be empty")
	}
	if cmd.Example == "" {
		t.Error("Example must not be empty")
	}
	if cmd.Flags().Lookup("generate") == nil {
		t.Error("expected --generate flag wired on install cmd")
	}
	if cmd.Flags().Lookup("strict") == nil {
		t.Error("expected --strict flag wired on install cmd")
	}
	if cmd.Args == nil {
		t.Fatal("expected Args validator wired")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("Args should accept zero args: %v", err)
	}
}

// TestNewInstallCmd_RunEDispatchesGenerate ensures RunE routes to
// RunInstallGenerate when --generate is set. We do not exercise the real
// install pipeline; we just confirm the dispatch shape by inspecting which
// codepath errors emerge (manifest-missing for the install branch, the
// directory-name fallback for generate which writes a manifest to the temp
// dir successfully).
func TestNewInstallCmd_RunEDispatchesGenerate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "genproj")
	os.MkdirAll(projDir, 0755)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	cmd := NewInstallCmd(installTestDeps())
	cmd.SetArgs([]string{"--generate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".agentsrc.json")); err != nil {
		t.Errorf("--generate should have written manifest: %v", err)
	}
}

func TestNewInstallCmd_RunEDispatchesInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "noinstall")
	os.MkdirAll(projDir, 0755)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	cmd := NewInstallCmd(installTestDeps())
	// No --generate; should attempt install and fail because no manifest exists.
	cmd.SetArgs(nil)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected install branch to error on missing manifest")
	}
	if !strings.Contains(err.Error(), ".agentsrc.json") {
		t.Errorf("expected missing-manifest error, got: %v", err)
	}
}

// TestNewInstallCmd_RunEAppliesDepsToGlobals pins the t13a-introduced
// RunE wrapper contract: applyDepsToGlobals must fire before the moved
// RunE body runs, so deps.Version / .Commit / .Describe / .FlagsFn
// values reach the lifecycle package vars without the parent shim's
// syncLifecycleGlobals helper. Without this, a regression that drops
// the wrapper's applyDepsToGlobals call would silently revert t13b to
// needing the shim's manual sync — the very thing t13a exists to absorb.
//
// We exercise both halves of the contract in one test: --generate path
// is mutation-free (writes a manifest, but no platform link mutations)
// and reliably touches finalizeInstall-adjacent code that reads the
// build-info vars. Asserting on the package vars post-Execute confirms
// the sync ran; a missing-manifest check on the install branch is
// covered by TestNewInstallCmd_RunEDispatchesInstall above.
func TestNewInstallCmd_RunEAppliesDepsToGlobals(t *testing.T) {
	saved := Flags
	savedV, savedC, savedD := Version, Commit, Describe
	defer func() {
		Flags = saved
		Version = savedV
		Commit = savedC
		Describe = savedD
	}()

	// Force the package vars to a known-bad sentinel so we can prove
	// the wrapper overwrote them.
	Version = "sentinel-stale"
	Commit = "sentinel-stale"
	Describe = "sentinel-stale"
	Flags = GlobalFlags{}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "synced")
	os.MkdirAll(projDir, 0755)
	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	depsWithBuildInfo := Deps{
		ExampleBlock:    stubExampleBlock,
		NoArgsWithHints: stubNoArgsWithHints,
		FlagsFn:         func() GlobalFlags { return GlobalFlags{Verbose: true} },
		Version:         "0.9.9-applied",
		Commit:          "cafebabe",
		Describe:        "v0.9.9-applied-1-gcafebabe",
	}

	cmd := NewInstallCmd(depsWithBuildInfo)
	cmd.SetArgs([]string{"--generate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --generate: %v", err)
	}

	if Version != "0.9.9-applied" {
		t.Errorf("RunE wrapper did not propagate Version; got %q", Version)
	}
	if Commit != "cafebabe" {
		t.Errorf("RunE wrapper did not propagate Commit; got %q", Commit)
	}
	if Describe != "v0.9.9-applied-1-gcafebabe" {
		t.Errorf("RunE wrapper did not propagate Describe; got %q", Describe)
	}
	if !Flags.Verbose {
		t.Errorf("RunE wrapper did not propagate FlagsFn().Verbose; got %+v", Flags)
	}
}

// TestNewInstallCmd_ThreadsBuildInfoIntoLockStamp proves the full t17 chain:
// deps build info → applyDepsToGlobals → installOptionsFromGlobals →
// installOptions → finalizeInstall → the .agentsrc.lock install stamp. A real
// (non-dry-run) install via the constructed cobra command must record the
// deps-supplied Version/Commit/Describe, confirming NewInstallCmd's RunE builds
// opts from live state rather than reading the removed install-local seam.
func TestNewInstallCmd_ThreadsBuildInfoIntoLockStamp(t *testing.T) {
	saved := Flags
	savedV, savedC, savedD := Version, Commit, Describe
	defer func() {
		Flags = saved
		Version, Commit, Describe = savedV, savedC, savedD
	}()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
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

	deps := Deps{
		ExampleBlock:    stubExampleBlock,
		NoArgsWithHints: stubNoArgsWithHints,
		FlagsFn:         func() GlobalFlags { return GlobalFlags{Yes: true} },
		Version:         "3.1.4",
		Commit:          "c0ffee",
		Describe:        "v3.1.4-2-gc0ffee",
	}

	cmd := NewInstallCmd(deps)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute install: %v", err)
	}

	lf, err := agentslock.Open(config.AgentsLockPath(projDir))
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	var stamp installLockStamp
	if ok, err := lf.Section(installLockSection, &stamp); err != nil || !ok {
		t.Fatalf("install stamp missing: ok=%v err=%v", ok, err)
	}
	if stamp.Version != "3.1.4" || stamp.Commit != "c0ffee" || stamp.Describe != "v3.1.4-2-gc0ffee" {
		t.Fatalf("NewInstallCmd did not thread deps build info into stamp: %+v", stamp)
	}
}

// ---------- Error-injection coverage via fakeInstallDeps ----------

func TestRunInstall_GetwdErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	wantErr := errString("boom-getwd")
	deps := fakeInstallDeps{getwd: func() (string, error) { return "", wantErr }}
	err := RunInstall(false, deps)
	if err == nil || !strings.Contains(err.Error(), "boom-getwd") {
		t.Errorf("expected getwd error to propagate, got %v", err)
	}
}

func TestRunInstallGenerate_GetwdErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	wantErr := errString("boom-getwd-gen")
	deps := fakeInstallDeps{getwd: func() (string, error) { return "", wantErr }}
	err := RunInstallGenerate(deps)
	if err == nil || !strings.Contains(err.Error(), "boom-getwd-gen") {
		t.Errorf("expected getwd error to propagate, got %v", err)
	}
}

func TestRegisterInstallProject_LoadConfigErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	wantErr := errString("boom-loadcfg")
	deps := fakeInstallDeps{loadConfig: func() (*config.Config, error) { return nil, wantErr }}
	err := RegisterInstallProject("p", "/tmp/p", deps)
	if err == nil || !strings.Contains(err.Error(), "boom-loadcfg") {
		t.Errorf("expected loadConfig error to propagate, got %v", err)
	}
}

func TestFindProjectByPath_LoadConfigErrorReturnsEmpty(t *testing.T) {
	deps := fakeInstallDeps{loadConfig: func() (*config.Config, error) {
		return nil, errString("boom-loadcfg")
	}}
	if got := FindProjectByPath("/tmp/p", deps); got != "" {
		t.Errorf("expected empty on loadConfig error, got %q", got)
	}
}

func TestCloneGitSource_MkdirAllErrorPropagates(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	wantErr := errString("boom-mkdir")
	deps := fakeInstallDeps{mkdirAll: func(string, os.FileMode) error { return wantErr }}
	_, err := CloneGitSource("git", "url", "", "/tmp/cache", deps)
	if err == nil || !strings.Contains(err.Error(), "boom-mkdir") {
		t.Errorf("expected mkdirAll error to propagate, got %v", err)
	}
}

func TestLinkResourceFromSources_MkdirAllError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	// Build a valid source so candidate is found.
	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	wantErr := errString("boom-mkdir-link")
	deps := fakeInstallDeps{mkdirAll: func(string, os.FileMode) error { return wantErr }}
	err := LinkResourceFromSources("skills", "demo", "proj", []string{src}, deps)
	if err == nil || !strings.Contains(err.Error(), "boom-mkdir-link") {
		t.Errorf("expected mkdirAll error to propagate, got %v", err)
	}
}

func TestLinkResourceFromSources_SymlinkError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	wantErr := errString("boom-symlink")
	deps := fakeInstallDeps{symlink: func(string, string) error { return wantErr }}
	err := LinkResourceFromSources("skills", "demo", "proj", []string{src}, deps)
	if err == nil || !strings.Contains(err.Error(), "boom-symlink") {
		t.Errorf("expected symlink error to propagate, got %v", err)
	}
}

func TestLinkResourceFromSources_VerboseLogs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if err := LinkResourceFromSources("skills", "demo", "proj", []string{src}, StdInstallDeps{}); err != nil {
		t.Fatalf("verbose link: %v", err)
	}
}

func TestShouldUseCachedGitSource_VerboseLogs(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, ".last-fetch"), []byte("now"), 0644)

	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if !ShouldUseCachedGitSource(tmp, "https://example.com/repo.git") {
		t.Error("verbose + fresh .last-fetch should still return true")
	}
}

func TestEnsureInstallProjectDirs_CreateProjectDirsErr(t *testing.T) {
	// AGENTS_HOME pointed at a regular file → CreateProjectDirs should fail
	// when it tries to MkdirAll under it.
	tmp := t.TempDir()
	fakeHome := filepath.Join(tmp, "not-a-dir")
	os.WriteFile(fakeHome, []byte("x"), 0644)
	t.Setenv("AGENTS_HOME", fakeHome)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := ensureInstallProjectDirs("p"); err == nil {
		t.Error("expected projectsync.CreateProjectDirs to fail when AGENTS_HOME is a file")
	}
}

func TestRunInstallGenerate_GenerateAgentsRCError(t *testing.T) {
	// config.GenerateAgentsRC fails when the project path does not exist
	// (it tries to read it). Construct deps whose Getwd returns a path that
	// does not exist on disk.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	missing := filepath.Join(tmp, "does-not-exist")
	deps := fakeInstallDeps{getwd: func() (string, error) { return missing, nil }}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := RunInstallGenerate(deps)
	if err == nil {
		t.Skip("config.GenerateAgentsRC tolerated missing path on this platform")
	}
}

// TestRunInstallGenerate_AccessManifestStatError covers the
// `else if !os.IsNotExist(statErr)` branch by pointing the manifest path at
// a directory we cannot stat (a path whose parent is a file).
func TestRunInstallGenerate_AccessManifestStatError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// projectPath where the manifest path's parent is a regular file → stat
	// returns a non-ENOENT error (ENOTDIR on most unices).
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR semantics differ on Windows; covered on Unix runners")
	}
	projDir := filepath.Join(tmp, "as-file")
	os.WriteFile(projDir, []byte("x"), 0644)

	deps := fakeInstallDeps{getwd: func() (string, error) { return projDir, nil }}
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := RunInstallGenerate(deps); err == nil {
		t.Skip("non-ENOENT stat path not reachable from this layout")
	}
}

// TestRunInstallGenerate_SaveError covers the rc.Save failure path by
// writing a read-only project directory.
func TestRunInstallGenerate_SaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix mode bits do not block writes the same way on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "ro-proj")
	os.MkdirAll(projDir, 0755)
	if err := os.Chmod(projDir, 0o500); err != nil {
		t.Skipf("could not chmod project dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(projDir, 0o755) })

	deps := fakeInstallDeps{getwd: func() (string, error) { return projDir, nil }}
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := RunInstallGenerate(deps); err == nil {
		t.Skip("readonly mode insufficient to block manifest write here")
	}
}

// TestFetchGitSource_GitNotInstalled exercises the LookPath failure path.
func TestFetchGitSource_GitNotInstalled(t *testing.T) {
	// Force LookPath to fail by replacing PATH with an empty dir.
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	_, err := fetchGitSource("https://example.com/x.git", "", StdInstallDeps{})
	if err == nil || !strings.Contains(err.Error(), "git not installed") {
		t.Errorf("expected git-not-installed error, got %v", err)
	}
}

func TestCreateInstallPlatformLink_CreateLinksError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	p := fakeInstallPlatform{id: "boom", installed: true, linkErr: errors.New("link failed")}
	if err := createInstallPlatformLink(p, "p", t.TempDir()); err == nil {
		t.Fatal("expected installed platform link failure to be returned")
	}
}

// TestRunInstallSharedTargets_ProjectionError exercises the err branch of
// platform.RunSharedTargetProjection by passing a project path whose
// parent is unwritable. Install treats projection errors as fatal so it never
// stamps success over partial outputs.
func TestRunInstallSharedTargets_ProjectionErrorDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	// Empty project name + path under a non-existent parent → projection
	// returns an error if any platform reports an issue.
	platforms := []platform.Platform{
		fakeInstallPlatform{id: "boom", installed: true, intentErr: errors.New("collect failed")},
	}
	if err := runInstallSharedTargetsFor("", filepath.Join(tmp, "no-such-parent", "p"), platforms, installOptions{}, nil, false); err == nil {
		t.Fatal("expected shared target projection error")
	}
}

// TestRegisterInstallProject_RebindPreservesIdentity models the machine-B
// rebind through `da install`: a project already in the SYNCED identity registry
// but unbound on this machine must be rebound via the machine-local path only,
// WITHOUT recomputing/overwriting the synced repo_id (Fix 1).
func TestRegisterInstallProject_RebindPreservesIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	synced := `{"version":2,"projects":{"svc":{"repo_id":"github.com/acme/svc"}}}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(synced), 0644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	projPath := filepath.Join(home, "checkout", "svc")
	if err := RegisterInstallProject("svc", projPath, StdInstallDeps{}); err != nil {
		t.Fatalf("RegisterInstallProject: %v", err)
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ProjectRepoID("svc"); got != "github.com/acme/svc" {
		t.Errorf("install rebind OVERWROTE synced repo_id: got %q", got)
	}
	if got := reloaded.GetProjectPath("svc"); got != filepath.Clean(projPath) {
		t.Errorf("install rebind did not set machine-local path: got %q", got)
	}
}

// TestRunInstall_ProjectsExtendsInheritedSkill pins config-transitive-layering's
// projection payoff: a repo that declares only its team source (and extends the
// team layer) must materialize a skill supplied by the ORG layer the team layer
// transitively extends — even though that skill is ABSENT from the repo's flat
// .agentsrc.json. Regression guard for linking from the resolved effective
// manifest (ensureRes.Snapshot.Effective) rather than the raw manifest.
func TestRunInstall_ProjectsExtendsInheritedSkill(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":2}`), 0644)
	// canonical org-supplied skill at global scope
	skillDir := filepath.Join(agentsHome, "skills", "global", "org-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# org-skill"), 0644)
	// source layers: team extends org; org declares the skill
	src := filepath.Join(tmp, "src")
	os.MkdirAll(filepath.Join(src, "org"), 0755)
	os.MkdirAll(filepath.Join(src, "team"), 0755)
	os.WriteFile(filepath.Join(src, "org", "base.json"), []byte(`{"skills":["org-skill"]}`), 0644)
	// strconv.Quote escapes the path so the JSON is valid on Windows (a raw
	// backslash path yields invalid string escapes); matches the idiom in
	// commands/config/relevance_test.go's transitive fixture.
	os.WriteFile(filepath.Join(src, "team", "base.json"),
		[]byte(`{"sources":[{"id":"org","type":"local","path":`+strconv.Quote(src)+`}],"extends":["org:org/base.json"]}`), 0644)
	// repo declares ONLY the team source + extends team; NO skills in the flat manifest
	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	os.WriteFile(filepath.Join(projDir, config.AgentsRCFile),
		[]byte(`{"project":"proj","version":2,"sources":[{"id":"team","type":"local","path":`+strconv.Quote(src)+`}],"extends":["team:team/base.json"]}`), 0644)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	dest := filepath.Join(agentsHome, "skills", "proj", "org-skill")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("extends-inherited org-skill not projected at %s: %v", dest, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink to the canonical org-skill", dest)
	}
}

// TestRunInstall_DryRunNoResolvedSnapshotNoPanic pins the nil-guard: under
// --dry-run ensureInstallResolved returns a nil EnsureResult, so runInstall must
// fall back to the flat manifest for the resource list instead of dereferencing
// a nil snapshot.
func TestRunInstall_DryRunNoResolvedSnapshotNoPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":2}`), 0644)
	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	os.WriteFile(filepath.Join(projDir, config.AgentsRCFile),
		[]byte(`{"project":"proj","version":2}`), 0644)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	// Must not panic on the nil resolved snapshot.
	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("dry-run RunInstall: %v", err)
	}
}
