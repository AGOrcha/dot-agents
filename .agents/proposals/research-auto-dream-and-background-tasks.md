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

### Adjacent evaluations — lessons/skills graduation pipeline (added 2026-05-28 second fold-in)

A second pass through `research/evaluations/*.md` (six files) and
`research/AUTONOMOUS_WORKFLOW_MANAGEMENT_RESEARCH.md` materially adds to
the picture. The dream-cycle / consolidation primitive is **named under
five different synonyms** across these evaluations, all pointing at the
same operation:

| Synonym (source) | Eval file | What it labels |
|---|---|---|
| dream cycle (Thoth, KG-adjacent) | `articles-evaluation-kg-and-adjacent.md` (folded above) | 4-phase nightly KG maintenance |
| reweave (arscontexta) | `lessons-and-memory.md` §A; `workflow-orchestration.md` W.5, B-3 | Backward-pass update of prior artifacts after a new one lands |
| `memify()` (akshay_pachaar) | `articles-evaluation-kg-and-adjacent.md` (folded above) | Four ops: strengthen / prune / auto-tune / add-derived |
| graduation (second-brain-needs-two-authors, skill-tiering-contract) | `skills-rules-graduation.md` S.4, S.7, F.4; `lessons-and-memory.md` Part C | `author: agent → author: human` flip + version bump + behavior-preservation gate |
| promotion / managed compounding (intuitiveml, witcheer L4) | `workflow-orchestration.md` G.2 (in inventory addendum); `skills-rules-graduation.md` Part B | Lifecycle: observe → evaluate → retire; not chaotic proliferation |

**Synonym-reconciliation recommendation** (new OQ-F below): adopt
**"consolidation"** as the canonical umbrella term in dot-agents'
in-repo vocabulary, with the following narrower terms reserved for the
specific phase they describe:

- **Consolidation** = the umbrella class of "scheduled, idempotent,
  proposal-gated work that re-derives durable artifacts from
  accumulated raw signal." Synonyms above all map here. This is what
  the C.4 spec slot should be named.
- **Dream cycle** = the specific scheduler task implementing
  consolidation against the `KGNote` corpus (Thoth's 4 phases). One
  task, one name, no overload.
- **Graduation** = the specific *write step* inside consolidation that
  flips `author: agent → author: human` AND bumps `version: x.y.z` AND
  passes the app-type profile's §6.2 behavior-preservation gate
  (`skills-rules-graduation.md` F.4). Always a proposal/review-gated act.
- **Reweave** = the propagation walker that runs as a sub-phase of
  consolidation against the *plan graph* — distinct store, shared
  shape (`workflow-orchestration.md` second-pass finding #3:
  *"Reweave (plan graph) and KG derivation propagation are one
  primitive"*).
- **Promotion** = the user-facing CLI verb (`da kg promote`,
  `da review approve`) that lands a graduation. In dot-agents
  vocabulary, **promotion always operates on `KGNote` rows**, not on
  a separate memory file — see "Memory IS the KG" subsection below.

So: `consolidation` (class) ⊃ `dream cycle` (task) ⊃ `reweave` (sub-phase) +
`graduation` (write step) ⊆ `promotion` (CLI surface).

**Critical load-bearing constraint (`workflow-spec-plan-inventory.md`
"Logical Flaws to Correct" §1, quoted verbatim):**

> "Nightly consolidation must not be confused with KG staleness. The
> scoped-KG spec is explicit: staleness is event-driven, while time-based
> review nudges are separate. A dream cycle can dedupe, propose links, and
> raise review nudges, but it must not mark facts stale merely because
> they aged."

This is THE governing rule for the §4 `scoped-kg-dream-cycle` task. The
§4 phase 4 ("Confidence decay") already complied with this in spirit
(no clock-based staleness; fires `review_due` only when access_count=0
AND no linked plans AND no derivation children). Calling the constraint
out explicitly here, with its canonical statement, removes any future
ambiguity. Section §4 phase 4 is now considered the authoritative
encoding of this constraint.

Other material adds from this second fold-in:

- **One schema, three projections** (`lessons-and-memory.md` F.1;
  reaffirmed in `workflow-spec-plan-inventory.md` second-pass #5).
  `KGNote` is the canonical schema; lessons (`LESSON.md`), Claude-Code
  auto-memory files, Cursor rules, Codex prompts are projections that
  read/write the same warm-store rows. **Consequence for the §4 task
  tree:** the per-NoteType sub-tasks (`lesson-consolidator`,
  `decision-consolidator`, etc.) are not separate consolidators; they are
  consolidation passes parameterized by NoteType against the single
  `KGNote` store. Earlier framing implied per-store dialects; correct
  framing is per-NoteType *projection rules* on one canonical schema.
- **Same-scope vs cross-scope contradiction** (`lessons-and-memory.md`
  E, F.4; `workflow-spec-plan-inventory.md` second-pass #4). Same-scope
  contradictions auto-stale the older entry; cross-scope disagreements
  remain fresh on both sides and surface in `contradictions` read-time
  metadata. **Consequence for §4 phase 4:** the `review_due` pruning
  candidate score uses only same-scope contradiction count; cross-scope
  disagreement is never a pruning signal.
- **Graduation = author flip + version bump + behavior-preservation
  gate, in that order** (`skills-rules-graduation.md` F.4). Refines the
  earlier `proposal/review` framing: graduation is not one write but
  three independently-auditable steps. A scheduler task emitting a
  graduation proposal must produce all three diffs, not just the
  `author:` flip.
- **First production scheduled job is `fold-back triage`, not
  `scoped-kg-dream-cycle`** (`hooks-and-platform.md` H.5;
  `lessons-and-memory.md` L.3 + L.4 sequencing). Triage is simpler
  (read fold-back observations, cluster, score, propose plan updates)
  and uses existing primitives; it's the right MVP before the broader
  dream-cycle task. **Recommendation update for §4 ordering:** before
  `scoped-kg-dream-cycle`, land `fold-back-triage` as the v1.1
  scheduler task. Both are scope-aware; both are proposal-gated;
  triage is the smaller surface and surfaces operational lessons that
  shape the dream-cycle's design.
### Memory IS the KG (added 2026-05-28 third fold-in — maintainer correction)

A material reframing applies to every "memory" reference in §1–§7 of the
original draft. **dot-agents has no separate memory layer.** What the
external corpus calls "memory" (OpenClaw `MEMORY.md`, Claude Code
`/mnt/memory/`, Hermes Dreaming workspace store) is in dot-agents
**precisely the `KGNote` corpus** behind the graph-backend adapter.

Sources confirming this:

- `.agents/workflow/specs/graph-backend-adapter-contract/design.md` —
  the canonical contract for note + edge + symbol-link storage. There
  is no second store for "memory." The `KGNoteStore` and
  `NoteSymbolLinkStore` interfaces are the entire persistence surface
  for the memory class of artifacts.
- `.agents/workflow/specs/scoped-knowledge-graphs/design.md` §4.3
  "review-nudge policy expression" — review-nudge IS the
  consolidation-trigger mechanism; it is defined per-note-type on the
  KG, not on any separate memory layer.
- `lessons-and-memory.md` F.1 (already cited in §1.6 above) —
  "**one schema, three projections**" — `KGNote` is the canonical
  schema; lessons / Claude-Code auto-memory / Cursor rules / Codex
  prompts are all *projections* that read and write the same
  warm-store rows.

**Consequences for this proposal (corrections applied throughout):**

1. **No `da memory` namespace.** The proposed `da memory consolidate`
   and `da memory promote` commands (§5, §6 OQ-E, §7 "future") were
   wrong-shaped. The correct surface is `da kg consolidate` and
   `da kg promote`, operating on `KGNote` rows. `da memory` would
   imply a second store; none exists, none is planned.
2. **No "memory storage" question in OQ-D.** The graph-backend adapter
   already owns this surface. The watermark sidecars R3 specifies are
   service-internal scheduler bookkeeping, not memory. The
   `last_accessed_at` / `access_count` / `capture_lag_seconds` /
   `version_state` GAP-ADOPT items are additive `KGNote` columns
   landed via an adapter schema migration (per the adapter contract's
   §10 migration machinery), not a new store.
3. **OpenClaw `MEMORY.md` / Claude Code memory-file references in §1**
   describe an external ecosystem's choice. The dot-agents analogue is
   "a `KGNote` of NoteType=lesson|decision|rule|... reached
   `version_state: approved` via the graduation step in §1.6." Reading
   that note IS reading memory.
4. **Scheduler tasks under `da service` write proposals against the
   KG**, not against any memory file. `scoped-kg-dream-cycle` is named
   exactly right for this reason — it operates on the scoped KG.
5. **`da kg` becomes the canonical CLI surface for consolidation /
   promotion / dream-cycle status**, alongside its existing query +
   CRUD verbs. Reserve verbs: `consolidate`, `promote`, `dream-status`,
   `review-due` (the latter already implied by scoped-KG §4.3).

OpenClaw vocabulary (`memory promote`, `memory status`, `memory
rem-harness`) maps cleanly onto `da kg promote`, `da kg dream-status`,
`da kg dream-replay` if we want to preserve ergonomic transfer for
users from that ecosystem — without committing to a second store the
spec stack does not have.

### Architect role + cell vs compound vs molecule
  (`skills-rules-graduation.md` F.1; `agent-execution.md` F.1;
  `workflow-spec-plan-inventory.md` second-pass #2). Specs are T3
  cells; plans+bundles are T2 compounds; the *runtime* is the
  orchestrator. The proposal/review loop is the **Architect's** primary
  workflow primitive. Consolidation tasks therefore emit *proposals*
  (cell-tier candidates) that the Architect graduates; they never
  produce compound-tier artifacts directly. This matches OpenClaw's
  "only Deep Sleep writes; only after gating" exactly: consolidation
  proposes, the Architect (or `da review approve`) writes.
- **Scheduled jobs must declare `--scope` per write**
  (`hooks-and-platform.md` F.3). Already noted but worth elevating:
  any task on `da service` that writes to the warm store names its
  target scope. Cross-scope dedup is read-time metadata, never a write.
- **Verifier hooks ride app-type-profiles, not freelance scripts**
  (`hooks-and-platform.md` F.5). Tangential to naming but governs how
  consolidation tasks self-verify: a `scoped-kg-dream-cycle` proposal
  goes through `da profile verify` against the relevant profile (e.g.
  the `research` profile for research-claim NoteTypes; the `code`
  profile for rule NoteTypes), not against a freelance verifier.
- **Recursive accountability** (`workflow-spec-plan-inventory.md`
  second-pass #10). This proposal itself, once acted on, should be
  re-verified quarterly against the `research` profile. Cite this
  proposal in any v1.x C.4 spec as the research artifact whose
  freshness needs to age out.

**Operator-mode evidence** from `AUTONOMOUS_WORKFLOW_MANAGEMENT_RESEARCH.md`
line 306: the future-work bucket ("promote future work to active work,
split into subplans, or archive completed work") is the operator-mode
projection of consolidation against the *plan graph*. Reweave at plan
close already covered this in §4; the autonomous-workflow doc validates
the framing from a second in-repo source.

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
  ├── task: iterlog-ingester               (R3 v1; fsnotify, real-time)
  ├── task: rescore-on-rubric-bump         (R3 v1; interval, idempotent)
  ├── task: fold-back-triage               (NEW v1.1 MVP — intuitiveml-style; see OQ-G)
  ├── task: kg-staleness-refresh           (deferred per R3 OQ1; consumed by below)
  └── task: scoped-kg-dream-cycle          (NEW v1.2 — Thoth 4-phase; canonical C.4 home)
       ├── pass: lesson-NoteType           (projection of one canonical schema)
       ├── pass: decision-NoteType         (same store; projection rule differs)
       ├── pass: research-claim-NoteType   (same store; profile = `research`)
       └── ... per NoteType
```

Note: the per-NoteType nodes are **projection passes against the single
`KGNote` warm store**, not separate consolidators. Earlier framing in this
proposal implied per-store dialects; §1.6 fold-in (`lessons-and-memory.md`
F.1) corrects this to one canonical schema with per-NoteType projection
rules.

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
  `da workflow sweep`, `da workflow drift`. **Per "Memory IS the KG"
  fold-in (§1.7 above):** consolidation/promotion CLI lands under
  `da kg` (`da kg consolidate`, `da kg promote`, `da kg dream-status`),
  NOT under a separate `da memory` namespace. OpenClaw's `memory
  promote` ergonomics transfer cleanly without committing to a second
  store.

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
- **OQ-D — ~~Does the v1 service need a memory-store at all~~?**
  **RESOLVED 2026-05-28 (maintainer correction, see §1.7 "Memory IS the
  KG"):** there is no separate memory store. The graph-backend adapter
  contract is the persistence surface; `KGNote` rows ARE the memory.
  The GAP-ADOPT items (`last_accessed_at`, `access_count`,
  `capture_lag_seconds`, `version_state`) land as additive `KGNote`
  columns via an adapter schema migration (per
  graph-backend-adapter-contract §10), not as a new store. R3
  watermark sidecars are service-internal scheduler bookkeeping,
  unrelated to memory.
- **OQ-E — ~~`da memory` namespace?~~** **RESOLVED 2026-05-28
  (maintainer correction, see §1.7):** no separate namespace. The
  consolidation/promotion CLI surface lands under `da kg`
  (`da kg consolidate`, `da kg promote`, `da kg dream-status`,
  `da kg review-due`). OpenClaw vocabulary maps onto these verbs
  without implying a second store.
- **OQ-F — Adopt the consolidation/dream-cycle/reweave/graduation/promotion
  taxonomy** as the canonical in-repo vocabulary? (Per §1.6 fold-in.) The
  five terms above currently appear as informal synonyms across
  `research/evaluations/*.md`, and that ambiguity will leak into spec /
  task / proposal text once C.4 opens. **Recommendation:** land a
  one-paragraph "consolidation taxonomy" block in
  `.agents/rules/dot-agents/workflow-artifact-model.md` (or a new
  `consolidation-vocabulary.md` rule) before C.4. This is a rule-level
  edit, not a code change; cost is one PR; reversibility is trivial; it
  saves every future C.4 reviewer the cost of re-deriving the taxonomy.
- **OQ-G — Sequencing: `fold-back-triage` MVP before
  `scoped-kg-dream-cycle`?** Per `hooks-and-platform.md` H.5 +
  `lessons-and-memory.md` L.3/L.4. Fold-back triage is the smaller
  surface (read fold-back observations, cluster, propose plan updates),
  uses existing primitives, and surfaces operational signals that shape
  the bigger dream-cycle task. **Recommendation:** land `fold-back-triage`
  as the first `da service` scheduler task beyond R3's two v1 tasks; then
  build `scoped-kg-dream-cycle` once C.4 spec exists.

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

### ~~Where a future `da memory` would slot in~~ — NOT APPLICABLE

Removed 2026-05-28 (third fold-in, see §1.7 "Memory IS the KG"). There
is no separate memory store in dot-agents; the graph-backend adapter
already owns this surface. Consolidation/promotion verbs land under
`da kg` (`da kg consolidate`, `da kg promote`, `da kg dream-status`,
`da kg review-due`), not under a new `da memory` parent.

## Decision request

1. **Confirm** that `da service` (per r3-background-worker-service spec) is
   the canonical service command name — no rename to `da dream`, `da daemon`,
   `da auto-research`, etc.
2. **Confirm** that auto-dream-class workloads land as **scheduler tasks
   inside `da service`**, not as a peer top-level command.
3. **Resolve OQ-B** by unifying `workflow-orchestrator-daemon` references
   with `r3-background-worker-service`.
4. **OQ-D / OQ-E resolved** by §1.7 "Memory IS the KG" fold-in — no
   separate memory store, no `da memory` namespace; consolidation
   verbs land under `da kg`.
5. **Defer** OQ-C (scoped-kg-dream-cycle scheduler task + C.4 spec
   slot) until R3 v1 ships, then re-open as v1.x proposal alongside
   the C.4 spec under `.agents/workflow/specs/scoped-kg-dream-cycle/`.

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
- `.agents/workflow/specs/scoped-knowledge-graphs/design.md` (NoteType + 4 drivers + review-nudge)
- `.agents/workflow/specs/graph-backend-adapter-contract/design.md` (canonical persistence surface — KGNoteStore IS the memory layer; cited in §1.7)
- `.agents/workflow/specs/graph-bridge-contract/design.md` (workflow_memory intent referenced via context lane)
- `.agents/workflow/specs/kg-command-surface-readiness/design.md` (existing `da kg` verbs + breakdown)
- `.agents/lessons/agents-lack-autonomous-timers/LESSON.md` (timer trap)
- [AI Agent Memory in 2026: Auto Dream, Context Files, and What Actually Works (Quimby, dev.to)](https://dev.to/max_quimby/ai-agent-memory-in-2026-auto-dream-context-files-and-what-actually-works-39m8)
- [OpenClaw Dreaming Guide 2026: Background Memory Consolidation for AI Agents (czmilo, dev.to)](https://dev.to/czmilo/openclaw-dreaming-guide-2026-background-memory-consolidation-for-ai-agents-585e)
- [LLM Sleep Based Learning: REM-style Cycles and Synthetic Dreaming (gallahat, substack)](https://gallahat.substack.com/p/llm-sleep-based-learning-implementing)
- [Dreaming — Automatic Background Memory Consolidation (Hermes Agent issue #25309)](https://github.com/NousResearch/hermes-agent/issues/25309)
- [rem-sleep-skill (stewnight)](https://github.com/stewnight/rem-sleep-skill)
- [Evolving Memory (EvolvingAgentsLabs)](https://github.com/EvolvingAgentsLabs/evolving-memory)
