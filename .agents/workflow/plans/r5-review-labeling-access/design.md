# R5 — Human-in-the-loop review, labeling, and access layer — design

**Status:** draft (2026-05-25)
**Parent spec:** `.agents/workflow/specs/agent-run-scoring-observability-platform/design.md` (D1–D5)
**Depends on:** R1 (`internal/scoring/`, shipped), R3 (collection endpoint host; D2), R2 (UI surface)
**Consumes:** persisted iteration scores in `.agents/active/iteration-log/iter-N.score.yaml`
**Produces:** persisted label records, persisted audit log, a new rubric `human_label` signal feeding R1 on recomputation.

## Problem

R1 scores runs from telemetry. There is no path for a human reviewer to (a) say "the score got this wrong / right," (b) annotate a run with structured feedback (correctness, scope creep, hallucination, etc.), or (c) feed that judgment back into the next score recomputation. R5 closes that loop and adds the access controls required to do so safely (reviewer vs. admin vs. read-only) with a tamper-evident audit trail.

## Decisions

### D5.1 — Label storage: YAML sidecars under the iteration log

Labels live next to the scores they comment on, mirroring R1's `iter-N.score.yaml` sidecar pattern (`internal/scoring/persist.go`). Path: `.agents/active/iteration-log/iter-N.labels.yaml` (multi-label, append-on-edit). Rationale: the existing telemetry pipeline (`BuildSignalSets` / `ScoreAll`) already reads from this directory; a new `signal_human_label.go` extractor can pick up labels without an out-of-band store. A SQLite mirror can be added later inside R3 if query patterns demand it.

### D5.2 — Labels become a new `SignalHumanLabel` signal in a v3 rubric bump (RubricVersion 3.0.0)

Per umbrella D3, any change to the signal mix requires a major rubric bump. The new signal is:
- ID `human_label`, weight ~0.15 (re-balanced; correctness still dominates).
- Sub-score = mean of structured label dimensions (`correctness`, `scope_judgement`, `hallucination_free`, each on a 0–1 scale derived from a fixed enum).
- Absent when no human label exists for the iteration — preserves R1's "absent does not vote" invariant (`internal/scoring/scorer.go` weighted_mean_renormalized).
- TwoWay = false in v1 of R5; it is the human's verdict, not a self-report.

This keeps R5's reward signal first-class without retro-invalidating any RubricVersion-2.0.x score (umbrella spec §29).

### D5.3 — RBAC: identity-via-header against an admin-managed local user file

Single-tenant per umbrella D5. Three roles (`reviewer`, `admin`, `readonly`). Identity is established by a signed token (`Authorization: Bearer <token>`) presented to the R3-hosted endpoints; tokens are issued by `da review users add --role reviewer <email>` (admin-only CLI) and persisted in `~/.agents/review/users.yaml` with hashed-secret + role. This avoids dragging in an OAuth dependency for a tool that has no production multi-tenant requirement and keeps the auth surface auditable as plain YAML. A pluggable `Authenticator` interface keeps the door open to swap in OIDC later.

### D5.4 — Audit log: JSON-lines, content-addressed, retained indefinitely (resolves the open question)

Format: newline-delimited JSON, one record per mutating action, fields `{ts, actor, role, action, target, before_hash, after_hash, request_id}`. Each record's `prev_hash` chains to the previous record's SHA-256, giving a tamper-evident log without a real ledger. File: `.agents/active/review/audit.log.jsonl`, rotated yearly (`audit.log.<year>.jsonl`). Retention is **indefinite by default** (the workflow already ships a tiny on-disk footprint; labels are governance-relevant signal); a `da review audit prune --before <date>` admin-only command can compact older years on operator request. Read-only verification: `da review audit verify` walks the chain.

Rationale for "no automatic retention window": labels feed R1 scoring forever (a re-score under a new RubricVersion must be reproducible), so the audit log must outlive any single rubric version. JSON-lines is chosen over the YAML sidecar pattern because the audit log is append-only / high-volume / line-oriented — the wrong shape for YAML sidecars.

### D5.5 — Mount surface inside R3

R5 contributes three HTTP route trees to the R3 service (per umbrella D2):
- `GET /api/runs[/:id]` — extended by R5 with a `labels[]` field (R2 already owns the base list/detail surface; R5 augments the response, behind an interface so the augmentation is one composition point).
- `POST /api/runs/:iteration/labels` (reviewer+admin) — submit a label.
- `PATCH /api/runs/:iteration/labels/:id` (label-owner or admin) — edit.
- `GET /api/review/audit` (admin) — paginated audit view.
- `POST/GET/DELETE /api/review/users` (admin) — user/role management.

The R3 plan owns the HTTP host + middleware chain; R5 owns the route handlers and the auth/audit middleware that wraps them.

### D5.6 — R2 dependency: only the dashboard skeleton needs to ship first

R5's UI is two new routes inside R2's React app: `/review` (list+filter runs by score / unlabeled-first) and `/review/:iteration` (inspect + label form). The R5 work is unblocked once R2 has shipped: (a) the API client + auth-header plumbing, and (b) a routable shell with at least one "run detail" page that R5 can wrap. R5 does not need R2's real-time push surface — labeling is a deliberate, post-hoc action.

## Open decisions deferred to implementation

- **Label form schema**: which structured dimensions? Proposal: `{correctness: 0..3, scope: on-target|partial|breach, hallucination: none|minor|major, free_text: string ≤ 4000 chars}`. Locked at the `label-model` task.
- **Token issuance UX**: print once + persist hash, or persist plaintext? Default: print once, store bcrypt hash; revisit if it's annoying in practice.
- **Multi-label conflict resolution**: when two reviewers label the same iteration differently, how does the `human_label` signal aggregate? Proposal: mean of latest-per-reviewer; locked at the `r1-integration` task.

## Verification (plan-level)

1. End-to-end smoke (single Go test): seed users file → POST a label as `reviewer-A` → `da score iteration N --recompute` → assert the resulting sidecar's breakdown contains a `human_label` row with `Present=true` and the expected `SubScore`.
2. RBAC: `readonly` token POSTing a label returns 403; `reviewer` POSTing to `/api/review/users` returns 403.
3. Audit chain: write 5 labels, run `da review audit verify`, assert OK; flip a byte in record 3, assert verify reports the break point.
