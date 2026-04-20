package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

type hintError struct {
	message string
	hints   []string
}

func (e *hintError) Error() string {
	return e.message
}

func stubDeps(yes bool) Deps {
	return Deps{Flags: GlobalFlags{Yes: yes}}
}

func setupAgentsEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, "agents")
	projectPath = filepath.Join(tmp, "repo")

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	return agentsHome, projectPath
}

func writeAgentMD(t *testing.T, projectPath, agentName string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", "agents", agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + agentName + "\ndescription: test agent\n---\n\n# " + agentName + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeCanonicalAgent(t *testing.T, agentsHome, projectName, agentName string) string {
	t.Helper()
	dir := filepath.Join(agentsHome, "agents", projectName, agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + agentName + "\ndescription: test agent\n---\n\n# " + agentName + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestImportAgentIn_CreatesSymlinksAndRegisters(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "importproj")
	canonical := writeCanonicalAgent(t, agentsHome, "importproj", "imported-agent")

	if err := ImportAgentIn("imported-agent", projectPath); err != nil {
		t.Fatalf("ImportAgentIn: %v", err)
	}

	repoAgents := filepath.Join(projectPath, ".agents", "agents", "imported-agent")
	fi, err := os.Lstat(repoAgents)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected .agents/agents symlink, got %v", fi.Mode())
	}
	target, err := os.Readlink(repoAgents)
	if err != nil {
		t.Fatal(err)
	}
	if target != canonical {
		t.Errorf(".agents/agents target = %q, want %q", target, canonical)
	}

	claudePath := filepath.Join(projectPath, ".claude", "agents", "imported-agent")
	cfi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf(".claude/agents symlink: %v", err)
	}
	if cfi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected .claude/agents symlink")
	}
	clTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if clTarget != canonical {
		t.Errorf(".claude/agents target = %q, want %q", clTarget, canonical)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "imported-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("Agents = %v; want imported-agent", rc.Agents)
	}
}

func TestImportAgentIn_Idempotent(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "importproj2")
	writeCanonicalAgent(t, agentsHome, "importproj2", "idem-import")

	if err := ImportAgentIn("idem-import", projectPath); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if err := ImportAgentIn("idem-import", projectPath); err != nil {
		t.Fatalf("second import: %v", err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range rc.Agents {
		if a == "idem-import" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want 1 listing of idem-import, got %d in %v", n, rc.Agents)
	}
}

func TestImportAgentIn_ErrorCanonicalMissing(t *testing.T) {
	_, projectPath := setupAgentsEnv(t, "missingcanon")

	err := ImportAgentIn("nope", projectPath)
	if err == nil {
		t.Fatal("expected error for missing canonical agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q; want 'not found'", err.Error())
	}
}

func TestImportAgentIn_ErrorNoProjectName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{Version: 1, Project: ""}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	writeCanonicalAgent(t, agentsHome, "global", "orphan")

	err := ImportAgentIn("orphan", projectPath)
	if err == nil {
		t.Fatal("expected error for empty project name")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestImportAgentIn_ErrorRepoLocalRealDir(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "importproj3")
	writeCanonicalAgent(t, agentsHome, "importproj3", "clash-import")
	writeAgentMD(t, projectPath, "clash-import")

	err := ImportAgentIn("clash-import", projectPath)
	if err == nil {
		t.Fatal("expected error when repo has real directory")
	}
	if !strings.Contains(err.Error(), "real directory") {
		t.Errorf("error = %q; want 'real directory'", err.Error())
	}
}

func TestImportAgentIn_ErrorRepoLocalMispointedSymlink(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "importproj4")
	canonical := writeCanonicalAgent(t, agentsHome, "importproj4", "mis-import")

	repoLocal := filepath.Join(projectPath, ".agents", "agents", "mis-import")
	if err := os.MkdirAll(filepath.Join(projectPath, ".agents", "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(agentsHome, "other"), repoLocal); err != nil {
		t.Fatal(err)
	}

	err := ImportAgentIn("mis-import", projectPath)
	if err == nil {
		t.Fatal("expected error for mispointed symlink")
	}
	if !strings.Contains(err.Error(), "not the canonical path") {
		t.Errorf("error = %q", err.Error())
	}
	if _, err := os.Stat(filepath.Join(canonical, "AGENT.md")); err != nil {
		t.Errorf("canonical AGENT.md: %v", err)
	}
}

func TestPromoteAgentIn_ConvergesRepoLocalToManagedSymlink(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest")
	writeAgentMD(t, projectPath, "my-agent")

	if err := PromoteAgentIn("my-agent", projectPath, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	canonicalPath := filepath.Join(agentsHome, "agents", "myprojtest", "my-agent")
	repoLocalPath := filepath.Join(projectPath, ".agents", "agents", "my-agent")

	cfi, err := os.Lstat(canonicalPath)
	if err != nil {
		t.Fatalf("canonical path not created at %s: %v", canonicalPath, err)
	}
	if cfi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical path %s should be a real directory, got symlink", canonicalPath)
	}
	if !cfi.IsDir() {
		t.Errorf("canonical path %s should be a directory, got %v", canonicalPath, cfi.Mode())
	}
	if _, err := os.Stat(filepath.Join(canonicalPath, "AGENT.md")); err != nil {
		t.Errorf("canonical AGENT.md missing: %v", err)
	}

	rfi, err := os.Lstat(repoLocalPath)
	if err != nil {
		t.Fatalf("repo-local path missing after promote: %v", err)
	}
	if rfi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("repo-local path %s should be a symlink after promote, got %v", repoLocalPath, rfi.Mode())
	}
	target, err := os.Readlink(repoLocalPath)
	if err != nil {
		t.Fatalf("readlink repo-local: %v", err)
	}
	if target != canonicalPath {
		t.Errorf("repo-local symlink target = %q, want %q", target, canonicalPath)
	}

	claudePath := filepath.Join(projectPath, ".claude", "agents", "my-agent")
	cfi2, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf(".claude/agents symlink missing: %v", err)
	}
	if cfi2.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".claude/agents/%s should be a symlink, got %v", "my-agent", cfi2.Mode())
	}
	clTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("readlink .claude/agents: %v", err)
	}
	if clTarget != canonicalPath {
		t.Errorf(".claude/agents symlink target = %q, want %q", clTarget, canonicalPath)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "my-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf(".agentsrc.json Agents = %v; want 'my-agent' to be present", rc.Agents)
	}
}

func TestPromoteAgentIn_PreservesManifestUnknownFields(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	manifest := `{
  "version": 1,
  "project": "regproj",
  "sources": [{"type":"local"},{"type":"git","url":"https://example.com/repo.git"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "customPolicy": {"interval": "daily", "auto": true},
  "myteam": "platform"
}`
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	writeAgentMD(t, projectPath, "extra-agent")

	if err := PromoteAgentIn("extra-agent", projectPath, false); err != nil {
		t.Fatalf("PromoteAgentIn: %v", err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if len(rc.ExtraFields) < 2 {
		t.Fatalf("ExtraFields: got %d keys, want at least 2; keys: %v", len(rc.ExtraFields), rc.ExtraFields)
	}
	if _, ok := rc.ExtraFields["customPolicy"]; !ok {
		t.Error("ExtraFields missing 'customPolicy' after promote")
	}
	if _, ok := rc.ExtraFields["myteam"]; !ok {
		t.Error("ExtraFields missing 'myteam' after promote")
	}
	var policyVal map[string]any
	if err := json.Unmarshal(rc.ExtraFields["customPolicy"], &policyVal); err != nil {
		t.Fatalf("unmarshal customPolicy: %v", err)
	}
	if policyVal["interval"] != "daily" {
		t.Errorf("customPolicy.interval: got %v, want daily", policyVal["interval"])
	}
	if len(rc.Sources) < 2 {
		t.Errorf("Sources: want at least 2 entries preserved, got %+v", rc.Sources)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "extra-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("Agents should include extra-agent, got %v", rc.Agents)
	}
}

func TestPromoteAgentIn_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest2")
	writeAgentMD(t, projectPath, "idem-agent")

	if err := PromoteAgentIn("idem-agent", projectPath, false); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if err := PromoteAgentIn("idem-agent", projectPath, false); err != nil {
		t.Fatalf("second promote: %v", err)
	}

	canonical := filepath.Join(agentsHome, "agents", "myprojtest2", "idem-agent")
	fi, err := os.Lstat(canonical)
	if err != nil {
		t.Fatalf("canonical missing after second promote: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical should be a real directory after second promote, got symlink")
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	count := 0
	for _, a := range rc.Agents {
		if a == "idem-agent" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Agents list has %d occurrences of 'idem-agent'; want 1. list=%v", count, rc.Agents)
	}
}

func TestPromoteAgentIn_ForceOverwritesCanonicalDir(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "forceproj")
	writeAgentMD(t, projectPath, "force-agent")

	destPath := filepath.Join(agentsHome, "agents", "forceproj", "force-agent")
	if err := os.MkdirAll(filepath.Join(destPath, "stale"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destPath, "AGENT.md"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := PromoteAgentIn("force-agent", projectPath, true); err != nil {
		t.Fatalf("PromoteAgentIn with --force: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destPath, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "stale" {
		t.Errorf("expected repo AGENT.md to replace stale canonical file")
	}
	if !strings.Contains(string(data), "test agent") {
		t.Errorf("expected promoted AGENT.md from repo fixture, got %q", string(data))
	}
}

func TestPromoteAgentIn_ErrorAgentNotFound(t *testing.T) {
	_, projectPath := setupAgentsEnv(t, "myprojtest3")

	err := PromoteAgentIn("nonexistent", projectPath, false)
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q; want 'not found' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorNoProjectName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{Version: 1, Project: ""}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	writeAgentMD(t, projectPath, "some-agent")

	err := PromoteAgentIn("some-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error message = %q; want 'project name' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorRepoLocalSymlinkMispoints(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest5")
	writeAgentMD(t, projectPath, "mis-agent")

	repoLocalPath := filepath.Join(projectPath, ".agents", "agents", "mis-agent")
	if err := os.RemoveAll(repoLocalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(agentsHome, "other"), repoLocalPath); err != nil {
		t.Fatal(err)
	}

	err := PromoteAgentIn("mis-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error when repo-local symlink points elsewhere, got nil")
	}
	if !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("error message = %q; want 'already a symlink but points to' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorExistingCanonicalWithoutForce(t *testing.T) {
	agentsHome, projectPath := setupAgentsEnv(t, "myprojtest4")
	writeAgentMD(t, projectPath, "clash-agent")

	destPath := filepath.Join(agentsHome, "agents", "myprojtest4", "clash-agent")
	if err := os.MkdirAll(destPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destPath, "AGENT.md"), []byte("---\nname: x\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := PromoteAgentIn("clash-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error when canonical path is a real directory, got nil")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error message = %q; want '--force' substring", err.Error())
	}
}

func TestPromoteAgentIn_ErrorMissingAGENTmd(t *testing.T) {
	_, projectPath := setupAgentsEnv(t, "noagentmd")
	dir := filepath.Join(projectPath, ".agents", "agents", "empty-dir")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	err := PromoteAgentIn("empty-dir", projectPath, false)
	if err == nil {
		t.Fatal("expected error without AGENT.md, got nil")
	}
	if !strings.Contains(err.Error(), "AGENT.md") {
		t.Errorf("error message = %q; want 'AGENT.md' substring", err.Error())
	}
}

func TestRemoveAgentIn_UnlinksSymlinksAndManifest(t *testing.T) {
	d := stubDeps(false)
	agentsHome, projectPath := setupAgentsEnv(t, "rmproj")
	writeCanonicalAgent(t, agentsHome, "rmproj", "gone-agent")

	if err := ImportAgentIn("gone-agent", projectPath); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := RemoveAgentIn(d, "gone-agent", projectPath, false); err != nil {
		t.Fatalf("RemoveAgentIn: %v", err)
	}

	if pathExists(filepath.Join(projectPath, ".agents", "agents", "gone-agent")) {
		t.Error("expected .agents/agents/gone-agent removed")
	}
	if pathExists(filepath.Join(projectPath, ".claude", "agents", "gone-agent")) {
		t.Error("expected .claude/agents/gone-agent removed")
	}
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rc.Agents {
		if a == "gone-agent" {
			t.Errorf("agents list should not include gone-agent: %v", rc.Agents)
		}
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "agents", "rmproj", "gone-agent", "AGENT.md")); err != nil {
		t.Errorf("canonical AGENT.md should remain without --purge: %v", err)
	}
}

func TestRemoveAgentIn_DriftSymlinkWithoutManifestEntry(t *testing.T) {
	d := stubDeps(false)
	agentsHome, projectPath := setupAgentsEnv(t, "driftproj")
	canonical := writeCanonicalAgent(t, agentsHome, "driftproj", "drift-agent")

	repoLocal := filepath.Join(projectPath, ".agents", "agents", "drift-agent")
	if err := os.MkdirAll(filepath.Join(projectPath, ".agents", "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, repoLocal); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAgentIn(d, "drift-agent", projectPath, false); err != nil {
		t.Fatalf("RemoveAgentIn: %v", err)
	}
	if pathExists(repoLocal) {
		t.Error("expected drift symlink removed")
	}
}

func TestRemoveAgentIn_ErrorNotLinked(t *testing.T) {
	d := Deps{
		ErrorWithHints: func(message string, hints ...string) error {
			return &hintError{message: strings.TrimSpace(message), hints: append([]string{}, hints...)}
		},
	}
	_, projectPath := setupAgentsEnv(t, "nolink")

	err := RemoveAgentIn(d, "missing", projectPath, false)
	if err == nil {
		t.Fatal("expected error")
	}
	cliErr, ok := err.(*hintError)
	if !ok {
		t.Fatalf("expected hintError, got %T", err)
	}
	wantMsg := `agent "missing" is not linked in this project`
	if cliErr.message != wantMsg {
		t.Fatalf("unexpected error message:\n got: %q\nwant: %q", cliErr.message, wantMsg)
	}
	if got := strings.Join(cliErr.hints, "\n"); !strings.Contains(got, "dot-agents agents list") {
		t.Fatalf("expected agents-list recovery hint, got %q", got)
	}
}

func TestRemoveAgentIn_ErrorRealDirectory(t *testing.T) {
	d := stubDeps(false)
	_, projectPath := setupAgentsEnv(t, "realdirproj")
	writeAgentMD(t, projectPath, "real-agent")

	err := RemoveAgentIn(d, "real-agent", projectPath, false)
	if err == nil {
		t.Fatal("expected error for real directory")
	}
	if !strings.Contains(err.Error(), "real directory") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRemoveAgentIn_PurgeDeletesCanonical(t *testing.T) {
	d := stubDeps(true)
	agentsHome, projectPath := setupAgentsEnv(t, "purgeproj")
	writeCanonicalAgent(t, agentsHome, "purgeproj", "purge-me")
	if err := ImportAgentIn("purge-me", projectPath); err != nil {
		t.Fatalf("import: %v", err)
	}

	if err := RemoveAgentIn(d, "purge-me", projectPath, true); err != nil {
		t.Fatalf("RemoveAgentIn: %v", err)
	}
	canonical := filepath.Join(agentsHome, "agents", "purgeproj", "purge-me")
	if _, err := os.Stat(filepath.Join(canonical, "AGENT.md")); !os.IsNotExist(err) {
		t.Errorf("expected canonical tree removed: %v", err)
	}
}

func TestRemoveAgentIn_ErrorPurgeCanonicalSymlink(t *testing.T) {
	d := stubDeps(true)
	agentsHome, projectPath := setupAgentsEnv(t, "symproj")
	other := filepath.Join(agentsHome, "agents", "symproj", "other")
	if err := os.MkdirAll(filepath.Join(other, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	badCanon := filepath.Join(agentsHome, "agents", "symproj", "bad-sym")
	_ = os.RemoveAll(badCanon)
	if err := os.Symlink(other, badCanon); err != nil {
		t.Fatal(err)
	}
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	rc.Agents = append(rc.Agents, "bad-sym")
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	err = RemoveAgentIn(d, "bad-sym", projectPath, true)
	if err == nil {
		t.Fatal("expected error when canonical path is symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q", err.Error())
	}
}

func dotAgentsModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

// TestObs1776215397478320000_AgentResourceLifecycleCompleted verifies proposal obs-1776215397478320000
// (agents CLI parity work): archived agent-resource-lifecycle TASKS are all completed and the repo
// still carries the agents command surface promised by that plan.
func TestObs1776215397478320000_AgentResourceLifecycleCompleted(t *testing.T) {
	root := dotAgentsModuleRoot(t)
	histTasks := filepath.Join(root, ".agents", "history", "agent-resource-lifecycle", "TASKS.yaml")
	data, err := os.ReadFile(histTasks)
	if err != nil {
		t.Skipf("history agent-resource-lifecycle TASKS not in this checkout: %v", err)
	}

	var doc struct {
		PlanID string `yaml:"plan_id"`
		Tasks  []struct {
			ID     string `yaml:"id"`
			Status string `yaml:"status"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse history TASKS: %v", err)
	}
	if doc.PlanID != "agent-resource-lifecycle" {
		t.Fatalf("plan_id = %q", doc.PlanID)
	}
	if len(doc.Tasks) == 0 {
		t.Fatal("expected tasks in history TASKS.yaml")
	}
	for _, task := range doc.Tasks {
		if task.Status != "completed" {
			t.Fatalf("task %q status = %q, want completed (proposal 539 closure)", task.ID, task.Status)
		}
	}

	agentsBridge := filepath.Join(root, "commands", "agents.go")
	bridge, err := os.ReadFile(agentsBridge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bridge), "NewAgentsCmd") {
		t.Fatal("commands/agents.go missing NewAgentsCmd bridge")
	}
	cmdTree := filepath.Join(root, "commands", "agents", "cmd.go")
	cmdSrc, err := os.ReadFile(cmdTree)
	if err != nil {
		t.Fatal(err)
	}
	cs := string(cmdSrc)
	for _, needle := range []string{"promote <name>", "import <name>", "remove <name>", "list [project]"} {
		if !strings.Contains(cs, needle) {
			t.Fatalf("commands/agents/cmd.go missing %q — agents lifecycle CLI incomplete", needle)
		}
	}
	for _, leaf := range []string{"promote.go", "import.go", "remove.go"} {
		p := filepath.Join(root, "commands", "agents", leaf)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected agents subcommand file %s: %v", leaf, err)
		}
	}

	claudeGo := filepath.Join(root, "internal", "platform", "claude.go")
	cg, err := os.ReadFile(claudeGo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cg), "syncScopedDirSymlinksTargets") ||
		!strings.Contains(string(cg), "createAgentsLinks") {
		t.Fatal("internal/platform/claude.go expected createAgentsLinks wiring for agents refresh parity")
	}
}
