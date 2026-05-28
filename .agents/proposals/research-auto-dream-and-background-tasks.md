# Research — auto-dream pattern + background-tasks design for the dot-agents service command

**Scope:** project-local proposal (per `[[proposal-routing]]`). Informs the
service command name + invocation pattern that R3 will land. Does NOT itself
modify code or rules.

**Created:** 2026-05-28
**Bundle:** `.agents/active/delegation-bundles/del-research-auto-dream-and-background-tasks-1779991498.yaml`
**Author:** research-auto-dream-and-background-tasks subagent

## §1 What is "auto-dream"?

"Auto-dream" is **not on disk** in `~/.agents`, `~/Documents/tmp/lacp`, or this
repo (verified via `find` for `*dream*` + `grep` for `auto-dream|auto_dream|autodream`
— only hits are unrelated `node_modules` shiki language tokens and this proposal's
own bundle). However it **is a real, currently-emerging 2026 industry term** that
the user is almost certainly drawing from. The dev.to "AI Agent Memory in 2026:
Auto Dream, Context Files, and What Actually Works" article (Quimby, 2026)
documents it as a **named, scheduled Claude Code feature**; OpenClaw, Hermes,
and the `rem-sleep-skill` repo describe analogous patterns under "Dreaming" /
"REM Sleep."

### Industry-canonical definition (Claude Code "Auto Dream")

> "Automated memory consolidation feature in Claude Code that processes agent
> context similarly to human sleep cycles. The system reorganizes, refines,
> and purges accumulated session data to maintain cognitive performance over
> extended interactions."

Trigger rule (Claude Code): **24+ hours elapsed AND 5+ sessions** since last
consolidation. Three phases:

1. **Orient** — audit existing memory storage to establish baseline.
2. **Gather** — read-only analysis of local session transcripts to identify
   "patterns, corrections, decisions, and lessons that should persist."
3. **Consolidate** — merge new findings into memory, remove stale entries,
   resolve contradictions, convert relative→absolute timestamps.

### Adjacent industry-canonical pattern ("Dreaming," OpenClaw / Hermes / rem-sleep-skill)

Three-phase, **cron-scheduled (default `0 3 * * *`)** background process:

1. **Light Sleep (ingest + stage)** — read daily memory + session transcripts,
   dedup via Jaccard, stage candidates in short-term recall. **Never writes
   `MEMORY.md`.**
2. **REM Sleep (pattern recognition)** — lookback window (default 7 days),
   extract recurring themes, identify candidate truths. **Never writes
   `MEMORY.md`.**
3. **Deep Sleep (promote)** — score candidates against weighted signals,
   apply reinforcement boosts, filter by threshold, promote survivors to
   `MEMORY.md`. **Only writer.**

CLI surface: `/dreaming on|off|status`, `openclaw memory promote [--apply --limit N]`,
`openclaw memory promote-explain "<key>"`, `openclaw memory rem-harness`.

State lives in `memory/.dreams/` (short-term-recall, phase-signals,
daily-ingestion, session-corpus, events.jsonl audit log). Human-readable
outputs are `DREAMS.md` (diary) and `MEMORY.md` (promoted entries).

### Distinction from auto-research (active iterative improvement)

| Axis | Auto-research (LACP autoresearch) | Auto-dream (OpenClaw / Claude Code) |
|---|---|---|
| Goal | Maximize a Health Score by **editing code** | Maintain memory hygiene by **editing memory** |
| Object of change | Source files (`tui/`, `optimize.py`, config) | Memory files (`MEMORY.md`, `.dreams/`) |
| Loop shape | LOOP FOREVER, ratchet on metric improvement | Cron-triggered, threshold-gated, idempotent |
| Failure semantics | `git checkout --` revert if metric drops | Light/REM never write; only Deep writes |
| Concurrency model | Single autonomous worker, never stops | Scheduled batch, runs and exits |
| Analogue | Hill-climbing optimizer | REM consolidation / GC |
| Output artifact | git commits | Promoted memory entries + dream diary |

**Both are autonomous background work, but they are not the same class.**
Auto-research is *active* (changing the world); auto-dream is *latent*
(consolidating what's already been seen). Equivalent to "the agent at work" vs
"the agent asleep." Both are pre-conditions for long-running agent systems;
neither replaces the other.

## §2 Background-tasks pattern survey

| Platform / pattern | Scheduling | Auth + secrets | State persistence | Failure semantics | Relevance to dot-agents |
|---|---|---|---|---|---|
| **LACP `lacp-brain-nightly` cron** | OS cron `0 0 * * *` invokes `bin/lacp-brain-nightly --apply` | Inherits caller env | `~/.lacp/logs/nightly.log` + brain repo git history | Logs swallow failures; manual recovery | Closest in-spirit precedent for a `da` background command. External cron, single binary, idempotent |
| **LACP autoresearch (`autoresearch/program.md`)** | Foreground `LOOP FOREVER` driven by an agent harness; not OS-scheduled | None (no creds touched) | `autoresearch/results.tsv` + git commits | `git reset --hard HEAD~1` on metric regression | Model for "active" autonomous worker; *not* the model for a service command |
| **Claude Code Auto Dream** | Internal trigger: 24h elapsed + 5 sessions; fires on session start | None | Per-platform memory file (`CLAUDE.md`, `MEMORY.md`) | Idempotent merge; safe to re-run | Direct precedent if `da` wants memory-consolidation behavior |
| **OpenClaw Dreaming (managed cron)** | Managed cron job inside the gateway, default `0 3 * * *`, configurable per `frequency` setting; reconciled on gateway startup | Inherits gateway env | `memory/.dreams/*.json` + `events.jsonl` audit | Three-phase gate (Light/REM read-only; only Deep writes); audit trail mandatory | Strongest pattern for *staged, restart-safe, observable* background work. Maps directly onto R3's watermark + event-bus design |
| **Anthropic memory tool + prompt caching** | Per-request (turn-bounded); cache TTL governs reuse | Anthropic API key | Server-side cache + client-side memory file | Cache miss → recompute | Not a scheduler; relevant only for the *content* a dream-style task consolidates |
| **Cloudflare Workflows / Durable Objects alarms** | Durable execution with first-class `alarm()` timers; replay-safe | Per-binding | DO storage / Workflow checkpoints | Step-level retry, dead-letter queue, observability built in | Aspirational target if dot-agents ever needs distributed scheduling; today overkill |
| **Cobra subcommand + `Stop` hook (claude-code skill)** | Hook fires at agent stop; no timer | Inherits user shell | Whatever the hook command writes | Hook exit code = success/fail; no retry | Already used by `iteration-close`; useful for "end of session" consolidation, NOT for cron-class work |
| **`da workflow sweep`** | User-triggered; intended to be called by an external scheduler (or `da workflow drift` watcher) | Inherits user env | Repo-local `.agents/active/*` + plan files | Reports drift, optional apply; idempotent | Already the *manual* shape of an auto-dream-ish maintenance command. Missing piece is the scheduler |
| **`da workflow drift`** | User-triggered read-only | Inherits user env | None (pure read) | Read-only — safe | Same shape as Light Sleep (read-only stage) |
| **Coach per-message poll (`[[agents-lack-autonomous-timers]]` v1)** | Implicit: piggyback on every incoming SendMessage | N/A | None | Latency bounded by message frequency, not timer | Workaround, not a real scheduler. v2 is `[[workflow-orchestrator-daemon]]` |
| **`workflow-orchestrator-daemon` (planned)** | Long-running `da workflow run` with OS timer | Inherits caller | Event queue | Daemon owns retry + emit | This IS the unbuilt service command we are naming |
| **R3 `da service run` (planned, see spec)** | Foreground long-running cobra command with internal scheduler (~300-500 LoC, fsnotify + interval); operator owns systemd/launchd | Inherits caller | Per-task `.agents/active/service-state/<task>.watermark.yaml` (atomic write) | Panic-recovery per task; bounded shutdown ≤5s; in-process pub/sub bus | The spec is already accepted. This proposal exists to refine its **command name** |

### Top-3 patterns most relevant to dot-agents

1. **OpenClaw Dreaming (managed cron + staged gates + audit log)** — the
   discipline of "Light/REM never write; only Deep writes" maps onto R3's
   watermark sidecars and its non-blocking event bus. Adopt the three-stage
   read-then-promote semantic for any consolidation task we add.
2. **LACP brain-nightly cron** — proves operators are happy with `external
   cron → single binary --apply` for nightly maintenance work. Matches R3's
   D1 decision (operator owns daemonization). Keep this as the v1 deploy story.
3. **Cloudflare Workflows / DO alarms** — the aspirational target for
   *distributed* durable work. R3's spec D2 explicitly notes the re-evaluation
   trigger (swap to `river` if R4 grows multi-machine workers). Same logic
   applies if we ever want auto-dream across many repos.

## §3 dot-agents existing background-task surface

### What we already have

- **`da workflow sweep`** — manual drift detection + optional apply across
  managed repos. The "actor" shape is right; the scheduling is absent.
- **`da workflow drift`** — manual read-only drift detection. Auto-dream
  Light Sleep analogue.
- **`da workflow checkpoint` / `close-task` / `start-task`** — iteration-close
  primitives, fired by the `iteration-close` skill on each loop turn.
- **`da score run` / `da score iteration` / `da score session`** — telemetry
  rollup over iteration-log sidecars. These are exactly what R3's
  rescore-on-rubric-bump task wraps.
- **`da session stats`** — per-platform session aggregation (read-only).
- **`Stop` / `SessionStart` / `UserPromptSubmit` hook surface** — wired
  through `~/.agents/hooks/` and propagated by `da refresh`. Fires on
  session events, not on a timer.
- **R1 outcome scoring** — shipped 2026-05-25; supplies the data any
  consolidation task would read.

### What's missing

1. **No long-running process.** Every command above is fire-and-exit. No
   place for a fsnotify watcher, an interval task, or an event bus to live.
2. **No internal scheduler.** Cron-style cadence has to come from OS cron
   (LACP-style) or a subagent's per-message poll (lesson trap).
3. **No event bus.** Consumers (R2 dashboard, R5 review queue) cannot react
   to telemetry without polling.
4. **No memory-consolidation primitive.** No analogue of Auto Dream / OpenClaw
   Deep Sleep that takes the lesson + iteration-log corpus and emits a
   promoted, deduped summary. Lessons today are hand-curated under
   `.agents/lessons/`. The closest automated thing is `fold-back`, but that
   is event-driven (loop observation), not scheduled.
5. **No HTTP surface.** R2 + R5 need it; R3 spec reserves it.

R3 (`r3-background-worker-service`) covers items 1, 2, 3, 5. Item 4
(memory consolidation) is **uncovered** by any current plan or spec, and is
the strongest candidate "auto-dream task" if/when the user wants one.

## §4 Recommendation — service command name + invocation pattern

### Recommended name: keep `da service` (per R3 spec D1)

Adopt the R3 spec's decision **as-is**:

```
da service run        # foreground long-running scheduler + HTTP host
da service status     # health snapshot
da service stop       # POST loopback /admin/stop (per OQ4)
```

The naming is consistent with the existing surface (`da workflow`, `da kg`,
`da score`, `da session` are all noun-scoped parents with verb children).
`service` is the noun that best captures "the long-running thing that hosts
background tasks and exposes HTTP." It does NOT bias toward any one task
class (auto-dream, auto-research, observability) — it is the **host**,
not the workload.

### Invocation pattern: foreground long-running + operator-owned daemonization

Adopt R3 D1 verbatim:

- `da service run` runs in the foreground.
- Daemonization is the operator's job (systemd unit on Linux, launchd plist
  on macOS, `nohup` for ad-hoc).
- External `cron` remains the right tool for *batch* maintenance work that
  doesn't need a long-running host (LACP `brain-nightly` model). Continue
  to support `da workflow sweep` as a cron-callable, idempotent CLI; do not
  fold it into the service unless real-time triggers are required.

This gives two complementary invocation modes:

| Mode | Use when | Example |
|---|---|---|
| **Long-running service** | Need fsnotify, in-process events, HTTP, or sub-minute scheduling | `da service run` under systemd |
| **External cron + idempotent CLI** | Daily/weekly maintenance; no live consumers | `0 3 * * * da workflow sweep --apply` |

### Where auto-dream-style work fits

The R3 spec lists 2 v1 tasks (iter-log fsnotify ingester + rescore-on-rubric-bump).
It explicitly defers a KG-staleness-refresh task (OQ1). The natural slot for
auto-dream-class work is **a third scheduler task** added in v1.1 or v1.2:

```
da service run
  ├── task: iterlog-ingester          (fsnotify, real-time)
  ├── task: rescore-on-rubric-bump    (interval, idempotent)
  ├── task: kg-staleness-refresh      (deferred per OQ1)
  └── task: lesson-consolidator       (NEW — auto-dream analogue; deferred)
```

`lesson-consolidator` would:

1. **Light Sleep equivalent** — read iter-logs + active/fold-back observations
   over a lookback window. Stage candidates. Never write.
2. **REM Sleep equivalent** — cluster candidates against existing
   `.agents/lessons/*/LESSON.md` headlines. Identify recurring patterns
   that should become lessons OR additions to existing lessons. Never write.
3. **Deep Sleep equivalent** — emit `.agents/proposals/lesson-<name>.md`
   (project-local proposals; never auto-modify `~/.agents/rules/`). The
   user reviews via `da review` (global) or by hand (project-local).

This keeps **all writes gated through the existing proposal/review loop** —
the auto-dream task suggests, the operator promotes. Mirrors OpenClaw's
"only Deep Sleep writes; only after gating" discipline.

## §5 Naming-consistency review

Since auto-dream **is** a real industry term (and one the user is clearly
tracking), the dot-agents surface should be ready to host an `auto-dream`
*task* without confusing it with the *service*. Recommended naming
discipline:

- **Hosts (long-running processes):** noun-scoped parent — `da service`,
  `da workflow`, `da kg`. Never name a host after a single task class.
- **Tasks inside the host:** named by what they consolidate, not by the
  biological metaphor — `lesson-consolidator`, `kg-staleness-refresh`,
  `rescore-on-rubric-bump`, `iterlog-ingester`.
- **User-facing batch commands (cron-callable):** noun + verb pair —
  `da workflow sweep`, `da workflow drift`. If/when a memory-consolidation
  CLI lands ahead of the scheduler task, name it `da memory consolidate`
  (foreground analogue) and `da memory promote` (mirrors OpenClaw's CLI
  surface, which users may already know).

**Do not** name a top-level command `da dream` or `da auto-dream`. Reasons:

1. The biological metaphor is non-discoverable for new contributors.
2. It commits the surface to one workload class before we have shipped
   even one scheduler task.
3. It collides with OpenClaw's `/dreaming` slash command convention, which
   is per-skill, not per-binary.
4. The R3 spec already named the host (`service`) and was accepted.

If the user genuinely wants a verb that signals "autonomous maintenance"
across both auto-research and auto-dream classes, the right move is a
**rules-level taxonomy** (`docs/AUTONOMOUS_TASKS.md`) that classifies
each scheduler task as `active` (auto-research) vs `latent` (auto-dream)
— not a command rename.

## §6 Open questions

- **OQ-A — Is "auto-dream" the user's term, or borrowed from external work?**
  External: the dev.to + OpenClaw articles are dated 2026 and pre-date this
  proposal. The user has likely read one of them. Confirm with the user that
  the intended semantic is the "memory consolidation" one (not, e.g., a
  general "any background autonomy" umbrella).
- **OQ-B — Should the service command consume from `workflow-orchestrator-daemon`,
  or replace it?** The lesson `[[agents-lack-autonomous-timers]]` names
  `workflow-orchestrator-daemon` as the v2 timer source. The R3 spec is the
  same concept under a different name (`da service`). **Recommendation:**
  treat them as the same plan; rename `workflow-orchestrator-daemon` proposals
  to point at `r3-background-worker-service` to avoid two names for one thing.
- **OQ-C — Does the project want a `lesson-consolidator` task in v1.x?** Not
  scoped by any current spec. If yes, fold into r3 as a deferred task; if no,
  document as a future proposal. The shape is well-understood (OpenClaw maps
  it almost 1:1) and the gating (proposals not direct writes) is already in
  place.
- **OQ-D — Does the v1 service need a memory-store at all,** or only the
  watermark sidecars R3 already specifies? Auto-dream-class work (read iter
  logs → emit proposals) does not need new storage; reuses the existing
  iter-log + proposals dirs. Confirm before opening any v1.x ticket.
- **OQ-E — `da memory` namespace?** If the user wants the OpenClaw CLI
  ergonomics (`memory promote`, `memory rem-harness`, `memory status`)
  available foreground without spinning up a long-running service, that's
  a separate command parent. Decide whether to reserve `da memory` now.

## §7 Existing `da` surface — classification table

| Subcommand | Category | Notes |
|---|---|---|
| `da init` | scaffold | One-shot bootstrap of `~/.agents/` |
| `da add` | scaffold | Add project to management |
| `da remove` | scaffold | Remove project from management |
| `da refresh` | lifecycle | Re-link managed setup; idempotent |
| `da import` | scaffold | Pull configs into `~/.agents/` |
| `da status` | observability | Read-only; managed projects + link health |
| `da doctor` | observability | Read-only diagnostics |
| `da skills` | lifecycle | Manage skills inventory |
| `da agents` | lifecycle | Manage agent registry |
| `da hooks` | lifecycle | Hook bundle CRUD |
| `da rules` | lifecycle | Rule file CRUD |
| `da mcp` | lifecycle | MCP config CRUD |
| `da settings` | lifecycle | Settings CRUD |
| `da review` | maintenance | Process proposals (`~/.agents/proposals/`) |
| `da sync` | maintenance | Git ops on `~/.agents/` |
| `da explain` | observability | Concept lookup |
| `da install` | scaffold | Set up project from `.agentsrc.json` |
| `da session` | observability | Per-platform session inspection |
| `da workflow` | **mixed (runtime + observability)** | Houses orient, next, eligible, sweep, drift, fanout, merge-back, fold-back, advance, etc. |
| `da kg` | runtime | Knowledge graph CRUD + queries |
| `da score` | observability | Compute + query agent-run outcome scores |
| `da help` | meta | – |
| `da completion` | meta | – |

### Where `da service` slots in

`da service` introduces a **new category — runtime host.** No current parent
plays this role; `da workflow run` (hypothetical) would have implied that
the workflow surface owned the lifetime of a daemon, which it doesn't.
`da service` cleanly says: "this is the long-running process that the other
workloads hang off of."

### Where a future `da memory` (if approved per OQ-E) would slot in

`da memory` would join `da kg` and `da score` in the **runtime** category —
all three are noun-scoped CRUD-and-query surfaces for a single data domain
(graph, scores, memory). A `da memory consolidate` foreground command would
mirror `da score run`; a `da memory promote` would mirror `da workflow advance`.
Defer until the user confirms OQ-C + OQ-E.

## Decision request

1. **Confirm** that `da service` (per r3-background-worker-service spec) is
   the canonical service command name — no rename to `da dream`, `da daemon`,
   `da auto-research`, etc.
2. **Confirm** that auto-dream-class workloads land as **scheduler tasks
   inside `da service`**, not as a peer top-level command.
3. **Resolve OQ-B** by unifying `workflow-orchestrator-daemon` references
   with `r3-background-worker-service`.
4. **Defer** OQ-C/OQ-D/OQ-E (lesson-consolidator, memory storage, `da memory`
   namespace) until R3 v1 ships, then re-open as v1.x proposals.

## Sources

- `~/Documents/tmp/lacp/config/autoresearch-program.md` (LACP autoresearch loop)
- `~/Documents/tmp/lacp/autoresearch/program.md` (LACP brain autoresearch)
- `~/Documents/tmp/lacp/config/lacp-brain-nightly.cron` (cron template)
- `.agents/workflow/specs/r3-background-worker-service/design.md` (accepted R3 spec)
- `.agents/workflow/specs/agent-run-scoring-observability-platform/design.md` (umbrella spec)
- `.agents/lessons/agents-lack-autonomous-timers/LESSON.md` (timer trap)
- [AI Agent Memory in 2026: Auto Dream, Context Files, and What Actually Works (Quimby, dev.to)](https://dev.to/max_quimby/ai-agent-memory-in-2026-auto-dream-context-files-and-what-actually-works-39m8)
- [OpenClaw Dreaming Guide 2026: Background Memory Consolidation for AI Agents (czmilo, dev.to)](https://dev.to/czmilo/openclaw-dreaming-guide-2026-background-memory-consolidation-for-ai-agents-585e)
- [LLM Sleep Based Learning: REM-style Cycles and Synthetic Dreaming (gallahat, substack)](https://gallahat.substack.com/p/llm-sleep-based-learning-implementing)
- [Dreaming — Automatic Background Memory Consolidation (Hermes Agent issue #25309)](https://github.com/NousResearch/hermes-agent/issues/25309)
- [rem-sleep-skill (stewnight)](https://github.com/stewnight/rem-sleep-skill)
- [Evolving Memory (EvolvingAgentsLabs)](https://github.com/EvolvingAgentsLabs/evolving-memory)
