//go:build windows

package graphstore

import "path/filepath"

// venvBinSubdirs returns the executable subdirectory names used by Python
// virtualenvs, most-specific first. Windows venvs put executables under
// Scripts/; bin/ is probed second so a developer with a POSIX-style .venv
// checked out on a Windows host still resolves.
func venvBinSubdirs() []string {
	return []string{"Scripts", "bin"}
}

// venvExeCandidates returns possible absolute paths for executable `name`
// inside the venv rooted at venvDir. Windows executables may carry a .exe
// extension; the plain name is also probed so a shimmed (extensionless)
// entrypoint still resolves.
func venvExeCandidates(venvDir, name string) []string {
	var out []string
	for _, sub := range venvBinSubdirs() {
		out = append(out, filepath.Join(venvDir, sub, name))
		out = append(out, filepath.Join(venvDir, sub, name+".exe"))
	}
	return out
}

// venvPythonNames returns the candidate Python interpreter file names to probe
// in a venv's executable directory, most-preferred first. Windows venvs ship
// python.exe (no python3.exe); the .exe variants are preferred so an actual
// Windows executable is selected before any extensionless shim.
func venvPythonNames() []string {
	return []string{"python.exe", "python3.exe", "python", "python3"}
}

// venvPythonFallback is the bare interpreter name to return when no Python
// interpreter is found alongside the CRG binary. exec.Command applies PATHEXT
// to resolve "python" -> python.exe via PATH at run time.
func venvPythonFallback() string {
	return "python"
}
