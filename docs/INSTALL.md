# Install & onboard `da` — one paste

New to dot-agents? You don't have to read the whole site first. Paste the block
below into your AI coding agent (Claude Code, Cursor, Codex, Copilot, OpenCode,
Antigravity, …). It installs `da`, runs the guided **onboarding**, and tailors a
setup to *your* project — asking you questions instead of making you read docs.

## The one paste

```text
Set me up with dot-agents (da). Do this in order, and ask before any destructive change:

1. Install the binary: run `brew tap AGOrcha/tap && brew install da` (or, if Homebrew
   isn't available, the Go / install-script method from
   https://github.com/AGOrcha/dot-agents#installation). Confirm with `da --version`.

2. Run the dot-agents `onboard` skill (skill://onboard). Detect my setup path
   — from-home (a shared/team ~/.agents git URL), from-manifest (this repo already
   commits a real .agentsrc.json), or fresh (empty home) — run exactly that path,
   link my active editor with `da refresh`, then verify with `da status --audit`
   and `da doctor`. If the path is ambiguous, ask me the single disambiguating question.

3. Analyze this project and ask me: what am I building, my stack / app_type, my
   editor/harness, and how much automation I want. Then tailor:
   - use the `pipeline-architect` skill to design my execution_profile
     (app_type → verifier/reviewer sequence + topology + review lenses),
   - use the `skill-architect` skill for any repeatable workflow I describe,
   - create bounded subagents for my recurring delegated tasks.

4. Teach me the da primitives I'll actually use, grounded in what you just set up:
   `da workflow orient | checkpoint | verify | advance`, `da workflow fanout` +
   merge-back for bounded delegation, `da review` for proposals, and `da kg` for
   project memory. For each, tell me WHEN to reach for it in my loop.

Keep it to the minimum decisions. Prefer the conservative/standard option and tell me
what you chose.
```

## What it does

| Step | What happens | Backed by |
|---|---|---|
| 1. Install | Gets the `da` binary onto your machine | Homebrew / Go / script |
| 2. Onboard | Detects your path, scaffolds `~/.agents`, links your editor, health-checks | [`onboard`](https://github.com/AGOrcha/dot-agents/tree/master/.agents/skills) skill (`from-home` / `from-manifest` / `fresh` → verify) |
| 3. Tailor | Designs your execution profile, skills, and subagents around *your* stack | `pipeline-architect`, `skill-architect` skills |
| 4. Teach | Shows the primitives + when to use each, in context | `da workflow` / `da review` / `da kg` |

The tailoring step is where dot-agents stops being generic: `pipeline-architect`
maps your `app_type` (e.g. `go-cli`, `web-spa`, `cloudflare-worker`) to the right
verifier/reviewer sequence and fan-out topology, so your agents run the *right*
checks for *your* code — not a one-size-fits-all pipeline.

## Prefer to do it by hand?

```sh
brew tap AGOrcha/tap && brew install da   # or: go install …  (see the README)
da --version
da onboard                                # same guided flow, run directly
da status --audit && da doctor            # verify
```

Then read the [AI development lifecycle](/concepts/ai-dev-lifecycle) to see where
each primitive fits in the end-to-end loop.

## Next

- [AI development lifecycle](/concepts/ai-dev-lifecycle) — the end-to-end loop, with drill-downs.
- [Getting Started](../README.md) — the fuller tour.
- [Layered configuration](LAYERED_CONFIG_GUIDE.md) — how `extends` + the lockfile work.
- [Observability dashboard](OBSERVABILITY_DASHBOARD.md) — live iteration/score telemetry.
