# Deep dive — anti-patterns (§7)

Operational depth behind `instructions/gotchas.md`. Model- and registry-agnostic: no model,
vendor, or closed registry is named, and no plan-scoped evidence anchor appears. Source of
truth: `docs/full-loop-pipeline-craft.md` §7. Read this **before** shipping a config edit or an
emit — each failure mode below carries the invariant that defeats it.

---

## 1. Hand-editing emitted swarm / pipeline YAML

Once the swarm projections are emitted from the routing IR, a hand-edit is **drift**: it diverges
the running pipeline from the stage profiles and defeats the single-source routing of §2. The
edit lives only in the artifact, so the next re-emit silently reverts it — or worse, it persists
and the artifact no longer matches the IR it claims to come from.

- **Rule:** Treat emitted swarm/pipeline YAML as a build artifact; regenerate from the IR and
  refuse hand-edits. Re-emit must be byte-identical.

## 2. Rate-limit / usage-limit walls mid-loop

A loop that treats a rate-limit or usage-limit wall as a hard crash **strands slots** — the wave
never reconciles and the held slots never free. These walls appear even in scratchpad and review
sessions, so no pipeline is exempt.

- **Rule:** Make rate-limit and usage-limit walls first-class **resumable stop conditions**, not
  crashes; checkpoint before signal-class kills.

## 3. Unverifiable self-reported completion

Narrated result/verification text with **zero** persisted tool result lets a coarse substring
scorer rate work as high while nothing corroborates it. The narration is free to claim a success
the tools never confirmed, and a keyword scorer cannot tell the difference.

- **Rule:** Never gate an advance on self-report; require an anchor **plus** a real tool/verifier
  record.

## 4. Stale-status drift

"Status said done" is not auditable. A cached status can be stale the moment it is read, and a
pipeline that advances on it will act on a world that no longer exists. Canonical state must win
over stale checkpoints.

- **Rule:** Re-read canonical task/delegation/PR/slot/eligible state before **every** mutation,
  re-run slots and eligible after **every** canonical write, and block the next wave on any
  ambiguous state. Never advance on cached status.

## 5. Numeric-index diversity binding

Binding the cross-family gate to "reviewer slot N" or an assumed lens order **breaks silently**
when the list changes — the gate still runs, but against the wrong (possibly same-family) lens,
so the review is invalid while appearing to pass.

- **Rule:** Bind diversity gates to the **named** adversarial lens with asserted family
  inequality, never to numeric slot order.

## 6. Cross-scope worker dispatch

Dispatching workers into overlapping write-scope shatters the disjoint-slice invariant — two
workers edit the same files and the merge is corrupt or lost. A common trigger is widening a
bundle's scope across packages to satisfy a stray failing test.

- **Rule:** Enforce write-scope disjointness at dispatch (the max-batch conflict computation) and
  a refusing test-scope gate; never widen a bundle's scope across packages to satisfy a stray
  test.

---

## Pre-ship checklist

Before any emit or config ship, confirm each of the six invariants above, then run the SKILL.md
**Verify** sequence: lint the config layers → resolve topology/lenses for the touched app-type →
resolve-prompt per stage (non-empty model/model family; `reviewer.family != executor.family`) →
emit with a dry-run. A failure at any gate means fix the Layer-1 IR, never patch the artifact.
