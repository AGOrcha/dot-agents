# Lesson: proactively apply the capabilities you build (dogfood immediately)

## Mistake

After building + improving a capability — the `release-docs-refresh` **audit matrix**
(dedicated per-category auditors) and the two analyst agents — I *described how it would
run* with parallel sub-auditors instead of **proactively running it**. The maintainer had
to tell me to run it ("I'll have to tell you how to run the audit docs matrix … instead of
you being more proactive by using it after landing these agent upgrades").

## Pattern

Building a capability is only half the work; **applying it is the point.** "We just
created/improved an agent/skill/tool" is itself the trigger to **use it now on the live
project** — dogfood it, then fold/route what it finds. Don't build-and-move-on.

## Rule

- After creating or improving a skill/agent/audit capability, **proactively run it** on the
  current work without waiting to be asked.
- For audit/analysis capabilities, that means: **fan out the parallel sub-auditors per the
  matrix**, synthesize, and route the findings (doc-stale → fix; code-violates-contract /
  spec-divergence / design-issue → fold-back/proposal).
- Treat each capability upgrade as "build → **immediately apply** → act on output," not
  "build → describe → wait."

## Evidence

Building the audit matrix + `promise-gap-analyst` and stopping left value on the table. When
finally run (on prompt), the parallel matrix sweep found real drift in **4 of 6** categories
in one pass — skill-command-integration (stale: skill-architect "future", `da config`
absent), workflow-commands (+ a confirmed `workflow commit --dry-run` code gap),
error-messages (10 findings: raw `fmt.Errorf` where the contract requires typed errors),
DEMO (6 omissions) — and confirmed 2 clean (hooks-events, resource-commands). All
discoverable the moment the capability existed.

Sibling of the docs-refresh discipline; see `.agents/skills/.../release-docs-refresh/`.
