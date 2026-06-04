# Spec: Config-v2 Coherence — moved

**Status:** folded into the canonical `config-distribution-model` spec.

The coherence model (scopes / sources / units / the §7A lock, the `EnsureResolved` auto-sync
seam, decisions D1–D14, requirements R1–R8, and done criteria) now lives as **§15 of
[`config-distribution-model/design.md`](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock)** — the single canonical contract for the config
distribution + coherence model.

This stub is preserved so existing cross-references (delegation bundles, merge-backs, task
notes that name `config-v2-coherence`) keep resolving. New references should point at
`config-distribution-model` §15.

**Why folded:** the coherence redesign upgrades the same lockfile/source/tier surface the
distribution-model spec already owns (§4/§6/§7/§8 and the §1–§2 tier framing). Maintaining a
second design.md created two competing sources of truth for one contract; the coherence model
is a refinement *of* the distribution model, not a parallel spec.

**Accountable plan tasks** (unchanged): the `config-v2-migration` finish-line tasks
**p4f** (units-lock wiring), **p4c-rest** (`config sync`/`lint`), and **p1c**
(verifier-profile migration) trace their success criteria to
`config-distribution-model` §15.5.
