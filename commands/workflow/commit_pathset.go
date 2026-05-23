// commit_pathset.go derives the deterministic, scoped set of paths to stage
// for `da workflow commit`. It is the pure-function half of the
// workflow-commit-command spec (decision #3): "Deterministic, scoped path set
// — never `git add -A`."
//
// The caller is responsible for actually running git status and shelling /
// go-git'ing the stage; this file owns only the derivation logic so it is
// testable from fixtures without a real worktree or subprocesses.
package workflow

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Managed roots for workflow-state commits. Anything outside these is only
// eligible if the caller explicitly named it in the MutationSurface (per the
// spec's "session-touched state paths" clause). Trailing slashes anchor the
// match to a directory boundary so e.g. ".agents/workflow_old/" never matches.
const (
	managedRootPlans   = ".agents/workflow/"
	managedRootHistory = ".agents/history/"
)

// StatusEntry is one line of `git status --porcelain=v2 -z` after parsing.
// XY is the 2-char status code; OrigPath is non-empty only for renames /
// copies. Submodule reflects whether porcelain v2 reported the entry with the
// "S" sub-state marker (field 3 starts with 'S' instead of 'N').
type StatusEntry struct {
	XY        string
	Path      string
	OrigPath  string
	Submodule bool
	Untracked bool
}

// DerivePathSet returns the sorted, deduplicated set of paths to stage. The
// derivation rules (from the spec's Decisions §3 and the wc-path-derivation
// task notes) are:
//
//   - Include any tracked change (modified, deleted, renamed, copied, added,
//     unmerged) whose path is under one of the managed roots
//     (.agents/workflow/, .agents/history/) OR is named in MutationSurface.
//   - Include untracked (?) entries only when the caller named them in
//     MutationSurface. This is what excludes pre-existing-untracked dirs:
//     the workflow mutation surface is authoritative about which untracked
//     paths it just created.
//   - Always exclude submodule-pointer entries even if they fall under a
//     managed root. A submodule pointer change is a separate kind of commit
//     (submodule bump) and conflating it with workflow state silently
//     mutates an SCM relationship.
//   - For rename / copy entries, stage both the new path and the original
//     path so git captures the rename intent (R) or deletion (the index
//     entry the old path leaves behind).
//
// Determinism: the output is sorted lexicographically and deduplicated, so
// the same (status, surface) input pair always produces the same output.
func DerivePathSet(status []StatusEntry, mutationSurface []string) []string {
	surface := make(map[string]struct{}, len(mutationSurface))
	for _, p := range mutationSurface {
		surface[p] = struct{}{}
	}

	picked := make(map[string]struct{})
	consider := func(path string, untracked, submodule bool) {
		if path == "" || submodule {
			return
		}
		_, inSurface := surface[path]
		if untracked && !inSurface {
			return
		}
		if !inManagedRoot(path) && !inSurface {
			return
		}
		picked[path] = struct{}{}
	}

	for _, e := range status {
		consider(e.Path, e.Untracked, e.Submodule)
		// Rename / copy origin must also be staged so the unstaged
		// deletion-of-old-path lands alongside the addition-of-new-path —
		// otherwise the commit shows the new file as untracked and leaves
		// the old one tracked-but-missing.
		consider(e.OrigPath, false, e.Submodule)
	}

	out := make([]string, 0, len(picked))
	for p := range picked {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// inManagedRoot reports whether path is under one of the workflow-managed
// roots. Strict prefix match plus a directory boundary so a path like
// ".agents/workflow_old/x" does not match.
func inManagedRoot(path string) bool {
	return strings.HasPrefix(path, managedRootPlans) ||
		strings.HasPrefix(path, managedRootHistory)
}

// ParseStatus parses `git status --porcelain=v2 -z` output into StatusEntry
// slices. The v2 format keeps fields fixed-width and NUL-terminates each
// entry, so the parser is straightforward; v1 is intentionally not supported
// because v1 cannot reliably distinguish submodule sub-states from regular
// modifications.
//
// Reference: https://git-scm.com/docs/git-status#_porcelain_format_version_2
func ParseStatus(raw []byte) ([]StatusEntry, error) {
	entries := make([]StatusEntry, 0)
	for _, rec := range splitNUL(raw) {
		if len(rec) == 0 {
			continue
		}
		switch rec[0] {
		case '#':
			// header (branch.oid / branch.head / branch.upstream / ...).
			continue
		case '1':
			e, err := parseV2Ordinary(rec)
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		case '2':
			// Rename / copy: the entry has an extra "score path NUL origPath"
			// tail; the origPath is the NEXT NUL-separated record.
			e, err := parseV2Rename(rec)
			if err != nil {
				return nil, err
			}
			// origPath was packed into rec by splitNULRenamePair below — see
			// the caller for the pairing logic.
			entries = append(entries, e)
		case 'u':
			e, err := parseV2Unmerged(rec)
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		case '?':
			entries = append(entries, StatusEntry{
				XY:        "??",
				Path:      string(rec[2:]),
				Untracked: true,
			})
		case '!':
			// Ignored entries: surface neither — they are never candidates.
			continue
		default:
			return nil, fmt.Errorf("unrecognized porcelain v2 record kind %q", rec[0])
		}
	}
	return entries, nil
}

// splitNUL splits raw on NUL bytes, but pairs the trailing-origPath that
// follows a rename ('2') record into the same logical chunk so callers see
// "<2-record><NUL><origPath>" as one slice with an internal NUL.
func splitNUL(raw []byte) [][]byte {
	out := make([][]byte, 0)
	for len(raw) > 0 {
		nul := bytes.IndexByte(raw, 0)
		var rec []byte
		if nul < 0 {
			rec = raw
			raw = nil
		} else {
			rec = raw[:nul]
			raw = raw[nul+1:]
		}
		if len(rec) > 0 && rec[0] == '2' {
			// Rename / copy record: the next NUL-separated chunk is the
			// origPath. Splice it back in with a separator we can find later.
			nul2 := bytes.IndexByte(raw, 0)
			var orig []byte
			if nul2 < 0 {
				orig = raw
				raw = nil
			} else {
				orig = raw[:nul2]
				raw = raw[nul2+1:]
			}
			joined := make([]byte, 0, len(rec)+1+len(orig))
			joined = append(joined, rec...)
			joined = append(joined, 0)
			joined = append(joined, orig...)
			out = append(out, joined)
			continue
		}
		out = append(out, rec)
	}
	return out
}

// parseV2Ordinary parses a porcelain v2 "1" record:
//
//	1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
func parseV2Ordinary(rec []byte) (StatusEntry, error) {
	fields := bytes.SplitN(rec, []byte{' '}, 9)
	if len(fields) < 9 {
		return StatusEntry{}, fmt.Errorf("malformed porcelain v2 ordinary record %q", rec)
	}
	return StatusEntry{
		XY:        string(fields[1]),
		Path:      string(fields[8]),
		Submodule: isSubmoduleSub(fields[2]),
	}, nil
}

// parseV2Rename parses a porcelain v2 "2" record with the spliced origPath:
//
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <score> <path>NUL<origPath>
func parseV2Rename(rec []byte) (StatusEntry, error) {
	fields := bytes.SplitN(rec, []byte{' '}, 10)
	if len(fields) < 10 {
		return StatusEntry{}, fmt.Errorf("malformed porcelain v2 rename record %q", rec)
	}
	tail := fields[9]
	nul := bytes.IndexByte(tail, 0)
	if nul < 0 {
		return StatusEntry{}, fmt.Errorf("rename record missing origPath separator: %q", rec)
	}
	return StatusEntry{
		XY:        string(fields[1]),
		Path:      string(tail[:nul]),
		OrigPath:  string(tail[nul+1:]),
		Submodule: isSubmoduleSub(fields[2]),
	}, nil
}

// parseV2Unmerged parses a porcelain v2 "u" record:
//
//	u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
func parseV2Unmerged(rec []byte) (StatusEntry, error) {
	fields := bytes.SplitN(rec, []byte{' '}, 11)
	if len(fields) < 11 {
		return StatusEntry{}, fmt.Errorf("malformed porcelain v2 unmerged record %q", rec)
	}
	return StatusEntry{
		XY:        string(fields[1]),
		Path:      string(fields[10]),
		Submodule: isSubmoduleSub(fields[2]),
	}, nil
}

// isSubmoduleSub reports whether the v2 sub-state field marks the entry as a
// submodule. The field is 4 bytes: 'N' for non-submodule, 'S' followed by
// three flag chars (cCmMuU…) for submodules.
func isSubmoduleSub(sub []byte) bool {
	return len(sub) > 0 && sub[0] == 'S'
}
