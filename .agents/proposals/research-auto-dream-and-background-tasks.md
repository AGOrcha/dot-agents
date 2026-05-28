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

### In-repo canonical source (added 2026-05-28 fold-in)

The original draft above was outside-in. A primary in-repo source was missed:
**`research/articles-evaluation-kg-and-adjacent.md`** (947 lines) catalogues
the same primitive under three converging external names and already files
`[GAP-ADOPT]` action items against dot-agents:

- **Thoth's dream cycle** (line 244) — concrete 4-phase nightly process:
  (i) duplicate merging at 0.93+ similarity → (ii) description enrichment →
  (iii) relationship inference → (iv) confidence decay on relations older
  than 90 days. Caveat lifted at line 244: *"in dot-agents this should
  produce `review_due` / proposed links, not clock-based staleness"* —
  matches the gating discipline (§4 below).
- **arscontexta's reweave** + **akshay_pachaar's `memify()`** (lines 124, 137,
  299, 317, 627) — three independent articles on the same consolidation
  primitive. The research doc names this convergence as the strongest signal
  in the corpus: *"three independent articles on the same consolidation
  primitive."*
- **`memify()`'s four ops** (line 124) — `strengthen / prune / auto-tune /
  add-derived` — are near-1:1 with scoped-KG's four drivers
  (source-mutation / derivation-mutation / revocation / contradiction).
- **C.4 placeholder** — the corpus expects a `dream-cycle / consolidation`
  spec at index C.4. That is the natural canonical home for the scheduler
  task this proposal names below.
- **"Reread the past"** (lines 299, 317) — the load-bearing motivation, not
  hygiene: meaning is re-derived as the ontology evolves. Sharpens our §2.6
  propagation work.

`[GAP-ADOPT]` items already filed against dot-agents in that research file
that an auto-dream-class scheduler task would consume:

- **dream-cycle / nightly consolidation job** (line 254) — direct adoption
  recommendation; Thoth's 4 phases map onto scoped-KG maintenance.
- **`last_accessed_at` + `access_count` on `KGNote`** (line 282) — two
  additive fields; feed (1) `kg query` recency scoring, (2) pruning
  candidate set (access_count = 0 in window + no linked active plans).
- **`capture_lag_seconds` on `KGNote`** (line 358) — "speed-of-surfacing"
  metric; weights recently-captured notes higher for review-nudge firing.
- **Semantic deduplication pass** (line 164) — Blockify's 80–85% cosine
  dedup is the model; human-validated merge proposals, not autonomous.
- **`version_state` enum on `KGNote`** (line 165) — pairs with author field
  and the dream-cycle's pruning decision.

**Material correction to §4 below:** the recommended scheduler task in
§4 was originally named `lesson-consolidator`. The in-repo corpus points
at a **broader, KG-scoped consolidator** (`scoped-kg-dream-cycle`) whose
inputs are the full `KGNote` corpus, not just lessons. Lessons are one
NoteType; restricting scope to lessons would re-do the work for every
other NoteType. Section §4 has been amended to reflect this.

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
auto-dream-class work is **a third scheduler task** added in v1.1 or v1.2,
and the canonical-source survey above sharpens its scope to the **full
`KGNote` corpus**, not just lessons:

```
da service run
  ├── task: iterlog-ingester               (fsnotify, real-time)
  ├── task: rescore-on-rubric-bump         (interval, idempotent)
  ├── task: kg-staleness-refresh           (deferred per OQ1; consumed by below)
  └── task: scoped-kg-dream-cycle          (NEW — Thoth 4-phase; deferred to C.4 spec)
       ├── lesson-consolidator             (lessons NoteType only)
       ├── decision-consolidator           (decisions NoteType only)
       ├── research-claim-consolidator     (research-claims NoteType only)
       └── ... per NoteType
```

`scoped-kg-dream-cycle` would implement Thoth's 4 phases against the
`KGNote` corpus (per-scope), gated through the proposal/review loop. The
4 ops map to `memify()`'s vocabulary (strengthen / prune / auto-tune /
add-derived) which converges with scoped-KG's existing 4 drivers:

1. **Duplicate merging** (Thoth phase 1 / `memify().prune`) — cluster notes
   within scope by cosine similarity ≥0.93 (Thoth) or 0.80–0.85 (Blockify);
   emit `.agents/proposals/kg-merge-<scope>-<id>.md` per candidate cluster.
   Never auto-merges. Human reviews via `da review`.
2. **Description enrichment** (Thoth phase 2 / `memify().add-derived`) —
   re-derive note frontmatter where the ontology has grown (per
   `[[scoped-knowledge-graphs]]` §2.6 propagation). Emit proposals.
3. **Relationship inference** (Thoth phase 3 / `memify().strengthen`) —
   infer missing links from co-citation patterns. Emit proposals; existing
   `derivation-mutation` driver consumes if approved.
4. **Confidence decay** (Thoth phase 4 / `memify().auto-tune`) — NOT
   clock-based per the research-file caveat; instead fire `review_due`
   when access_count = 0 in lookback window AND no linked active plans
   AND no derivation children. Consumes the `last_accessed_at` /
   `access_count` GAP-ADOPT fields above.

This keeps **all writes gated through the existing proposal/review loop** —
the auto-dream task suggests, the operator promotes. Mirrors OpenClaw's
"only Deep Sleep writes; only after gating" discipline AND Thoth's "produce
proposed links, not clock-based staleness" caveat from the research-file
fold-in.

`lesson-consolidator` shrinks to one NoteType-specific sub-task inside
the broader `scoped-kg-dream-cycle` host — preserves the original
narrowed-shape recommendation while making room for `decision-consolidator`
and per-NoteType siblings to come on the same scheduler hook.

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
- **OQ-C — Does the project want a `scoped-kg-dream-cycle` task in v1.x?**
  Broader than the original `lesson-consolidator` framing — the in-repo
  research file (`research/articles-evaluation-kg-and-adjacent.md`) names
  the **C.4 dream-cycle / consolidation spec** as the canonical home and
  files `[GAP-ADOPT]` items that this task would consume. If yes, fold into
  r3 as a deferred task and **also** open the C.4 spec under
  `.agents/workflow/specs/scoped-kg-dream-cycle/design.md`. The 4-phase
  shape is well-understood (Thoth + Blockify + memify converge); gating
  via proposals is already in place.
- **OQ-D — Does the v1 service need a memory-store at all,** or only the
  watermark sidecars R3 already specifies? Auto-dream-class work (read iter
  logs → emit proposals) does not need new storage; reuses the existing
  iter-log + proposals dirs. **However** the GAP-ADOPT items
  (`last_accessed_at`, `access_count`, `capture_lag_seconds`,
  `version_state`) are additive `KGNote` fields — they extend the existing
  warm store, not introduce a new one. Confirm before opening any v1.x
  ticket; if accepted, these become part of the C.4 spec's "consumed
  inputs" section.
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

**Primary in-repo source (fold-in 2026-05-28):**

- `research/articles-evaluation-kg-and-adjacent.md` (947 lines) — corpus
  evaluation; cites Thoth's dream cycle (line 244), arscontexta's reweave
  (line 124), akshay_pachaar's `memify()` (lines 117, 124, 137), Blockify's
  semantic dedup pipeline (lines 155, 164), Weng's episodic/semantic/procedural
  taxonomy (lines 117, 120). Names the C.4 consolidation spec as the
  canonical home; files `[GAP-ADOPT]` items consumed by this proposal's §4.

**dot-agents internal:**

- `~/Documents/tmp/lacp/config/autoresearch-program.md` (LACP autoresearch loop)
- `~/Documents/tmp/lacp/autoresearch/program.md` (LACP brain autoresearch)
- `~/Documents/tmp/lacp/config/lacp-brain-nightly.cron` (cron template)
- `.agents/workflow/specs/r3-background-worker-service/design.md` (accepted R3 spec)
- `.agents/workflow/specs/agent-run-scoring-observability-platform/design.md` (umbrella spec)
- `.agents/workflow/specs/scoped-knowledge-graphs/design.md` (NoteType + 4 drivers)
- `.agents/lessons/agents-lack-autonomous-timers/LESSON.md` (timer trap)
- [AI Agent Memory in 2026: Auto Dream, Context Files, and What Actually Works (Quimby, dev.to)](https://dev.to/max_quimby/ai-agent-memory-in-2026-auto-dream-context-files-and-what-actually-works-39m8)
- [OpenClaw Dreaming Guide 2026: Background Memory Consolidation for AI Agents (czmilo, dev.to)](https://dev.to/czmilo/openclaw-dreaming-guide-2026-background-memory-consolidation-for-ai-agents-585e)
- [LLM Sleep Based Learning: REM-style Cycles and Synthetic Dreaming (gallahat, substack)](https://gallahat.substack.com/p/llm-sleep-based-learning-implementing)
- [Dreaming — Automatic Background Memory Consolidation (Hermes Agent issue #25309)](https://github.com/NousResearch/hermes-agent/issues/25309)
- [rem-sleep-skill (stewnight)](https://github.com/stewnight/rem-sleep-skill)
- [Evolving Memory (EvolvingAgentsLabs)](https://github.com/EvolvingAgentsLabs/evolving-memory)
