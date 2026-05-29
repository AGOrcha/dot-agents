# R5 — Human-in-the-loop review, labeling, and access layer — spec

**Status:** draft (2026-05-27)
**Scope:** project (dot-agents)
**Parent spec:** [`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)
**Plan:** `.agents/workflow/plans/r5-review-labeling-access/` (PLAN.yaml + plan-level design.md)
**Sibling specs:** [`r2-observability-dashboard`](../r2-observability-dashboard/design.md), [`r4-code-task-generation-eval`](../r4-code-task-generation-eval/design.md)

## Problem

R1 scores runs from telemetry; R2 surfaces those scores. Neither lets a human reviewer assert "the score got this wrong / right," annotate a run with structured judgment (correctness, scope creep, hallucination), or have that judgment feed the next recomputation. There is also no access boundary — today the service binds to loopback and treats every caller equally. R5 closes the feedback loop and adds the access controls required to do so safely with a tamper-evident audit trail.

## Goals

1. Let a reviewer attach structured judgment to any iteration or session — labels persist, are auditable, and feed R1 as a first-class signal on recomputation.
2. Distinguish three roles (reviewer, admin, read-only) and enforce them at the API layer.
3. Make every mutating action append-only auditable; no silent edits.
4. Sit inside the existing service host (R3); add routes, do not fork a parallel service.
5. Extend, do not replace, R2's read surface — reviewer routes are layered on top.

## Personas

- **Reviewer.** Inspects flagged or recent runs, attaches a structured label and free-text comment, occasionally edits their own label.
- **Admin.** Manages users and roles, prunes the audit log on explicit operator request, verifies audit-chain integrity, can edit any reviewer's labels in the rare correction case.
- **Read-only.** Sees the dashboard and label history; cannot write.

## Decisions

### D5.1 — Labels live next to scores, as YAML sidecars

Labels are persisted as `iter-N.labels.yaml` sidecars in the same iter-log directory as the score sidecars R1 already writes. Multi-label is append-on-edit (no destructive overwrite).

**Why:** the existing telemetry pipeline already reads from this directory. A new signal extractor can pick up labels without an out-of-band store. A SQLite mirror can be added later inside R3 if query patterns demand it.

**Rejected:** SQLite-first (forks the source-of-truth question R2 also deferred); a separate label service (introduces a second deployment artefact for no v1 win).

### D5.2 — Labels become a first-class signal in a major rubric version bump

Per the umbrella spec D3, any change to the signal mix requires a major rubric version bump. R5 introduces a `human_label` signal that the new rubric version includes. The signal is **absent** when no human label exists for the iteration, preserving R1's invariant that absent signals do not vote.

**Why:** treating labels as a first-class signal is the whole point of "feed the loop." Hiding labels in a sidebar makes them a comment, not a reward signal. The version bump preserves comparability of older scores under the prior rubric version (no retroactive score invalidation).

**Rejected:** patching the existing rubric version in place (breaks comparability across time); a parallel "label score" outside the rubric (forks the metric).

### D5.3 — Identity is a bearer token presented to the service; users are managed via CLI

The service expects an `Authorization: Bearer <token>` header. Tokens are issued by admin-only CLI (`da review users add --role reviewer <email>`) and recorded with a hashed secret and role in an admin-managed local users file. A pluggable authenticator interface keeps the door open to swap in OIDC later.

The users file lives at **`~/.config/da/review/users.yaml`** — honoring `$XDG_CONFIG_HOME` (→ `$XDG_CONFIG_HOME/da/review/users.yaml`) and falling back to `~/.config/da/review/users.yaml`. It is **per-host, never synced**, written 0600.

**Why this path (not AGENTS_HOME):** AGENTS_HOME is the git-synced config tree that `da sync` commits and pushes. RBAC users carry hashed secrets and are per-deployment, so they must never resolve into AGENTS_HOME — committing them would push auth state to the shared remote. Cross-host/team RBAC is the obs-server's job, not a synced file. The users file therefore lives in the same local-secrets home as the existing `~/.config/da/credentials.json` (external-agent-sources / agorcha §5.4), where the future auth-proxy injector centralizes credential handling.

**Why bearer + CLI:** single-tenant per umbrella D5. No OAuth dependency; the auth surface is auditable as plain YAML. The CLI is the same surface that already manages other agent configuration.

**Rejected:** no auth (umbrella D5 mandates RBAC); OAuth/OIDC first (heavy dependency for a single-tenant tool); SSH-key-based auth (couples to the user's shell config; doesn't extend to a browser session); users file in AGENTS_HOME (git-synced — would leak hashed secrets to the shared remote on every `da sync`).

> **Principle — secrets never live in AGENTS_HOME.** Any secret / auth / credential state resolves into the per-host local-secrets home `~/.config/da/` (honoring `$XDG_CONFIG_HOME`), NEVER AGENTS_HOME (which `da sync` git-commits and pushes). This covers `~/.config/da/credentials.json` (external-agent-sources / agorcha §5.4) and `~/.config/da/review/users.yaml` (this spec), and is the home the future auth-proxy injector centralizes on.

### D5.4 — Audit log is append-only JSON-lines with hash-chained records

Every mutating action writes one line: `{ts, actor, role, action, target, before_hash, after_hash, prev_hash, request_id}`. Each record's `prev_hash` is the SHA-256 of the previous record, giving a tamper-evident chain without a real ledger or external dependency. File: `.agents/active/review/audit.log.jsonl`, rotated yearly. Retention is indefinite by default; an admin-only prune command can compact older years on operator request.

**Why:** labels feed scoring forever (a re-score under a new rubric version must be reproducible), so the audit log must outlive any single rubric version. JSON-lines beats YAML sidecars because the log is high-volume, append-only, and line-oriented — the wrong shape for YAML sidecars.

**Rejected:** YAML-sidecar audit (parse cost grows with log size; append is awkward); external ledger / database (operational dep we do not need); RFC-3161 / external trusted-timestamper (overkill for single-tenant).

### D5.5 — Routes mount inside R3; R5 owns the auth + audit middleware

R5 contributes three route trees to the R3-hosted service:
- An augmentation of R2's existing read routes (adds `labels[]` to existing payloads behind a composition point).
- New write routes for labels (submit, edit).
- New admin routes for the audit view and user management.

R3 owns the HTTP host, middleware chain, and lifecycle. R5 owns the route handlers and the auth/audit middleware that wraps them.

**Why:** umbrella D2 says R3 hosts both R2 and R5. Forking a separate listener forks the deploy story.

### D5.6 — Reviewer UI is two new routes inside R2's SPA

The R5 UI is `/review` (filterable list of runs) and `/review/:iteration` (inspect + label form). R5 reuses R2's React shell, query client, and auth-header plumbing. The push channel is not needed (labeling is post-hoc and deliberate).

**Why:** one frontend artefact, one auth-header plumb. R5 ships UI as routes inside R2's bundle, not a separate SPA.

### D5.7 — Label model is structured + free-text; structure is enum-bounded

A label carries:
- `correctness` (0-3 scale)
- `scope_judgement` (on-target | partial | breach)
- `hallucination` (none | minor | major)
- `free_text` (≤ 4000 chars)

The structured dimensions feed the `human_label` signal sub-score; the free text is human-readable and surfaces in the UI but does not directly affect the score.

**Why:** unstructured free text alone can't feed a numerical signal. Pure structure misses the "why" the reviewer can articulate. Enum-bounded structure prevents free-text drift while keeping the cognitive load on the reviewer low.

### D5.8 — Multi-label aggregation: mean of latest-per-reviewer

When multiple reviewers label the same iteration, the `human_label` signal sub-score is the mean of each reviewer's most recent structured label. An admin override label (explicitly marked) supersedes reviewer labels for that iteration.

**Why:** simple, defensible, reproducible. Locked at implementation of the R1 integration task; revisitable if it produces poor outcomes.

## Requirements (behavioral)

1. **R1.** A reviewer can submit a structured label + free text against any iteration via a POST to a labeled-iteration route; the label persists as a YAML sidecar entry.
2. **R2.** A reviewer can edit a label they own; an admin can edit any label. Each edit appends to the sidecar (not destructive) and writes an audit record.
3. **R3.** A read-only role can fetch labels but cannot POST or PATCH.
4. **R4.** A `human_label` signal appears in iteration scores under the new rubric version when at least one label exists; the signal is absent (does not vote) when no label exists.
5. **R5.** Scores under the prior rubric version remain queryable and unchanged; the bump does not retroactively invalidate them.
6. **R6.** Every mutating action (label submit, label edit, user create, user delete, role change) writes one append-only audit record with hash chain.
7. **R7.** `da review audit verify` walks the chain and reports the first integrity break point, if any, in non-zero exit code.
8. **R8.** Token presentation is enforced at the middleware layer; a missing or invalid token returns a 401, an insufficient role returns a 403.
9. **R9.** Admin-only CLI manages users; a non-admin invocation is rejected.
10. **R10.** The R2 dashboard's run detail surface displays labels (with reviewer identity + timestamp) for every iteration that has them, behind the composition point R2 owns.

## Done criteria (verifiable)

1. End-to-end test: seed a users file, POST a label as a reviewer token, run `da score iteration N --recompute`, assert the resulting sidecar's breakdown contains a `human_label` row with `Present=true` and the expected sub-score.
2. A read-only token POSTing a label returns 403; a reviewer token POSTing to `/api/review/users` returns 403; a missing token returns 401.
3. Write 5 labels in sequence, run `da review audit verify`, assert OK exit. Flip a byte in record 3, re-verify, assert non-zero exit naming record 3.
4. Re-scoring an iteration with a label under the new rubric version produces a non-null `human_label` sub-score; an iteration with no label produces an absent (does-not-vote) entry.
5. The same iteration scored under the prior rubric version still returns the prior version's breakdown unchanged (rubric-version reproducibility holds).
6. Admin CLI `da review users add --role reviewer test@example.com` adds the user; `da review users list` shows the new entry with a hashed secret (plaintext appears only once at issuance).
7. The dashboard `/review` route renders a list of runs filtered by "has unlabeled iterations"; clicking through to `/review/:iteration` shows the label form and any existing labels.
8. Two reviewers labelling the same iteration produce a `human_label` sub-score that is the mean of their latest structured labels.

## Open questions (must resolve before or during implementation)

1. **OQ1 — Token issuance UX.** Print plaintext once at issuance and store only the hash, or persist plaintext for re-display? Recommendation: print-once + hash; revisit if reviewers report friction.
2. **OQ2 — Admin label override precedence.** When an admin edits a reviewer's label vs. submits their own admin-marked label, which one feeds the signal? Recommendation: admin's *own* label, when present, overrides the reviewer mean entirely; admin edits to a reviewer's label are tracked as the reviewer's label (audit captures who edited).
3. **OQ3 — Label form versioning.** The structured-dimension enum is locked at v1; future additions (severity, reproducibility) require a label-schema version field. Recommendation: include `label_schema_version` on every label record now to avoid a later migration.
4. **OQ4 — Audit log rotation trigger.** Yearly is the proposal; should size-based rotation also fire (e.g. > 100 MB)? Recommendation: yes, size cap as an OR-condition; pin the cap at implementation.
5. **OQ5 — Eval-run labeling.** Can a reviewer label an eval iteration (from [`r4`](../r4-code-task-generation-eval/design.md)) the same way as a production iteration? Recommendation: yes — same sidecar shape, same signal, eval runs become a useful labeled corpus for rubric tuning.
6. **OQ6 — Cross-iteration labels.** Some judgment applies to a whole session (e.g. "this whole wave was a regression"), not a single iteration. v1 ships iteration-level only; session-level labels are deferred.
7. **OQ7 — Loopback enforcement when auth is enabled.** When R5 is active, does the service still bind only to loopback? Recommendation: yes by default; an admin config flag promotes it to a bound interface, gated on at least one reviewer user existing.

## Deferred (explicitly out of scope)

- Session-level labels (see OQ6).
- Comment threads / multi-turn reviewer discussion on a single iteration.
- Reviewer-to-reviewer notifications.
- Webhook / external integration of labels (Slack, GitHub PR comment, etc.).
- OAuth / OIDC integration (forward door only).
- Per-tenant isolation (umbrella D5 says single-tenant).
- Label-driven automatic re-scoring on a schedule (re-score is explicit `da score iteration N --recompute` in v1).
- Bulk label import.
- Label conflict-resolution UX beyond the mean rule in D5.8.
- An external trusted-timestamp service for the audit log.

## Relationship to other specs and plans

- **[`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)** — parent; D3 (rubric versioning) is why the new signal requires a major bump; D5 (RBAC) is the role contract.
- **`r1-outcome-scoring`** (completed) — the rubric R5 extends. The `human_label` signal sits inside the new rubric version's signal list.
- **[`r2-observability-dashboard`](../r2-observability-dashboard/design.md)** — R5's UI is two routes inside R2's SPA; R5 augments R2's payloads behind a composition point R2 owns.
- **[`r4-code-task-generation-eval`](../r4-code-task-generation-eval/design.md)** — eval iterations are labelable through the same surface (see OQ5). Eval + label together yield a labeled corpus for rubric tuning.
- **`r3-background-worker-service`** (sibling plan) — owns the HTTP host and middleware chain R5's routes mount into.
- **[[verifier-owns-ci-watch-shift-left]]** — R5's audit-chain verification (`da review audit verify`) is a candidate CI gate in the project overlay; a broken chain in the dot-agents repo's own audit log should fail PR CI.
- **[[worker-owns-pr-readiness-loop]]** — R5's label-submit path is one of the few mutation surfaces a worker might be tempted to hit; the worker briefing should explicitly exclude label submission (label submission is a human action by spec).
- **`config-explain-live-surface`** (proposal in `.agents/proposals/codex-019e6245-examination-and-sequenced-plan.md`) — the future `da config explain` surface should be able to report the configured `auth.users_file` path and bound-interface state.

## Candidate canonical-plan tasks (appendix; not yet materialized)

The plan-level `design.md` already sketches the implementation; this is the spec-side index.

1. **t-label-schema** — define the label record schema (v1, including `label_schema_version` per OQ3); JSON schema; Go types.
2. **t-label-persist** — sidecar write/read path; append-on-edit semantics; sidecar test fixtures.
3. **t-rubric-vNext** — bump rubric to the new major version including the `human_label` signal; ensure prior version still loads + scores.
4. **t-signal-human-label** — new signal extractor that reads label sidecars and produces a sub-score; absent-does-not-vote test.
5. **t-users-file** — admin-managed users file format; hashed-secret storage; load/save helpers.
6. **t-auth-middleware** — `Authorization: Bearer` middleware; 401/403 enforcement; pluggable authenticator interface.
7. **t-audit-chain** — JSON-lines writer + SHA-256 chaining + rotation policy (yearly + size cap per OQ4).
8. **t-cli-review-users** — `da review users {add,list,remove}` admin-only commands.
9. **t-cli-review-audit** — `da review audit {tail,verify,prune}` commands.
10. **t-routes-labels** — POST/PATCH label routes wired into R3's host; R2 payload augmentation.
11. **t-routes-admin** — audit + users admin routes.
12. **t-frontend-review-list** — `/review` route in R2's SPA.
13. **t-frontend-review-detail** — `/review/:iteration` route + label form.
14. **t-e2e-label-flow** — end-to-end test per done-criterion 1.
15. **t-e2e-rbac** — RBAC tests per done-criterion 2.
16. **t-e2e-audit-tamper** — audit tamper test per done-criterion 3.
17. **t-eval-labeling-bridge** — confirm eval iterations are labelable through the same surface (OQ5).

The plan-level design at `.agents/workflow/plans/r5-review-labeling-access/design.md` already encodes the implementation tech for each; this spec is the contract the plan is accountable to.
