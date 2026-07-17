# Anti-patterns: pipeline-architect

Operationalizes **§7** of the public guide `docs/full-loop-pipeline-craft.md`.
Read **before** shipping a config edit or an emit.

**Deep dive:** [`references/anti-patterns.md`](../references/anti-patterns.md) explains each
failure mode and the invariant that defeats it. This file is a concise loader.

---

1. **Hand-editing emitted swarm YAML.** The runtime projections are a build artifact emitted from
   the IR; a hand-edit diverges the running pipeline from the stage profiles and defeats
   single-source routing.
   - **Rule:** Treat emitted swarm/pipeline YAML as a build artifact; regenerate from the IR and
     refuse hand-edits. Re-emit must be byte-identical.
2. **Rate-limit / usage-limit walls mid-loop.** A loop that treats a wall as a hard crash strands
   slots; walls appear even in scratchpad/review sessions.
   - **Rule:** Make rate-limit and usage-limit walls first-class **resumable stop conditions**,
     not crashes; checkpoint before signal-class kills.
3. **Unverifiable self-reported completion.** Narrated result/verification text with zero persisted
   tool result lets a coarse scorer rate the work high while nothing corroborates it.
   - **Rule:** Never gate a transition on self-reported completion; require an anchor + a real
     tool/verifier record.
4. **Stale-status drift.** "Status said done" is not auditable; canonical state must win over stale
   checkpoints.
   - **Rule:** Re-read canonical task/delegation/PR/slots/eligible state **before every mutation**
     and re-check slots/eligible **after every write**; block on ambiguity; never advance on cached
     status.
5. **Numeric-index diversity binding.** Binding the cross-family gate to a numeric slot or assumed
   lens order breaks silently when the list changes.
   - **Rule:** Bind diversity gates to the **named** adversarial lens with asserted family
     inequality, never to numeric slot order.
6. **Cross-scope worker dispatch.** Dispatching workers into overlapping write-scope shatters the
   disjoint-slice invariant.
   - **Rule:** Enforce write-scope disjointness at dispatch (max-batch) and refuse cross-package
     scope widening to satisfy a stray test.

---

## Pre-ship checklist

Before any emit or config ship, confirm each of the six invariants above, then run the SKILL.md
**Verify** sequence: lint the config layers → resolve topology/lenses → resolve-prompt per stage
(non-empty model/model family; `reviewer.family != executor.family`) → emit with a dry-run. A
failure at any gate means fix the Layer-1 IR, never patch the artifact.
