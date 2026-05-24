package scoring

// This file is one slice of the R1 outcome-scoring feature: the git-topology
// objective extractors. The `landed` and `scope` signals each have an objective
// source that is checkable independently of the agent — git commit-survival and
// the changed file set — and this is where that objective evidence is read.
// See docs/OUTCOME_SCORING_RUBRIC.md for the authoritative signal definitions.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

// trunkRef is the branch the `landed` signal measures survival into. The rubric
// names it "master"; dot-agents history is on master.
const trunkRef = "master"

// GitSignals carries the objective, git-derived sub-scores for one iteration.
// Each field is a SignalValue: a sub-score in [0,1] when the topology could be
// read, or absent when it could not — absent is first-class and the assemble
// slice falls back to the self-reported source.
type GitSignals struct {
	// LandedObserved is the objective `landed` signal: did the iteration's
	// commit survive into the trunk. 1.0 when reachable and not reverted, 0.0
	// when reverted or orphaned, absent when the commit SHA cannot be resolved.
	LandedObserved SignalValue
	// ScopeObserved is the objective `scope` signal: the fraction of the
	// commit's changed files that fall inside the task's declared write_scope.
	// Absent when no write_scope is resolvable for the task — true for most
	// historical iterations, where the assemble slice falls back to scope_note.
	ScopeObserved SignalValue
}

// ExtractGitSignals computes the git-topology objective signals for one
// iteration against the repository at repoDir.
//
// It returns an error only for unexpected failures — chiefly repoDir not being
// a git repository. A commit SHA that simply does not resolve is NOT an error:
// it is an absent signal, because v1 iteration-log entries carry abbreviated
// SHAs from since-rebased history and some carry no SHA at all.
func ExtractGitSignals(rec IterationRecord, repoDir string) (GitSignals, error) {
	if err := assertGitRepo(repoDir); err != nil {
		return GitSignals{}, err
	}
	return GitSignals{
		LandedObserved: extractLanded(rec, repoDir),
		ScopeObserved:  extractScope(rec, repoDir),
	}, nil
}

// assertGitRepo confirms repoDir is inside a git work tree. A non-repo is the
// one unexpected failure ExtractGitSignals reports as an error.
func assertGitRepo(repoDir string) error {
	out, err := runGit(repoDir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("scoring: %s is not a git repository: %w", repoDir, err)
	}
	if strings.TrimSpace(out) != "true" {
		return fmt.Errorf("scoring: %s is not a git work tree", repoDir)
	}
	return nil
}

// --- landed ----------------------------------------------------------------

// extractLanded computes the objective `landed` signal — see rubric section 1.
//
//   - empty rec.Commit                       → absent
//   - SHA resolves, reachable, not reverted  → 1.0
//   - SHA resolves, reachable, reverted      → 0.0
//   - SHA resolves but unreachable           → 0.0
//   - SHA does not resolve                   → message-match fallback, else absent
func extractLanded(rec IterationRecord, repoDir string) SignalValue {
	raw := strings.TrimSpace(rec.Commit)
	if raw == "" {
		return AbsentSignal("no commit SHA on iteration entry")
	}

	sha, ok := resolveCommit(repoDir, raw)
	if !ok {
		// v1 entries carry abbreviated SHAs from since-rebased history.
		// Best-effort: find a master commit whose change is the same work by
		// matching the recorded summary against commit subjects.
		if fallback, found := matchByMessage(repoDir, rec); found {
			if reverted, who := revertedAfter(repoDir, fallback); reverted {
				return PresentSignal(0.0, fmt.Sprintf("unresolved SHA %s; message-matched %s, later reverted by %s", short(raw), short(fallback), short(who)))
			}
			return PresentSignal(1.0, fmt.Sprintf("unresolved SHA %s; message-matched %s reachable from %s", short(raw), short(fallback), trunkRef))
		}
		return AbsentSignal(fmt.Sprintf("commit SHA %s does not resolve and no message match", short(raw)))
	}

	if !isAncestorOfTrunk(repoDir, sha) {
		return PresentSignal(0.0, fmt.Sprintf("commit %s not reachable from %s", short(sha), trunkRef))
	}
	if reverted, who := revertedAfter(repoDir, sha); reverted {
		return PresentSignal(0.0, fmt.Sprintf("commit %s reachable but reverted by %s", short(sha), short(who)))
	}
	return PresentSignal(1.0, fmt.Sprintf("commit %s reachable from %s, not reverted", short(sha), trunkRef))
}

// resolveCommit resolves a (possibly abbreviated) commit-ish to its full SHA.
// The second return is false when the ref does not name an existing commit —
// not an error, just an absent objective signal.
func resolveCommit(repoDir, ref string) (string, bool) {
	// ^{commit} forces the ref to denote a commit object, so a tag or tree
	// with a colliding abbreviation cannot masquerade as the iteration commit.
	out, err := runGit(repoDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", false
	}
	sha := strings.TrimSpace(out)
	return sha, sha != ""
}

// isAncestorOfTrunk reports whether sha is reachable from the trunk branch.
func isAncestorOfTrunk(repoDir, sha string) bool {
	_, err := runGit(repoDir, "merge-base", "--is-ancestor", sha, trunkRef)
	// merge-base --is-ancestor exits 0 when sha is an ancestor, 1 when not.
	return err == nil
}

// minAbbrevLen is the shortest hex run in a revert message treated as a
// candidate commit reference. Git's default abbreviation is 7; anything shorter
// is too collision-prone to trust as a SHA reference.
const minAbbrevLen = 7

// unit / record separators for the multi-field `git log` formats below. %H is a
// fixed 40-hex-digit field, so a record splits cleanly into SHA and remainder
// at unitSep without an ambiguity check.
const (
	unitSep   = "\x1f"
	recordSep = "\x1e"
)

// logRecord is one parsed `git log` record: the commit SHA and the remaining
// formatted field (a subject or a full body, per the format string used).
type logRecord struct {
	sha   string
	field string
}

// parseLogRecords splits the output of a `git log --format=%H<unitSep>…<recordSep>`
// invocation into structured records, dropping the empty trailing element the
// record separator leaves behind.
func parseLogRecords(out string) []logRecord {
	var recs []logRecord
	for _, raw := range strings.Split(out, recordSep) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		sha, field, _ := strings.Cut(raw, unitSep)
		recs = append(recs, logRecord{sha: strings.TrimSpace(sha), field: field})
	}
	return recs
}

// revertedAfter best-effort scans the trunk history after sha for a later
// commit whose message names sha as a revert. It returns the reverting commit's
// SHA when found. This is heuristic: a revert whose message was hand-edited to
// drop the SHA reference will be missed.
func revertedAfter(repoDir, sha string) (bool, string) {
	out, err := runGit(repoDir, "log", trunkRef, "--format=%H"+unitSep+"%B"+recordSep)
	if err != nil {
		return false, ""
	}
	for _, rec := range parseLogRecords(out) {
		if rec.sha == sha {
			// Reached the commit itself; nothing earlier in this list can
			// have reverted it (git log is newest-first).
			break
		}
		if !strings.Contains(strings.ToLower(rec.field), "revert") {
			continue
		}
		if bodyReferencesSHA(rec.field, sha) {
			return true, rec.sha
		}
	}
	return false, ""
}

// bodyReferencesSHA reports whether a commit message names sha — either in full
// or by any abbreviation of at least minAbbrevLen hex digits. Revert messages
// in this history abbreviate the reverted SHA to varying lengths, so a fixed
// abbreviation width would miss some.
func bodyReferencesSHA(body, sha string) bool {
	for _, tok := range strings.FieldsFunc(body, func(r rune) bool { return !isHexDigit(r) }) {
		if len(tok) >= minAbbrevLen && len(tok) <= len(sha) && strings.HasPrefix(sha, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

// isHexDigit reports whether r is a hexadecimal digit.
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// matchByMessage is the squashed/rebased-away fallback: when the verbatim SHA
// is gone, find a trunk commit whose subject equals the iteration's recorded
// impl summary. The summary is the most stable cross-rebase identifier the
// iteration log carries.
func matchByMessage(repoDir string, rec IterationRecord) (string, bool) {
	subject := strings.TrimSpace(rec.Impl.Summary)
	if subject == "" {
		return "", false
	}
	out, err := runGit(repoDir, "log", trunkRef, "--format=%H"+unitSep+"%s"+recordSep)
	if err != nil {
		return "", false
	}
	for _, lr := range parseLogRecords(out) {
		if strings.EqualFold(strings.TrimSpace(lr.field), subject) {
			return lr.sha, true
		}
	}
	return "", false
}

// --- scope -----------------------------------------------------------------

// extractScope computes the objective `scope` signal — see rubric section 5.
// The sub-score is the fraction of the commit's changed files that fall inside
// the task's declared write_scope. It is absent when either the commit cannot
// be resolved or no write_scope is resolvable for the task — in the latter case
// the assemble slice falls back to the self-reported scope_note.
func extractScope(rec IterationRecord, repoDir string) SignalValue {
	raw := strings.TrimSpace(rec.Commit)
	if raw == "" {
		return AbsentSignal("no commit SHA on iteration entry")
	}
	sha, ok := resolveCommit(repoDir, raw)
	if !ok {
		return AbsentSignal(fmt.Sprintf("commit SHA %s does not resolve", short(raw)))
	}

	scope, found := resolveWriteScope(repoDir, rec.TaskID)
	if !found {
		return AbsentSignal(fmt.Sprintf("no write_scope declared for task %q", rec.TaskID))
	}

	changed, err := changedFiles(repoDir, sha)
	if err != nil {
		return AbsentSignal(fmt.Sprintf("cannot read changed files for %s", short(sha)))
	}
	if len(changed) == 0 {
		// An empty commit declares no files; vacuously within scope.
		return PresentSignal(1.0, fmt.Sprintf("commit %s changed no files", short(sha)))
	}

	inScope := 0
	for _, f := range changed {
		if pathInScope(f, scope) {
			inScope++
		}
	}
	frac := float64(inScope) / float64(len(changed))
	return PresentSignal(frac, fmt.Sprintf("%d/%d changed files within declared write_scope of task %q", inScope, len(changed), rec.TaskID))
}

// changedFiles lists the files a commit touched, via `git show --name-only`.
func changedFiles(repoDir, sha string) ([]string, error) {
	out, err := runGit(repoDir, "show", "--name-only", "--format=", sha)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// pathInScope reports whether a changed file falls within a declared
// write_scope. A scope entry that ends in "/" is a directory prefix matching
// every file beneath it; any other entry is an exact file path. Both sides are
// slash-normalized so the comparison is OS-independent.
func pathInScope(file string, scope []string) bool {
	file = path.Clean(filepath.ToSlash(file))
	for _, entry := range scope {
		entry = filepath.ToSlash(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.HasSuffix(entry, "/") {
			prefix := path.Clean(strings.TrimSuffix(entry, "/"))
			if file == prefix || strings.HasPrefix(file, prefix+"/") {
				return true
			}
			continue
		}
		if file == path.Clean(entry) {
			return true
		}
	}
	return false
}

// resolveWriteScope best-effort resolves a task's declared write_scope from the
// canonical plan tree. It scans every .agents/workflow/plans/*/TASKS.yaml under
// repoDir for a task whose id equals taskID and carries a non-empty
// write_scope. The second return is false when no such task is found — true for
// most historical iterations, whose plans predate write_scope.
func resolveWriteScope(repoDir, taskID string) ([]string, bool) {
	if strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	matches, err := filepath.Glob(filepath.Join(repoDir, ".agents", "workflow", "plans", "*", "TASKS.yaml"))
	if err != nil {
		return nil, false
	}
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc tasksFile
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}
		for _, t := range doc.Tasks {
			if t.ID == taskID && len(t.WriteScope) > 0 {
				return t.WriteScope, true
			}
		}
	}
	return nil, false
}

// tasksFile is the minimal shape of a canonical plan TASKS.yaml — only the
// fields the scope resolver reads.
type tasksFile struct {
	Tasks []struct {
		ID         string   `yaml:"id"`
		WriteScope []string `yaml:"write_scope"`
	} `yaml:"tasks"`
}

// --- git plumbing ----------------------------------------------------------

// runGit runs `git -C repoDir args...` and returns its stdout. A non-zero exit
// is returned as an error carrying the captured stderr; callers decide whether
// that is genuinely unexpected or just an absent signal.
func runGit(repoDir string, args ...string) (string, error) {
	full := append([]string{"-C", repoDir}, args...)
	cmd := execabs.Command("git", full...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// short abbreviates a SHA for human-readable detail strings, leaving anything
// already short (or non-SHA) untouched.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
