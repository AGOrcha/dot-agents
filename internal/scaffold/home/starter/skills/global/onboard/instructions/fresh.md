# Path: fresh (scaffold from scratch)

Use when there is no shared config to adopt and no repo manifest to install —
an empty `~/.agents` and a project you want to start managing.

## Steps

1. **Scaffold the home** — create `~/.agents/` and link the active harness
   globally:
   ```bash
   da init
   ```
   Useful flags:
   - `-y` / `--yes` — auto-confirm prompts (non-interactive).
   - `-n` / `--dry-run` — show what it would create without writing.
   - `-f` / `--force` — clobber an existing home (it backs up the old one first).
     Only needed if `~/.agents` already exists and you want to start over.

   If `~/.agents/config.json` already exists (home was initialized earlier on
   this machine), **skip this step** and start at step 2.

2. **Bind your project** — register the repo, generate its `.agentsrc.json`, and
   link the platform config into it:
   ```bash
   da add <project-path>
   ```
   Run once per project (e.g. `da add ~/code/myrepo`).

3. **Link platforms** — re-detect installed editors and re-link:
   ```bash
   da refresh
   ```

4. **Confirm health:**
   ```bash
   da status --audit
   da doctor
   ```
   `da status --audit` shows file-level link detail per project; `da doctor` is a
   read-only health check that flags anything broken.

## Then

Continue to `instructions/verify.md` to install your editor (if not already) and
re-run the link + health check so newly-installed harnesses are picked up.
