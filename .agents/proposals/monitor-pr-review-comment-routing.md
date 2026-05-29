# Proposal: PR Review + Comment Routing Contract for Monitor and Daemon

**Status:** draft
**Created:** 2026-05-28
**Scope:** project-local (dot-agents)
**Related:** `[[r3-background-worker-service]]` (background-worker service), `[[layered-pr-fanout-spec]]`

## §1 Problem

Today's PR-watch monitor (Claude Code `Monitor` tool, "PR watch v4 — filtered"
pattern) surfaces two event classes per in-flight PR:

1. CI check state transitions (per check)
2. Merge events (PR closed → merged)

It does **not** surface four other signal classes that drive orchestrator
decisions:

3. **Submitted reviews** — `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`
   states with body text
4. **File-line review comments** — per-line discussion threads on the diff
5. **Issue-level conversation comments** — top-level PR conversation
6. **PENDING reviews** — drafts visible only to the review author until
   "Submit review" is clicked in the GitHub UI; the body field is absent
   from the REST API while pending

The gap caused 2 missed maintainer reviews in a single session:

- `#143` (today) — review was `PENDING` when the orchestrator polled;
  CHANGES_REQUESTED body did not surface until the maintainer hit submit
- `#153` (earlier) — same failure mode

In both cases the maintainer had to re-mention the feedback in chat for the
orchestrator to act on it. This proposal formalizes the polling contract
that closes the gap, both for today's coach-driven monitor and for tomorrow's
productionized background-worker service (`da service` in detached/daemon mode).

## §2 API endpoints and dedup contract

Per in-flight PR, poll three endpoints every **30s** (configurable; see §8):

| Endpoint | Purpose |
| --- | --- |
| `gh api repos/<O>/<R>/pulls/<N>/reviews` | Review summaries: `state`, `body`, `submitted_at`, `user.login` |
| `gh api repos/<O>/<R>/pulls/<N>/comments` | File-line review comments: `path`, `line`, `body`, `in_reply_to_id` |
| `gh api repos/<O>/<R>/issues/<N>/comments` | Issue-level conversation comments |

**Dedup contract:**

- Maintain per-PR-per-endpoint cursors: `latest_review_id`,
  `latest_review_comment_id`, `latest_issue_comment_id`
- On each poll, emit only items with `id > cursor`; advance cursor to
  `max(id)` of the page
- **Reset all three cursors to 0 on any new HEAD SHA** for the PR (the
  worker pushed; previous-iteration review state is now stale)
- `review.pending` detection is independent: maintain a per-PR boolean
  `pending_signaled` that resets when the PR receives any new submitted
  review or when the draft is dismissed (see §3)

## §3 Filter rules

Only emit signal events. Drop noise per the following rules.

**Bot author drop-list** (extensible per project):

- `sonarqubecloud[bot]`
- `github-actions[bot]`
- Any `*[bot]` not on an allowlist (allowlist initially empty)

**Pending-review handling:**

- Skip `state == "PENDING"` for the normal review path — the body is
  invisible until submitted
- Emit **one-shot** `review.pending` per PR per pending-author when first
  detected ("📝 PR #N · maintainer has a draft review pending — body not
  yet visible") so the orchestrator can nudge if relevant
- Re-arm the one-shot only after the draft transitions to a submitted
  state (state becomes APPROVED / CHANGES_REQUESTED / COMMENTED) OR is
  dismissed (review disappears from the list)

**Trivial-acknowledgement drop-list** (case-insensitive, trimmed match
against the maintainer's own comment body):

- `lgtm`
- `merged`
- `accepted with option *` (where `*` is any single token)
- Single-emoji bodies (reactions converted to comments)
- Empty body

These are dropped only when authored by the PR-owner (self-ack). Other
authors' comments are surfaced regardless.

## §4 Event payload schemas

Four event types. All payloads are JSON, suitable for either inline
single-line rendering (§5) or structured ingestion (§7).

```json
// review.submitted
{
  "type": "review.submitted",
  "pr": 159,
  "author": "NikashPrakash",
  "state": "CHANGES_REQUESTED",
  "summary": "first 200 chars of body",
  "review_id": 12345,
  "submitted_at": "2026-05-28T17:30:00Z"
}
```

```json
// review_comment.posted
{
  "type": "review_comment.posted",
  "pr": 159,
  "author": "NikashPrakash",
  "path": "docs/web/src/pages/demos/[...slug].astro",
  "line": 17,
  "body": "first 200 chars",
  "in_reply_to_id": null,
  "comment_id": 54321,
  "created_at": "2026-05-28T17:31:00Z"
}
```

```json
// issue_comment.posted
{
  "type": "issue_comment.posted",
  "pr": 159,
  "author": "NikashPrakash",
  "body": "first 200 chars",
  "comment_id": 67890,
  "created_at": "2026-05-28T17:32:00Z"
}
```

```json
// review.pending  (one-shot per PR per pending-author until state changes)
{
  "type": "review.pending",
  "pr": 159,
  "author": "NikashPrakash",
  "detected_at": "2026-05-28T17:29:00Z"
}
```

**Truncation rule:** `summary` and `body` fields are truncated to the
first 200 characters with no ellipsis. Consumers needing the full body
re-fetch via the embedded `review_id` / `comment_id`.

## §5 Surfacing format to the orchestrator

Decision-class events render as one line per event:

```
📝 PR #<N> · <author> · <type-tag> · <state-or-path-or-summary>
```

Examples:

```
📝 PR #143 · NikashPrakash · review-CHANGES_REQUESTED · two usability items on resource graphs (see file-comments)
📝 PR #143 · NikashPrakash · file-comment · src/pages/graphs/da-resources.astro:42 — clicking the node...
📝 PR #143 · NikashPrakash · issue-comment · please rebase onto master before I take another look
📝 PR #153 · NikashPrakash · review-pending · draft body not visible until submit
```

`<type-tag>` values: `review-APPROVED`, `review-CHANGES_REQUESTED`,
`review-COMMENTED`, `review-pending`, `file-comment`, `issue-comment`.

## §6 Today's implementation: coach charter extension

The coach (Claude Code agent running the orchestration loop) gains a
charter extension: in addition to its existing CI/merge polling, run the
§2–§4 contract per in-flight PR.

The extension was applied inline this session via SendMessage to the
running coach (id `afda224c2597a7499`). For durability across coach
respawns, the polling responsibility must be added to the coach charter
at `.agents/active/coach/overnight-strategy.md` as a follow-up doc edit
(out of scope for this proposal).

## §7 Tomorrow's implementation: background-worker service

The productionized background-worker service (`da service` in detached mode,
per r3-background-worker-service spec) absorbs this polling as part of its
event-stream contract. The same §4 payload shapes flow into the service's
event ingester; the service emits to whatever orchestrator is currently
attached (coach, headless loop, dashboard).

**Co-design implication:** the service's event schema must include these
four event types as first-class citizens — not bolted on later. The
field shapes in §4 are the contract surface; the service may add fields
but must not rename or remove the existing ones.

## §8 Open questions

1. **Poll cadence.** 30s default. Should this be configurable per-project
   (`.agentsrc.json`) or per-PR (e.g. tighter cadence on `awaiting_owner_review`,
   slower on `draft`)?
2. **Inverse direction.** Should the orchestrator be able to POST replies
   back to GH via the same daemon? Out of scope here; track as a separate
   proposal.
3. **Webhook vs poll.** ~~The daemon could expose a webhook receiver
   instead of polling.~~ **RESOLVED 2026-05-28 (maintainer review #160
   line 44):** support **both** — webhook is the preferred push transport
   for new-event arrival; poll is the fallback / catch-up mechanism when
   webhook delivery is missed or the daemon hasn't been publicly reachable.
   The contract surface MUST tolerate either source emitting the same event
   shape (idempotency-keyed on `<pr_id, comment_id|review_id, sub_type>`).
4. **Spam-rate protection.** ~~Threshold and batching window TBD.~~
   **RESOLVED 2026-05-28 (maintainer review #160 line 44):** batching is
   **required**, not optional. Add `review_batch.posted` digest event with
   `count` + `path[]` + `comment_id[]`. Default window: **60s**
   (configurable per-project via `.agentsrc.json`). Linked-comments
   correlation: when a single review's comments target related files,
   group them into one digest entry with `linked_comments: true`.
5. **Promoted to spec.** Once the background-worker service spec finalizes, §4 should
   graduate to `workflow/specs/r3-background-worker-service/design.md` (or a sibling event-contract spec)
   as the canonical event-shape definition.
6. **NEW — pluggable event-contract surface.** Maintainer review #160 line
   147 surfaced cross-PR convergence: this proposal's event-contract pattern
   and #157's hook-sentinels-generic/custom path want the **same** generic-
   pluggable shape so dot-agents doesn't have to rework code each time a new
   event / sentinel / hook type is added. Tracked as sibling task
   `[[unified-pluggable-event-contract]]` (added to pr10-branch-split plan
   2026-05-28). This proposal's §4 event-shape registry SHOULD be one of
   the first consumers of that contract once landed (model:
   `verifier_profiles` registry — schema-additive, pluggable, no central
   code edits per new type). Cross-ref:
   `[[hook-schema-extension-mechanism]]` (#157 followup) — same underlying
   request, sentinel side.

Of the original four, **(3) webhook vs poll** and **(4) spam-rate batching**
are now resolved and folded above. **(6) pluggable event-contract** is the
new contract-affecting open question pending the sibling task's design.

## §9 Relationship to other proposals

- `[[r3-background-worker-service]]` (background-worker service / `da service`) — consumes this contract as part of
  its event-stream surface; §4 payload shapes are first-class
- `[[layered-pr-fanout-spec]]` §3.2 — `awaiting_owner_review` sub-status;
  `review.submitted` with `state == CHANGES_REQUESTED` transitions tasks
  back to `in_progress` per spec §6.2 (already-locked decision)
- `[[layered-pr-fanout-spec]]` §3.4 — `blocked-on:<ref>` state could be
  auto-set when a review explicitly blocks on something external (future
  enhancement, not in scope here)
