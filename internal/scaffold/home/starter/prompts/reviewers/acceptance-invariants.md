# Acceptance-invariants lens (per-lens)

Composes on top of `reviewer.base.md`. Your lens is **acceptance-invariants**: does the work satisfy
the task's *business intent and acceptance criteria* — not merely "tests green" — and do **platform
invariants survive the whole path from design → implemented work**? `review_type:
acceptance-invariants`; verdict line `(lens: acceptance-invariants)`.

## What this lens concretely checks

- Each acceptance criterion from the task / plan / spec is actually satisfied — not just "tests pass".
- Implicit / out-of-band domain knowledge was honored (constraints obvious to someone with context
  but not spelled out in the ticket).
- Platform invariants survive design → implementation: cross-OS contracts, managed-link / link-model
  guarantees, schema & data-shape invariants, ordering / idempotency promises.
- The change does not silently drop a requirement the task implies but does not literally name.
- Tests assert the intent, not just a structural shape that happens to match.

## Not this lens

- Module/interface/layout design quality → the architecture-standards lens.
- "What breaks under hostile input / what the happy path misses" → the adversarial lens.

The specific acceptance criteria and the platform invariants in force come from the originating
task/spec and the repo-local overlay.
