---
spec: loop-agent-pipeline
iter: 1
purpose: Track decisions made (and residual sub-questions) across plan-iter.1.md, review.1.agent-thoughts.md, and review.1.human-thoughts.md — so plan-iter.2 + TASKS.yaml can be authored against a stable direction.
status: all main decisions locked (incl. D1 merge CLI shape, D6 direction + TOC, and D7 iter-log shape); D2.a and D3.a resolved inline during plan-iter.2 authorship; two fork-scoped open items (Q1, Q2) remain in the external-sources design fork
---

# Decisions — loop-agent-pipeline (iter 1)

Legend: **Resolution** = locked direction. **Follow-up** = sub-question that emerged from the resolution and still needs answering before the plan can be written cleanly.

---

## D1 — `workflow verify record` vs `review-decision.yaml`: MERGE

**Resolution**: merge. Reviewer calls `workflow verify record` at end of review. The CLI writes two artifacts from one call:
1. Appends a row to the project's global `verification-log.jsonl` (existing behavior — the global project trace).
2. Creates / updates `.agents/active/verification/<task_id>/review-decision.yaml` (the per-task pipeline artifact consumed by orchestrator closeout).

**Source** (human-thoughts): *"merge them, verify record currently writes out the reviewers notes to the project's `verification-log.jsonl`, it could add there as a form of global project trace, and create the review-decision.yaml thats in repo for the pipeline."*

**Resolution (D1.a)**: make `workflow verify record` a **structured flag-first writer**. The review-agent should call one CLI command with explicit flags; the CLI then writes both the global `verification-log.jsonl` row and the per-task `review-decision.yaml`.

Why this is the better fit:
- Same reason as D7: reliability lives in the CLI contract, not in asking a weaker model to author an intermediate YAML file and then ingest it.
- Review decisions have real semantics (`accept|reject|escalate`, failed gates, notes, escalation reason). Those should be validated as enums / structured lists at the command boundary.
- One canonical writer avoids drift between a hand-authored `review-decision.yaml` and the jsonl audit row.

Recommended CLI shape:

```text
workflow verify record \
  --task-id <task> \
  --phase-1-decision accept|reject|escalate \
  --phase-2-decision accept|reject|escalate \
  --failed-gate <gate-id> \
  --reviewer-notes "..." \
  --escalation-reason "..."
```

Recommended rules:
- `--phase-1-decision` and `--phase-2-decision` are required enums.
- `overall_decision` is CLI-derived from the two phase decisions.
- `--failed-gate` is repeatable rather than comma-packed free text.
- `--escalation-reason` is required when either phase is `escalate`; otherwise omitted.
- `review-decision.yaml` is CLI-owned output, not agent-authored input.
- `--from-decision <path>` can exist only as a compatibility/import path for humans or migration tooling, but it should **not** be the default agent path.

**Field diff confirmed**: keep `verification-log.jsonl` lean. It does not need to mirror the full YAML surface; retain the existing audit-trace summary shape and add only the minimum structured fields needed for queryability if current rows lack them. The rich decision payload lives in `review-decision.yaml`.

---

## D2 — `workflow fold-back`: INCLUDE `update`

**Resolution**: post-closeout orchestrator can both `create` new observations and `update` existing ones (refine as later slices land).

**Source** (human-thoughts): *"include update"*.

**Follow-up (D2.a — confirm before plan)**: identity scheme for observations. Without a stable id, `update` has nothing to target. Two viable options:
- (a) **Stable slug**: orchestrator passes `--id <slug>` on create; `update --id <slug>` merges. Slugs are human-authored per reasoning path (e.g. `coverage-regression-phase-3b`).
- (b) **Auto id**: create returns an id (short hash); orchestrator stores it in the reasoning path's state and passes it on update. Machine-friendly; requires state tracking.

**Recommendation**: start with (a) — slugs map 1:1 to reasoning paths (coverage-regression, schema-drift, cross-task-conflict, budget-escalation, fold-back-triage) and are already enumerable. (b) can layer on later.

---

## D3 — Verifier-deny send-back: reviewer-first, per-verifier policy, TDD-fresh enforced by environment

**Resolution** (option c with default path specified):
1. Verifier fails → reviewer reads `<type>.result.yaml` first (NOT immediate impl-agent loopback by default).
2. Reviewer decides:
   - **Send back to impl-agent** if the failure is diagnosable as an implementation gap.
   - Otherwise **diagnose the verification itself** (did the verifier run correctly, was the environment healthy).
3. Verification diagnosis branches:
   - **Transient** (environmental, flake, timeout) → retry the verifier.
   - **Genuine** → delegate the fix to the proper owner: verifier-agent (bad test / wrong assertion / outdated scenario) or impl-agent (actual code defect).
4. After fix, verifier(s) rerun → reviewer re-evaluates.
5. Per-verifier policy override stays available via `verifier_profiles.<type>.deny_policy: immediate-sendback | reviewer-gated` for cases where reviewer-gating is overkill (e.g. compile-fail from unit verifier could shortcut).

**NEW assumption enforced BY ENVIRONMENT**: *by the time verifier-agents run, the test suite must be **fresh from the TDD refresh at task (or slice) start.*** Old / untouched tests indicate the TDD step was skipped; the pipeline treats that as a plan-level defect, not a code-level failure. Enforcement lives in the environment (pre-verifier gate), not in agent self-reporting alone.

**Source** (human-thoughts §3): *"c. But I'd want the default to be review artifacts then decide if can send immediatly back to impl-agent, if not then diagnose the verification and see what if anything went wrong. If a transient error retry, if it's a genuine error delegate the fix to the proper owner (verifier-agent or impl-agent), then have the verifier(s) rerun it's tasks, then back to reviewer. One assumption I want to enforce by environment is by the point the verifier agent(s) are running the tests, the tests must all be fresh from the TDD refresh at task (or slice) start."*

**Follow-up (D3.a — enforcement mechanism)**: exact pre-verifier gate design. Sketch:
- **Gate step** runs before any verifier: scan the commit range from task start to now; require at least one `*_test.go` (or language-equivalent) modification inside `write_scope_touched`, OR an explicit `tests_unchanged_justified: true` field on `impl-handoff.yaml` with reviewer sign-off.
- **Retry cap**: default 2 retries per verifier per task before reviewer escalation. Configurable via `verifier_profiles.<type>.max_retries`.
- **Escalation signal**: reviewer writes `decision: escalate` + `escalation_reason` to `review-decision.yaml`; post-closeout orchestrator opens a fold-back observation.
- **iter-log hook**: `impl.self_assessment.tdd_refresh_performed: bool` (auto-populated from gate result, not trusted from agent).

Resolvable inline during plan-iter.2 authorship.

---

## D4 — ralph-pipeline plan-completion break check: `workflow next --json` (da-native)

**Resolution**: use `da --json workflow next --plan <id>` as the loop-break signal. Empty / no-result → break. If `workflow next` does not accept a plan-scope filter today, the fallback inside the same task is `da --json workflow tasks <plan>` checking for any `status == pending`. **No python3, no narrative parsing of `workflow orient` text.**

**Evidence**: `docs/generated/GLOBAL_FLAG_COVERAGE.md` confirms `--json` on `workflow next`, `workflow tasks`, `workflow orient`. `workflow next` semantically matches "is there still unblocked work".

**Plan impact (p1-pipeline-loop)**: if `workflow next` lacks `--plan` / plan-scope today, adding that filter is part of p1's `write_scope` (`commands/workflow.go` + matching tests).

---

## D5 — `--project-overlay` vs `--prompt-file`: KEEP BOTH (different bundle fields, different roles)

**Resolution**: the two flags are **not** duplicates — they map to different fields in the delegation bundle:

| Flag | Bundle path | Semantic role |
|---|---|---|
| `--project-overlay` | `worker.project_overlay_files` | *How this role operates* — persistent role/project guidance (AGENT.md-like) |
| `--prompt-file` | `prompt.prompt_files` | *What to do now* — task-specific runtime prompt |

`bin/tests/ralph-orchestrate` passes the same `.agents/active/active.loop.md` to both only because single-worker design conflated role-guidance with per-task prompt. Under role-pure pipeline they diverge:
- `--project-overlay` → per-role agent profile (impl-agent / verifier-`<type>` / review-agent AGENT.md)
- `--prompt-file` → per-task prompt file generated from the slice content (NOT a shared `active.loop.md`)

**Plan impact**: fold into the orchestrator-awareness task (see D8). No schema change.

---

## D6 — External-sources (Phase 7): FORK — design doc first, integrate later

**Resolution**: option (b) — split Phase 7 into two passes:
1. **Design fork** (separate from this plan's main body): audit current source consumption, enumerate needs, propose schema + implementation for future evolution.
2. **Integrate**: once the design doc lands, come back and integrate its outputs into this plan (or a follow-on plan) and land the schema + CLI changes.

**Source** (human-thoughts): *"b, want to fork to do that come back and we integrate the results"*.

**Plan impact**:
- `p7-external-sources` as originally scoped is **removed from this plan's main body**.
- Replace with a lightweight placeholder task `p7-sources-design-fork` whose `write_scope` is a design doc (`.agents/workflow/specs/external-agent-sources/design.md`) — no code changes in this plan.
- Actual schema/CLI work lands later, informed by the design doc.

### Direction locked for the fork (outcomes of mid-discussion)

These are locked decisions to carry into the design-doc fork — NOT re-open during fork authorship unless new evidence contradicts them.

**Architecture & roadmap**:
- **Option B in v1.5 → Option C in v2**. v1.5 restructures `source` into `{ transport, auth, content }` so auth is shared across transports (git, http, local); v2 elevates registry to first-class citizen with signing + rich discovery.
- Two concrete consumer personas ground the design:
  - **Insurance co** (gov contracts, CMMC L2 + FIPS overhaul, Okta IdP, self-hosted registry on private network, VPN/onsite users).
  - **General public** (both publish and consume).

**Registry wire protocol**: **OCI Distribution spec** (Docker Registry v2 semantics).
- Enterprise pre-approval (customer Harbor / Artifactory / ECR / GHCR installations already on compliance allowlists).
- Mature auth protocol (Docker Registry v2 token auth over OAuth2).
- Sigstore / cosign ecosystem sits on top natively (for v2 attestation).

**Reference registry server**: **BYO for v1.5** — customer points dot-agents at any OCI-compatible registry they already run. Thin-wrapper `dot-agents serve-registry` (embedding `distribution/distribution`) stays on **v2 roadmap** — ship only if evidence shows small-shop demand without existing OCI infra.

**Registry content model — separate OCI artifact types, not bundled**:

| Content | OCI media type | Example name |
|---|---|---|
| Agent definition | `application/vnd.dotagents.agent.v1+tar` | `agent/impl-agent@1.4` |
| Skill | `application/vnd.dotagents.skill.v1+tar` | `skill/review-pr@2.0` |
| Verifier profile | `application/vnd.dotagents.verifier.v1+tar` | `verifier/playwright-api@1.2` |
| Bundle manifest (pointer doc) | `application/vnd.dotagents.bundle.v1+json` | `bundle/sre-stack@1.0` |

Rationale (summarized): lifecycle independence, asymmetric consumption (consumers pull one thing by name), cleaner per-artifact trust boundaries, native OCI discovery by media type, partial cache invalidation on refresh. Bundles are **pointer documents** that list member refs by tag or digest — not inlined content. Pulling a bundle resolves into N independent artifact pulls sharing the same cache semantics.

**Sub-decisions locked**:

| Sub-decision | Resolution |
|---|---|
| Namespace on the wire | OCI repo-path form: `<org>/verifier/<name>`, tag `1.2` → `GET /v2/<org>/verifier/<name>/manifests/1.2`. Type prefix becomes part of the repo path; predictable across any OCI registry. Moving an artifact between types is rare; rename-when-it-happens is fine. |
| Bundle manifest shape | Custom `application/vnd.dotagents.bundle.v1+json` pointer document listing member refs. NOT OCI Image Index (image-index is a multi-arch discriminator — wrong semantic fit). |
| Version semantics | Tags on the wire (OCI native). Client-side resolver does SemVer matching (`^1.4` → highest matching tag). Digest pinning supported as `pinned:sha256:...` form for reproducibility. |

**Knock-on schema locked**:
- `.agentsrc.json` `packages` entries use `<type-prefix>/<name>@<version-form>`:
  ```
  "packages": [
    "verifier/playwright-api@^1.2",
    "agent/impl-agent@1.4",
    "skill/review-pr@pinned:sha256:..."
  ]
  ```
  Prefix is mandatory; `@` is the version separator; `^N`, exact tag, or `pinned:sha256:...` are the three version forms.
- `verifier_profiles` map (from D10) points at refs when sourced from a registry:
  ```
  "verifier_profiles": {
    "unit": "verifier/unit@^1.0",           # registry-sourced
    "api":  "agents/verifiers/api-sre"      # local-sourced (existing form retained)
  }
  ```
- Publish CLI is per-artifact: `dot-agents publish agent ./path`, `dot-agents publish verifier ./path`, `dot-agents publish skill ./path`, `dot-agents publish bundle ./manifest.yaml`.
- Media type acts as a client-side schema gate — pulling an agent artifact into a `verifier_profiles` slot is caught before execution.

**FIPS posture for v1.5** (web-verified as of April 2026):
- **Single binary, no build variant**. Build with Go 1.24.3+ using `GOFIPS140=inprocess` (tracks latest MIP-state module automatically — currently v1.0.0, CAVP cert A6650, reached CMVP Modules In Process list May 2025, still In Review with no fully validated certificate yet). Runtime opt-in via `GODEBUG=fips140=on`.
- **Rationale**: MIP-state modules are acceptable for deployment in regulated environments per NIST IG D.G with documented risk acknowledgment. CMMC L2 assessor practice generally accepts MIP-state. Non-FIPS users are unaffected by the build flag.
- **BoringCrypto variant (Linux amd64/arm64, CGO)** stays on **persona-gated roadmap**, shipped only if a compliance team requires strict-validated (not MIP). Do not pre-build.
- **Dependency**: Open question Q1 below (persona's compliance team ruling on MIP vs strict-validated) gates whether we ever ship the BoringCrypto variant.

**Auth provider model (v1.5)**:
- `oauth2-auth-code-pkce` — browser callback flow (primary for Okta IdP persona). Token store: `keychain` (default) | `file` | `env`. Refresh tokens handled via OIDC discovery.
- `mtls` — for PKI-first orgs (client cert + key + CA).
- `bearer` — static token from env / file (CI and simple cases).
- `credential-helper` — external binary returning creds on stdout (git-credential-helper pattern); catches bespoke enterprise token issuance without us hardcoding every scheme.
- **Device code flow** — fallback OAuth2 form for headless/CI environments where browser callback isn't viable.

**Audit logging for CMMC AU-2 / AU-3** — **in scope for v1.5** (low cost while CMMC-shaping):
- Event taxonomy: `auth.login`, `auth.token_refresh`, `auth.logout`, `registry.fetch_manifest`, `registry.fetch_blob`, `registry.publish`, `signature.verify`, `cache.hit`, `cache.miss`.
- Event schema: `{timestamp, actor, principal, action, target, outcome, trace_id}` (structured JSON).
- Default sink: stderr. Configurable override: file, syslog, JSONL, HTTP endpoint.
- Retention: customer-managed; dot-agents does not store.

### D6.a — Design-doc TOC (locked)

Target path: `.agents/workflow/specs/external-agent-sources/design.md`

```
1. Current state audit
   - Existing sources (local, git) — fields, auth story, gaps
   - How consumers select from a source today vs. what enterprise customers need

2. Consumer personas & regulatory constraints
   - Insurance co: CMMC L2, FIPS, Okta IdP, self-hosted registry, VPN network model
   - General public: publish + consume, zero-infra consumption
   - Constraint → design-requirement mapping

3. Transport + content + auth architecture (Option B, v1.5)
   - Transport types: http, git, local
   - Shared auth block (applies across transports)
   - Content layout: tree | tarball | registry

4. Auth provider model
   - oauth2-auth-code-pkce (Okta / other OIDC IdPs) — callback flow, token store, refresh
   - mtls
   - bearer (static, env / file / vault)
   - credential-helper (external binary escape hatch)
   - device-code (headless / CI fallback)
   - Cross-cutting: token storage (keychain | file | env), rotation, revocation

5. Registry content model
   - Separate OCI artifact types (agent / skill / verifier / bundle)
   - Media types, repo-path namespacing (`<org>/verifier/<name>`)
   - Bundle manifest shape (custom pointer doc — NOT OCI image index)
   - Version semantics (tags on wire, SemVer + digest-pin client-side)

6. Registry wire protocol (OCI Distribution)
   - Why OCI (enterprise pre-approval, mature auth, sigstore ecosystem)
   - Reference server: BYO for v1.5; thin-wrapper on v2 roadmap
   - Interaction with public registries (GHCR default? — Q2 below)

7. FIPS posture
   - Go 1.24.3+ with `GOFIPS140=inprocess`; runtime `GODEBUG=fips140=on` opt-in
   - MIP-state acceptance per NIST IG D.G; documented risk-acknowledgment pattern
   - Strict-validated escape hatch (BoringCrypto variant) gated on persona feedback (Q1)
   - Single binary in v1.5; no build variant unless persona requires strict-validated

8. Audit logging (CMMC AU-2 / AU-3)
   - Event taxonomy (auth.*, registry.*, signature.*, cache.*)
   - Event schema (timestamp, actor, principal, action, target, outcome, trace_id)
   - Destinations: stderr (default) + configurable sink (file, syslog, JSONL, HTTP endpoint)
   - Retention (customer-managed; dot-agents does not store)
   - Alignment with existing dot-agents structured-logging patterns

9. Trust & attestation (v2 material, flagged in v1.5)
   - HTTPS + registry auth carries integrity for v1.5
   - Cosign / sigstore signatures on roadmap
   - In-toto attestation on roadmap

10. Caching & offline
    - Content-addressed cache at `~/.agents/cache/<sha256>/`
    - `.agentsrc.lock` for digest pinning
    - Offline behavior (fall back to cache)

11. Rollout phasing
    - v1.5 (Option B): transport+auth+content split, OCI wire, BYO registry,
      audit events, no FIPS variant, no signing
    - v2 (Option C): registry-as-first-class, thin-wrapper server, signing,
      rich discovery

12. Migration
    - Existing git sources: backward-compatible shorthand vs. retrofit
    - `.agentsrc.json` schema version bump
    - Existing agent profile authors: how they publish to OCI

13. Open questions (see below)
```

### Open items (carried into the fork, NOT blocking this plan)

- **Q1 (persona dependency)**: Ask the insurance persona's CMMC assessor — "is MIP-state FIPS 140-3 module acceptable per NIST IG D.G, or is a fully-validated CMVP certificate required?" Answer determines whether a BoringCrypto variant ever ships. 80% confidence the answer is "MIP is fine." Until that confirmation, single binary + `GOFIPS140=inprocess` is the plan.
- **Q2 (public default registry)**: Where does the public registry live? Options: (a) GHCR under a `dot-agents` org — zero infra, GitHub identity handles abuse/TOS; (b) owned infrastructure — full control, ongoing cost/ops; (c) no default — `.agentsrc.json` must explicitly configure. Lean toward (a) for zero-infra, but defer the call to the fork.

---

## D7 — Iteration-log ownership + schema evolution

### Current state (audited from `commands/workflow.go:970-1010` + `schemas/workflow-iter-log.schema.json`)

**CLI-written (deterministic, single author today)**:
`schema_version`, `iteration`, `date`, `wave`, `task_id`, `commit`, `files_changed`, `lines_added`, `lines_removed`, `first_commit`

**Agent-written (today: loop-worker fills all of these single-handedly)**:
`item`, `scenario_tags`, `feedback_goal`, `tests_added`, `tests_total_pass`, `retries`, `scope_note`, `summary`, `self_assessment{13 sub-fields}`

The flat shape assumes one agent authors everything agent-side.

### Problem under role-pure pipeline
Three+ agents contribute per iteration (impl, verifiers[], review). Flat fields force one agent to overwrite another's context, lose per-role evidence, and make auto-collection ambiguous (whose `tests_added`? whose `self_assessment.committed_after_tests`?).

**Resolution**: keep the on-disk iter-log as **nested role sub-objects** (`impl`, `verifiers[]`, `review`) and make the small-model reliability problem a **CLI interface problem**, not a YAML-authoring problem.

Reasoning locked:
- Disk shape and agent-facing interface are separable concerns.
- The role set is fixed enough to justify named sub-objects: `impl` is singleton, `review` is singleton, `verifiers` is naturally an array keyed by verifier type.
- Flat `role_contributions[]` would make every downstream reader iterate and filter for a speculative future extension case, while nested blocks match the actual ownership model now.
- Reliability for Haiku / low-effort Sonnet comes from never asking the agent to hand-author nested YAML. The CLI owns merge semantics, validation, and auto-derived fields.

### Schema evolution — `schema_version: 2`
Replace flat agent fields with three role-scoped sub-objects. CLI deterministic fields stay at top-level unchanged.

```yaml
# CLI fields unchanged
schema_version: 2
iteration: N
date: YYYY-MM-DD
wave: <plan>
task_id: <task>
commit: <sha>           # commit SHA of whichever agent most-recently called log-to-iter
files_changed: N        # cumulative across the iteration's commits
lines_added: N
lines_removed: N
first_commit: false

# NEW: per-role contribution blocks
impl:
  item: "..."
  summary: "..."
  scope_note: on-target|scope-breach|partial|""
  feedback_goal: "..."              # AUTO — carried from bundle
  retries: N
  focused_tests_added: N            # impl-side focused tests only
  focused_tests_pass: true|false|null
  self_assessment:
    read_loop_state: bool
    one_item_only: bool
    committed_after_tests: bool
    aligned_with_canonical_tasks: bool
    persisted_via_workflow_commands: "yes|no|paused — <reason>"
    stayed_under_10_files: bool
    no_destructive_commands: bool
    scoped_tests_to_write_scope: bool       # NEW — D12 compliance
    tdd_refresh_performed: bool             # NEW — D3 enforcement hook (auto from gate, not agent-trusted)

verifiers:                                  # array, one entry per verifier type that ran
  - type: unit|api|ui_e2e|batch|streaming
    status: pass|fail|partial               # AUTO — parsed from <type>.result.yaml
    gate_passed: bool                       # AUTO — parsed from <type>.result.yaml
    tests_added: N
    tests_total_pass: bool|null
    scenario_tags: [...]
    retries: N                              # D3 — per-verifier retry count this iteration
    result_artifact: .agents/active/verification/<task_id>/<type>.result.yaml
    self_assessment:
      tests_positive_and_negative: bool
      tests_used_sandbox: bool
      exercised_new_scenario: bool
      ran_cli_command: bool
      cli_produced_actionable_feedback: "yes|no|informative-nonblocking"
      linked_traces_to_outcomes: bool

review:
  phase_1_decision: accept|reject|escalate|""      # D8 — broad domain-stability review
  phase_2_decision: accept|reject|escalate|""      # D8 — tech-lead architectural review
  overall_decision: accept|reject|escalate|""      # AUTO — derived from phase_1 + phase_2
  failed_gates: [...]
  escalation_reason: ""
  reviewer_notes: "..."
  decision_artifact: .agents/active/verification/<task_id>/review-decision.yaml
  verify_record_appended: true|false              # AUTO — set by workflow verify record (D1)
```

### Ownership map — who fills what

| Field group | Filled by | When / how |
|---|---|---|
| CLI deterministic (top-level) | Each agent calling `workflow checkpoint --log-to-iter N` | After each agent's commit |
| `impl.*` | impl-agent | `workflow checkpoint --log-to-iter N --role impl` right after its commit |
| `verifiers[<type>].*` | verifier-`<type>` agent | `workflow checkpoint --log-to-iter N --role verifier --verifier-type <type>` after writing `<type>.result.yaml` |
| `review.*` | review-agent | `workflow checkpoint --log-to-iter N --role review` after writing `review-decision.yaml` |

### Small-model-safe agent interface

Agents do **not** write YAML directly. They call role-specific CLI entry points and let the CLI own:
- block selection (`impl` vs `verifiers[<type>]` vs `review`)
- merge behavior
- schema validation
- auto-derived fields from artifacts / bundle / gate state

Preferred interface rules:
- `--role` selects the block; the agent never specifies YAML paths.
- Use a small number of role-specific scalar flags for common fields.
- Use constrained enums where semantics matter (`scope_note`, review decisions, verifier feedback statuses).
- Support `--template` / `--payload-file` so a weaker model can fill a generated JSON stub instead of generating nested content from scratch.
- Reject overwrite-by-accident; use explicit replace semantics when a role needs to rewrite its own block.

Representative shape:

```text
workflow checkpoint --log-to-iter N --role impl
workflow checkpoint --log-to-iter N --role verifier --verifier-type <type>
workflow checkpoint --log-to-iter N --role review
workflow checkpoint --log-to-iter N --role impl --template > /tmp/iter-impl.json
workflow checkpoint --log-to-iter N --role impl --payload-file /tmp/iter-impl.json
```

This keeps the durable file ergonomic for humans and downstream readers while giving low-effort models a narrow, retryable command surface.

### New auto-collection opportunities (CLI-derived; zero agent input)

| Field | Source | Implementation note |
|---|---|---|
| `impl.feedback_goal` | bundle's `verification.feedback_goal` | Read the bundle at stub creation |
| `impl.focused_tests_added` | git diff HEAD~1 — count new `*_test.go` (+ language-equivalents) files / top-level funcs | Extends `gitIterDiffStat` |
| `impl.focused_tests_pass` | exit code of impl-agent's focused test invocation | Needs capture hook (nice-to-have; falls back to agent-written) |
| `impl.self_assessment.tdd_refresh_performed` | result of the pre-verifier TDD-fresh gate (D3) | CLI writes, not agent |
| `verifiers[].type` | `--verifier-type` flag | Echo |
| `verifiers[].status` / `gate_passed` | parse `<type>.result.yaml` inline during log-to-iter call | Same pattern as bundle read |
| `verifiers[].result_artifact` | constructed path | String interpolation |
| `review.phase_N_decision` / `overall_decision` / `failed_gates` | parse `review-decision.yaml` | Same pattern |
| `review.decision_artifact` | constructed path | String interpolation |
| `review.verify_record_appended` | set `true` after successful `workflow verify record` (D1) | Cross-command flag |

### New `workflow checkpoint --log-to-iter` CLI surface

```
workflow checkpoint --log-to-iter N                                           # CLI-only (legacy; writes v1-compatible top-level fields)
workflow checkpoint --log-to-iter N --role impl                               # impl-agent call
workflow checkpoint --log-to-iter N --role verifier --verifier-type <type>    # one per verifier
workflow checkpoint --log-to-iter N --role review                             # review-agent call
```

Each `--role` invocation **merges** into the existing `iter-N.yaml` (first call creates the file with CLI fields + the role's block; later calls add their own blocks; never overwrite other roles' blocks). Schema validation runs on the merged document before write.

### Migration
- Bump `schema_version` → 2. Keep v1 struct readable for historical iter-logs in `.agents/history/`.
- New CLI writes v2 only.
- v1 stubs in active trees (if any) migrate on first v2 write: load v1 → project into v2 (`item` → `impl.item`, `self_assessment.*` split across roles by heuristic) → rewrite.

### Locked implications for plan-iter.2
- The iter-log task writes `schema_version: 2` with nested role blocks.
- `workflow checkpoint --log-to-iter` grows role-aware entry points and merge validation.
- Weak-model ergonomics are handled by the CLI contract (`--role`, per-role flags, optional template/payload-file flow), not by flattening the persisted schema.

---

## D8 — Role split + orchestrator awareness (from human-thoughts §identity)

**Resolution**:
- `loop-worker` AGENT.md **stays untouched**. Pattern E callers must not break.
- New agent definition: `~/.agents/agents/dot-agents/impl-agent/AGENT.md` — senior-developer flavor, app_type-aware.
- Verifier agents: one skill per type under `.agents/skills/verifiers/<type>/` with senior-SDET flavor, app_type-aware.
- **Review-agent is two-phase**:
  1. Broad domain-stability review (is the slice moving the project in the right direction, does the domain model still hold).
  2. Tech-lead architectural review (standards, architectural decisions properly implemented and complete).
- **Focused unit tests are impl-agent's responsibility** (mandatory, during/after implementation). Typed verifier suites run after, by dedicated verifier agents.
- **Orchestrator skill + prompts (`orchestrator-session-start`, `ralph-orchestrate`, `.agents/active/orchestrator.loop.md`) MUST be updated** to pick the right profile per role and generate per-task prompt files. Original plan was silent on this — add an explicit task (call it `p8-orchestrator-awareness`, or fold into p5 — to be decided during plan-iter.2).

---

## D9 — Command ownership (resolves the "missed commands" audit)

| Command | Owner in new pipeline | Notes |
|---|---|---|
| `workflow merge-back` | **verifier aggregate step** — fills new verification fields derived from `*.result.yaml` artifacts | Requires new verification fields on the merge-back artifact; reviewer consumes those fields |
| `workflow checkpoint --log-to-iter` | **each agent fills its own role block** | See D7 |
| `workflow verify record` | **review-agent** (flag-first writer per D1) | Writes both global `verification-log.jsonl` row and per-task `review-decision.yaml` |
| `workflow fold-back create` / `update` | **post-closeout orchestrator**, grouped by task/slice run (related observations land together; clearly unrelated observations stay separate). `update` enabled per D2 | Needs identity scheme (D2.a) |
| `workflow fanout --verifier-sequence` | **new flag, workflow fanout** — resolves `app_type` → sequence, writes into bundle | Schema + CLI change; same `write_scope` |
| `dot-agents refresh` | Deferred to the D6 fork (registry source type not in this plan) | — |

**Accept-path flow**:
impl-agent commits → writes `impl-handoff.yaml` + `focused-unit.result.yaml` → calls `workflow checkpoint --log-to-iter N --role impl` → pre-verifier TDD-fresh gate (D3) → verifiers each write `<type>.result.yaml` + `workflow checkpoint --log-to-iter N --role verifier --verifier-type <type>` → verifier aggregate calls `workflow merge-back` (filling new verification fields) → reviewer reads validation artifacts → calls `workflow verify record` with structured decision flags (CLI writes `review-decision.yaml` + audit row) → calls `workflow checkpoint --log-to-iter N --role review` → orchestrator closeout (archive + cleanup per D13).

---

## D10 — Schema + CLI deltas co-located with feature task (no floating schemas)

Pattern inherited from `loop-runtime-refactor` phase-5d (iter-log schema): **schema change ships in the same task as the feature**.

| Task | Schemas to include in `write_scope` |
|---|---|
| `p3a-result-schema` | `schemas/verification-result.schema.json` (NEW) |
| `p5-fanout-dispatch` | `schemas/workflow-delegation-bundle.schema.json` (add `verifier_sequence`), `schemas/workflow-plan.schema.json` (add `app_type`), `schemas/agentsrc.schema.json` (add `verifier_profiles`, `app_type_verifier_map`) |
| D7 iter-log evolution task | `schemas/workflow-iter-log.schema.json` (schema_version: 2 + role sub-objects) |
| D1 verify-record merge task | possibly `schemas/workflow-verify-record.schema.json` if one exists; `schemas/verification-decision.schema.json` (NEW if needed) |
| ~~`p7-external-sources`~~ | **Moved to D6 fork — no schema change in this plan** |

Per human-thoughts: *"have the schemas ready / embedded in p5 so that it can create the file or use the file and then integrate it properly into the cli tool"* — schemas land first, then CLI integration in the same task.

---

## D11 — Task dependency correction

`p5-fanout-dispatch.depends_on` must include **p3a** (verification-result schema is the contract p5 writes into bundles). Plan-iter.1 lists `[p3b, p3c, p4]` only.

---

## D12 — Parallel verifier isolation (scope-limited for this plan)

**OUT OF SCOPE**: worktree-per-worker isolation + PR reintegration cycle (needs separate design).

**IN SCOPE**:
- Focused unit tests scoped to `write_scope_touched` files during implementation (impl-agent side).
- Full configured suite runs on push / PR via CI (no in-pipeline full-suite execution per worker).
- Unit verifier AGENT.md command: `go test ./... -race -count=1 -timeout=300s` (`-count=1` disables caching).
- Playwright (api + ui_e2e verifiers): `PLAYWRIGHT_BASE_PORT` env convention for parallel port assignment; include `playwright install --with-deps chromium` one-time check in AGENT.md.

---

## D13 — Verification artifact directory lifecycle (accept path)

Directory: `.agents/active/verification/<task_id>/`

**Populate**:
- impl-agent → `impl-handoff.yaml` + `focused-unit.result.yaml`
- each verifier-`<type>` → `<type>.result.yaml`
- review-agent → `review-decision.yaml`

**Cleanup (accept path)**: delegation closeout archives the directory alongside merge-back to `.agents/history/<plan>/verification/<task_id>/`, then deletes from active.

**Deny paths**: see D3 (reviewer-first diagnosis; directory persists across retries; archived on final resolution).

**Creation**: `ralph-pipeline` `mkdir -p`'s the directory right before spawning verifiers for the task (it knows `task_id` and is the control-plane). Can shift to impl-agent if a cleaner owner emerges during plan-iter.2.

---

## D14 — `RALPH_RUN_PLAN` scoping

- Accepts single id OR comma-separated list (multi-plan filter).
- `discover_unblocked_tasks()` must honor the filter: iterate only plans in the list.
- `workflow next` call in the break check (D4) must match: one call per listed plan; break only when **all** return empty.

---

## D15 — TDD discipline (applies to every task)

- Tests written **before** implementation against the task's acceptance criteria.
- Create new tests for uncovered scenarios; update existing tests when compile breaks or scenario changed; remove tests whose business scenario is out of date.
- Applies at **both** levels: impl-agent focused-unit AND verifier-run suites (each verifier type follows TDD within its scope — e.g. api verifier adds/updates Playwright contract tests before the impl-agent changes handlers).
- **Enforcement**: pre-verifier environment gate (D3). `impl.self_assessment.tdd_refresh_performed` in iter-log is set from the gate's result, not trusted from agent self-report.

---

## D16 — Explicit out-of-scope for this plan

- Worktree-based parallel worker isolation + PR reintegration (future).
- Regression-suite daemon built from verify-time tests (future; part of a broader "daemon processes" design pass).
- External-sources schema + CLI work (D6 fork — design doc first, integrate later).

---

## Residual follow-ups after plan-iter.2

No main-plan follow-ups remain open from iter-1. D2.a and D3.a were resolved inline in [`plan-iter.2.md`](./plan-iter.2.md).

### Fork-scoped open items (D6 external-sources — NOT blocking this plan)

| ID | Question | Resolves during |
|---|---|---|
| Q1 | Insurance persona's CMMC assessor ruling: MIP-state FIPS 140-3 acceptable per NIST IG D.G, or strict-validated CMVP certificate required? Determines whether BoringCrypto variant ships. | External-sources design fork; persona outreach |
| Q2 | Public default registry host: (a) GHCR under a `dot-agents` org [lean — zero infra], (b) owned infrastructure, (c) no default. | External-sources design fork |

---

## Next step after plan-iter.2

1. Generate the canonical `PLAN.yaml` / `TASKS.yaml` for `loop-agent-pipeline` from `plan-iter.2.md`.
2. Carry Q1 and Q2 only inside the external-sources design fork; they do not block the main task graph.

With D1, D2, D3, D6, and D7 now locked strongly enough for authoring, the next artifact should be the da-style plan/task files rather than another markdown planning pass.
