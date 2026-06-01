// Package testutil provides shared test scaffolding helpers used by
// *_test.go files across the dot-agents tree. The package exists to
// eliminate cross-package fixture duplication that SonarCloud flags
// (Cluster D in plans/sonarqube-pr10/findings.md): per-package copies
// of setupTempProject / writeFixtureRC / manifest writers / git-repo
// init that all do the same thing under different names.
//
// Helper conventions:
//   - All helpers accept *testing.T as the first argument and call
//     t.Helper() so test stack traces stay accurate.
//   - Helpers t.Fatal on prerequisite errors (no returned errors).
//     Callers always want immediate test failure on fixture setup.
//   - Helpers do NOT call t.TempDir() themselves. Callers control
//     the lifecycle root and pass paths in.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"golang.org/x/sys/execabs"
)

// NewTempProject creates a self-contained agentsHome + repo pair under
// t.TempDir(), points AGENTS_HOME at agentsHome, and writes a minimal
// .agentsrc.json (Version=1, Project=projectName, one local source).
// Returns (agentsHome, projectPath).
//
// Replaces commands/agents/agents_test.go::setupAgentsEnv and
// commands/skills/promote_test.go::setupSkillsEnv.
func NewTempProject(t *testing.T, projectName string) (agentsHome, projectPath string) {
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

	// Isolate HOME too, not just AGENTS_HOME. Some resource flows (e.g. the skill
	// platform mirror in commands/skills/promote.go) resolve a base via
	// config.UserHomeDir() for HOME-relative targets like ~/.claude. Without this,
	// those writes escape the sandbox into the developer's real ~/.agents and
	// ~/.claude (leaving dangling symlinks + orphan dirs). USERPROFILE covers the
	// Windows home resolution path.
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)

	WriteAgentsRC(t, projectPath, &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: "local"}},
	})
	return agentsHome, projectPath
}

// WriteAgentsRC saves rc into projectPath/.agentsrc.json. When rc is
// nil, a minimal default (Version=1, Sources=[{Type:"local"}]) is used.
//
// Replaces the repeated `rc := &config.AgentsRC{...}; rc.Save(projectPath)`
// 6-line block scattered across agents/skills setups, and the inline
// .agentsrc.json string literal in workflow_integration_test.go and
// scaffold_hooks_test.go.
func WriteAgentsRC(t *testing.T, projectPath string, rc *config.AgentsRC) {
	t.Helper()
	if rc == nil {
		rc = &config.AgentsRC{
			Version: 1,
			Sources: []config.Source{{Type: "local"}},
		}
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
}

// WriteAgentManifest creates projectPath/.agents/agents/<agentName>/
// and writes AGENT.md with a frontmatter (`name`, `description: test
// agent`) and a body. Bucket-aware wrapper around the generic
// writeManifest core.
//
// Replaces commands/agents/agents_test.go::writeAgentMD.
func WriteAgentManifest(t *testing.T, projectPath, agentName string) {
	t.Helper()
	writeRepoLocalManifest(t, projectPath, "agents", agentName, "AGENT.md", "test agent")
}

// WriteSkillManifest creates projectPath/.agents/skills/<skillName>/
// and writes SKILL.md with a frontmatter and body.
//
// Replaces commands/skills/promote_test.go::writeSkillMD.
func WriteSkillManifest(t *testing.T, projectPath, skillName string) {
	t.Helper()
	writeRepoLocalManifest(t, projectPath, "skills", skillName, "SKILL.md", "test skill")
}

// WriteCanonicalAgent creates the canonical agentsHome/agents/<projectName>/
// <agentName>/ directory with an AGENT.md fixture. Returns the directory.
//
// Replaces commands/agents/agents_test.go::writeCanonicalAgent.
func WriteCanonicalAgent(t *testing.T, agentsHome, projectName, agentName string) string {
	t.Helper()
	return writeCanonicalManifest(t, agentsHome, "agents", projectName, agentName, "AGENT.md", "test agent")
}

// WriteCanonicalSkill creates the canonical agentsHome/skills/<projectName>/
// <skillName>/ directory with a SKILL.md fixture. Returns the directory.
//
// Symmetric counterpart to WriteCanonicalAgent.
func WriteCanonicalSkill(t *testing.T, agentsHome, projectName, skillName string) string {
	t.Helper()
	return writeCanonicalManifest(t, agentsHome, "skills", projectName, skillName, "SKILL.md", "test skill")
}

// WriteScopeFile creates agentsHome/<bucket>/<scope>/ and writes
// <baseName> with the given content. Used by mcp/settings/rules tests
// where the fixture is just "drop a file under the scope tree" with no
// manifest semantics.
//
// Replaces inline mkdir+writeFile blocks in commands/{mcp_settings,rules}_test.go
// and internal/platform/{mcp_settings,rules}_test.go.
func WriteScopeFile(t *testing.T, agentsHome, bucket, scope, baseName string, content []byte) {
	t.Helper()
	dir := filepath.Join(agentsHome, bucket, scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, baseName), content, 0644); err != nil {
		t.Fatal(err)
	}
}

// WriteScopeFilePath extends WriteScopeFile to accept a nested relative
// path under agentsHome/bucket/scope/, e.g. ".github/hooks/pre-tool.json".
// The relative path components are MkdirAll'd as needed. Equivalent to
// WriteScopeFile when relPath has no separator.
//
// Unlocks the seed loop in
// TestRestoreFromResourcesCounted_RestoresDirectoryBuckets and similar
// nested-path call sites surfaced in t5's merge-back.
func WriteScopeFilePath(t *testing.T, agentsHome, bucket, scope, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(agentsHome, bucket, scope, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0644); err != nil {
		t.Fatal(err)
	}
}

// NewTempAgentsHome creates a t.TempDir() root, sets HOME and
// AGENTS_HOME envs (with .agents/ subdir under HOME), and returns
// (tmp, agentsHome).
//
// Lighter than NewTempProject when a test needs full env-var control
// but no project tree or .agentsrc.json. Absorbs the recurring 5-line
// preamble:
//
//	tmp := t.TempDir()
//	t.Setenv("HOME", tmp)
//	agentsHome := filepath.Join(tmp, ".agents")
//	if err := os.MkdirAll(agentsHome, 0755); err != nil { t.Fatal(err) }
//	t.Setenv("AGENTS_HOME", agentsHome)
//
// This helper intentionally calls t.TempDir() internally (the env-var
// setup wraps the temp dir; the helper exists to bundle them). Use
// NewTempProject when you also need a project directory + canonical
// agents tree; use this when you don't.
func NewTempAgentsHome(t *testing.T) (tmp, agentsHome string) {
	t.Helper()
	tmp = t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome = filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	return tmp, agentsHome
}

// InitGitRepo runs `git init` in repoPath, sets test author/committer
// identity (env + config), writes the supplied path→content map, then
// `git add .` + `git commit -m "init"`. File paths in the map are
// relative to repoPath and use forward slashes (converted via
// filepath.FromSlash). Map iteration is sorted by path so file
// write-order is deterministic across runs.
//
// Replaces commands/workflow/testutil_test.go::initWorkflowTestRepo,
// commands/scaffold_hooks_test.go::initShellHookTestRepo, and the
// inline git block in workflow_integration_test.go::TestWorkflow_EmptyStateGraceful.
func InitGitRepo(t *testing.T, repoPath string, files map[string]string) {
	t.Helper()

	// execabs.Command resolves "git" via PATH but rejects relative paths
	// containing "/", matching the project-wide hardening for go:S4036
	// applied during sonarqube-pr10 sq3. Test code only — runs in
	// developer-controlled environments, hard-coded tool name.
	run := func(args ...string) {
		t.Helper()
		cmd := execabs.Command("git", append([]string{"-C", repoPath}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run("init")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		path := filepath.Join(repoPath, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[rel]), 0644); err != nil {
			t.Fatal(err)
		}
	}

	run("add", ".")
	run("commit", "-m", "init")
}

// WritePreservationManifest creates a temp project whose .agentsrc.json contains
// extra fields (customPolicy, myteam) and a multi-source Sources list — the
// fixture used by TestPromote*_PreservesManifestUnknownFields. Returns
// (agentsHome, projectPath) with AGENTS_HOME set.
func WritePreservationManifest(t *testing.T) (agentsHome, projectPath string) {
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
	// Isolate HOME as well as AGENTS_HOME — promote's skill platform mirror
	// resolves HOME-relative targets (~/.claude) via config.UserHomeDir(); without
	// this the mirror leaks into the real ~/.agents and ~/.claude. See NewTempProject.
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)
	const manifest = `{
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
	return agentsHome, projectPath
}

// AssertExtraFieldsPreserved checks that rc retains customPolicy + myteam extra
// fields and a multi-source Sources list after a promote operation.
func AssertExtraFieldsPreserved(t *testing.T, rc *config.AgentsRC) {
	t.Helper()
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
}

// writeRepoLocalManifest is the common body of WriteAgentManifest and
// WriteSkillManifest. Bucket is "agents" or "skills"; manifest is
// "AGENT.md" or "SKILL.md"; descTag is the literal value of the
// description: frontmatter line (e.g. "test agent").
func writeRepoLocalManifest(t *testing.T, projectPath, bucket, name, manifest, descTag string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", bucket, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + descTag + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, manifest), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeCanonicalManifest is the common body of WriteCanonicalAgent and
// WriteCanonicalSkill. Returns the directory path.
func writeCanonicalManifest(t *testing.T, agentsHome, bucket, projectName, name, manifest, descTag string) string {
	t.Helper()
	dir := filepath.Join(agentsHome, bucket, projectName, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + descTag + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, manifest), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
