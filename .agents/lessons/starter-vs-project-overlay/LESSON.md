# Starter (generic product surface) vs project dev overlay

## Pattern

The starter at `internal/scaffold/home/starter/` is shipped to every consumer of dot-agents. The dot-agents-dev overlay (under `.agents/rules/dot-agents/` in this repo) layers ON TOP of the generic starter for consumers who are *also developing dot-agents itself*.

The split is sharper than "generic vs project-specific":

**Generic starter (ships to everyone who uses `da`):**
- `da workflow advance / merge-back / fanout / tasks / orient` — the product's CLI surface
- Bundle / write_scope / fold-back / iteration-log conventions — the product's data model
- `.agents/active/` paths — the product's runtime artifact layout
- Bounded-loop-worker discipline — the product's subagent contract
- Skill chains, profile loading conventions — the product's runtime
- Anything documented in the spec → these are the consumer-facing contract

**dot-agents dev overlay (only for someone developing dot-agents itself):**
- `gh pr create / checks / view` — GitHub workflow specific to where dot-agents itself is hosted
- `go run ./cmd/dot-agents ...` — local-dev shortcut (production consumers use installed `da`)
- `/tmp/dot-agents-dev` — local build binary path used by dev sessions
- SonarCloud gate automation, `coverage-exceptions.txt` lookups — dot-agents' own CI gate machinery
- References to dot-agents-internal PR numbers, master-hygiene patterns
- Specific repo paths like `internal/scaffold/home/starter/`, `commands/internal/`

## Root cause

Other projects (hypothetical) consuming dot-agents would write their own dev overlay against the same generic starter. Mixing the two means every consumer drags around references to dot-agents' GitHub repo, CI setup, and local-dev paths that are noise to them.

## Rule

When authoring new starter content OR new lessons/skills:
1. Write the generic version first — substitute `da` for `go run ./cmd/dot-agents`, drop `gh pr` references, drop SonarCloud automation specifics.
2. If a piece of advice is ONLY useful when developing dot-agents itself (e.g. "dot-agents won't build, use go run ./cmd/dot-agents instead"), put it in the dev overlay — not the starter.
3. When updating an existing starter file, audit any `gh pr`, `/tmp/dot-agents-dev`, `go run ./cmd/dot-agents`, or `coverage-exceptions.txt` mentions. If they're only meaningful to dot-agents devs, extract them to the overlay.
4. Lessons in `.agents/lessons/` are repo-local but can still encode generic patterns — note in the lesson which audience it serves.

## Audit findings (file as cleanup)

- `iteration-close/instructions/gotchas.md:5-6` — `/tmp/dot-agents-dev`, `go run ./cmd/dot-agents` advice
- `review-delta/SKILL.md:14` — "In the dot-agents repo, use go run ./cmd/dot-agents"
- `loop-worker/instructions/gotchas.md:22` — `dot-agents won't build` recovery (dev-only scenario)
- `loop-worker/instructions/startup.md:22`, `agents/loop-worker/AGENT.md:22` — `go run ./cmd/dot-agents workflow tasks` (should be `da workflow tasks` in starter)

## Cross-references

- `[[loop-worker-vs-general-purpose]]` — the bounded-loop-worker contract is GENERIC starter content; spawning subagents from a dot-agents dev session is dev-overlay specifics
- `[[init-bypasses-hooks-package]]` — concerns dot-agents internal architecture, dev-overlay territory
