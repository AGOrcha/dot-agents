---
id: obs-config-verify-staleness-message-misroutes
kind: observation
status: unreviewed
created: 2026-07-12
area: commands/config/verify.go, internal/config/staleness.go
severity: low
surfaced_by: additive .agentsrc.json edit (registering a reviewer stage profile) during
  the transcript-analysis-and-pipeline-craft plan
---

# `config verify` staleness warn prints equal digests and misroutes the remedy

## Observation

After an additive local `.agentsrc.json` edit, `da config verify` reports:

```
[warn] config-staleness   local config changed since last resolve
                          (lock sha256:0817a, now sha256:0817a) — run `da config sync`
```

The two abbreviated `inputs_digest` values are **identical** (`0817a` == `0817a`), and
running `da config sync` does **not** clear the warn (verified: `config sync` reports
"no external layers declared; local stack re-resolved", then verify re-warns with the
same equal-digest message). `da refresh` also does not clear it. The warn is
non-failing (`Summary: 3 passed, 1 warning(s), 0 failed — OK`), so it is advisory noise,
not a gate.

## Root cause

`internal/config/staleness.go:Staleness` fires `Fresh=false` when ANY of three reasons
holds: `ReasonInputsDigest` (expected != lock.InputsDigest), `ReasonDeclaredSet`
(`declaredSetChanged(rc, lock.Units)`), or `ReasonUnitDigest` (only when a non-nil
`recompute` is passed). `commands/config/verify.go:verifyStaleness` calls
`cfg.Staleness(cwd, "", nil)` — `recompute` is nil, so `ReasonUnitDigest` cannot fire —
then, on `!Fresh`, unconditionally prints:

```go
"local config changed since last resolve (lock %s, now %s) — run `da config sync`",
abbrevSHA(recorded), abbrevSHA(res.ExpectedInputsDigest)
```

surfacing only the `inputs_digest` pair. When the firing reason is `ReasonDeclaredSet`
(the declared layer/package unit set drifted from `lock.Units`), `inputs_digest` can be
equal, so the message shows two equal hashes and recommends `da config sync` — which
rewrites the lock's **config (layers) section**, not the **units** set, so it never
resolves a declared-set drift. The user is looped onto a remedy that cannot clear the
state.

## Suggested fix (separate reviewed change — NOT applied here)

1. `verifyStaleness` should surface `res.Reasons` (e.g. "declared unit set drifted") and
   route each reason to the remedy that actually clears it, instead of hard-coding the
   `inputs_digest` narrative + `da config sync`.
2. When `res.Reasons` is exactly `{ReasonDeclaredSet}` and the printed digests are equal,
   either recommend the units-resolve path that rewrites `lock.Units`, or fix `config sync`
   to also reconcile the units set for a local-only stack.
3. Guard: a verify message that prints two equal abbreviated SHAs is self-contradictory —
   add a test asserting the message names the real reason when `inputs_digest` matches.

## Evidence

- Repro: edit `.agentsrc.json` (add a reviewer `stage_profiles` entry), `da refresh`,
  `da config sync`, `da config verify` → persistent equal-digest staleness warn.
- Code: `commands/config/verify.go:283-300`; `internal/config/staleness.go:344-371`
  (`declaredSetChanged` at :362, `ReasonUnitDigest` gated on non-nil `recompute` at :365).
