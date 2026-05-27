# pr-ci verifier profile owns CI watch — shift-left the readiness loop (dot-agents project overlay)

## Pattern

In the **dot-agents project overlay** specifically (dev-of-dot-agents flow), a dedicated **`pr-ci` verifier profile** — registered alongside `unit`/`api`/`batch`/etc. in `.agentsrc.json` — owns the PR-CI-watch loop. The impl subagent ships code + tests + PR and exits cleanly at merge-back. The `pr-ci` verifier (the existing global `verifier` agent, dispatched with the `pr-ci.project.md` prompt overlay) polls `gh pr checks` + SonarCloud quality-gate API, classifies surfaced issues, and either:

1. **Auto-fixes mechanically** (cog complexity → extract helper; dup literal → const; missing test → add focused test for branch coverage; stale-allowlist → prune entry), OR
2. **Returns a structured fix-up brief** to the impl subagent (architectural/security/spec issues; anything that requires intent the impl author owns)

The verifier loops until terminal `READY` or hard fold-back, then writes `.agents/active/verification/<task_id>/pr-ci.result.yaml`. Only then does the parent orchestrator see a single terminal signal.

Classes shifted left into `pr-ci`:

- CI test failures (incl. transient infra reruns)
- SonarQube CRITICAL/HIGH issues (S3776 cog, S1192 dup, S131 case-default, S7679 positional-params)
- Security hotspots needing batch-review
- Duplicated-lines-density (file-level + project-level)
- Coverage gate (per-file + per-package threshold)
- Stale-allowlist hygiene

## Root cause

`[[worker-owns-pr-readiness-loop]]` (the prior pattern) bundles two concerns into one impl subagent: implementation craft AND CI/Sonar orchestration. Impl subagents are good at the first and average at the second — each impl session burns context re-learning the SonarCloud API, the `gh pr checks` schema, the coverage-gate.sh exit codes. With ~5-10 PRs per wave and ~3-7 fix-up cycles per PR, that's 50-100x the same context being re-paid by different impl subagents.

A specialized verifier profile:
- Has the API shapes encoded in its prompt
- Doesn't reload them per fix
- Knows the project's CI workflow (`.github/workflows/test.yml`) intimately
- Can pattern-match on cog/dup/cov classes that the same impl agent might mis-classify
- Owns the cross-PR memory ("I've seen this Dockerfile S8482 pattern before — use NodeSource apt-repository, not curl-pipe-bash with checksum")

This is **dot-agents project overlay** material — NOT generic starter (per `[[starter-vs-project-overlay]]`). Other consumers of `da` will have different CI surfaces (GitLab CI, Jenkins, no CI at all) and different gate tools (no Sonar, custom linters). Each project owns its own `verifier_profiles` + `app_type_verifier_map`.

## Integration mechanism (the audit-grounded shape)

**The "agent" is the existing `~/.agents/agents/global/verifier/AGENT.md`** — NOT a new standalone subagent file. The audit (`.agents/proposals/pr-ci-verifier-integration-audit.md`) confirmed that creating a `pr-ci-verifier` AGENT.md would collide with the shipped generic `verifier` and miss the existing `verifier_profiles` registry.

What changes is **config + a prompt file** — mirroring how `unit`/`api`/`batch`/`streaming`/`ui-e2e` are already wired:

1. **`.agentsrc.json` — `verifier_profiles` entry:**
   ```json
   "pr-ci": {
     "label": "PR CI watch (gh + SonarCloud)",
     "prompt_files": [".agents/prompts/verifiers/pr-ci.project.md"]
   }
   ```

2. **`.agentsrc.json` — `app_type_verifier_map` appended:**
   ```json
   "app_type_verifier_map": {
     "go-cli": ["unit", "pr-ci"]
   }
   ```

3. **`.agents/prompts/verifiers/pr-ci.project.md`** — the prompt overlay carrying the watch loop, classification table, auto-fix patterns, escalation format. Mirrors the structure of `unit.project.md` (role boundary, preconditions, commands, output schema). NEW file; project-overlay-local; does NOT ship via generic starter.

4. **Artifact:** `.agents/active/verification/<task_id>/pr-ci.result.yaml`, validated against `schemas/verification-result.schema.json` with `verifier_type: pr-ci`.

5. **Invocation:** `da workflow verify record --kind custom --verifier-type pr-ci` writes the artifact. No CLI changes required.

The `pr-ci` profile is added to `go-cli`'s verifier sequence so `da workflow fanout` automatically resolves it after `unit`. For dot-agents-specific tasks that need only PR-CI watch (no unit re-run), workers can override with `--verifier-sequence pr-ci`.

## Rule (concrete watch loop the prompt file must encode)

The prompt file `.agents/prompts/verifiers/pr-ci.project.md` carries:

1. **Inputs from the impl handoff** at `.agents/active/verification/<task_id>/impl-handoff.yaml`: `pr_number`, `impl_agent_id`, `feedback_channel` (path to write fix-up briefs).

2. **Watch loop:**
   - Poll `gh pr checks <pr>` every 60-90s
   - Poll `https://sonarcloud.io/api/qualitygates/project_status?projectKey=NikashPrakash_dot-agents&pullRequest=<pr>` every 60-90s
   - Detect terminal state (all green, READY) OR issue surfaced

3. **On surfaced issue, classify and route:**

   | Issue class | pr-ci action |
   |---|---|
   | S3776 cog complexity | auto-fix: extract helpers (mirror `commands/review.go` / `internal/platform/hooks_test.go` factoring) |
   | S1192 dup literal | auto-fix: extract const; if tabular → data-driven refactor per `[[const-extraction-triggers-cpd-on-tables]]` |
   | Coverage <95% | auto-fix: add focused test for the uncovered branch (`go tool cover -func`) |
   | Stale-allowlist | auto-fix: remove the entry from `scripts/coverage-exceptions.txt` |
   | Security hotspot (pre-existing) | batch-review SAFE via `mcp__sonarqube__change_security_hotspot_status` with rationale |
   | Security vulnerability NEW | escalate to impl: write fix-up brief, exit |
   | CPD on tabular data | data-driven refactor per `[[const-extraction-triggers-cpd-on-tables]]` |
   | Test failure (deterministic) | rerun once; if still failing → escalate |
   | Test failure (architectural intent gap) | escalate to impl |
   | Modify/rename merge conflict | escalate to orchestrator |

4. **Terminal output**: write `.agents/active/verification/<task_id>/pr-ci.result.yaml` with `status: pass | fail | partial`, the readiness summary line (`N/N checks · gate OK · new_cov X% · dup Y% · hotspots Z% · 0 issues`), and the list of auto-fix commits or escalation briefs produced.

5. **Closeout**: parent runs `da workflow delegation closeout --decision accept` which auto-advances task status (per `[[verify-task-status-vs-pr-history]]`).

**Lens reviewers (architecture-standards / acceptance-invariants / adversarial) are orthogonal** and pre-merge (one spawn per lens). They audit the merged-back work. The `pr-ci` profile is post-merge-back, CI-state-only. See PR #122 for the lens agent definitions (`internal/scaffold/home/starter/agents/global/<lens>-reviewer/AGENT.md` — forward-looking; lands when that PR merges).

## How to apply (sequenced work)

1. **Land codex PR-B + PR-C** (direct contract auto-materialize + snapshot API + `da config explain`). These land structured artifacts the `pr-ci` prompt consumes for clean handoff (avoids ad-hoc gh+Sonar polling for what should be a typed input).
2. **Author `.agents/prompts/verifiers/pr-ci.project.md`** — mirror the `unit.project.md` structure. Cap at ~150 lines. Include the classification table verbatim from above.
3. **Edit `.agentsrc.json`** — add the `pr-ci` entry to `verifier_profiles`, append `"pr-ci"` to `app_type_verifier_map.go-cli`. Validate against `schemas/agentsrc.schema.json`.
4. **Edit `schemas/agentsrc.schema.json`** — if the existing `verifier_profiles.<name>` schema doesn't cover a `prompt_files` array of `.project.md` paths, extend it (audit found the field already exists).
5. **Update orchestrator skill briefings** in `~/.agents/skills/dot-agents/iteration-close/` to reference `pr-ci` as the post-merge-back verifier for `app_type: go-cli`.
6. **Trim `[[worker-owns-pr-readiness-loop]]`** to clarify: that pattern was the bridging state. With staged dispatch + the `pr-ci` profile, impl exits at merge-back; the verifier profile owns the loop downstream. The lesson stays as historical context AND as the fallback for environments without a `pr-ci` profile.

## Cross-references

- `[[worker-owns-pr-readiness-loop]]` — prior pattern, now superseded for dot-agents-dev; still valid for consumers without a verifier overlay
- `[[starter-vs-project-overlay]]` — `pr-ci.project.md` belongs in the dot-agents dev overlay, NOT generic starter
- `[[loop-worker-vs-general-purpose]]` — the existing global `verifier` agent is dispatched per profile; no new subagent type
- `[[const-extraction-triggers-cpd-on-tables]]` — `pr-ci` S1192 auto-fix must check for tabular layout first
- `[[no-lazy-allowlist-tech-debt]]` — `pr-ci` auto-prunes STALE-ALLOWLIST but never ADDS allowlist entries
- `[[verify-task-status-vs-pr-history]]` — `pr-ci` exits with merge-back; parent's `delegation closeout` handles the auto-advance
- `.agents/proposals/pr-ci-verifier-integration-audit.md` — full audit ground-truthing this lesson's claims
