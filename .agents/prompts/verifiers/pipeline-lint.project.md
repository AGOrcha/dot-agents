# Pipeline-lint verifier — dot-agents repo overlay (meta)

Repo-local committed layer for the `meta` profile (self-improvement surface: agent/subagent defs,
pipeline/loop DSL, skills+tools, agentic-config). The base contract (`verifiers/verifier.base.md`)
and kind (`verifiers/pipeline-lint.md`) do not resolve in this repo, so this overlay is
**self-sufficient**. Role: validate that touched agent/subagent defs + pipeline/DSL + config changes
are schema-valid AND that a dry-run parse proves they do not wedge the loop (**loop-safety**), and
that the change is neither destructive nor a no-op gate.

## How to run — schema + resolution + loop-safety dry-run

1. **Config schema validity** (touched `.agentsrc.json` / config layers): `da config verify` and
   `da config lint` — 0 errors. A failure here is terminal.
2. **Profile/pipeline resolution (the DSL).** For each app_type touched:
   `da config relevance --filter topology --app-type <t> --json` and `--filter lenses` resolve. For
   each touched stage-profile slug: `da workflow resolve-prompt --kind verifier|reviewer --slug <slug>`
   composes with **no dangling/unreadable `prompt_file`**. A stage pointing at a missing prompt is fail.
3. **Agent/subagent + skill/tool defs parse:** `da agents list --json` parses every touched agent def
   (frontmatter/manifest valid); `da skills list` for touched skills. A def that won't parse is fail.
4. **Loop-safety dry-run (no-wedge):** `da workflow eligible --json` + `da workflow next` still resolve
   an actionable next step (not empty while tasks remain, not a cyclic block); `da workflow health`
   clean; `da workflow drift` clean. The loop MUST still be able to advance after the change.

## Assert (positive + negative)

- **Positive:** schema-valid; every touched app_type/slug resolves; every def parses; the loop still
  yields a next action.
- **Negative — destructive / no-op / wedge guard (REJECT any):** (a) a `verifier_sequence` or `lens_set`
  that HAD gates emptied — verification becomes a rubber stamp (no-op gate); (b) a required gate or
  closeout step removed; (c) a stage/profile pointing at a nonexistent prompt/agent (wedge); (d) a
  cyclic task-block or a gate with no terminating decision (loop wedge — the loop can never advance);
  (e) a hook/config that prunes or clobbers managed outputs (destructive). Prove discrimination: a
  known-bad edit (dangling `prompt_file`, or an emptied gate) is CAUGHT.

## Record + evidence

`da workflow verify record --kind test --status pass|fail --verifier-type pipeline-lint --summary "..."`.
On fail, name the first failing artifact + rule. Capture: `config verify`/`lint` output, the
`resolve-prompt` + `relevance` result per touched slug/app_type, and the `eligible`/`next` dry-run
showing the loop still advances. Any schema error, dangling ref, no-op gate, or wedge ⇒ `--status fail`.
