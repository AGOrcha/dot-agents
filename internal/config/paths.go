package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// UserHomeDir resolves the user's home directory, honoring $HOME when set
// before falling back to os.UserHomeDir(). This is the convention for CLI
// tools (git, gh, …): on Windows os.UserHomeDir() reads %USERPROFILE% and
// ignores $HOME, which (a) breaks per-test isolation that sets HOME to a
// temp dir — causing cross-test pollution and writes into the real runner
// profile — and (b) ignores explicit shell/WSL HOME overrides. Honoring
// $HOME first fixes both with zero behavior change when $HOME is unset
// (the normal Windows desktop case). Mirrors the os.UserHomeDir signature
// for drop-in use.
func UserHomeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// AgentsHome returns the path to the ~/.agents directory.
func AgentsHome() string {
	if override := os.Getenv("AGENTS_HOME"); override != "" {
		return override
	}
	home, _ := UserHomeDir()
	// Uniform ~/.agents on every OS (Windows: C:\Users\<user>\.agents).
	// The prior %APPDATA%\.agents special-case split the managed root from
	// where the rest of the code resolves user home, which broke link
	// resolution on restricted Windows hosts.
	return filepath.Join(home, ".agents")
}

// UserHome returns the current user's home directory.
func UserHome() string {
	home, _ := UserHomeDir()
	return home
}

// AgentsStateDir returns the XDG state directory for dot-agents.
func AgentsStateDir() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, _ := UserHomeDir()
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "dot-agents")
}

// AgentsContextDir returns the local workflow context directory under ~/.agents.
func AgentsContextDir() string {
	return filepath.Join(AgentsHome(), "context")
}

// ProjectContextDir returns the local workflow context directory for a project.
func ProjectContextDir(project string) string {
	return filepath.Join(AgentsContextDir(), project)
}

// HooksScopeDirIn returns the canonical hooks directory for a scope under an
// explicit agents-home root: <agentsHome>/hooks/<scope>. This is the single
// definition of the canonical hooks-scope path model. Callers that already
// hold a resolved agents-home (the `da hooks` scope-tree guard, which must
// honor a test-injected root rather than the process environment) use this
// form so the path model can never drift between entrypoints.
func HooksScopeDirIn(agentsHome, scope string) string {
	return filepath.Join(agentsHome, "hooks", scope)
}

// HooksScopeDir returns the canonical hooks directory for a scope rooted at
// the resolved AgentsHome(): ~/.agents/hooks/<scope>. scope is either
// "global" or a managed project name. `da remove --clean`'s canonical-dir
// cleanup resolves the hooks subtree through this helper instead of
// independently concatenating "hooks/<scope>".
func HooksScopeDir(scope string) string {
	return HooksScopeDirIn(AgentsHome(), scope)
}

// ExpandPath expands a path with ~ to the full absolute path.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := UserHomeDir()
		return filepath.Clean(filepath.Join(home, path[2:]))
	}
	if path == "~" {
		home, _ := UserHomeDir()
		return filepath.Clean(home)
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			return filepath.Clean(abs)
		}
	}
	return filepath.Clean(path)
}

// DisplayPath converts an absolute path to a ~ prefixed display path.
func DisplayPath(path string) string {
	home, _ := UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + filepath.ToSlash(path[len(home):])
	}
	return path
}

// UserHomeRoots returns the applicable user home directories.
// When AGENTS_WINDOWS_MIRROR is set for WSL, includes the Windows home too.
func UserHomeRoots() []string {
	home, _ := UserHomeDir()
	roots := []string{home}

	windowsMirror := os.Getenv("DOT_AGENTS_WINDOWS_MIRROR")
	windowsHome := os.Getenv("DOT_AGENTS_WINDOWS_HOME")
	if windowsMirror == "true" && windowsHome != "" && windowsHome != home {
		roots = append(roots, windowsHome)
	}
	return roots
}

// SetWindowsMirrorContext checks if the repo path is under a WSL Windows mount
// and sets the relevant env vars.
func SetWindowsMirrorContext(repoPath string) {
	re := regexp.MustCompile(`^/mnt/c/Users/([^/]+)(/|$)`)
	if m := re.FindStringSubmatch(repoPath); len(m) > 1 {
		os.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "true")
		os.Setenv("DOT_AGENTS_WINDOWS_HOME", "/mnt/c/Users/"+m[1])
	} else {
		os.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "false")
		os.Setenv("DOT_AGENTS_WINDOWS_HOME", "")
	}
}
