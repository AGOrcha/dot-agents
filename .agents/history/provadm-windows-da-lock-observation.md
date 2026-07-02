# ProvAdm observation - Windows da lock and workflow mutator failures

Date: 2026-06-30
Reporter: GitHub Copilot session in ProvAdm workspace

## Purpose

This note is intentionally limited to tool-behavior observation for dot-agents. Product implementation artifacts for the credentialing work belong in the ProvAdm repos, not here.

## Observed commands

- `da config explain --all --json` in:
  - `c:\Users\nprakash1\Documents\ProvAdm\prov-provider-admin-ui`
  - `c:\Users\nprakash1\Documents\ProvAdm\provider-admin-automation`
- `da workflow plan create provadm-credentialing-ui-hardening ...` in:
  - `c:\Users\nprakash1\Documents\Pers\dot-agents`
- `da workflow task add provadm-credentialing-ui-hardening ...` in:
  - `c:\Users\nprakash1\Documents\Pers\dot-agents`

## Observed failure pattern

- Config explain failed before meaningful manifest reading with an agentslock path-creation problem involving `.agentsrc.lock.lock`.
- Workflow mutators hit Windows file-write failures on workflow artifacts with `Access is denied` symptoms.
- The session evidence consistently pointed at lock-path or write-path handling rather than malformed `.agentsrc.json`, `PLAN.yaml`, or `TASKS.yaml` content.

## Representative failure details captured in the session

- Repo memory note captured during the session:
  - `da config explain --all --json` in `prov-provider-admin-ui` and `provider-admin-automation`, plus some `da workflow` mutators in `dot-agents`, can fail before reading content with agentslock path creation errors like `.agentsrc.lock.lock` / plan-file `Access is denied`.
- Session summary recorded the same Windows symptoms as the controlling issue:
  - `.agentsrc.lock.lock` path creation failure in both product repos during `da config explain --all --json`
  - `da workflow plan create ...` failing with mkdir/access issues
  - `da workflow task add ...` failing with `Access is denied` on `TASKS.yaml`

## Why this matters

- These failures prevented native `da` management of the workflow artifacts during the session.
- The product repos could still be edited directly, but the dot-agents workflow/verification loop was degraded on this Windows workstation.
- The observed behavior matches the broader Windows lock-path risk already documented in:
  - `c:\Users\nprakash1\Documents\Pers\dot-agents\.agents\history\rca-windows-agentslock-escape.md`
  - `c:\Users\nprakash1\Documents\Pers\dot-agents\.agents\lessons\live-smoke-must-run-on-every-target-os\LESSON.md`

## Suggested follow-up for dot-agents

- Reproduce `da config explain --all --json` on this workstation in both ProvAdm repos with lock tracing enabled.
- Inspect the parent-directory creation path for `.agentsrc.lock.lock` before manifest reads.
- Reproduce the workflow mutator writes against `PLAN.yaml` and `TASKS.yaml` on Windows to determine whether the failure is lock acquisition, path normalization, or file-handle reuse.