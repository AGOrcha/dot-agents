package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// AgentsHome returns the path to the ~/.agents directory.
func AgentsHome() string {
	if override := os.Getenv("AGENTS_HOME"); override != "" {
		return override
	}
	home, _ := os.UserHomeDir()
	// On Windows use %APPDATA%\.agents if home detection is ambiguous
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, ".agents")
		}
	}
	return filepath.Join(home, ".agents")
}

// UserHome returns the current user's home directory.
func UserHome() string {
	home, _ := os.UserHomeDir()
	return home
}

// AgentsStateDir returns the XDG state directory for dot-agents.
func AgentsStateDir() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, _ := os.UserHomeDir()
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "dot-agents")
}

// ExpandPath expands a path with ~ to the full absolute path.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			return abs
		}
	}
	return path
}

// DisplayPath converts an absolute path to a ~ prefixed display path.
func DisplayPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// UserHomeRoots returns the applicable user home directories.
// When AGENTS_WINDOWS_MIRROR is set for WSL, includes the Windows home too.
func UserHomeRoots() []string {
	home, _ := os.UserHomeDir()
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
