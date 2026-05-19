# Plan: Legacy Shell Prune and `src/share` Rehome

**Status:** active  
**Spec:** [design.md](../../specs/legacy-shell-prune-share-rehome/design.md)

## Why this plan exists

The repo still carries a partial shell-era payload under `src/lib/` and `src/share/`, but the supported command path is Go-first. The remaining shell files are not the primary product surface anymore; the main reason they still exist is that `scripts/install.sh` still packages them.

Discovery findings from this session:

- `src/lib/**` is not used by the current Go CLI.
- `src/bin/dot-agents` is now a source-checkout launcher, not the old installable shell entrypoint.
- `scripts/install.sh` still assumes the older packaging model and still tries to rewrite launcher variables that the current `src/bin/dot-agents` no longer has.
- `src/share/templates/standard/` mixes bootstrap files, authoring templates, and legacy installer payloads in one tree.

That means this is not a bash-parity project. It is a product-surface cleanup project with one real decision gate: what install/bootstrap paths are still supported.

## Execution order

### P0: Decide the supported installer and bootstrap contract

Do not start deletion work until the repo decides:

1. whether curl install remains supported
2. whether `dot-agents init` should seed shipped starter skills/agents or only minimal canonical directories/files

Current decision:

- curl install remains supported through a replacement `scripts/install.sh`
- `dot-agents init` seeds the curated starter bundle:
  - create canonical directories
  - generate starter home files
  - scaffold embedded runtime hook bundles
  - seed starter global skills and starter agents from shipped runtime assets where provided
- `skills new` and `agents new` stay responsible for authoring templates

### P1: Classify `src/share`

Every file under `src/share/templates/standard/` needs one owner label:

- runtime bootstrap
- runtime authoring template
- docs example
- delete

This inventory becomes the migration checklist.

Bootstrap consequence:

- `skills/global/**` under `src/share/templates/standard/` is runtime-bootstrap content
- `config.json` is generated at init time, so the checked-in template copy is not authoritative

### P2: Re-home runtime-owned assets

Move runtime-owned scaffold files into Go-owned embedded asset trees:

- `internal/scaffold/home/`
- `internal/scaffold/templates/`

Then update `commands/init.go`, `commands/skills.go`, and `commands/agents/new.go` to consume the canonical source.

### P3: Replace the installer

This direction is now chosen:

- keep `scripts/install.sh` as a supported path
- rewrite it around supported install targets instead of `src/lib` / `src/share` payload copying
- make the installer target-aware:
  - default target: Go CLI
  - explicit alternate target: TS port (`da-ts`)

Recommended interface:

- `--port go|ts`
- `DOT_AGENTS_PORT=go|ts` as an env fallback for curl-driven installs

Do not keep `src/lib` alive only because the old installer expects it.

### P4: Delete the legacy shell tree

Once P2 and P3 land:

- remove remaining `src/lib/**`
- remove runtime payloads from `src/share/**`
- keep docs-only examples somewhere outside the runtime tree
- clean docs/tests that still describe the shell layer as active

## Verification

- `go test ./...` on the cleanup branch
- `rg -n "src/lib|src/share/templates/standard" .` only returns intentional docs/history references
- smoke the supported install/bootstrap path
- if scaffolds move, smoke `dot-agents init`, `dot-agents skills new`, and `dot-agents agents new`
