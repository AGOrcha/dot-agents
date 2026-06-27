# Shipped content must be self-contained (no dangling contributor refs)

## Pattern

Scaffolded skills and agents under `internal/scaffold/home/starter/` ship to every
`da` end-user. Those users install the *product* — they do NOT get the dot-agents repo.
So any reference in shipped content to a **contributor-only design artifact** dangles for
them: the path resolves to nothing in their installed environment.

The contributor-only artifact classes that dangle when shipped:

- `.agents/proposals/<id>.md` / `<id>.yaml` referenced as a *specific named file*
  (e.g. `.agents/proposals/scientific-method-spine-domain-general.md`)
- `.agents/workflow/specs/<named-id>/design.md` referenced as a *specific contributor spec*
  (e.g. `.agents/workflow/specs/agent-ops-hardening/design.md`,
  `.agents/workflow/specs/ideation-system-composition/design.md`)
- `.agents/lessons/<name>/` or a bare lesson name in backticks
  (e.g. `` `prototype-experiment-fidelity-gate` ``, `` `tests-must-drive-the-production-path` ``)
- evidence sidecars (`evidence/<...>.md`, `evidence/depth-degradation-dogfood.md`) and
  section citations into them (`sidecar v3 family-2`, `§v4/§v4.9`)

These are *provenance* — they tell a maintainer where a claim came from. The shipped
instruction already states the method/finding; the pointer adds nothing for the user and
breaks for them.

## What is NOT a dangling ref (do not scrub these)

User-runtime paths and product surface are legitimate in shipped content because they
exist in the user's own `da`-managed project / home:

- `~/.agents/proposals/<id>.yaml`, `.agents/proposals/<id>.md` as a *general placeholder*
  for the user's own proposal/review loop (`da review`, `da workflow fanout`)
- `.agents/lessons/<name>/LESSON.md`, `.agents/workflow/specs/<id>/design.md`,
  `.agents/active/experiments/`, `.agents/history/<id>/` as `<placeholder>` paths the user
  writes/reads
- `da ...` CLI commands; co-shipped files in the same skill (e.g. `instructions/fidelity-gate.md`)

The line: a *specific named* contributor artifact dangles; a `<placeholder>` user-runtime
path or a `da` command does not.

## Root cause

A skill is usually authored *inside* the dot-agents repo, where the proposal, spec, lesson,
and evidence files all exist — so the cross-reference resolves for the author and the
dangle is invisible. It only breaks once the skill is installed standalone.

## Rule

**Shipped content is self-contained or references only co-shipped files, `da` commands, or
`<placeholder>` user-runtime paths.** When a shipped instruction needs provenance:

1. If the instruction stands alone, **remove** the pointer (pure provenance).
2. If a pointer is genuinely useful, **reshape** it to a co-shipped sibling file in the same
   skill (`see fidelity-gate.md`) or a `da` command — never a repo-internal design artifact.
3. Keep the method content (the finding, the number, the checklist). Drop only the dangling
   external pointer — e.g. keep "a lossy hand-back degraded the artifact 16→13", drop
   "(evidence sidecar v3 family-2)".

## How to apply (pre-ship grep)

Before shipping or editing a scaffolded skill/agent, grep for repo-internal refs:

```
grep -rnE '\.agents/proposals/|\.agents/workflow/specs/[a-z]|\.agents/lessons/[a-z]|evidence/[a-z]|<specific-lesson-name>' \
  internal/scaffold/home/starter/skills/ internal/scaffold/home/starter/agents/
```

Triage each hit: a *named* contributor artifact → fix; a `<placeholder>` user path or `da`
command → leave.

## Audit findings (file as separate follow-ups, own branch/PR)

- `skills/global/delegation-lifecycle/instructions/workflow.md:7` and `:112` — both cite
  `.agents/workflow/specs/agent-ops-hardening/design.md` (§3.5 / §3.10), a contributor-only
  spec. On master; needs its own fix.

## Cross-references

- `[[starter-vs-project-overlay]]` — sibling: generic starter ships how-to-USE `da`, the dev
  overlay holds how-to-DEVELOP dot-agents. This lesson is the *reference-integrity* corollary:
  even legitimately-generic starter content must not point at repo-internal design artifacts.
- `[[symbol-only-references-in-artifacts]]` — same family: artifacts reference by durable
  anchor, not a pointer that breaks out of context (line numbers there; ship-context here).
