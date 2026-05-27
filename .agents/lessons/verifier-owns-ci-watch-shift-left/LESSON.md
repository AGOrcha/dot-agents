# pr-ci verifier profile owns CI watch — shift-left the readiness loop (generic starter default; per-project commands override)

## Pattern

`pr-ci` is a **generic verifier_profile shipped via the starter** — registered by default in every dot-agents-managed project. The existing global `verifier` agent (`~/.agents/agents/global/verifier/AGENT.md`) is dispatched per profile with the `pr-ci.project.md` prompt overlay. The impl subagent ships code + tests + PR and exits cleanly at merge-back. The `pr-ci` verifier polls the PR-CI surface, classifies surfaced issues, and either:

1. **Auto-fixes mechanically** (cog complexity → extract helper; dup literal → const; missing test → add focused test for branch coverage; stale-allowlist → prune entry), OR
2. **Returns a structured fix-up brief** to the impl subagent (architectural/security/spec issues; anything that requires intent the impl author owns)

The verifier loops until terminal `READY` or hard fold-back, then writes `.agents/active/verification/<task_id>/pr-ci.result.yaml`. Only then does the parent orchestrator see a single terminal signal.

**Why generic:** PR-CI watching is a universal concern. Every project ships a PR somewhere, runs some CI, has some SAST/coverage gate. The **classification + auto-fix template is identical across platforms**; only the **data-fetching commands** differ. Defaulting to the common-case stack (GitHub + SonarCloud) covers the majority directly, and projects on other platforms (Bitbucket, GitLab, CircleCI, CodeQL, Snyk, etc.) override with their platform-specific commands.

## Default substrate (GitHub + SonarCloud)

When a project doesn't override, `pr-ci` assumes:

- **CI fetch:** `gh pr checks <n>` (GitHub PRs)
- **SAST fetch:** SonarCloud quality-gate API at `/api/qualitygates/project_status?projectKey=<key>&pullRequest=<n>`
- **Coverage fetch:** SonarCloud's `new_coverage` metric + project-local `scripts/coverage-gate.sh` if present
- **Hotspot batch-review:** SonarCloud `change_security_hotspot_status` API (via the mcp__sonarqube tool or direct REST)
- **Merge conflict / mergeability:** `gh pr view <n> --json mergeable,mergeStateStatus`

These choices are encoded in the default `pr-ci.project.md` prompt the starter ships.

## Override mechanism (per-project, when defaults don't fit)

Projects on different stacks add a project-local override at `.agents/prompts/verifiers/pr-ci.project.md`. The `verifier_profiles` registry entry points at THAT file instead of the starter default. Examples:

| Project stack | Override authors |
|---|---|
| **GitHub + SonarCloud** (dot-agents itself + default starter) | NO override needed — this is the default substrate |
| GitHub + CodeQL (GitHub Advanced Security) | CI fetch via `gh pr checks`; SAST fetch via CodeQL CLI / GitHub Advanced Security API (CodeQL is GitHub-proprietary — pairs naturally with `gh`) |
| Bitbucket + 3rd-party SAST (Snyk, Veracode, Checkmarx, etc.) | CI fetch via `bb pr show <n>` (or REST API); SAST fetch via whichever scanner the project standardized on (Bitbucket has no first-party scanner) |
| GitLab + GitLab Code Quality | CI fetch via `glab mr view <n>`; SAST fetch via GitLab MR Code Quality report |
| CircleCI + Snyk | CI fetch via CircleCI API; SAST fetch via Snyk CLI |
| No SAST, just CI | drop the SAST section; keep `gh pr checks` (or platform equivalent) |
| No CI at all | template not applicable — disable `pr-ci` for the project |

The **classification table is identical** in all overrides — only the command snippets change. The override file declares its own substrate at the top (which commands to use for fetch X), and the classification + auto-fix logic stays verbatim from the default.

## Classes shifted left into `pr-ci` (universal)

- CI test failures (incl. transient infra reruns)
- SAST CRITICAL/HIGH issues (rule patterns vary by analyzer; the verifier prompt maps SonarQube rules by default; overrides map CodeQL/Snyk/etc.)
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

## Why generic-with-overrides (correcting earlier framing)

An earlier draft of this lesson placed `pr-ci` as dot-agents-dev-overlay-only (per `[[starter-vs-project-overlay]]`). That under-sold the universality of PR-CI watching as a concept. The corrected boundary:

- **Template universal → starter default** — the classification table, the auto-fix patterns, the escalation format are stack-agnostic
- **Data-fetching commands varying → per-project override** — `gh` vs `bb` vs `glab` vs CircleCI API; SonarQube rule keys vs CodeQL rule keys vs Snyk severity buckets
- **The starter ships the GitHub+SonarCloud default** because it's the dominant case (and what dot-agents itself uses)
- **Projects with other stacks** drop a `.agents/prompts/verifiers/pr-ci.project.md` override; `verifier_profiles.pr-ci.prompt_files` points there

This means the `[[starter-vs-project-overlay]]` lesson's boundary needs a nuance: starter holds the template; overlay supplies platform data. The two-tier model isn't "generic vs project" — it's "template vs commands". An update to that lesson is filed as a separate follow-up.

## Integration mechanism (the audit-grounded shape)

**The "agent" is the existing `~/.agents/agents/global/verifier/AGENT.md`** — NOT a new standalone subagent file. The audit (`.agents/proposals/pr-ci-verifier-integration-audit.md`) confirmed that creating a `pr-ci-verifier` AGENT.md would collide with the shipped generic `verifier` and miss the existing `verifier_profiles` registry.

What ships: **starter default + override path** — mirroring how `unit`/`api`/`batch`/`streaming`/`ui-e2e` are registered, but with the prompt LIVING IN STARTER instead of per-project:

1. **`.agentsrc.json` starter template — `verifier_profiles` entry:**
   ```json
   "pr-ci": {
     "label": "PR CI watch (default: gh + SonarCloud; project may override)",
     "prompt_files": ["@starter:prompts/verifiers/pr-ci.project.md"]
   }
   ```

   The `@starter:` prefix (or equivalent v2 source-aware reference per config-v2-migration spec §5) routes the prompt fetch through the starter source. Projects override by replacing the array with a project-local path:
   ```json
   "prompt_files": [".agents/prompts/verifiers/pr-ci.project.md"]
   ```

2. **`.agentsrc.json` starter template — `app_type_verifier_map` appended:**
   ```json
   "app_type_verifier_map": {
     "go-cli": ["unit", "pr-ci"],
     "go-http-service": ["unit", "api", "pr-ci"]
   }
   ```
   `pr-ci` lands in every app_type's verifier sequence by default; projects can opt-out by editing the map.

3. **Starter `internal/scaffold/home/starter/prompts/verifiers/pr-ci.project.md`** — NEW file shipped with starter. Default GitHub+SonarCloud substrate. Mirrors `unit.project.md` structure (role boundary, preconditions, commands, output schema). Includes the classification table verbatim.

4. **Project override (when needed):** `.agents/prompts/verifiers/pr-ci.project.md` in the consuming project. Replaces the substrate section (CI fetch / SAST fetch / coverage fetch / hotspot review API); keeps the classification + auto-fix logic verbatim.

5. **Artifact:** `.agents/active/verification/<task_id>/pr-ci.result.yaml`, validated against `schemas/verification-result.schema.json` with `verifier_type: pr-ci`.

6. **Invocation:** `da workflow verify record --kind custom --verifier-type pr-ci` writes the artifact. No CLI changes required.

For dot-agents-specific tasks that need only PR-CI watch (no unit re-run), workers can override with `--verifier-sequence pr-ci`.

## Rule (concrete watch loop the prompt file must encode)

The prompt file `prompts/verifiers/pr-ci.project.md` (starter default OR project override) carries:

1. **Substrate declaration** (the part that varies per project):
   - Which CLI to fetch CI state from (`gh`, `bb`, `glab`, custom)
   - Which API to fetch SAST state from (Sonar quality-gate, CodeQL, Snyk, custom)
   - Project-specific coverage gate command (default `scripts/coverage-gate.sh`)

2. **Inputs from the impl handoff** at `.agents/active/verification/<task_id>/impl-handoff.yaml`: `pr_number`, `impl_agent_id`, `feedback_channel` (path to write fix-up briefs).

3. **Watch loop:** poll the substrate's fetch commands every 60-90s until terminal state.

4. **On surfaced issue, classify and route (universal table):**

   | Issue class | pr-ci action |
   |---|---|
   | Cog complexity (S3776 / equivalent) | auto-fix: extract helpers (mirror `commands/review.go` / `internal/platform/hooks_test.go` factoring) |
   | Dup literal (S1192 / equivalent) | auto-fix: extract const; if tabular → data-driven refactor per `[[const-extraction-triggers-cpd-on-tables]]` |
   | Coverage <95% | auto-fix: add focused test for the uncovered branch (`go tool cover -func` or language-equivalent) |
   | Stale-allowlist | auto-fix: remove the entry from the project's allowlist file |
   | Security hotspot (pre-existing) | batch-review SAFE via SAST API with rationale |
   | Security vulnerability NEW | escalate to impl: write fix-up brief, exit |
   | CPD on tabular data | data-driven refactor per `[[const-extraction-triggers-cpd-on-tables]]` |
   | Test failure (deterministic) | rerun once; if still failing → escalate |
   | Test failure (architectural intent gap) | escalate to impl |
   | Modify/rename merge conflict | escalate to orchestrator |

5. **Terminal output**: write `.agents/active/verification/<task_id>/pr-ci.result.yaml` with `status: pass | fail | partial`, the readiness summary line, and the list of auto-fix commits or escalation briefs produced.

6. **Closeout**: parent runs `da workflow delegation closeout --decision accept` which auto-advances task status (per `[[verify-task-status-vs-pr-history]]`).

**Lens reviewers (architecture-standards / acceptance-invariants / adversarial) are orthogonal** and pre-merge (one spawn per lens). They audit the merged-back work. The `pr-ci` profile is post-merge-back, CI-state-only. Lens agent definitions ship via starter (per PR #122).

## How to apply (sequenced work)

1. **Land codex PR-B + relevant config-v2 phases** (p0-schema, p1-resolver, p4-config-explain). These land the source-aware `prompt_files` resolution that lets `@starter:prompts/verifiers/pr-ci.project.md` resolve correctly + the override path stay clean.
2. **Author `internal/scaffold/home/starter/prompts/verifiers/pr-ci.project.md`** — the GitHub+SonarCloud default. Mirror `unit.project.md` structure. Cap at ~200 lines (longer than per-project verifier prompts because it carries both the substrate AND the classification template). Include the classification table verbatim from above.
3. **Edit the starter's `.agentsrc.json` template** — add the `pr-ci` entry to `verifier_profiles`, append `"pr-ci"` to `app_type_verifier_map.<each-app-type>`. Validate against `schemas/agentsrc.schema.json`.
4. **Document override path** in starter README + a new `prompts/verifiers/README.md` that explains how to author a project-local override for non-GitHub or non-SonarCloud stacks.
5. **Update orchestrator session-start skill** in starter (`skills/global/iteration-close/`) to reference `pr-ci` as the post-merge-back verifier expected by default.
6. **Trim `[[worker-owns-pr-readiness-loop]]`** — done via PR #125 (lesson now correctly framed as fallback when no verifier profile registered).
7. **Follow-up: update `[[starter-vs-project-overlay]]` lesson** to capture the "template universal / commands per-project" nuance.

## Cross-references

- `[[worker-owns-pr-readiness-loop]]` — prior pattern, now superseded for any project with a `pr-ci` registration; still valid as fallback
- `[[starter-vs-project-overlay]]` — needs an update: the boundary is "template (generic) vs commands (per-project)", not just "starter vs overlay". Filed as follow-up.
- `[[loop-worker-vs-general-purpose]]` — the existing global `verifier` agent is dispatched per profile; no new subagent type
- `[[const-extraction-triggers-cpd-on-tables]]` — `pr-ci` S1192 auto-fix must check for tabular layout first
- `[[no-lazy-allowlist-tech-debt]]` — `pr-ci` auto-prunes STALE-ALLOWLIST but never ADDS allowlist entries
- `[[verify-task-status-vs-pr-history]]` — `pr-ci` exits with merge-back; parent's `delegation closeout` handles the auto-advance
- `.agents/proposals/pr-ci-verifier-integration-audit.md` — full audit ground-truthing this lesson's claims (note: audit predates the generic-default framing; substrate is correct, scope is now broader)
