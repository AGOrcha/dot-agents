---
name: "onboard"
description: "Get a brand-new da (dot-agents) user set up with minimal thinking. Detects the right setup path — adopt a shared/team home config, install an existing repo manifest, or scaffold fresh — then links the active editor and runs a health check. Use the first time you run da on a machine, when a repo has a .agentsrc.json you haven't installed yet, or when someone gives you a shared da config URL. Supports paths: from-home, from-manifest, fresh."
argument-hint: "[from-home <git-url> | from-manifest | fresh | <project-path>]"
---

# Onboard a new da user

Get someone from "just installed `da`" to "editor linked, project bound, health
check green" with as few decisions as possible. There are exactly three setup
paths; this skill detects which one applies, runs it, then always verifies.

> Assumes the `da` binary is installed (`brew tap AGOrcha/tap && brew install da`,
> or your platform's equivalent). If `da --version` fails, install it first.

## Workflow

1. **Detect** — Read `instructions/detect.md` and run the detection probes. They
   resolve to one of three paths. If the signals are ambiguous, ask the single
   disambiguating question that instruction file specifies — do not guess.

2. **Run the matching path** — dispatch to exactly one:
   - **from-home** → `instructions/from-home.md`
     The user has a shared/team home config published as a git URL (a teammate's
     or another machine's `~/.agents`). Adopt it.
   - **from-manifest** → `instructions/from-manifest.md`
     This repo already commits a `.agentsrc.json` with real config sources.
     Install it.
   - **fresh** → `instructions/fresh.md`
     Empty home, no manifest. Scaffold a new `~/.agents` and bind the project.

3. **Verify** — Always finish with `instructions/verify.md`: install/confirm your
   editor, re-link with `da refresh`, then `da status --audit` and `da doctor`.

## Decision tree (quick reference)

| Signal | Path |
|---|---|
| You have a shared/team `da` config git URL | from-home |
| This repo already has a committed `.agentsrc.json` with non-local sources | from-manifest |
| Empty `~/.agents`, no repo manifest | fresh |

## Notes

- Run path steps in order; each later command depends on the earlier one.
- Never invent flags. Every command in the instruction files is real — if you
  need options, run `da <command> --help`.
- The editor/harness link step (`da refresh`) is what writes Claude Code, Cursor,
  Codex, Copilot, and OpenCode config into your projects. Install your editor
  first, then refresh so `da` auto-enables it.
