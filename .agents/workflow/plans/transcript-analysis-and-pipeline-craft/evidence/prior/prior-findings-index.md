# Prior-analysis findings index

Every finding/claim in the prior analyses, one line each, grouped by report and preserving
each report's own section order (which maps to the canonical spine
**sources → anchors → timeline → coverage/confidence → gaps**). Local IDs (`DA-*`, `EFF-*`,
`BEH-*`, `PLAN-*`) are stable handles for synthesis/falsification cross-reference; each line
carries a source anchor (`<token>#L<range>`). Digests (rubric R4) are recorded once per source
in the legend so any anchor is auditable.

## Source legend (anchors + R4 digests)

| token | source (ref:path) | sha256 (raw file) |
|---|---|---|
| `DA` | dot-agents @ `origin/analysis/top-level-basic-from-coordination-state` : `.agents/active/handoffs/payout-dot-agents-agent-behavior-evidence-2026-07-10.md` (ref `22189454…efc10`) | `9e553378c6942b3a91e8b9d1e4edd0e4ef89564435a7c753c60c29355b966fff` |
| `EFF` | adminapp-agc @ `feature/analysis-doc` : `planning/analysis/asdlc-a1/ai-assisted-delivery-efficiency-gains-report-2026-07-10.md` (ref `bce2a6e7…85a54`) | `e816322f88c670a5ecb736ab812e8690dc0fd6f78917d2467a90145e4de11e95` |
| `BEH` | adminapp-agc @ `feature/analysis-doc` : `planning/analysis/asdlc-a1/adminapp-orga-agent-behavior-evidence-2026-07-10.md` | `7b5851c5e4a7837d4d9153ec20408045216f257a2cd4b30b7d9b6be3d498aa19` |
| `PLAN` | adminapp-agc @ `feature/analysis-doc` : `planning/adminapp-analysis-tooling-and-config-promotion-2026-07.md` (companion note) | `e4321163d534957535db95c7b72302523f64697e79186ccdd5573cbfa20070e9` |

Sensitivity (R5): all four are already committed analysis prose (no raw transcript lines), redacted
by their authors to `Case`/session-id-prefix form; treated `internal`. No excerpt in this index
reproduces a credential, absolute home path, or non-public identifier beyond what the sources
already publish (Windows usernames appear in the source paths; not reproduced here).

---

## Report `DA` — payout-wrk + dot-agents agent-behavior evidence

### Sources / method
- **DA-S1** — Evidence sources: payout-wrk `.agents/workflow|history|lessons`, dot-agents `.agents/workflow/specs|plans` + `.agents/history`, plus recovered VS Code chat transcripts under `workspaceStorage/*/chatSessions/*.jsonl`. `DA#L10-19`
- **DA-S2** — Copilot session store returned **no** matching sessions for payout-wrk or dot-agents; hidden transcripts were later found in VS Code workspaceStorage (local dot-agents, devcontainer/workspace-file variants, WSL payout-wrk, local payout-wrk). `DA#L20-41`
- **DA-S3** — Memo is built from workflow/history artifacts + recovered JSONLs + embedded `created_at`/`updated_at` + git commit times + **file create/modify time as fallback**. `DA#L42-47`

### Anchors (time anchors)
- **DA-A1** — dot-agents planner evidence-backed write-scope: design last commit `2026-07-04T18:01:01-04:00`. `DA#L53`
- **DA-A2** — dot-agents workflow-parallel-orchestration `p3-eligible-cmd`: merge-back `created_at 2026-04-21T07:00:00Z`; archived commit `2026-05-19T16:02:04-04:00`. `DA#L54`
- **DA-A3** — dot-agents loop-discipline-stop-hooks: `created_at 2026-05-25T15:16:58Z`, `updated_at 2026-07-09T20:38:07Z`; closeout commit `2026-07-09T16:38:23-04:00`. `DA#L55`
- **DA-A4** — payout websocket-hub-test-suite: `created_at 2026-05-16T03:35:16Z` → `updated_at 2026-05-17T01:22:20Z`; archive commit `2026-05-16T21:25:09-04:00`. `DA#L56`
- **DA-A5** — payout po-core-api-se acceptance-coverage: `created_at 2026-05-16T03:35:16Z` → `updated_at 2026-05-16T15:23:40Z`; closeout commit `2026-05-16T11:29:25-04:00`. `DA#L57`
- **DA-A6** — payout loop-discipline-stop-hooks-backfill: `created_at 2026-07-07T15:01:24Z`, `updated_at 2026-07-09T20:17:06Z`; migration commit `2026-07-09T16:17:20-04:00`. `DA#L58`

### Timeline (recovered hidden-transcript signals)
- **DA-T1** — `2026-05-12` hidden session titled "Create payout plan for adminapp" → payout thinking already applied to AdminApp delivery planning. `DA#L64`
- **DA-T2** — `2026-05-19` transcript references to `agents-config/rules/payout/agents.md` → active rule-layer readback, not just static files. `DA#L66`
- **DA-T3** — `2026-05-29` references to shared agent config/runtime overlay for AdminApp + same split direction planned for payout (layered reusable config over repo-local duplication). `DA#L68-71`
- **DA-T4** — `2026-04-21` devcontainer-file transcripts corroborate devcontainer/WSL-context work preceding Windows-first flow. `DA#L72-73`
- **DA-T5** — Behavior signal: payout/dot-agents influenced AdminApp process design earlier/more directly than the artifact-only pass could prove; environment mode matters in cross-time comparison. `DA#L75-78`

### Coverage / findings — dot-agents workflow-model evolution
- **DA-C1** — Planning evolved from prose plans → execution contracts (task goal, locked decisions/invariants, required reads, verification target, explicit exclusions/stop conditions). `DA#L82-103`
- **DA-C2** — Workflow started auditing itself: completed-plan-audit introduces evidence precedence, risk-ordered audit queue, verdict classes (`verified-complete`, `completed-with-doc-drift`, `completed-with-evidence-gaps`, `reopen-recommended`). `DA#L105-119`
- **DA-C3** — Meta-loop became an explicit operating model: separate WORK vs REFINEMENT loop; orchestrator-managed cross-plan task state; lessons/proposals fed back as future work. `DA#L121-131`
- **DA-C4** — Companion task-contract files (`p0-sentinel-cli.contract.md`, `p3b-companion-discipline-skills.contract.md`, `p6-payout-backfill.contract.md`) pin interfaces/acceptance/exclusions/source-maps/sequencing when `TASKS.yaml` is too dense. `DA#L132-148`
- **DA-C5** — Merge-back is durable to late worker failure: `p3-eligible-cmd` parent wrote merge-back after confirming commit + verification when the worker env became inaccessible. `DA#L150-160`
- **DA-C6** — Loop/process reviews are first-class artifacts: `loop-improvements-review/impl-results.1.md` tunes prompt guidance, loop-state structure, evidence-budget rules (not product code). `DA#L162-177`

### Coverage / findings — payout-wrk downstream proof
- **DA-C7** — Delegated acceptance work can be tightly bounded + measurably verified: `stripe-webhook-acceptance-sync-row` recorded exact commands and threshold (`acceptance_coverage.sh 51.30% >= 51% threshold exit 0`). `DA#L181-193`
- **DA-C8** — Merge-back can preserve a pass while honestly documenting 3 deviations (pre-existing untouched-package baseline skew; SonarQube blocked by external service; `gofmt -l` sandbox-blocked, validated transitively via pre-commit) + noting an unrelated untracked file left untouched. `DA#L195-211`
- **DA-C9** — Dirty live branches handled via "prove on live state, then re-derive minimal delta in isolated worktree" (`loop-discipline-stop-hooks-backfill`: config delta, skill-resolution fixes, residual gaps, hook dry-runs, preservation checks). `DA#L213-231`
- **DA-C10** — Negative controls prove enforcement is real: `isp-p6negtest.json` with intentionally missing artifact returned `{"decision":"block"}` + exit `2`. `DA#L233-243`
- **DA-C11** — Lessons promoted into reusable guardrails: `commit-scope-hygiene/LESSON.md` (task commit swept unrelated edits → stage only task paths/hunks; separate quality fixes). `DA#L245-257`

### Gaps / cross-cutting lessons + guidance
- **DA-L1** — Cross-repo lesson: prose planning → execution contracts (queues alone insufficient). `DA#L263-266`
- **DA-L2** — Cross-repo lesson: "completed" → "auditable completed" (status insufficient; may need audit/reopen). `DA#L267-270`
- **DA-L3** — Cross-repo lesson: happy-path → adversarial verification (negative controls + honest deviation recording). `DA#L271-273`
- **DA-L4** — Cross-repo lesson: fragile worker sessions → durable parent reconciliation. `DA#L274-276`
- **DA-L5** — Cross-repo lesson: dirty-branch improvisation → isolated clean derivation (esp. migration/config/backfill). `DA#L277-279`
- **DA-L6** — Cross-repo lesson: repeated friction → promoted guidance (lessons/contracts/plan audits). `DA#L280-281`
- **DA-G1** — Guidance: prefer a task contract in addition to `TASKS.yaml` for high-risk tasks. `DA#L285`
- **DA-G2** — Guidance: require exact verification commands + thresholds in acceptance work. `DA#L286`
- **DA-G3** — Guidance: include ≥1 negative control when validating gates/hooks/enforcement. `DA#L287`
- **DA-G4** — Guidance: record deviations structurally to separate slice-local truth from tool/env issues. `DA#L288`
- **DA-G5** — Guidance: on dirty live branch, prove behavior then re-derive minimal delta in clean worktree. `DA#L289`
- **DA-G6** — Guidance: treat any repeated correction as a lesson/rule/planner-upgrade candidate. `DA#L290`

---

## Report `EFF` — AI-assisted delivery efficiency gains (synthesis)

### Sources / method
- **EFF-X0** — Executive assessment: real efficiency gain, clearest in **time-to-context, bounded execution, verification reuse**, not a single "% faster" headline; old work reconstructs story from scattered artifacts, workflow-era concentrates it into a small canonical surface (`PLAN.yaml`/`TASKS.yaml`/`{plan}.plan.md`/per-task merge-back/`verification/merge-back.result.yaml`); transcript checkpoints fill gaps. `EFF#L11-25`
- **EFF-S1** — Local `.copilot/session-store.db`: `41` sessions, `157` turns, `18` checkpoints; 3 useful sessions (`16512afa` region-override 38t/5c, `c5d33036` parity/perf 26t/4c, `2d63ab0a` perf 31t/2c). `EFF#L30-43`
- **EFF-S2** — Coverage limit: `.copilot` strongest for Jun-Jul 2026; **no direct hits** for `APPDEV-10376/9175/7817`; 10376 recoverable only from `.copilot/session-state` + Agents-Insiders metadata mirror. `EFF#L44-46`
- **EFF-S3** — Recovered VS Code workspaceStorage: `580` hidden `chatSessions/*.jsonl` (Insiders) vs `9` (Code) + `6` emptyWindow with no relevant matches; Insiders `session-store.db` had schema but no rows → JSONL workspace store authoritative; Agents-Insiders = metadata/log mirror only. `EFF#L48-56`
- **EFF-S4** — Recovered transcripts materially improved coverage for 10376/9175/7817/credentialing/payout/dot-agents. `EFF#L58-65`
- **EFF-S5** — Workflow-era artifact sources enumerated (5 plan/history paths). `EFF#L67-73`
- **EFF-S6** — Older/history-era sources enumerated (9175, 7817, credentialing). `EFF#L75-79`
- **EFF-S7** — Time-anchoring method: workflow-era→`PLAN.yaml` timestamps; transcript-backed→session-store + checkpoints; history-only→git commit else **fs create/modify fallback**; orga-agc→git commit. `EFF#L81-89`
- **EFF-A1..A8** — Applied time-anchor table: 8 cases (9175 git+mtime; 7817 ctime/mtime fallback; credentialing wf ts; 10376 CLI session-state+wf ts; 13778 region wf ts+session; 13778 payer-ideation wf ts; perf-parity wf ts+session; orga-agc git commit). `EFF#L92-101`

### Coverage — case-study comparison
- **EFF-CS** — 8-row case matrix (method era / time anchor / structured task graph / evidence shape / efficiency signal) for 9175, 7817, 10376, 13778 region, 13778 payer-ideation, credentialing, perf-parity, orga-agc. `EFF#L105-114`

### Lenses
- **EFF-E1** — Three delivery modes: raw prompting (9175), intermediary structure (7817), workflow-era (10376/13778/perf-parity/credentialing); workflow-era = lowest rediscovery cost + best delegation/verification reuse *when front-loaded content is strong*. `EFF#L116-124`
- **EFF-E2** — Credentialing is the edge case: workflow-era but degraded evidence path from early Windows `da` friction, not weaker intent. `EFF#L124`
- **EFF-E3** — Correctness/iteration lens: human corrections mostly supply ownership/milestone/proof/env/companion expectations; agent discoveries mostly implementation detail/query shape/lifecycle bugs/stale-branch/companion work; workflow-era reduced rediscovery but did not eliminate corrections when bundle/spec/plan failed to front-load. `EFF#L126-141`

### Timeline — what changed with the workflow model
- **EFF-W1** — Context reacquisition got much faster; **proxy: old cards need 15-31 mixed files, new re-entered from 3 canonical files + 1-2 task artifacts.** `EFF#L145-163`
- **EFF-W2** — Work decomposed early (13778 region: 9 tasks, 4 delegated archives) → less scope blending, safer delegation, preserved slice rationale, verification attached to the changed slice. `EFF#L165-186`
- **EFF-W3** — Model supports parallel ideation (13778 payer-ideation: 4 `app_type: ideation` tasks, 3 delegated evidence lines + synthesis). `EFF#L188-200`
- **EFF-W4** — Verification became reusable not narrative-only (`merge-back.result.yaml`, review decisions, scope sidecars, `verification_required: true`). `EFF#L202-220`
- **EFF-W5** — Cross-repo delivery is much more governable (10376: 9 tasks, one plan, explicit deps across 7 repos + local SQL validation). `EFF#L222-238`

### Timeline — what transcripts added beyond artifacts
- **EFF-TR1** — 13778 region: turns clarified M1 NPF-only boundary, requested early live smoke tuples (→`p1-live-smoke-tuples`), PR-ready declaration, recovered companion work → `p5/p6/p7`. `EFF#L244-257`
- **EFF-TR2** — Region parity/perf: preserve remeasure scripts, investigate 204 vs 1.0, collapse to one `regions-search.sql` template. `EFF#L258-264`
- **EFF-TR3** — Perf parity: redirect to fresh live explain plans in `CLAIMPRD_TEST`; second-pass `SearchProvider` completed in `da` flow. `EFF#L266-271`
- **EFF-TR4** — 10376 pre-plan context recovered from hidden stores: sessions `10ee1b05…` ("Create SpecialProgram HTTP Endpoints"), `a0f54a43…` ("Analyze And Migrate Request Model"), backend validation, NumberFormatException, three-version split, `enrollmentAttributes`, UI composition. `EFF#L272-298`
- **EFF-TR5** — 9175/7817 older cases were also chat-iteration-heavy (referenced decisions/acceptance/verification/aggregation/impl-results notes + reuse of focused-review patterns). `EFF#L300-318`
- **EFF-TR6** — Credentialing + Windows tooling friction corroborated by transcripts (Playwright CRUD timeouts; `da config explain --all --json` / `da workflow` mutators failing on Windows). `EFF#L320-331`

### Coverage — delegation-bundle quality
- **EFF-B1** — Key finding: highest remaining gap is **bundle-content quality** — strong plan/spec prose but comparatively thin archived `delegation.yaml` worker contract; several later corrections = signals of incomplete front-loading; loop count tracks front-loaded-constraint quality more than presence of workflow files. `EFF#L333-345`
- **EFF-B2** — Bundle model expects `scope.write_scope`/`prompt`/`context.required_files`/`verification`/`closeout.worker_must`, but archived 13778 contracts preserved mostly `write_scope`/`success_criteria`/owner-status → context stays in plan/spec prose, not the payload. `EFF#L347-363`
- **EFF-B3** — Positive front-loading example: 10376 plan (contract redesign, 4 data-shape profiles, repo-by-repo map, local DB validation, env override, phased verification, shared query guidance, acceptance matrix). `EFF#L365-386`
- **EFF-B4** — Mixed example: 13778 good decomposition but milestone/ownership, live-smoke fixture, companion completeness not front-loaded soon enough (folded back as p5/p6/p7). `EFF#L387-409`
- **EFF-B5** — Mixed example: perf-parity weak proof front-loading ("query-plan evidence where available" too weak; proof method must be in the initial contract). `EFF#L411-424`

### Gaps — correction taxonomy + drafting guidance
- **EFF-CT1** — Correction category: ownership/scope (belongs in spec decisions). `EFF#L430-439`
- **EFF-CT2** — Correction category: verification/proof (belongs in plan verification strategy + bundle). `EFF#L441-451`
- **EFF-CT3** — Correction category: companion-work completeness (needs a standard companion-surface checklist). `EFF#L453-463`
- **EFF-CT4** — Correction category: validation-data (live tuples/parameter buckets/remeasure scripts as explicit task inputs). `EFF#L465-475`
- **EFF-CT5** — Correction category: environment/tooling (belongs partly in shared config, partly repo-family overlays). `EFF#L477-487`
- **EFF-DG1** — Spec-quality rules: lock ownership split, milestone boundary, source-of-truth contract, proof method, fixture/data strategy. `EFF#L491-515`
- **EFF-DG2** — Plan/task-quality rules: companion-surface scan, environment matrix, decision locks, required reads, app_type-specific proof. `EFF#L517-539`
- **EFF-DG3** — Delegation-bundle rules: bundle must carry required_reads, decision_locks, env/connection guidance, verification commands/expectations, exclusions/deferred, companion-surface status, expected reusable artifacts. `EFF#L541-559`
- **EFF-PD1..PD7** — 7 additional transferable lessons from payout/dot-agents (companion contracts; negative controls; structural deviation recording; isolated clean derivation; auditable-completed; promote friction fast; exact commands/thresholds). `EFF#L561-591`
- **EFF-WH1** — Where guidance lives: `adminapp-agents-config` = AdminApp domain front-loading (companion checklist, validation/evidence patterns, repo-family overlays). `EFF#L592-616`
- **EFF-WH2** — Where guidance lives: `Platform/orga-agc` = generic OrgA scaffolding (org→team→repo layering live; `layers/org/base.json`; `recipes/README.md`); platform-vs-domain split stated. `EFF#L617-644`

### Coverage / confidence + caveats
- **EFF-MG1** — High-confidence gains: context rehydration efficiency, verification reuse, delegation safety, cross-repo coordination. `EFF#L647-662`
- **EFF-MG2** — Medium-confidence gains: raw implementation throughput (no paired timing), reduced rework (mostly qualitative). `EFF#L663-671`
- **EFF-LC1** — Caveat: no defensible single percentage (no consistent timings/retention/verification timestamps). `EFF#L674-676`
- **EFF-LC2** — Caveat: transcript retention incomplete (10376/9175/7817 absent from shared DB; 10376 required deep mining). `EFF#L677-680`
- **EFF-LC3** — Caveat: transition friction (credentialing Windows `da` write issues → direct file population). `EFF#L681-683`
- **EFF-LC4** — Caveat: not every plan fully closed (perf-parity still active = process-maturity evidence, not completed-cycle efficiency). `EFF#L684-686`
- **EFF-BL** — Bottom line: delivery became materially more efficient once work moved into the inner-loop/meta-loop model, esp. medium-complex cross-repo cards; safest numeric headline is context-rehydration (15-31 artifacts → 3 files + task records), not raw coding speed. `EFF#L687-706`
- **EFF-RI1..RI4** — Recommended instrumentation: auto-record per-plan lead + per-task elapsed time; keep transcript retention longer; archive every completed plan together; keep `app_type` for later segmentation. `EFF#L708-714`

---

## Report `BEH` — AdminApp + OrgA agent-behavior evidence (case-study record)

### Sources / method
- **BEH-S1** — Companion source-style record to `EFF`; sources = `.copilot/session-store.db`, recovered `workspaceStorage/*/chatSessions/*.jsonl`, Agents-Insiders metadata/log mirrors, workspace-session mapping JSONs, AdminApp workflow/history artifacts, `Platform/orga-agc` git. `BEH#L1-24`
- **BEH-S2** — Coverage was broader than the first pass once beyond `.copilot/session-store.db`: session-backed evidence for 13778 region-override, parity/query-shape, perf-parity, 10376, 9175, 7817, credentialing. `BEH#L26-39`
- **BEH-S3** — Inventory findings: hidden pool in Insiders `workspaceStorage/chatSessions`; Agents-Insiders lacked equivalent pool but preserved `copilotcli.session.metadata.json` + chat log; emptyWindow 6 / Code 9 JSONLs not useful; Insiders `session-store.db` had schema but no rows. `BEH#L40-48`
- **BEH-S4** — Anchor precedence: workflow ts → `.copilot` transcript windows → recovered JSONLs → git commit when stronger → **fs create/modify fallback**. `BEH#L50-56`
- **BEH-A1..A8** — Representative time-anchor table: 8 cases (9175, 7817, 10376, 13778 region, 13778 payer-ideation, credentialing, perf-parity, orga-agc). `BEH#L60-69`
- **BEH-S5** — Recovered inventory: `580` Insiders `chatSessions` JSONLs (hidden pool); 6 emptyWindow / 9 Code not material; `.copilot/session-state/*/events.jsonl` useful for CLI-era but not the only durable store — explains why devcontainer/worktree-era cases looked transcript-poor. `BEH#L71-80`

### Timeline / case set
- **BEH-C1** — `APPDEV-9175` (raw/history-style UI): 31 files (4 md, 21 screenshots, 4 JS probes, 2 JSON); rich evidence, weak canonicality, expensive rehydration; hidden transcripts (Apr-May 2026) show it was iteration-heavy, not just artifact-rich. `BEH#L84-112`
- **BEH-C2** — `APPDEV-7817` (intermediary): 15 files (9 md); later canonical `.plan.md` + `session-plan-aggregation.1.md` = reconciliation artifact; transcripts show repeated reconciliation against prior notes/validation/focused-review patterns. `BEH#L114-139`
- **BEH-C3** — `APPDEV-10376` (strongest front-loaded cross-repo plan): 4 special-program profiles, local DB validation, env override, phased verification, repo map, shared query guidance, acceptance matrix; recovered CLI sessions + 8-step feature arc (endpoint scaffolding → migration → backend hardening → NumberFormatException → versioning → API integration → UI composition → PL/SQL fix); session IDs traced to titles. `BEH#L141-227`
- **BEH-C4** — `APPDEV-13778 region override` (strongest transcript-backed workflow case): user correction made M1 NPF-only (non-NPF → integration-lib/P10, ingress separate); planner responded structurally (`p1-live-smoke-tuples`, later `p5/p6/p7`); delivery persisted corrections across sessions; gap = some corrections should have been front-loaded sooner. `BEH#L229-258`
- **BEH-C5** — `APPDEV-13778 payer-branch ideation` (ideation as first-class): 4 completed `app_type: ideation` tasks, 3 delegated evidence lines + synthesis; contract/routing/service-decision preserved as reviewable artifacts. `BEH#L260-274`
- **BEH-C6** — `adminapp-credentialing-ui-hardening` (workflow-era with evidence fallback): PLAN/TASKS + `app_type` + reviewer-lens + local CRUD evidence exist, but delegated archive shape not produced (Windows `da` write friction); transcripts confirm Playwright timeouts + `da` command failures. `BEH#L276-304`
- **BEH-C7** — `perf-preprd-uat-parity-2026-07` (active workflow, proof-method correction): good task structure but under-specified proof method; user redirected to fresh live explain plans / `CLAIMPRD_TEST` / parameter-bucket / original-vs-new comparison; workflow absorbs corrections better than it prevents them. `BEH#L306-323`
- **BEH-C8** — `Platform/orga-agc` (governance evidence): live repo exists; README org→team→repo layering; `layers/org/base.json`; `recipes/README.md` reserves platform `.da` recipes → platform split crossed from planning-only into implementation (scaffold stage). `BEH#L325-339`

### Coverage — workflow maturity signals
- **BEH-M1** — Context re-entry improved sharply (old = many mixed artifacts; new restarts from PLAN/TASKS/plan.md + selected merge-back) → time-to-context is a clear gain. `BEH#L343-356`
- **BEH-M2** — Verification became a reusable surface (`verification_required: true`, merge-back results, reviewer notes, scope sidecars, env/fixture-tied strategies). `BEH#L358-370`
- **BEH-M3** — Workflow still depends on front-loaded content quality (10376 strong; 13778/perf-parity needed later injection) → good at keeping corrections, still needs stronger defaults. `BEH#L372-389`
- **BEH-M4** — Companion surfaces are a recurrent planning blind spot (auth registration, ingress/routing, automation/bootstrap, integration-lib/gateway) → needs reusable companion-surface checklist. `BEH#L391-402`
- **BEH-M5** — Environment selection is part of the contract, not an implementation detail (Oracle connection, local vs live oracle, blocked graph/scope derivation). `BEH#L404-415`

### Gaps — lessons + lenses
- **BEH-TL1..TL7** — 7 transferable lessons (lock ownership/milestone earlier; front-load fixture/data strategy; proof method is contract; ideation as tracked app_type; companion-surface checks; preserve reviewer/verification structurally; split platform-generic → `Platform/orga-agc`, keep domain overlay in `adminapp-agents-config`). `BEH#L417-425`
- **BEH-E1** — Era lens 1 — raw prompting/no specific way (9175): flexible, high rediscovery cost, runtime/consumer issues become organizing structure. `BEH#L431-448`
- **BEH-E2** — Era lens 2 — intermediary (7817): better continuity, still pays narrative-reconciliation tax; "plan + several histories" not one spine. `BEH#L450-467`
- **BEH-E3** — Era lens 3 — workflow-era with tooling/env fallback (credentialing): workflow intent but `impl-results` retroactive repair; shows need for a sanctioned "degraded but canonical" evidence path. `BEH#L469-486`
- **BEH-E4** — Era lens 4 — workflow-era with strong canonical structure (10376/13778/perf-parity): lowest rediscovery cost, best cross-repo + auditable delegation, still sensitive to missing front-loaded ownership/proof. `BEH#L488-508`
- **BEH-CI** — Correctness/iteration/who-found-issues table (6 cases) with per-case iteration pressure, human vs agent found issues, needed clarifications, and direction-changing unknown-unknowns. `BEH#L510-521`
- **BEH-CI-I1** — Interpretation: raw/intermediary discover correctness late through runtime/consumer validation. `BEH#L525`
- **BEH-CI-I2** — Interpretation: workflow-era reduced rediscovery but correctness still depended on front-loaded ownership/proof/fixture. `BEH#L526`
- **BEH-CI-I3** — Interpretation: human-found issues mostly product/ownership/proof expectations. `BEH#L527`
- **BEH-CI-I4** — Interpretation: agent-found issues mostly implementation detail/query shape/lifecycle/stale-state/companion surfaces. `BEH#L528`
- **BEH-CI-I5** — Interpretation: worst direction changes came from unknown-unknowns (missing 2.0 parity objects/hidden milestone boundaries; consumer-host/runtime blockers; env/tooling friction). `BEH#L529-532`
- **BEH-EC** — Credentialing is a special edge case: not older/intermediary but workflow-era intent/structure/lenses with fallback mechanics → needs sanctioned degraded evidence mode + Windows-first `impl-results` rule; don't infer maturity from artifact names. `BEH#L534-549`
- **BEH-BL** — Bottom line: AdminApp/OrgA evidence supports the same distillation style; main difference is AdminApp has better local transcript evidence for a few cases while payout/dot-agents leaned on fs artifacts + git history. `BEH#L551-561`

---

## Report `PLAN` — AdminApp analysis-tooling + config-promotion companion note

(Companion narrative note; the authoritative surface is the adminapp workflow plan/spec. Digested
because it converts the two analysis reports' findings into a forward work plan — the direct
antecedent of *our* plan's tooling/rubric decisions.)

- **PLAN-R1** — Reconciliation: `orga-org-source-buildout` owns OrgA-platform reusable content; `adminapp-config-v2-revamp` owns AdminApp team-layer migration; `adminapp-analysis-tooling-and-config-promotion` owns the July follow-on (tooling, skills, automation promotion, bundle hardening). `PLAN#L19-30`
- **PLAN-G1** — Gap: evidence mining is too manual (findings came from stitching `.copilot/session-state` + Insiders `chatSessions` + `transcripts` + dot-agents artifacts + mtimes + git times); reusable only as analyst behavior, not tooling. `PLAN#L36-43`
- **PLAN-G2** — Gap: bundle/front-loading quality varies too much (largest correction loops from missing ownership, companion surfaces, verification targets, source-of-truth links, explicit unknowns). `PLAN#L45-52`
- **PLAN-G3** — Gap: `adminapp-automation` under-represented in shared config (real planner/generator/healer/QMetry workflow still repo-local). `PLAN#L54-56`
- **PLAN-G4** — Gap clarified: OrgA-platform vs AdminApp boundary clearer now (generic replay/starter/Playwright → orga-agc; domain overlay stays AdminApp). `PLAN#L58-60`
- **PLAN-G5** — Gap: the July analysis should become a repeatable capability (report refresh, evidence audit, era comparison, cross-workspace mining). `PLAN#L62-67`
- **PLAN-WA** — Workstream A: reusable workflow-evidence toolkit first — inventory, session normalization (one schema, tolerate partial sources), case-study correlation (anchors: story/repo/session/plan/keyword → ordered timeline), coarse evidence-quality scoring (5 dims), report rendering; design choices: configurable path roots, time-anchor confidence first-class, separate raw extraction from interpretive scoring, Windows-friendly-not-only. `PLAN#L94-161`
- **PLAN-WB** — Workstream B: shared skills over the toolkit (evidence-refresh, delivery-case-study, bundle-quality-audit, transcript-source-inventory) using split-file skill structure. `PLAN#L163-207`
- **PLAN-WC** — Workstream C: promote `adminapp-automation` (planner/generator/healer/QMetry agents + skills + rule/profile updates) into shared config. `PLAN#L209-285`
- **PLAN-WD** — Workstream D: planning/bundle hardening — bundle richness rubric (source-of-truth links, repo/consumer/companion surfaces, validation target + evidence path, env assumption, known unknowns, likely correction hotspots), drafting checklists, lessons promotion, evidence-repair guidance. `PLAN#L287-331`
- **PLAN-WE** — Workstream E: sharpen the `orga-agc` split (keep AdminApp domain map/operator workflows/business seams; move generic Playwright/test-gen/healer/starter/replay/scaffolding); rule of thumb = still useful after swapping repo names/seams → belongs in orga-agc. `PLAN#L333-353`
- **PLAN-EX** — Execution order + first slice: inventory → resolve session/story anchor → emit one markdown timeline → coarse score, using already-mined `APPDEV-10376` as the end-to-end proving case. `PLAN#L355-385`
- **PLAN-AC** — Wave acceptance: analyst can reproduce a feature timeline without manual rediscovery; automation shared behavior less repo-local; new planning/delegation artifacts encode front-loading lessons; platform/domain split clearer. `PLAN#L386-402`

---

## Skipped sources (with reason)

- **orga-agc @ `develop`** (`~/proj-docs/config-workspace/orga-agc`) — **no prior analysis/behavior report exists.** Tracked content is only the scaffold config repo (`README.md`, `layers/`, `recipes/`) plus one buildout plan (`.agents/workflow/plans/orga-org-source-buildout/`). orga-agc is a *subject* of `EFF`/`BEH` (the `Platform/orga-agc` case, EFF-WH2 / BEH-C8), not a source of prior findings. Nothing to digest; no findings extracted. (git log confirms scaffold + config-setup-plan commits only; branch `develop`, HEAD `b8305d4`.)
- **dot-agents branch non-report files** — the `analysis/top-level-basic-from-coordination-state` ref differs from its merge-base (`917908d`) by exactly one added file, the `DA` report above (`git diff --stat` = 1 file, +290). All other tree paths on that ref are unchanged working-repo state, not analysis output → out of scope, not digested.
