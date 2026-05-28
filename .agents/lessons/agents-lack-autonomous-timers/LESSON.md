# Claude Code agents have no autonomous timers

**Captured:** 2026-05-28 (session coach surfaced the structural gap when testing its new PR-review/comment polling charter)
**Expires:** when `workflow-orchestrator-daemon` lands with a real scheduler

## The trap

When you write an agent charter that says "poll every Ns" or "run X on a cadence" or "wake every Y minutes to do Z," you are implicitly assuming the agent has an autonomous timer. **It does not.** A Claude Code subagent (or background agent, or coach-style long-running spawn) only runs when it receives a `SendMessage` from its parent. Between messages, it is dormant. Charters that depend on time-based execution are structurally inoperative without an external scheduler.

## Why this matters

The dot-agents session coach (2026-05-28) received a charter extension to "poll PR reviews + comments every 30s." Between charter acceptance and the next maintainer message, several relevant events fired (a submitted file-comment on PR #159, three PENDING-review drafts on PRs #143/#153/#160). The coach surfaced none of them because no SendMessage arrived to tick the loop. The maintainer noticed the gap directly: "two comments on 160, one that references #157. was the latest comment on 159 handled?" — the coach hadn't even checked.

## Workarounds

### v1 (today): piggyback on every incoming message

Every charter responsibility that says "every Ns" becomes "at the start of every incoming message I receive." The coach runs a fresh poll cycle as the first step of every message-response turn, BEFORE responding to the message itself. Cheap (per-call cost low; rate-limit fine for typical PR counts); closes the gap.

Trade-off: latency is bounded by message frequency, not by Ns. If 5 minutes pass without a message, no polling happens. In practice, a session coach gets messages every 1-3 minutes during active orchestration; the gap is acceptable.

### v2 (tomorrow): productionized daemon owns the scheduler

`[[workflow-orchestrator-daemon]]` is a long-running process (CLI-launched `da workflow run`) that has a real OS timer. It polls per its own schedule and emits events into the orchestrator queue. The coach becomes a consumer of daemon events rather than the polling source. This is the only honest path to real time-based behavior.

## When to apply

Whenever you write a subagent charter or skill that includes language like:
- "every N seconds/minutes/hours"
- "on a cadence of X"
- "wake up at Y"
- "periodically check Z"
- "in the background"
- "asynchronously"

…stop. Either:
1. Reword to "at the start of every incoming message" (v1 workaround), OR
2. Note that the responsibility requires a daemon/external scheduler + add a follow-up task to graduate it once `[[workflow-orchestrator-daemon]]` lands

Do NOT pretend the agent has a timer when it doesn't. The charter will silently fail and the orchestrator will not know.

## Sibling pattern: external monitor with SendMessage feedback

A complementary approach: spawn a separate `Monitor` tool watching a real event source (`gh pr list --watch`, `gh run watch`, a webhook receiver). The Monitor tool DOES have its own scheduling — it emits notifications on event. The coach receives those notifications as messages, which then trigger its polling cycle per workaround v1. This works today and is what the "PR watch v4 — filtered" pattern does for CI/merge events. The gap was that it didn't watch reviews/comments — a deficiency that would be closed by extending the Monitor's watched event types (see `[[monitor-pr-review-comment-routing]]` proposal).

## Cross-references

- `[[workflow-orchestrator-daemon]]` (proposal) — the productionized scheduler
- `[[monitor-pr-review-comment-routing]]` (proposal, PR #160) — external monitor extension for review/comment events
- `[[layered-pr-fanout-spec]]` §3.3, §3.4 — decay/staleness annotations assume cadenced re-evaluation; same trap applies to runtime-enforced decay
