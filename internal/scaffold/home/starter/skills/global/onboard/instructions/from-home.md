# Path: from-home (adopt a shared home config)

Use when the user has a shared/team `da` home config published as a **git URL**
(a teammate's or another machine's `~/.agents`). This bootstraps `~/.agents` on
this machine from that source, then re-links and rebinds projects locally.

## Preconditions

- `~/.agents` must be **empty / absent** on this machine. `da init --from`
  refuses to clobber a populated home (use the **fresh** path, or move the
  existing `~/.agents` aside, if one is already there).
- Git auth is **ambient only** — an ssh-agent key or a git credential-helper.
  There is no login command. A credential-bearing URL (one with an embedded
  token/password) is refused and never stored, so use an `ssh://`/`git@` URL or
  a helper-backed `https://` URL.

## Steps

1. **Adopt the home** — clone and install the shared `~/.agents`:
   ```bash
   da init --from <git-url>
   ```
   This clones the source into staging, resolves it, renames it into `~/.agents`
   only on success (a failed clone never bricks a retry), and joins this machine
   to the synced identity registry with **zero project bindings** — every project
   arrives known-but-unbound (paths are never imported from the source machine).

2. **Re-detect platforms and re-link** — establish this machine's editor surface:
   ```bash
   da refresh
   ```

3. **Rebind each project** — point each repo at the adopted config:
   ```bash
   da add <project-path>
   ```
   Run once per project you work in. This generates/updates the project's
   `.agentsrc.json` and links the platform config into it.

## Then

Continue to `instructions/verify.md` (install editor → `da refresh` → audit →
`da doctor`).
