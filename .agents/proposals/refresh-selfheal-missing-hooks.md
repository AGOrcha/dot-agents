# Follow-up: da refresh should self-heal a missing ~/.agents/hooks/global/

**Surfaced:** 2026-05-29 by the dogfood-starter-hooks work (PR #187).

## The gap
The starter hooks are fully shipped + installable (HOOK.yaml bundles in
`internal/scaffold/hooks/global/`, gate scripts, `da workflow hook-sentinel`).
The supported install path is `da init` (seeds `~/.agents/hooks/global/`) +
`da refresh <project>` (renders into the project's settings). BUT on a machine
where `~/.agents/hooks/global/` was never seeded (e.g. set up before hooks
landed, or `da init` not re-run), **`da refresh` silently links no hooks** —
no error, no hint. Every project then runs without the orchestration hooks and
nobody knows.

## Fix options
1. **`da refresh` self-heals:** if `~/.agents/hooks/global/` is absent/empty,
   re-seed it from the embedded scaffold (`internal/scaffold/hooks/global/`)
   before relinking — same way `da init` would. Emit a one-line notice.
2. **`da doctor` flags it:** add a diagnostic — "hooks not seeded in
   ~/.agents/hooks/global/; run `da init` or `da refresh --seed-hooks`."
3. **Document** the `da init` seeding step in the refresh/hooks help + README.

Recommend (1) + (2): self-heal on refresh (idempotent, low-risk — it's
re-rendering managed scaffold content) AND a doctor check so the state is
visible. Gate the self-heal behind the same "managed content" guarantees as
the rest of refresh (don't clobber user-edited hook bundles).

## Cross-references
- PR #187 (dogfood enablement that found this)
- the hooks package + `da refresh` (commands) — where the self-heal lands
- `[[init-bypasses-hooks-package]]` lesson (related: init/settings hook wiring)

## Maintainer ruling 2026-05-29 (PR #187 comment): broaden beyond hooks

Not just `~/.agents/hooks/global/` — `da refresh` (and a `da doctor` check) should
self-heal **any missing managed starter content as a whole**: hooks, skills, agents,
rules, settings, MCP config. The same silent-dormancy failure applies to every
managed-resource family — if `~/.agents/<family>/` was never seeded (set up before a
family landed, or `da init` not re-run), `da refresh` links nothing for it and emits
no signal. Generalize the fix: refresh detects ANY absent/empty managed family under
`~/.agents/` and re-seeds it from the embedded scaffold (idempotent, managed-content
only — never clobber user-edited bundles), with a one-line notice; `da doctor` reports
any unseeded family. Title/scope updated accordingly: this is "refresh self-heals
missing starter content", hooks being the instance that surfaced it.
