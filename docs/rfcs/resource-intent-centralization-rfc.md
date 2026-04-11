# RFC: Resource Intent Centralization

**Date:** 2026-04-11
**Status:** Accepted — resolves design questions, implementation may begin
**Depends on:** `docs/PLATFORM_DIRS_DOCS.md`, `docs/CANONICAL_HOOKS_DESIGN.md`, Stage 1 canonical storage work in `platform-dir-unification`

---

## Problem Statement

`dot-agents` now has a mostly consistent canonical store under `~/.agents`, but the final projection step is still fragmented:

1. Import and refresh flows canonicalize files correctly, then hand repo projection back to each platform independently.
2. Shared repo-local targets such as `.agents/skills/<name>` are treated as if they were platform-owned, so multiple platforms try to create the same output path.
3. Low-level link helpers are intentionally conservative and cannot safely convert a populated imported directory into a managed mirror.

This creates a structural failure mode: import succeeds, canonical state is preserved, then refresh warns while relinking the same repo-local shared target. The fix is not a stronger symlink helper. The fix is a central plan for resource projection.

---

## Non-Goals

This RFC does not propose:

- Bash parity changes under `src/lib/**`
- Stage 2 bucket expansion (`commands`, `output-styles`, `modes`, `plugins`, `themes`, `prompts`)
- Auto-merging conflicting imported resources by content
- Reusing the proposal approval queue for advisory import conflicts
- Broadly destructive low-level filesystem helpers

These remain separate follow-on work.

---

## Design Decisions

### 1. Canonicalization And Projection Stay Separate

**Decision:** Canonicalization remains a command-layer concern; projection becomes a centralized planning/execution concern.

**Why:** Import needs to answer "what should the canonical source be?" Projection needs to answer "what outputs should exist right now?" Mixing them leads to platform-local special cases and makes refresh/install/remove diverge over time.

**Implication:** `import`, `refresh`, `install`, `remove`, `status`, and `explain` should all rely on a shared projection model, but canonical path mapping remains a distinct earlier step.

---

### 2. ResourceIntent Is A Small Declarative Data Model

**Decision:** Introduce a serializable `ResourceIntent` model and keep resolver/renderer behavior in a registry/executor layer.

**Required fields:**

- `IntentID`: stable identifier for diagnostics and tests
- `Project`: managed project name or `global`
- `Bucket`: canonical resource bucket (`skills`, `agents`, `hooks`, `rules`, `settings`, `mcp`)
- `LogicalName`: resource identity inside the bucket
- `TargetPath`: exact output path to materialize
- `Ownership`: `shared_repo`, `platform_repo`, or `user_home`
- `SourceRef`: typed canonical source descriptor
- `Shape`: `direct_dir`, `direct_file`, `render_single`, or `render_fanout`
- `Transport`: `symlink`, `hardlink`, or `write`
- `Materializer`: registry key that resolves or renders the output
- `ReplacePolicy`: replacement behavior for existing targets
- `PrunePolicy`: stale-output cleanup behavior
- `Provenance`: import/emitter metadata for diagnostics

**Supporting fields:**

- `Precedence`: deterministic intent ordering
- `ConflictKey`: defaults to `TargetPath`, but can group related conflicts later
- `MarkerFiles`: expected markers such as `SKILL.md`, `AGENT.md`, `HOOK.yaml`
- `EnabledOn`: optional platform allowlist mirroring canonical hook behavior
- `ReviewHint`: advisory metadata for collisions or lossy imports

**Why:** This follows the direction already proven useful in the shared hook contract: canonical metadata should be explicit, while emission behavior should be chosen by the executor instead of hidden in platform methods.

**Constraint:** `ResourceIntent` must not embed callbacks or platform-specific structs. Use registry keys plus typed descriptors so the shape stays documentable, testable, and evolvable.

---

### 3. SourceRef Must Be Typed, Not Just A Path

**Decision:** `SourceRef` is a typed canonical reference, not a raw string path.

**Minimum shape:**

- `Scope`: `global` or managed project name
- `Bucket`: canonical bucket
- `RelativePath`: path inside the canonical bucket
- `SourceKind`: `canonical_file`, `canonical_dir`, `canonical_bundle`, or other explicit future kinds
- `Origin`: optional import-origin label such as `agents`, `claude`, `copilot`, `cursor`, `codex`, `opencode`

**Why:** The executor and diagnostics need more than a filesystem path:

- status/explain need to say where a target came from
- import conflict handling needs stable origin labels
- future migrations may need to distinguish bundles from flat files without guessing from extensions

---

### 4. Ownership Is Explicit

**Decision:** Every projected output declares ownership up front.

**Ownership modes:**

- `shared_repo`: a repo-local target shared across multiple platforms or compatibility layers
- `platform_repo`: a repo-local target that belongs to one platform only
- `user_home`: a target written under a user home directory

**Why:** The current bug exists because ownership is implicit. `.agents/skills/<name>` behaves like a shared repo target, but is currently emitted from several platform implementations as if each platform owns it.

**Examples:**

- `.agents/skills/<name>`: `shared_repo`
- `.claude/skills/<name>` when emitted as a shared compatibility mirror: `shared_repo`
- `.codex/hooks.json`: `platform_repo`
- `.github/hooks/*.json`: `platform_repo`
- `~/.claude/settings.json`: `user_home`

---

### 5. A Central Planner/Executor Owns Shared Targets

**Decision:** Platforms declare needed outputs, then a central planner/executor aggregates intents before any shared-target writes occur.

**Execution model:**

1. Commands canonicalize inputs
2. Platforms and shared resolvers emit `ResourceIntent` values
3. Planner groups intents by conflict key
4. Identical intents dedupe
5. Incompatible intents fail fast with actionable diagnostics
6. Shared targets execute once
7. Platform-owned targets execute in platform-local adapters

**Why:** Shared outputs must be deduped and arbitrated before mutation. Calling `CreateLinks()` independently cannot safely do this because each platform sees only its own desired output.

---

### 6. Import Naming Conflicts Are Preserved, Not Overwritten

**Decision:** When an import collides with an existing canonical logical name but differs in content, preserve both variants and use origin-prefixed fallback naming.

**Algorithm:**

1. Normalize an import to its preferred canonical path
2. If destination is absent, import normally
3. If destination content is identical, skip as duplicate
4. If destination differs, create an alternate logical name prefixed by origin, for example `claude-foo` or `agents-foo`
5. If that fallback already exists, append a stable numeric suffix
6. Record an advisory conflict review note linking the primary and alternate canonical entries

**Why:** This keeps imports non-destructive and lets operators reconcile collisions later without losing either version.

**Naming rule:** Do not repeat scope in fallback names. Scope is already encoded in the canonical path.

---

### 7. Import Conflicts Use A Separate Advisory Queue

**Decision:** Add a dedicated import-conflict review-note queue under `~/.agents/review-notes/import-conflicts/`; do not reuse `~/.agents/proposals/`.

**Initial note fields:**

- `id`
- `status` (`pending`, `resolved`, `ignored`)
- `kind` (`duplicate_name`, `lossy_import`, `unsupported_native_shape`)
- `bucket`
- `scope`
- `logical_name`
- `canonical_target`
- `alternate_target`
- `origin`
- `rationale`
- `suggested_actions`
- `created_at`

**Why:** The proposal queue is for reviewed mutations that can be approved and applied. Import conflicts are advisory review items, not queued writes.

---

### 8. Non-Empty Directory Replacement Is Allowlisted And Centralized

**Decision:** Only the central executor may replace a non-empty repo directory with a managed link, and only for an explicit allowlist of shared managed targets.

**Initial allowlist:**

- `.agents/skills/<name>`
- additional shared targets may be added later by explicit RFC-backed rollout, not by helper broadening

**Replacement is allowed only when all of these are true:**

- the target path is on the allowlist
- the directory was imported successfully into canonical storage in the same operation
- rollback material already exists via backup/restore flow
- the executor can prove the populated directory is an import source being converged, not an arbitrary user directory

**Why:** The current low-level link helpers are correctly conservative. The safe fix is a narrow higher-level policy, not a global "recursive delete and relink" behavior.

---

### 9. Initial Rollout Is Shared Skill Convergence First

**Decision:** The first implementation slice should centralize only the highest-conflict shared repo targets, starting with project skill mirrors.

**First slice:**

- repo `.agents/skills/<name>`
- repo `.claude/skills/<name>` when emitted as a shared compatibility mirror
- any other repo-local shared compat targets still emitted by multiple platforms after that first migration

**Deferred in this RFC’s rollout:**

- native platform hook/config files
- native agent renderers
- new Stage 2 resource buckets

**Why:** Skills are the clearest current breakage, and the surrounding import/relink flows already expose the failure mode directly.

---

### 10. Status And Explain Must Read The Same Registry

**Decision:** `status` and `explain` should eventually read from the same resource registry/planner metadata used by refresh/install/remove.

**Why:** Once projection decisions are centralized, diagnostics must describe actual managed behavior instead of reconstructing it from platform-specific assumptions.

---

## Acceptance Criteria

Implementation may proceed when the following are treated as fixed design decisions:

1. `ResourceIntent` is a declarative data model with typed `SourceRef`
2. Shared repo targets are planned centrally before writes
3. Ownership is explicit per target
4. Import conflicts preserve both variants and write advisory review notes
5. Non-empty directory replacement is allowlisted and executor-only
6. First rollout scope is shared skill convergence, not all resources at once
7. `status` and `explain` eventually consume the same projection registry

---

## Blocking Risks

1. **Intent sprawl** — if new fields are added per-resource without a stable registry boundary, `ResourceIntent` will become another platform-specific dumping ground.

2. **Unsafe replacement drift** — if directory replacement logic leaks back into low-level helpers, future resource types may gain destructive behavior accidentally.

3. **Advisory queue neglect** — preserving both imported variants is safe, but operators still need a way to notice and resolve conflicts. The review-note queue must be surfaced by diagnostics later.

4. **Documentation drift** — platform compatibility locations change over time. `PLATFORM_DIRS_DOCS.md` should explicitly note which paths were re-verified and where verification was inconclusive.

---

## Implementation Order

1. Document this RFC and align active plans/status docs
2. Extract shared canonicalization/projection support into a neutral package
3. Add `ResourceIntent`, `SourceRef`, planner, and executor types
4. Migrate shared skill outputs onto the shared executor
5. Add import-conflict review-note generation and collision naming
6. Update status/explain to read the same registry metadata
7. Expand to additional shared targets only after the first slice is stable

---

## Status

This RFC resolves the architectural blockers for resource-intent centralization. The next work is implementation planning and loop execution, not further design churn.
