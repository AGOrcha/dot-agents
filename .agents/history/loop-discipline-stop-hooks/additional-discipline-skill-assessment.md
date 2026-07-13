# Companion Discipline-Skill Hook Assessment

Task: `p3b-companion-discipline-skills` (requirements R9.1, DC10).
Companions under review: `agent-handoff` and `delegation-lifecycle`.

This document decides, per companion skill, whether a native lifecycle hook is
justified now or whether the prose skill is sufficient. The bar is the design's
D7 observable-evidence boundary: a gate may issue **hard remediation** only on a
fact it can establish from repository state or declared sentinel data on every
supported platform; rules that depend on command history or conversation content
are advisory-only unless a platform supplies a readable trace. A hook is not
created merely because a skill relates to loop discipline (per the contract).

`PostToolUse` / `PostToolUseFailure` observation questions raised below are
recorded **only as inputs to the R1.5 evaluation** — plan
`r1-5-hook-enforcement-telemetry` (tasks `t1b-post-tool-observation-evaluation`,
`t2-scoring-signal`). This assessment creates no private telemetry or scoring,
and adds no prose that pretends to gate.

## Reference: what an enforced companion looks like here

The three discipline skills that *did* earn hooks (`iteration-close`, `isp`,
`loop-worker`) share a structural property the gate depends on: each writes a
**sentinel** at skill entry (`da workflow hook-sentinel write <skill>`) declaring
plan/task/run-id/agent-type and a list of `expected_artifacts`. The Stop /
SubagentStop gate (`internal/scaffold/hooks/global/iteration-close-gate/gate.sh`)
then performs a portable, deterministic check — does each declared artifact exist
on disk — and hard-blocks with native remediation output when one is missing. The
deterministic `PreToolUse` rule (block delegated `workflow advance`) is likewise
anchored to an active sentinel. No sentinel ⇒ the gate short-circuits to allow.

So "evidence-backed lifecycle hook" here means: a declared sentinel obligation
plus a repository-observable terminal fact, not prose intent. A companion skill
qualifies for a hook only if it owns such a declarable, on-disk-verifiable
obligation.

## agent-handoff

**Durable artifact / transition owned.** A handoff document at
`.agents/active/handoffs/YYYY-MM-DD-<slug>.md` (create mode). The skill is
user-invoked and advisory: it packages context for a successor at a session/task
boundary. Its other steps (commit completed work, clean up debugging code) are
side effects on the working tree, not a single declarable artifact.

1. **What a native hook could catch that prose cannot.** In principle, a Stop
   gate could refuse to let a session end with uncommitted work or without a
   written handoff. But neither is a deterministic, contract-mandated obligation:
   agent-handoff is *opt-in* (the user decides a handoff is warranted), and
   "uncommitted work at Stop" is a frequent, legitimate state (mid-task pause).
   Enforcing either would produce constant false positives — the opposite of the
   sentinel-anchored, opt-in obligations the existing gates verify.
2. **Evidence justifying a hook now.** None. There is no recurring observed
   failure mode (e.g., a measured rate of sessions ending without a needed
   handoff) on record. The skill ships no sentinel and declares no
   `expected_artifacts`, so the existing gate machinery has nothing portable to
   verify, and inventing an obligation without evidence would be evidence-free
   hard remediation — exactly what D7 forbids.
3. **Verdict: ship prose-only; no hook.** The complete starter tree (SKILL.md +
   `instructions/` + `templates/`) is the deliverable. Any future enforcement is
   gated on R1.5 evidence — specifically, a `PostToolUse`/Stop observation that a
   measurable fraction of governed sessions end with stranded WIP and no handoff.
   That observation question is routed to `r1-5-hook-enforcement-telemetry`
   (`t1b-post-tool-observation-evaluation`); it is non-blocking and must clear
   T1b's payload/redaction/dedup boundary before it could even inform a hook
   proposal. No bounded `PreCompact` continuity use is warranted either: unlike
   `iteration-close`, agent-handoff has no declared mid-flight obligation whose
   loss at compaction would silently break the loop.

## delegation-lifecycle

**Durable artifact / transition owned.** This skill is an *orchestrator*
procedure: fanout (`workflow fanout` → delegation contract + bundle) → worker
execution → merge-back → orient. Its terminal artifacts (`.agents/active/
delegation/<task>.yaml`, `delegation-bundles/<id>.yaml`, `merge-back/<task>.md`)
are already produced and verified by the **commands it drives** —
`workflow fanout`, `workflow merge-back`, `workflow delegation closeout` — and the
worker side is already governed by the shipped `loop-worker-gate` (SubagentStop
write-scope diff against the worker's sentinel) and `iteration-close-gate`
(delegated `workflow advance` blocked at `PreToolUse`).

1. **What a native hook could catch that prose cannot.** The candidate failure
   modes are orchestration-level: fanning out a task whose work already shipped,
   a bundle `write_scope` that decayed against HEAD, or an overlapping active
   delegation. The starter `instructions/workflow.md` already addresses these as
   pre-fanout checks. A hook anchored to the orchestrator turn cannot
   deterministically establish "this PR already merged" or "this symbol moved"
   from repository state alone at a fixed lifecycle point — these are
   trace/history-dependent judgments (D7's advisory-only category), and the
   orchestrator skill writes no sentinel of its own to anchor a gate.
2. **Evidence justifying a hook now.** None specific to this skill. The
   write-scope and closeout invariants that *are* deterministically enforceable
   are already enforced on the **worker** path by the existing
   `loop-worker-gate` / `iteration-close-gate` bundles — adding an
   orchestrator-side hook would duplicate, not extend, that coverage. There is no
   recorded recurring orchestrator-side failure that those gates miss.
3. **Verdict: ship prose-only; no new companion hook; fold enforcement into the
   existing bundles.** The complete starter tree (SKILL.md + three instruction
   files) is the deliverable. The deterministic, observable invariants this skill
   cares about are already covered by `loop-worker-gate` and
   `iteration-close-gate`; nothing new is justified. Post-hoc orchestration
   signals (e.g., fanout-to-already-shipped, detectable only after the fact) are
   `PostToolUse` observation candidates routed to
   `r1-5-hook-enforcement-telemetry` (`t1b-post-tool-observation-evaluation`),
   not a blind enforcement hook. No bounded `PreCompact` use is warranted: the
   orchestrator's continuity is already carried by the canonical plan/TASKS state
   and the per-worker sentinels, which survive compaction independently.

## Summary

| Companion | Owns declarable on-disk obligation + sentinel? | Hard-remediable fact (D7)? | Evidence now? | Decision |
| --- | --- | --- | --- | --- |
| agent-handoff | No (opt-in, no sentinel) | No | None | Prose-only; no hook. Re-evaluate only on R1.5 evidence. |
| delegation-lifecycle | No own sentinel; worker path already gated | Already covered by existing bundles | None new | Prose-only; fold any future enforcement into existing worker gates, not a new companion hook. |

**Conclusion.** Both companions ship complete starter trees and are sufficient as
prose discipline skills today. No new lifecycle hook is justified for either:
neither owns a sentinel-anchored, repository-observable terminal obligation that
the existing gates do not already cover, and there is no evidence of a recurring
failure that an evidence-backed hook would catch. Any enforcement decision waits
on observation telemetry from `r1-5-hook-enforcement-telemetry`
(`t1b-post-tool-observation-evaluation`), which must remain non-blocking and clear
its payload/privacy/dedup boundary before it could inform a follow-up hook
proposal. This assessment adds no prose-only enforcement.
