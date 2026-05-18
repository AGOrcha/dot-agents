//go:build !windows

package graphstore

import "path/filepath"

// venvBinSubdirs returns the executable subdirectory names used by Python
// virtualenvs, most-specific first. POSIX venvs put executables under bin/;
// Scripts/ is probed second only so a developer with a Windows-style .venv
// checked out on a POSIX host still resolves.
func venvBinSubdirs() []string {
	return []string{"bin", "Scripts"}
}

// venvExeCandidates returns possible absolute paths for executable `name`
// inside the venv rooted at venvDir. POSIX executables carry no extension.
func venvExeCandidates(venvDir, name string) []string {
	var out []string
	for _, sub := range venvBinSubdirs() {
		out = append(out, filepath.Join(venvDir, sub, name))
	}
	return out
}

// venvPythonNames returns the candidate Python interpreter file names to probe
// in a venv's executable directory, most-preferred first. POSIX venvs ship
// python3 (and a python alias).
func venvPythonNames() []string {
	return []string{"python3", "python"}
}

// venvPythonFallback is the bare interpreter name to return when no Python
// interpreter is found alongside the CRG binary. exec.Command resolves it via
// PATH at run time.
func venvPythonFallback() string {
	return "python3"
}
