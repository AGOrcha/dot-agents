# Path: from-manifest (install an existing repo manifest)

Use when the repo already commits a `.agentsrc.json` that declares real config
**sources** / **extends** layers. The manifest is the contract; you just need to
materialize it on this machine.

## Preconditions

- `~/.agents/config.json` must exist (the home must be initialized). If it is
  missing, run `da init` once first — it scaffolds the home without touching the
  repo's manifest:
  ```bash
  ls ~/.agents/config.json || da init
  ```

## Steps

1. **Install the manifest** — resolve declared sources, materialize skills/agents
   into `~/.agents`, and link every installed platform (rules, hooks, MCP,
   settings):
   ```bash
   da install
   ```
   Run this from the repo root (it reads `.agentsrc.json` in the current
   directory). Add `--strict` if you want it to fail when any declared resource
   is missing rather than warn.

2. **Sync layered config** (only if the manifest has `extends` / git layers) —
   re-fetch the layers and rewrite `.agentsrc.lock` so this machine resolves the
   exact same effective config as everyone else:
   ```bash
   da config sync
   ```
   Skip this for a `sources: [{ type: local }]`-only manifest with no `extends`.

## Then

Continue to `instructions/verify.md` (install editor → `da refresh` → audit →
`da doctor`).
