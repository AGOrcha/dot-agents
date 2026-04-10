# KG Phase 4: Lint And Maintenance

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 4
Status: Fourth phase
Depends on: KG Phase 3 (query surface — graph has content and query capability)

## Goal

Check graph integrity and knowledge quality. Provide maintenance operations for cross-link repair, stale detection, contradiction surfacing, and general housekeeping.

## Lint Checks

Required initial checks:

| Check | Description |
|-------|-------------|
| Broken links | Notes reference IDs that don't exist |
| Orphan pages | Notes with no inbound links and no source_refs |
| Missing source refs | Non-source notes with empty source_refs |
| Stale pages | Notes not updated beyond a threshold (e.g., 90 days) |
| Contradictory claims | Active notes with conflicting summaries on the same topic |
| Index drift | Notes that exist on disk but aren't in index.md |
| Oversize pages | Notes exceeding a reasonable size threshold |

## Implementation Steps

### Step 1: Link graph construction

- [ ] `buildLinkGraph(kgHome string) (map[string][]string, map[string]*GraphNote, error)`:
  1. Walk all `notes/` subdirectories
  2. Parse each note's frontmatter
  3. Build adjacency map: noteID -> []linked noteIDs
  4. Build note metadata map: noteID -> *GraphNote
  5. Return both maps
- [ ] Tests: build graph from fixture notes, handle empty graph

### Step 2: Individual lint checks

- [ ] `LintResult` struct: check (string), severity (error/warn/info), message (string), note_id (string, optional), path (string, optional)
- [ ] `lintBrokenLinks(graph map[string][]string, notes map[string]*GraphNote) []LintResult`:
  - For each note, check each link ID exists in the notes map
- [ ] `lintOrphanPages(graph map[string][]string, notes map[string]*GraphNote) []LintResult`:
  - Build reverse link map; pages with no inbound links and no source_refs are orphans
- [ ] `lintMissingSourceRefs(notes map[string]*GraphNote) []LintResult`:
  - Non-source notes with empty source_refs
- [ ] `lintStalePages(notes map[string]*GraphNote, threshold time.Duration) []LintResult`:
  - Notes where `updated_at` is older than threshold
- [ ] `lintIndexDrift(kgHome string, notes map[string]*GraphNote) []LintResult`:
  - Compare notes on disk vs entries in index.md
- [ ] `lintOversizePages(kgHome string, notes map[string]*GraphNote, maxBytes int) []LintResult`:
  - Check file size of each note
- [ ] Tests for each check with fixture data that triggers the condition

### Step 3: Contradiction detection (basic)

- [ ] `lintContradictions(notes map[string]*GraphNote) []LintResult`:
  - Group decision notes by topic keywords (simple: shared words in title)
  - For active notes in the same topic group, flag as potential contradiction
  - This is intentionally heuristic — agents do the real judgment
- [ ] Tests: contradicting decisions detected, non-contradicting pass

### Step 4: Aggregate lint runner

- [ ] `LintReport` struct: timestamp, checks_run (int), results []LintResult, summary (errors/warnings/info counts)
- [ ] `runGraphLint(kgHome string) (*LintReport, error)`:
  1. Build link graph
  2. Run all lint checks
  3. Aggregate results
  4. Write report to `ops/lint/lint-report.json`
  5. Append lint event to log
  6. Update health snapshot with lint-derived counts (broken_link_count, orphan_count, stale_count, contradiction_count)
- [ ] Tests: full lint run on fixture graph

### Step 5: `kg lint` subcommand

- [ ] `kgLintCmd` (Use: "lint") with `runKGLint()`:
  1. Verify KG is initialized
  2. Run full lint
  3. Display: grouped by severity, then by check type
  4. `--json` flag for machine output
  5. `--check <name>` flag to run only one check
  6. Exit code 1 if errors found (useful for CI)
- [ ] Tests: lint output formatting, single-check mode

### Step 6: Maintenance operations

- [ ] `kgMaintainCmd` (Use: "maintain") parent command
- [ ] `kgReweaveCmd` (Use: "reweave") with `runKGReweave()`:
  - Scan all notes for broken links, remove them
  - Scan for notes that should be linked (matching IDs in source_refs or title references) but aren't, add links
  - Report changes made
- [ ] `kgMarkStaleCmd` (Use: "mark-stale") with `runKGMarkStale()`:
  - Find notes older than threshold
  - Update their status to "stale"
  - Update index
  - Report count changed
- [ ] `kgCompactCmd` (Use: "compact") with `runKGCompact()`:
  - Archive superseded/archived notes to a `notes/_archived/` directory
  - Remove them from active index
  - Report count archived
- [ ] Tests for each maintenance operation

### Step 7: Fill in `contradictions` query intent

- [ ] Update Phase 3's `findContradictions()` stub to use `lintContradictions()`
- [ ] Contradictions query now returns real results
- [ ] Tests: contradictions query returns lint-derived results

## Files Modified

- `commands/kg.go`
- `commands/kg_test.go`

## Acceptance Criteria

- Lint detects broken links, orphans, stale pages, missing refs, index drift, oversize pages
- Contradiction detection surfaces potentially conflicting decisions
- Maintenance operations repair cross-links, mark stale content, compact archives
- Health snapshot reflects lint-derived quality metrics

## Verification

```bash
go test ./commands -run 'KGLint|LintBroken|LintOrphan|LintStale|Contradiction|Reweave|Compact'
go test ./commands
go test ./...
```
