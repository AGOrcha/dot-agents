// iter_log_index.go owns the iteration-log registry: an append-on-close index
// of the flat, global-sequential iteration log so per-plan traversal is
// O(index) instead of O(readdir-all-every-call), and archival can find a
// plan's iterations without re-scanning + parsing every record.
//
// The index is ADDITIVE. It is NEVER the source of next-N (NextIterationNumber
// stays a pure max-scan) and never the sole source of the plan→iterations
// mapping — every iter-N.yaml already carries its owning plan in the `wave`
// field, so a MISSING or partial index is a legal no-op: readers keep working
// and archival falls back to a wave-scan (the lazy rebuild). The file lives in
// the iteration-log dir alongside the iter-N.* artifacts, OUTSIDE the
// auto-managed commit roots, so committing it uses the same explicit-include
// seam (iterationCloseCommitWithIncludes) as the iter-logs themselves.
package workflow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/AGOrcha/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

// iterationIndexFileName is the JSONL index file (one entry per line) that
// sits beside the iter-N.* artifacts under .agents/active/iteration-log/.
const iterationIndexFileName = "index.jsonl"

// historyIterLogSubdir is the per-plan history subdirectory the archival move
// relocates a plan's iterations into: .agents/history/<plan>/iteration-log/.
const historyIterLogSubdir = "iteration-log"

// iterIndexEntry is one line of the iteration-log index: the minimal mapping a
// per-plan traversal needs. The sidecar file set (iter-N.hook-outcomes.yaml,
// iter-N.score.yaml) is intentionally NOT stored — it is derivable from N by
// convention (iterationArtifactPaths) and the score sidecar is written AFTER
// the checkpoint, so recording it at append time would be stale.
type iterIndexEntry struct {
	N         int    `json:"n"`
	PlanID    string `json:"plan_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// iterationIndexPath returns the canonical index path for a project's ACTIVE
// iteration log.
func iterationIndexPath(projectPath string) string {
	return filepath.Join(IterationLogDir(projectPath), iterationIndexFileName)
}

// loadIterationIndex reads the project's active index. A MISSING file is a
// legal no-op — it returns (nil, nil), the backward-compat contract that keeps
// every existing reader working when no index has ever been written.
func loadIterationIndex(projectPath string) ([]iterIndexEntry, error) {
	return loadIterationIndexAt(iterationIndexPath(projectPath))
}

// loadIterationIndexAt is the path-explicit variant used by the archival move
// (which reads both the active and the freshly-written history index).
func loadIterationIndexAt(path string) ([]iterIndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read iteration index %s: %w", path, err)
	}
	return parseIterationIndex(data, path)
}

// parseIterationIndex decodes JSONL bytes into entries. Blank lines are
// skipped; a malformed line is a hard error (the file exists and is expected
// to be well-formed once present).
func parseIterationIndex(data []byte, path string) ([]iterIndexEntry, error) {
	var entries []iterIndexEntry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e iterIndexEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parse iteration index %s: %w", path, err)
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan iteration index %s: %w", path, err)
	}
	return entries, nil
}

// entriesForPlan filters index entries to those owned by planID. An empty
// planID matches nothing — an unattributed iteration (wave "n/a" or "") is not
// owned by any plan and must never be swept into an archive.
func entriesForPlan(entries []iterIndexEntry, planID string) []iterIndexEntry {
	if planID == "" {
		return nil
	}
	var out []iterIndexEntry
	for _, e := range entries {
		if e.PlanID == planID {
			out = append(out, e)
		}
	}
	return out
}

// upsertIterationIndexEntry writes e into the project's active index: if an
// entry for e.N already exists it is REPLACED (checkpoints re-run for the same
// N across roles, so a blind append would duplicate the line); otherwise e is
// appended. The whole file is rewritten so the one-line-per-iteration invariant
// holds. The original created_at is preserved (first write wins) so the field
// records when the iteration first opened, not the latest role's pass.
func upsertIterationIndexEntry(projectPath string, e iterIndexEntry) error {
	path := iterationIndexPath(projectPath)
	entries, err := loadIterationIndexAt(path)
	if err != nil {
		return err
	}
	replaced := false
	for i := range entries {
		if entries[i].N != e.N {
			continue
		}
		if entries[i].CreatedAt != "" {
			e.CreatedAt = entries[i].CreatedAt
		}
		entries[i] = e
		replaced = true
		break
	}
	if !replaced {
		entries = append(entries, e)
	}
	return rewriteIterationIndexAt(path, entries)
}

// rewriteIterationIndex replaces the project's active index with entries.
func rewriteIterationIndex(projectPath string, entries []iterIndexEntry) error {
	return rewriteIterationIndexAt(iterationIndexPath(projectPath), entries)
}

// rewriteIterationIndexAt writes entries as JSONL to path, sorted ascending by
// N for a deterministic, human-scannable file.
func rewriteIterationIndexAt(path string, entries []iterIndexEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].N < entries[j].N })
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := jsonMarshal(e)
		if err != nil {
			return fmt.Errorf("marshal iteration index entry: %w", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := osMkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create iteration-log dir: %w", err)
	}
	if err := osWriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write iteration index %s: %w", path, err)
	}
	return nil
}

// iterationArtifactPaths returns the canonical file set for iteration N: the
// record plus its two optional sidecars, in a stable order. Callers stat each
// path and skip absentees — not every iteration has hook-outcomes or a score.
func iterationArtifactPaths(projectPath string, n int) []string {
	return []string{
		iterRecordPath(projectPath, n),
		hookOutcomeSidecarPath(projectPath, n),
		scoreSidecarPath(projectPath, n),
	}
}

// iterationsForPlan resolves the iteration numbers owned by planID, ascending.
// It prefers the index (O(index)); when the index is absent, unreadable, or
// names no entry for planID it falls back to scanning iter-*.yaml and reading
// each record's `wave` field (== plan_id, the same value the index stores).
// That fallback is the lazy rebuild that keeps archival correct under a
// missing/partial index.
func iterationsForPlan(projectPath, planID string) ([]int, error) {
	if planID == "" {
		return nil, nil
	}
	if entries, err := loadIterationIndex(projectPath); err == nil {
		if matched := entriesForPlan(entries, planID); len(matched) > 0 {
			ns := make([]int, 0, len(matched))
			for _, e := range matched {
				ns = append(ns, e.N)
			}
			sort.Ints(ns)
			return ns, nil
		}
	}
	return iterationsForPlanByWaveScan(projectPath, planID)
}

// iterationsForPlanByWaveScan reconstructs a plan's iteration set by reading
// the authoritative `wave` field from every iter-N.yaml — the lazy-rebuild
// path for a missing/partial index. A missing iteration-log dir yields nil.
func iterationsForPlanByWaveScan(projectPath, planID string) ([]int, error) {
	iterDir := IterationLogDir(projectPath)
	entries, err := nextIterReadDir(iterDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read iteration-log dir: %w", err)
	}
	var ns []int
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		m := iterLogFileRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		n, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			continue
		}
		if readIterRecordWave(filepath.Join(iterDir, de.Name())) == planID {
			ns = append(ns, n)
		}
	}
	sort.Ints(ns)
	return ns, nil
}

// readIterRecordWave reads just the `wave` field (the owning plan id) from an
// iter-N.yaml. Any read/parse failure yields "" so a single corrupt record
// cannot abort the wave-scan — it simply is not attributed to the plan.
func readIterRecordWave(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var rec struct {
		Wave string `yaml:"wave"`
	}
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return ""
	}
	return rec.Wave
}

// repoRelSlash converts an absolute path under projectPath into the
// repo-relative, forward-slashed form the commit seam names as an --include.
func repoRelSlash(projectPath, abs string) string {
	rel, err := filepath.Rel(projectPath, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// relocateIterationArtifacts moves iteration N's existing artifacts (record +
// optional sidecars) from the active iteration-log into dstDir and returns the
// repo-relative source-deletion and history-addition paths to name into the
// archive commit. Absent artifacts are skipped. In dry-run it prints the
// planned moves and mutates nothing (the returned paths are unused downstream).
func relocateIterationArtifacts(projectPath, dstDir string, n int, dryRun bool) ([]string, error) {
	var includes []string
	for _, src := range iterationArtifactPaths(projectPath, n) {
		if _, statErr := os.Stat(src); statErr != nil {
			continue
		}
		dst := filepath.Join(dstDir, filepath.Base(src))
		if dryRun {
			fmt.Printf("  [dry-run] move %s -> %s\n", config.DisplayPath(src), config.DisplayPath(dst))
		} else if err := osRename(src, dst); err != nil {
			return nil, fmt.Errorf("relocate %s: %w", filepath.Base(src), err)
		}
		includes = append(includes, repoRelSlash(projectPath, src), repoRelSlash(projectPath, dst))
	}
	return includes, nil
}

// archivePlanIterations relocates the iterations owned by planID from the
// active iteration-log into .agents/history/<plan>/iteration-log/, updates the
// active index (REMOVE-line: the active index mirrors the active log), writes a
// permanent per-plan history index, and returns the repo-relative paths to name
// into the archive commit via the explicit-include seam.
//
// Naming rationale: the source iter-N.* deletions AND the active index live
// OUTSIDE the auto-managed roots, so they stage under BOTH backends only when
// named. The history additions sit under the auto-managed .agents/history/ root
// (auto-staged in both modes); they are named too, which is harmless because
// DerivePathSet only picks named paths that actually appear in git status.
//
// dry-run performs no moves and mutates no index — it prints the planned moves
// and returns nil, so the caller's downstream commit (which never runs in
// dry-run) has nothing to consume.
func archivePlanIterations(projectPath, planID string, dryRun bool) ([]string, error) {
	ns, err := iterationsForPlan(projectPath, planID)
	if err != nil {
		return nil, err
	}
	if len(ns) == 0 {
		return nil, nil
	}

	// Load the active index once (tolerant: a corrupt or absent index degrades
	// to nil — ns already came from the wave-scan fallback in that case, and
	// the history index is synthesized from N + planID). It feeds both the
	// history-index metadata and the active-index remove-line rewrite.
	activeEntries, _ := loadIterationIndex(projectPath)
	idxByN := make(map[int]iterIndexEntry, len(activeEntries))
	for _, en := range activeEntries {
		idxByN[en.N] = en
	}

	dstDir := filepath.Join(historyBaseDir(projectPath), planID, historyIterLogSubdir)
	if !dryRun {
		if err := osMkdirAll(dstDir, 0o755); err != nil {
			return nil, fmt.Errorf("create history iteration-log dir: %w", err)
		}
	}

	moved := make(map[int]bool, len(ns))
	var includes []string
	var movedEntries []iterIndexEntry
	for _, n := range ns {
		inc, err := relocateIterationArtifacts(projectPath, dstDir, n, dryRun)
		if err != nil {
			return nil, err
		}
		includes = append(includes, inc...)
		moved[n] = true
		en := idxByN[n]
		en.N = n
		if en.PlanID == "" {
			en.PlanID = planID
		}
		movedEntries = append(movedEntries, en)
	}

	if dryRun {
		return nil, nil
	}

	indexIncludes, err := updateIndexesForArchive(projectPath, dstDir, activeEntries, moved, movedEntries)
	if err != nil {
		return nil, err
	}
	return append(includes, indexIncludes...), nil
}

// updateIndexesForArchive rewrites the active index without the moved
// iterations (only when it held entries — archival never materializes an index
// where none was, and a corrupt/absent index is left untouched) and writes the
// permanent per-plan history index. It returns the index paths to name into the
// archive commit.
func updateIndexesForArchive(projectPath, dstDir string, activeEntries []iterIndexEntry, moved map[int]bool, movedEntries []iterIndexEntry) ([]string, error) {
	var includes []string

	if len(activeEntries) > 0 {
		remaining := activeEntries[:0]
		for _, en := range activeEntries {
			if !moved[en.N] {
				remaining = append(remaining, en)
			}
		}
		if err := rewriteIterationIndex(projectPath, remaining); err != nil {
			return nil, err
		}
		includes = append(includes, repoRelSlash(projectPath, iterationIndexPath(projectPath)))
	}

	historyPath := filepath.Join(dstDir, iterationIndexFileName)
	if err := rewriteIterationIndexAt(historyPath, movedEntries); err != nil {
		return nil, err
	}
	includes = append(includes, repoRelSlash(projectPath, historyPath))
	return includes, nil
}
