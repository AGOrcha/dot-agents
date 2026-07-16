package platform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"go.yaml.in/yaml/v3"
)

type codex struct {
	io platformIO
}

const (
	codexAgentsDir      = ".agents"
	codexDir            = ".codex"
	codexConfigTOML     = "config.toml"
	codexHooksJSON      = "hooks.json"
	codexAgentsMarkdown = "AGENTS.md"
	codexAgentMDFile    = "AGENT.md"
)

// codexManagedTomlMarker is the durable provenance header written as the FIRST
// line of every dot-agents-rendered codex agent `.toml`. It is a TOML comment
// (codex ignores it) that lets the projection layer PROVE ownership before it
// overwrites or prunes a `.toml`: a file lacking this exact first line is
// treated as user-authored/foreign and is never replaced or deleted (defect 1
// — ReplaceIfManaged + managed-only prune, fail closed). Bump the version
// suffix only if the render format changes in a way that must invalidate old
// managed files.
const codexManagedTomlMarker = "# dot-agents:managed-render v1 (generated from AGENT.md; local edits are overwritten on refresh)"

// isManagedCodexTomlBytes reports whether data's first line is exactly the
// managed-render marker (tolerating a trailing CR for CRLF files).
func isManagedCodexTomlBytes(data []byte) bool {
	first := data
	if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
		first = data[:nl]
	}
	return strings.TrimRight(string(first), "\r") == codexManagedTomlMarker
}

// isManagedCodexToml reports whether the file at path is a dot-agents managed
// rendered codex toml (a REGULAR file whose first line is the provenance
// marker). A path that does not exist returns (false, nil). A symlink, dir, or
// any non-regular occupant returns (false, nil) — codex renders regular files
// only, so anything else is not ours. A real read/stat error OTHER than
// not-exist propagates so a permission fault is never silently downgraded to
// "not managed" (which could green-light overwriting a user file or skip a
// needed prune, defect 4).
func isManagedCodexToml(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return isManagedCodexTomlBytes(data), nil
}

// IsManagedCodexAgentTomlFile reports whether path is a dot-agents managed
// rendered codex agent `.toml` (carries codexManagedTomlMarker as its first
// line). Exported for commands/import.go (t2c): the project-scope import
// scan already walks `.codex/agents` (projectImportWalkDirs) and needs to
// skip re-importing dot-agents' OWN renders as if they were foreign user
// content — commands already imports internal/platform, so this is the one
// cycle-free direction for import.go to consult codex's provenance check.
func IsManagedCodexAgentTomlFile(path string) (bool, error) {
	return isManagedCodexToml(path)
}

func NewCodex() Platform { return &codex{io: stdPlatformIO{}} }

func (c *codex) ID() string          { return "codex" }
func (c *codex) DisplayName() string { return "Codex CLI" }

// SessionReader implementation.
// CODEX_SESSION_ID: not yet confirmed from Codex docs; update SessionEnvs when verified.
// ResolveModel: scans ~/.codex/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl for the model field.
func (c *codex) AIAgentPrefix() string    { return "codex" }
func (c *codex) SessionEnvs() []string    { return []string{"CODEX_SESSION_ID"} }
func (c *codex) EntrypointEnvs() []string { return nil }
func (c *codex) ResolveModel(home, _ /* projectPath */, sessionID string) string {
	return resolveCodexModelFromJSONL(home, sessionID)
}

// StatsReader implementation.
func (c *codex) ReadUsageStats(home string) *PlatformUsageStats {
	return codexReadUsageStats(home)
}

func codexReadUsageStats(home string) *PlatformUsageStats {
	indexPath := filepath.Join(home, codexDir, "session_index.jsonl")
	f, err := os.Open(indexPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	type entry struct {
		ID         string `json:"id"`
		ThreadName string `json:"thread_name"`
		UpdatedAt  string `json:"updated_at"`
	}
	var all []entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
			all = append(all, e)
		}
	}
	if len(all) == 0 {
		return nil
	}
	stats := &PlatformUsageStats{
		PlatformID:    "codex",
		TotalSessions: len(all),
	}
	start := 0
	if len(all) > 10 {
		start = len(all) - 10
	}
	for _, e := range all[start:] {
		stats.RecentSessions = append(stats.RecentSessions, SessionSummary{
			ID:        e.ID,
			Name:      e.ThreadName,
			UpdatedAt: e.UpdatedAt,
		})
	}
	return stats
}

// SessionTokenScanner implementation.
func (c *codex) ScanSessionTokens(home, _ /* projectPath */, sessionID, afterTimestamp string) SessionTokenMetrics {
	return codexScanSessionTokens(home, sessionID, afterTimestamp)
}

func (c *codex) IsInstalled() bool {
	return probeInstalled("codex")
}

func (c *codex) Version() string {
	return probeVersionLine("codex")
}

func (c *codex) HasDeprecatedFormat(repoPath string) bool { return false }
func (c *codex) DeprecatedDetails(repoPath string) string { return "" }

func (c *codex) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.ensureUserAgents(agentsHome); err != nil {
		return err
	}
	if err := c.ensureUserSkills(agentsHome); err != nil {
		return err
	}

	// AGENTS.md: global then project override
	if err := c.linkCodexAgentsMD(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .codex/config.toml
	if err := c.linkCodexConfigToml(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project agents → .codex/agents/*.toml (rendered by CollectAndExecuteSharedTargetPlan)
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project skills → .agents/skills/
	if err := c.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project hooks → .codex/hooks.json
	if err := c.createHooksLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

// linkCodexAgentsMD points the owned repo AGENTS.md at the highest-priority
// canonical source: the first existing global candidate, then a project
// override if present (the override symlink-replaces the global). Every
// links.SymlinkReplacing error propagates unchanged — this is the
// link-error-propagation contract added by a prior remediation.
func (c *codex) linkCodexAgentsMD(project, repoPath, agentsHome string) error {
	dst := filepath.Join(repoPath, codexAgentsMarkdown)
	globalCandidates := []string{
		filepath.Join(agentsHome, "rules", "global", "agents.md"),
		filepath.Join(agentsHome, "rules", "global", "agents.mdc"),
		filepath.Join(agentsHome, "rules", "global", "rules.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.mdc"),
	}
	if err := linkFirstResolvedAgentsCandidate(globalCandidates, dst); err != nil {
		return err
	}
	// Project override: symlink-replaces the global link above when present.
	projectCandidates := []string{
		filepath.Join(agentsHome, "rules", project, "agents.md"),
		filepath.Join(agentsHome, "rules", project, "agents.mdc"),
	}
	return linkFirstResolvedAgentsCandidate(projectCandidates, dst)
}

// linkFirstResolvedAgentsCandidate symlink-replaces dst with the first
// candidate that resolves to a real file, skipping a legitimately absent
// candidate (fsops.StatAllowMissing found=false, err=nil) and continuing the
// search. A real Stat error (permission denied, I/O failure, ...) aborts the
// search immediately and propagates — it must never be treated as "this
// candidate doesn't exist" and silently skipped. Managed-replace: a stale
// managed symlink is idempotently re-pointed; a genuine user-authored
// AGENTS.md is preserved as AGENTS.md.dot-agents-backup, never silently
// destroyed.
func linkFirstResolvedAgentsCandidate(candidates []string, dst string) error {
	for _, src := range candidates {
		_, found, err := fsops.StatAllowMissing(src)
		if err != nil {
			return err
		}
		if found {
			return links.SymlinkReplacing(src, dst, backupSidecar)
		}
	}
	return nil
}

// linkCodexConfigToml ensures .codex/ exists and symlink-replaces
// .codex/config.toml with the scoped codex.toml when one resolves. Link
// errors propagate unchanged.
func (c *codex) linkCodexConfigToml(project, repoPath, agentsHome string) error {
	if err := c.io.MkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, "codex.toml"); src != "" {
		// Managed-replace at a fixed owned path (.codex/config.toml).
		if err := links.SymlinkReplacing(src, filepath.Join(repoPath, codexDir, codexConfigTOML), backupSidecar); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	_, found, err := fsops.StatAllowMissing(globalAgents)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, codexDir, "agents")
		if err := c.io.MkdirAll(userAgentsDir, 0755); err != nil {
			continue
		}
		if err := c.writeCodexAgents(agentsHome, "global", userAgentsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) ensureUserSkills(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userSkillsDir := filepath.Join(homeRoot, codexAgentsDir, "skills")
		if err := syncScopedDirSymlinks(c.io, agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) createAgentsLinks(project, repoPath, agentsHome string) error {
	// Codex `.codex/agents/*.toml` (local-authored AND sourced) are BOTH
	// rendered and PRUNED by the exact projection: the render intents come
	// from SharedTargetIntents (BuildSharedCodexAgentTomlIntents) +
	// SourcedAgentFileIntents, and the exact/prune pass reaps stale managed
	// renders via ManagedRenderProjector (resource_plan.go). CreateLinks does
	// NOT prune here anymore: it has no caller-unit set, so a CreateLinks-time
	// prune could only re-derive "wanted" from the local canonical bucket and
	// would wrongly delete a freshly-projected SOURCED render (defect 3/4).
	// The exact projection runs before CreateLinks in every install/refresh
	// flow, so pruning is already done by this point.
	return nil
}

func (c *codex) createSkillsLinks(project, repoPath, _ string) error {
	return nil
}

func (c *codex) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := c.writeRepoHooks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return c.writeUserHomeHooks(project, agentsHome)
}

func (c *codex) writeRepoHooks(project, repoPath, agentsHome string) error {
	repoTarget := filepath.Join(repoPath, codexDir, codexHooksJSON)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err != nil {
		return err
	}
	if err := c.io.MkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	spec, err := resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json")
	if err != nil {
		return err
	}
	return emitPreferredHookFile(
		c.io,
		repoTarget,
		renderCodexHookConfig,
		spec,
		directSymlinkHookMode,
		func(p string) error { return removeRenderedCodexHookConfig(c.io, p) },
		repoBundles,
	)
}

func (c *codex) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	spec, err := resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		c.io,
		filepath.Join(codexDir, codexHooksJSON),
		renderCodexHookConfig,
		spec,
		directSymlinkHookMode,
		func(p string) error { return removeRenderedCodexHookConfig(c.io, p) },
		globalBundles,
	)
}

func (c *codex) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	var errs []error
	errs = append(errs,
		links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexAgentsMarkdown), agentsHome),
		links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, codexConfigTOML), agentsHome),
	)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(c.io, repoBundles, filepath.Join(repoPath, codexDir, codexHooksJSON), renderCodexHookConfig)
	}
	errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, codexHooksJSON), agentsHome))

	errs = append(errs, c.pruneManagedCodexAgentTomls(filepath.Join(repoPath, codexDir, "agents")))

	skillsDir := filepath.Join(repoPath, codexAgentsDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome))
		}
	}

	return errors.Join(errs...)
}

// writeCodexAgents renders each canonical AGENT.md as a `.toml` under dstRoot
// and prunes stale tomls. ENOENT on the canonical agents bucket is a no-op;
// other errors propagate.
func (c *codex) writeCodexAgents(agentsHome, scope, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, codexAgentMDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wanted := map[string]bool{}
	for _, entry := range entries {
		wanted[entry.Name+".toml"] = true
		dst := filepath.Join(dstRoot, entry.Name+".toml")
		if err := c.writeCodexAgentToml(dst, entry.File); err != nil {
			return err
		}
	}
	existing, err := os.ReadDir(dstRoot)
	if err != nil {
		// Absent dstRoot is a no-op (nothing rendered anything into it); a
		// present-but-unlistable path is a real fault to surface.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("listing codex agents dir %s: %w", dstRoot, err)
	}
	var errs []error
	for _, e := range existing {
		if !strings.HasSuffix(e.Name(), ".toml") || wanted[e.Name()] {
			continue
		}
		// Ownership-gated prune (defect 1): only a dot-agents managed render is
		// removable — a user-authored `.toml` sibling is left intact.
		candidate := filepath.Join(dstRoot, e.Name())
		isManaged, provErr := isManagedCodexToml(candidate)
		if provErr != nil {
			errs = append(errs, fmt.Errorf("codex toml provenance %s: %w", candidate, provErr))
			continue
		}
		if !isManaged {
			continue
		}
		if err := c.io.Remove(candidate); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("prune managed codex toml %s: %w", candidate, err))
		}
	}
	return errors.Join(errs...)
}

// pruneManagedCodexAgentTomls removes EVERY dot-agents managed rendered
// `.toml` under dstRoot (RemoveLinks teardown). Ownership is proven per-file
// via isManagedCodexToml, so a user-authored `.toml` sibling is never deleted
// (defect 1), and the wanted/pruned set is derived from on-disk provenance —
// NOT from a fallible lock read (defect 4). A missing dstRoot is a no-op; a
// present-but-unlistable dstRoot, a provenance-read fault, or a removal fault
// are aggregated and surfaced rather than silently swallowed (false
// convergence, defect 4).
func (c *codex) pruneManagedCodexAgentTomls(dstRoot string) error {
	existing, err := os.ReadDir(dstRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("listing codex agents dir %s: %w", dstRoot, err)
	}
	var errs []error
	for _, e := range existing {
		if !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		candidate := filepath.Join(dstRoot, e.Name())
		isManaged, provErr := isManagedCodexToml(candidate)
		if provErr != nil {
			errs = append(errs, fmt.Errorf("codex toml provenance %s: %w", candidate, provErr))
			continue
		}
		if !isManaged {
			continue
		}
		if err := c.io.Remove(candidate); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("prune managed codex toml %s: %w", candidate, err))
		}
	}
	return errors.Join(errs...)
}

// writeCodexAgentTomlFile renders agentMD to a codex `.toml` at dst and writes
// it with durable managed provenance (codexManagedTomlMarker as the first
// line), enforcing ResourceReplaceIfManaged and writing atomically (defect 1):
//
//   - An existing occupant that is NOT a dot-agents managed render is
//     content-aware (t2c, replacing the unconditional refuse — fold-back
//     obs-codex-toml-marker-upgrade-migration): a REGULAR FILE whose bytes
//     match the render body byte-for-byte is provably a prior dot-agents
//     output written before the marker existed (the marker-upgrade migration
//     collision every existing install hits on its first post-marker
//     refresh) and is silently mark-adopted — see
//     resolveUnmanagedCodexTomlCollision. A REGULAR FILE whose bytes diverge
//     is genuine foreign/user content: it is preserved non-destructively at
//     a sibling alternate path (RFC §6 stable naming) plus an
//     import-conflict review note, and the SAME on-disk "agents" bucket
//     project-scope walk `da import`/`da refresh` already runs
//     (projectImportWalkDirs' ".codex/agents" entry, commands/import.go)
//     picks the preserved sibling up as an ordinary import candidate on the
//     next pass — reusing the existing import-to-scope machinery rather
//     than a parallel one. A non-regular occupant (symlink, dir, device) is
//     never a render candidate and keeps the original fail-closed refuse.
//   - An absent path, or an existing managed render, is (re)written. The write
//     is a same-dir temp file + rename, so a concurrent reader never sees a
//     partial `.toml` (the prior Lstat-then-truncate-write could).
func writeCodexAgentTomlFile(io platformIO, dst, agentMD string) error {
	content, err := renderCodexAgentToml(agentMD)
	if err != nil {
		return err
	}
	managed := append([]byte(codexManagedTomlMarker+"\n"), content...)
	if err := io.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	isManaged, err := isManagedCodexToml(dst)
	if err != nil {
		return fmt.Errorf("codex toml provenance check %s: %w", dst, err)
	}
	if !isManaged {
		// Distinguish "absent" (fine, create it) from "present but not ours"
		// (content-aware collision resolution, never a silent clobber).
		info, statErr := os.Lstat(dst)
		switch {
		case statErr == nil:
			if err := resolveUnmanagedCodexTomlCollision(io, dst, content, info); err != nil {
				return err
			}
		case !os.IsNotExist(statErr):
			return fmt.Errorf("codex toml occupant check %s: %w", dst, statErr)
		}
	} else {
		// Perf (package-artifact-install t9): a verified managed render is ours
		// to replace, but the steady-state re-run (every unchanged `da install`
		// / `da refresh`) regenerates byte-identical content for every sourced
		// agent every time — profiling a 200-agent warm re-projection showed
		// this remove+write+rename triplet as the dominant cost (docs/
		// PERF_BUDGET.md). A full byte compare against the CURRENT on-disk
		// managed render — not a cheap mtime/size shortcut — skips the rewrite
		// only when content is provably unchanged, so a genuinely stale render
		// (source content changed) still regenerates correctly.
		existing, readErr := os.ReadFile(dst)
		if readErr == nil && bytes.Equal(existing, managed) {
			return nil
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("codex toml: reading prior managed render %s: %w", dst, readErr)
		}
		// A verified managed render is ours to replace; remove it first so the
		// atomic rename below lands on an absent path (Windows os.Rename cannot
		// replace an existing file). This unlink is safe: ownership was just
		// proven, and the removed bytes are a regenerable render.
		if err := io.Remove(dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("codex toml: removing prior managed render %s: %w", dst, err)
		}
	}
	tmp := dst + ".da-toml-tmp"
	if err := io.WriteFile(tmp, managed, 0644); err != nil {
		return err
	}
	if err := fsops.Rename(tmp, dst); err != nil {
		_ = io.Remove(tmp)
		return fmt.Errorf("codex toml: atomic rename %s: %w", dst, err)
	}
	return nil
}

func (c *codex) writeCodexAgentToml(dst, agentMD string) error {
	return writeCodexAgentTomlFile(c.io, dst, agentMD)
}

// resolveUnmanagedCodexTomlCollision handles an existing occupant at dst
// (Lstat already succeeded, info describes it) that lacks the managed-render
// marker, replacing t2b's unconditional fail-closed refuse with content-aware
// adoption (t2c, fold-back obs-codex-toml-marker-upgrade-migration):
//
//   - Not a regular file (symlink, dir, device, ...): never a render
//     candidate — refuse unchanged, leaving the occupant completely intact.
//   - A regular file whose bytes equal content (the render body dst is about
//     to receive, marker not yet applied): provably OUR prior output written
//     before the marker existed — return nil so the caller (re)writes the
//     now-marked bytes over it. No data changes; no error.
//   - A regular file whose bytes diverge from content: genuine foreign/user
//     content. It is preserved non-destructively at a sibling alternate path
//     in the same directory (RFC §6 stable naming, resource-intent-
//     centralization) and an import-conflict review note is written, then
//     nil is returned so the caller writes the managed render at the
//     canonical name. The occupant's bytes are never lost — either they
//     already equal what's about to be written, or they survive at the
//     alternate path.
func resolveUnmanagedCodexTomlCollision(io platformIO, dst string, content []byte, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite %s: not a dot-agents managed render (user-authored or foreign file) — leaving it intact", dst)
	}
	existing, err := os.ReadFile(dst)
	if err != nil {
		return fmt.Errorf("codex toml occupant read %s: %w", dst, err)
	}
	if bytes.Equal(existing, content) {
		// Byte-identical to the render body: this occupant is provably a
		// dot-agents render from before codexManagedTomlMarker existed (the
		// render format itself is unchanged — the marker was added as a new
		// FIRST line on top of it), so no marker-stripping/normalization is
		// needed on either side of this comparison. Adopt silently.
		return nil
	}
	return preserveDivergedCodexToml(io, dst, existing)
}

// preserveDivergedCodexToml moves a diverged, unmanaged occupant at dst to a
// sibling alternate path in the same directory (never deletes it), then
// writes an import-conflict review note recording both paths so a human can
// reconcile. The vacated dst then lets writeCodexAgentTomlFile's caller
// proceed to write the managed render at the canonical name. A review-note
// write failure is logged and does not block the (already non-destructive)
// preserve-and-adopt — the diverged bytes are safely on disk either way.
func preserveDivergedCodexToml(io platformIO, dst string, existing []byte) error {
	altPath := nextCodexPreexistingAltPath(dst)
	if err := io.WriteFile(altPath, existing, 0644); err != nil {
		return fmt.Errorf("codex toml: preserving diverged occupant %s: %w", dst, err)
	}
	if err := io.Remove(dst); err != nil && !os.IsNotExist(err) {
		_ = io.Remove(altPath)
		return fmt.Errorf("codex toml: vacating diverged occupant %s: %w", dst, err)
	}
	if err := writeCodexImportConflictReviewNote(dst, altPath); err != nil {
		ui.Warn(fmt.Sprintf("codex toml: could not write import-conflict review note for %s: %v", dst, err))
	}
	return nil
}

// nextCodexPreexistingAltPath returns the first free sibling path in dst's
// directory named <name>.codex-preexisting.toml, then
// <name>.codex-preexisting-2.toml, -3, … (RFC §6 stable naming, mirroring
// commands/import.go's importConflictStableBundleName sequence for hooks).
func nextCodexPreexistingAltPath(dst string) string {
	dir := filepath.Dir(dst)
	base := strings.TrimSuffix(filepath.Base(dst), ".toml")
	candidate := filepath.Join(dir, base+".codex-preexisting.toml")
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for n := 2; ; n++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s.codex-preexisting-%d.toml", base, n))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// codexImportConflictReviewNote mirrors the on-disk shape of
// commands.importConflictReviewNote (resource-intent-centralization RFC §6,
// commands/import.go) byte-for-byte on the YAML tags: internal/platform
// cannot import package commands (commands already imports internal/platform,
// so the reverse would cycle), so the shape is duplicated here rather than
// shared — a review note under ~/.agents/review-notes/import-conflicts/
// looks identical to an operator regardless of which package wrote it.
type codexImportConflictReviewNote struct {
	ID               string   `yaml:"id"`
	Status           string   `yaml:"status"`
	Kind             string   `yaml:"kind"`
	Bucket           string   `yaml:"bucket"`
	Scope            string   `yaml:"scope"`
	LogicalName      string   `yaml:"logical_name"`
	CanonicalTarget  string   `yaml:"canonical_target"`
	AlternateTarget  string   `yaml:"alternate_target"`
	Origin           string   `yaml:"origin"`
	Rationale        string   `yaml:"rationale,omitempty"`
	SuggestedActions []string `yaml:"suggested_actions,omitempty"`
	CreatedAt        string   `yaml:"created_at"`
}

// writeCodexImportConflictReviewNote writes the review note for a diverged
// unmanaged `.codex/agents/*.toml` collision. dst and altPath are recorded
// as-is (absolute paths) — writeCodexAgentTomlFile is called with only the
// materialized dst path (t2c intentionally leaves resource_plan.go's caller
// contract untouched), so a project-relative path is not reliably derivable
// here without re-deriving project identity from config; the absolute path
// is still a precise, actionable pointer for the human reviewing the note.
func writeCodexImportConflictReviewNote(dst, altPath string) error {
	agentsHome := config.AgentsHome()
	dir := filepath.Join(agentsHome, "review-notes", "import-conflicts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	id := fmt.Sprintf("ic-%d", time.Now().UnixNano())
	note := codexImportConflictReviewNote{
		ID:              id,
		Status:          "pending",
		Kind:            "unmarked_render_collision",
		Bucket:          "agents",
		Scope:           filepath.Dir(filepath.Dir(filepath.Dir(dst))),
		LogicalName:     strings.TrimSuffix(filepath.Base(dst), ".toml"),
		CanonicalTarget: dst,
		AlternateTarget: altPath,
		Origin:          "codex",
		Rationale:       "A pre-existing .codex/agents render collided with a dot-agents managed render at install/refresh time and lacked the managed-provenance marker (codexManagedTomlMarker); the diverged content was preserved at an alternate path per resource-intent-centralization RFC §6 rather than overwritten.",
		SuggestedActions: []string{
			"Compare canonical_target vs alternate_target and reconcile manually if the preserved content should be kept.",
			"Delete the alternate file after merging if it is redundant — a subsequent `da import` also picks it up as an ordinary project-scope import candidate.",
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := yaml.Marshal(&note)
	if err != nil {
		return err
	}
	fn := filepath.Join(dir, id+".yaml")
	return os.WriteFile(fn, append(data, '\n'), 0644)
}

func renderCodexAgentToml(agentMD string) ([]byte, error) {
	meta := readFrontmatter(agentMD)
	body, err := readAgentBody(agentMD)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(filepath.Dir(agentMD)), string(filepath.Ext(agentMD)))
	}
	description := strings.TrimSpace(meta["description"])
	model := strings.TrimSpace(meta["model"])
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", strconv.Quote(name))
	fmt.Fprintf(&b, "description = %s\n", strconv.Quote(description))
	if model != "" {
		fmt.Fprintf(&b, "model = %s\n", strconv.Quote(model))
	}
	if strings.TrimSpace(body) != "" {
		b.WriteString("developer_instructions = ")
		b.WriteString(tomlMultilineString(body))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

func readAgentBody(agentMD string) (string, error) {
	data, err := os.ReadFile(agentMD)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return text, nil
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		return text, nil
	}
	body := rest[end+len("\n---\n"):]
	body = strings.TrimLeft(body, "\n")
	return body, nil
}

func tomlMultilineString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"""`, `\"\"\"`)
	return "\"\"\"\n" + escaped + "\n\"\"\""
}

// BrokenLinks implements BrokenLinkReporter for the codex platform.
//
// Codex's project-scope single-file managed surface is AGENTS.md at the repo
// root — a managed symlink to the highest-priority canonical source under
// <agentsHome>/rules/{global,project}/agents.(md|mdc) (see linkCodexAgentsMD).
// The diagnostic contract carried over from doctor's previous inline
// projectSingleFiles table (collectSingleFileBrokenLinks → managedLinkBroken)
// is "report broken only when the entry is a resolvable managed link AND its
// target is missing" — a hard-linked or absent AGENTS.md is silently passed
// over. classifyManagedLink encodes that semantic exactly, matching claude's
// brokenMCPLink shape.
//
// ScanSingleFileLinks is intentionally NOT used here for the same reason
// documented on claude.brokenMCPLink: its hard-link-only canonical contract
// would flag a symlink-only managed AGENTS.md as broken, which would shift
// the existing doctor_test expectations (TestCollectBrokenLinks_BrokenAgentsMD
// is the central pin).
//
// PlatformID is set on the returned BrokenLink so JSON consumers can
// self-describe per-entry (BrokenLink struct contract).
func (c *codex) BrokenLinks(_, repoPath, _ string) []BrokenLink {
	return classifyCodexSingleFile(filepath.Join(repoPath, codexAgentsMarkdown))
}

// classifyCodexSingleFile returns the broken-link record for a single codex
// managed file path, or nil when the path is not a resolvable managed link
// or its target exists. Extracted from BrokenLinks for symmetry with claude's
// brokenMCPLink and to keep the per-platform helper trivial enough that P3's
// CountLinks/Badge migration can reuse the shape without refactor.
func classifyCodexSingleFile(linkPath string) []BrokenLink {
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		return nil
	}
	return []BrokenLink{{
		PlatformID:  "codex",
		LinkPath:    linkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
	}}
}

// PrintAudit implements AuditPrinter for the codex platform: it renders the
// AGENTS.md link, the .codex/config.toml + .codex/hooks.json links, the
// shared .agents/skills/ mirror, and the native .codex/agents/ TOML entries.
// Moved verbatim (output preserved) from the lifecycle-side printCodexAudit
// in Phase 5.
func (c *codex) PrintAudit(w io.Writer, _, repoPath, _ string) {
	fmt.Fprintf(w, "    %sCodex%s\n", ui.Cyan, ui.Reset)
	codexPrintAgentsMD(w, filepath.Join(repoPath, codexAgentsMarkdown))
	codexPrintSymlinkAudit(w, filepath.Join(repoPath, codexDir, codexConfigTOML), ".codex/config.toml")
	codexPrintSymlinkAudit(w, filepath.Join(repoPath, codexDir, codexHooksJSON), ".codex/hooks.json")
	codexPrintSkillsAudit(w, filepath.Join(repoPath, codexAgentsDir, "skills"))
	codexPrintAgentsAudit(w, filepath.Join(repoPath, codexDir, "agents"))
	fmt.Fprintln(w)
}

// codexPrintAgentsMD renders the AGENTS.md link/local-file/absent status to w.
func codexPrintAgentsMD(w io.Writer, path string) {
	if _, err := os.Lstat(path); err == nil {
		if state, _ := classifyManagedLink(path); state != linkStateNotALink {
			printLinkedStatusLine(w, codexAgentsMarkdown, path)
			return
		}
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, codexAgentsMarkdown, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(w, "      %s(no %s)%s\n", ui.Dim, codexAgentsMarkdown, ui.Reset)
}

// codexPrintSymlinkAudit renders a single codex managed-file link to w. A
// present-but-not-a-link path is a rendered/managed file on disk (e.g.
// .codex/hooks.json, .codex/config.toml), not an absent link; "(not linked)"
// is reserved for a truly absent path.
func codexPrintSymlinkAudit(w io.Writer, path, label string) {
	if state, _ := classifyManagedLink(path); state != linkStateNotALink {
		printLinkedStatusLine(w, label, path)
		return
	}
	if _, err := os.Lstat(path); err == nil {
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(w, "      %s-%s %s %s(not linked)%s\n", ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
}

// codexPrintSkillsAudit renders the shared .agents/skills/ mirror entries to w.
func codexPrintSkillsAudit(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if state, _ := classifyManagedLink(linkPath); state == linkStateNotALink {
			continue
		}
		if printLinkedStatusLine(w, ".agents/skills/"+entry.Name(), linkPath) {
			okCount++
		} else {
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(w, "      %s○%s .agents/skills/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
}

// codexPrintAgentsAudit renders the native .codex/agents/ TOML entries to w.
func codexPrintAgentsAudit(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(linkPath); err == nil {
			fmt.Fprintf(w, "      %s✓%s .codex/agents/%s %s(native TOML)%s\n", ui.Green, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			okCount++
		} else {
			fmt.Fprintf(w, "      %s✗%s .codex/agents/%s %s(unreadable)%s\n", ui.Red, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(w, "      %s○%s .codex/agents/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
}

func (c *codex) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(codexAgentsDir, "skills"))
	if err != nil {
		return nil, err
	}
	tomls, err := BuildSharedCodexAgentTomlIntents(project)
	if err != nil {
		return nil, err
	}
	return append(skills, tomls...), nil
}

// DirMirrorRoots implements DirMirrorRootsProvider (resource_plan.go):
// codex's skills bucket is dir-mirror shaped; its agents bucket is
// FILE-shaped (rendered `.toml`, see SourcedAgentFileIntents) and is
// deliberately absent here.
func (c *codex) DirMirrorRoots() map[string][]string {
	return map[string][]string{"skills": {filepath.Join(codexAgentsDir, "skills")}}
}

// SourcedAgentFileIntents implements SourcedAgentFileProjector
// (resource_plan.go, t2b): codex's agents bucket renders a `.toml` per
// entry rather than symlinking a whole dir, so a sourced "agents"-family
// unit gets a RenderSingle/Write intent — NOT the casDirectOrigin marker
// buildCASAgentFileIntents uses, since that marker routes execution
// straight to the symlink swap in executeResourceIntent, before the
// shape/transport switch that would otherwise dispatch to
// executeRenderSingleWrite. The intent's SourceRef addresses the unit's
// CAS AGENT.md directly (mirroring buildCASIntents' CAS addressing), so
// executeRenderSingleWrite reads and renders it exactly like a local
// canonical AGENT.md. Pruning this shape on removal is NOT handled by the
// generic symlink-only exact/prune scan — it is reaped by the managed-RENDER
// prune (resource_plan.go pruneManagedRenders) via ManagedRenderProjector,
// which proves ownership per-file with IsManagedRender/isManagedCodexToml.
func (c *codex) SourcedAgentFileIntents(project string, units []ResolvedUnit) []ResourceIntent {
	intents := make([]ResourceIntent, 0, len(units))
	for _, u := range units {
		if u.Family != "agents" {
			continue
		}
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.sourced.codex-toml.%s.%s", sanitizeIntentRoot(u.SourceID), u.Name),
			Project:     project,
			Bucket:      "agents",
			LogicalName: u.Name,
			TargetPath:  filepath.Join(codexDir, "agents", u.Name+".toml"),
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        "artifacts",
				Bucket:       "cache",
				RelativePath: filepath.Join(u.Family, config.StoreDigestDir(u.Digest), agentManifestName),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       "sourced-codex-agent-toml",
			},
			Shape:         ResourceShapeRenderSingle,
			Transport:     ResourceTransportWrite,
			Materializer:  codexAgentTomlMaterializer,
			ReplacePolicy: ResourceReplaceIfManaged,
			PrunePolicy:   ResourcePruneNone,
		})
	}
	return intents
}

// ManagedRenderDir implements ManagedRenderProjector (resource_plan.go,
// defect 3): the repo-relative directory codex renders its `.toml` files into.
// The exact/prune pass forces this directory into its scan so a stale managed
// render is reaped uniformly with the symlink shapes — even on a one-to-zero
// pass where no render intent names it.
func (c *codex) ManagedRenderDir() string { return filepath.Join(codexDir, "agents") }

// IsManagedRender implements ManagedRenderProjector (resource_plan.go): proves
// per-file whether path is one of codex's managed rendered `.toml`s (carries
// the provenance marker), so the exact prune deletes ONLY dot-agents renders
// and never a user-authored `.toml` in the same directory.
func (c *codex) IsManagedRender(path string) (bool, error) { return isManagedCodexToml(path) }

// CountLinks implements LinkCounter for the codex platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter previously inlined in status.go's
// codexTextBadge.
//
// Healthy: AGENTS.md, .codex/config.toml, .codex/hooks.json (when each is a
// resolvable managed link with a reachable target, or any present file —
// matching the historical addManagedCounts contract), plus any resolvable
// entry under .codex/agents/ or .agents/skills/. Broken: a resolvable
// managed link whose target is missing.
func (c *codex) CountLinks(_, repoPath, _ string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, codexAgentsMarkdown),
		filepath.Join(repoPath, codexDir, codexConfigTOML),
		filepath.Join(repoPath, codexDir, codexHooksJSON),
	})
	addManagedDirCounts(&ok, &broken, []string{
		filepath.Join(repoPath, codexDir, "agents"),
		filepath.Join(repoPath, codexAgentsDir, "skills"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the codex platform.
func (c *codex) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := c.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: "Codex", Present: ok > 0, Broken: broken > 0}
}

// codexOrphanBucket is the single canonical bucket codex owns for orphan
// reporting. claude owns "skills", codex owns "agents" — disjoint buckets so
// the doctor-side iterator never double-counts a canonical entry.
const codexOrphanBucket = "agents"

// OrphanCanonicals implements OrphanCanonicalReporter for the codex platform.
//
// codex owns the "agents" canonical bucket: entries under
// <agentsHome>/agents/<project>/ with no live back-link at
// <projectPath>/.agents/agents/<name>. A non-matching bucket returns nil so
// the (reporter, bucket) fan-out in doctor stays double-count free (claude
// owns "skills"). Detection is shared with claude via scanOrphanCanonicals.
func (c *codex) OrphanCanonicals(project, projectPath, agentsHome, bucket string) []OrphanCanonical {
	if bucket != codexOrphanBucket {
		return nil
	}
	return scanOrphanCanonicals(project, projectPath, agentsHome, bucket)
}

// codexUserBrokenDirs returns the managed directories codex scans for broken
// user-home links: ~/.codex/agents/. Mirrors the legacy lifecycle
// collectBrokenUserLinks codex block (a single managed agents dir).
func codexUserBrokenDirs(home string) []string {
	return []string{filepath.Join(home, codexDir, "agents")}
}

// codexUserConfigFiles returns the managed single-file user-config references
// codex maintains: ~/.codex/hooks.json. Used by the badge math (the
// broken-link surface is narrower — see codexUserBrokenDirs).
func codexUserConfigFiles(home string) []string {
	return []string{filepath.Join(home, codexDir, codexHooksJSON)}
}

// codexUserConfigDirs returns the managed directories codex counts for its
// user-config badge: ~/.codex/agents/ and ~/.agents/skills/. Mirrors the
// legacy lifecycle collectUserConfigPlatforms codex block.
func codexUserConfigDirs(home string) []string {
	return []string{
		filepath.Join(home, codexDir, "agents"),
		filepath.Join(home, codexAgentsDir, "skills"),
	}
}

// UserBrokenLinks implements UserConfigReporter for the codex platform. The
// broken-link surface is ~/.codex/agents/* (matching the legacy lifecycle
// collectBrokenUserLinks codex block); every entry carries PlatformID="codex".
func (c *codex) UserBrokenLinks(home string) []BrokenLink {
	return scanUserBrokenLinks("codex", nil, codexUserBrokenDirs(home))
}

// UserBadge implements UserConfigReporter for the codex platform: the
// user-config badge over ~/.codex/hooks.json, ~/.codex/agents/, and
// ~/.agents/skills/. Mirrors the legacy lifecycle countPlatformHealth("Codex",
// ...) badge math.
func (c *codex) UserBadge(home string) PlatformBadge {
	ok, broken := scanUserConfigCounts(codexUserConfigFiles(home), codexUserConfigDirs(home))
	return PlatformBadge{Name: "Codex", Present: ok > 0, Broken: broken > 0}
}
