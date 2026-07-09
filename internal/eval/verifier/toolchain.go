package verifier

import (
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// tscBin is the TypeScript compiler binary; it is both a direct-PATH candidate
// and the second token of the `npx tsc` fallback, so it lives in one place.
const tscBin = "tsc"

// lookPathFn resolves an executable name to a path on PATH. It is the seam
// exec.LookPath fills in production (NewBase); tests inject a deterministic
// resolver so the toolchain-missing path can be exercised without mutating the
// process PATH.
type lookPathFn func(string) (string, error)

// ToolchainError reports that a language's required interpreter or compiler is
// not installed on PATH, so verification could not even begin. It is a DISTINCT
// error type from VerifyError (a build/test step that started and then failed):
// the harness surfaces it as a "toolchain unavailable" abort instead of scoring
// the run, so a missing python3 / node / tsc is never mistaken for the agent
// producing bad code. Match it with errors.As(err, *ToolchainError).
type ToolchainError struct {
	// Language is the verifier language whose toolchain is missing.
	Language eval.Language
	// Binary is the logical command the TaskSpec asked for (argv[0]).
	Binary string
	// Tried lists the candidate invocations probed on PATH, in order.
	Tried []string
}

// Error implements error with an actionable, operator-facing message.
func (e *ToolchainError) Error() string {
	return fmt.Sprintf(
		"verifier: %s toolchain unavailable: %q not found on PATH (tried: %s); install it or run a language whose toolchain is present",
		e.Language, e.Binary, strings.Join(e.Tried, ", "))
}

// toolchainCandidates returns the ordered argv-prefix candidates that can stand
// in for the logical binary bin. The first candidate whose head resolves on
// PATH wins and replaces the leading token(s) of the command:
//
//   - python / python3 prefer python3 over python — many machines ship only
//     python3, and the generator emits a bare "python".
//   - tsc falls back to "npx tsc" when no standalone tsc is installed (the
//     common case: TypeScript is a devDependency, not a global binary).
//   - every other binary resolves only as itself.
//
// exec.LookPath applies the platform's executable-extension rules (PATHEXT on
// Windows), so "node" / "python" / "go" resolve node.exe / python.exe / go.exe
// there without any special-casing here.
func toolchainCandidates(bin string) [][]string {
	switch bin {
	case "python", "python3":
		return [][]string{{"python3"}, {"python"}}
	case tscBin:
		return [][]string{{tscBin}, {"npx", tscBin}}
	default:
		return [][]string{{bin}}
	}
}

// resolveToolchain rewrites cmd so its executable is one that actually resolves
// on PATH, preferring the per-language candidate order. On success it returns a
// new argv with the leading token(s) replaced by the resolved candidate (e.g.
// "python" → "python3", or "tsc …" → "npx tsc …") so the resolved binary flows
// into the executed command. When no candidate resolves it returns a
// *ToolchainError naming every candidate it probed. An empty cmd is returned
// unchanged (the run engine rejects it downstream with its own error).
func resolveToolchain(lang eval.Language, cmd []string, lookPath lookPathFn) ([]string, error) {
	if len(cmd) == 0 {
		return cmd, nil
	}
	bin := cmd[0]
	var tried []string
	for _, prefix := range toolchainCandidates(bin) {
		tried = append(tried, strings.Join(prefix, " "))
		if _, err := lookPath(prefix[0]); err == nil {
			resolved := make([]string, 0, len(prefix)+len(cmd)-1)
			resolved = append(resolved, prefix...)
			return append(resolved, cmd[1:]...), nil
		}
	}
	return nil, &ToolchainError{Language: lang, Binary: bin, Tried: tried}
}
