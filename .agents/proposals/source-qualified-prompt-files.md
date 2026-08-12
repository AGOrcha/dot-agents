# Source-qualified prompt files: sync-time fetch + lock-pinned prompt units

**Status:** RATIFIED, implemented (2026-08-11).
**Owning spec:** `.agents/workflow/specs/stage-profile-and-routing-consolidation/design.md` §7B
(the deferred source-resolution point that section now closes).
**Related:** `config-distribution-model` §5 (ref grammar), §7A (units lock), §15.7 (the
prompt-overlay deferral this partially resolves), R5 (reproducibility).

## 1. The gap

`PromptFileRef` shipped with a `source` field — the shape was carried forward from PR #40 into
`StageProfile.PromptFiles` — but **source resolution was never implemented**. Every consumer
flattened the ref to its `path`, and no code path ever fetched a prompt file from a config source.

Observed on a real team config: a layer fetched via `extends` declares

```jsonc
"stage_profiles": { "verifier": { "ts-lint": { "prompt_files": [
  "po-agents-config:verifiers/verifier.base.md",
  "po-agents-config:verifiers/ts-lint.md"
] } } }
```

and `da workflow resolve-prompt --kind verifier --slug ts-lint` returns `matched: true` with every
entry `scope: "unresolved", exists: false`. Three independent causes, all confirmed:

1. **No source branch in the probe.** `resolvePromptRef` probed absolute → `<project>/<entry>` →
   `<project>/.agents/prompts/<entry>` → `<home>/prompts/<entry>`, treating `source-id:path` as a
   literal filename.
2. **Provenance discarded before the probe.** `decodeProfilePromptFiles` flattened through
   `promptRefPath`, which returns only `Path` — so even the typed `{source, path, version}` form
   arrived at the probe as a bare path. `flattenBundlePromptPaths` dropped source at the same seam.
3. **Nothing on disk to resolve against.** The `extends` git fetch is an in-memory shallow clone and
   the cache stores only `layer.json` bytes per resolved SHA. Prompt files from the source tree were
   never fetched, so even a correct probe had nothing to find.

Constraint that shaped the fix: `resolve-prompt` is **offline by contract** — it resolves via
`config.NewLayeredResolver().ResolveLocked`, lock + cache only, never the network.

## 2. Ratified decision

**Grammar.** The typed object `{source, path, version}` is canonical and always source-qualified (an
undeclared source is honored as written, surfacing as unresolved-with-hint rather than being silently
downgraded). A **bare string** is source-qualified **only** when its prefix before `:` matches a
source id declared in the effective config — aligning with the §5 `source-id:path[@version]` ref
grammar (`ParseLayerRef`). Everything else keeps local-path semantics, so legacy entries and Windows
drive letters are unaffected. Source/version is preserved end to end; no seam may flatten it away.

**Kind.** A source-qualified prompt file is a first-class lock unit of a **new** kind, `prompt` —
not a reuse of `artifact`. A prompt is neither merged like a layer nor installed/invoked under a
trust-and-signing posture like an artifact; it is content pinned for composition.

## 3. Mechanism

1. **Sync-time fetch** — `LayeredResolver.Resolve` (behind `da config sync` / `da install`, the one
   network-touching path) collects the source-qualified prompt refs the effective config declares and
   fetches each through the SAME per-source-type `Fetcher` an `extends` layer uses, caching it
   content-addressed at `~/.agents/cache/config/<source-id>/<prompt-path>/<sha>/` (the layer cache
   layout, one cache writer/reader pair).
2. **Lock pinning** — each fetched file becomes a `units` entry keyed
   `<source-id>:<prompt-path>[@version]` with `kind: "prompt"` and its resolved digest (plus the
   §7A.4 cache key, so the existing revalidation gate applies uniformly).
3. **Offline probe** — `resolvePromptRef` gains a source branch: lock unit → cached path →
   `scope: "source"`, `exists: true`. A never-synced or pruned-cache prompt is `unresolved` with a
   `run \`da config sync\`` hint, following the `LockedRemoteLayerBytes` skip+sync-hint precedent. No
   fetch is ever triggered from this path.

**Failure policy.** A prompt that cannot be fetched is non-fatal to the resolve: it warns and carries
the previous pin forward. Prompts are composition input, not policy — failing an entire
`da config sync` over one stale prompt ref is the worse trade. Offline resolves never fetch and never
drop existing prompt pins.

## 4. Lock / R5 implications

- **Additive, no format break.** Prompt units live in the existing `units` section under the existing
  key grammar. Existing locks remain valid and are unchanged until the next resolve; sibling
  `layer` / `artifact` / `profile` units are untouched (the artifact-preserving read-modify-write
  under `agentslock.Update` is unchanged).
- **Staleness is unaffected.** `lockedUnitRefs` already restricts the declared-set comparison to
  `layer` + `artifact` kinds, so prompt units cannot trip `ReasonDeclaredSet` or churn the lock (the
  H9 frozen-no-op contract holds).
- **R5 extended.** Reproducibility previously covered config content only: the same source-set +
  lock digest guaranteed an identical effective config, while the prompt a verifier actually ran was
  unpinned. Prompt content now rides the same guarantee — a composed prompt is reproducible from the
  lock without re-resolving.
- **Ownership.** The prompt unit set is pass 1's own derived set (from the effective config's
  `stage_profiles`), so it is replaced wholesale on an online resolve: deleting a `prompt_files` entry
  drops its pin instead of leaving a permanently stale one.

## 5. Follow-ups (not in this change)

- **Cache pruning policy.** Nothing garbage-collects
  `~/.agents/cache/config/<source-id>/<prompt-path>/<old-sha>/` after a prompt's digest moves. The
  same is true of the layer cache today; a single `da cache prune` covering both is the right home.
- **`da config verify` awareness of prompt units.** Verify does not yet check that every pinned
  prompt unit's cached bytes are present, so a pruned cache is only discovered at
  `resolve-prompt` time.
- **Per-resolve clone memoization.** Each source-qualified prompt file costs one shallow clone on a
  git source (the fetcher must resolve HEAD even to serve its cache). N prompt files from one source
  ⇒ N clones per sync. A per-resolve (url, ref) → worktree memo inside the git fetcher would collapse
  them.
- **`da config explain` surfacing.** Prompt units are not yet listed in explain output alongside
  layers/profiles.
