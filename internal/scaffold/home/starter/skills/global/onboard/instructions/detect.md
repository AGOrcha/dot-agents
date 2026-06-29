# Detect the setup path

Run these probes (read-only) and pick exactly one path. Stop at the first match
in this order.

## Probes

1. **Is the home initialized on this machine?**
   ```bash
   ls ~/.agents/config.json
   ```
   - Present → `~/.agents` already exists. The user has run `da` here before;
     prefer the **from-manifest** or **fresh** path depending on the repo.
   - Missing → the home is not initialized on this machine. Either **from-home**
     (if they have a shared config URL) or **fresh**.

2. **Does the current repo already declare real config sources?**
   ```bash
   ls .agentsrc.json && cat .agentsrc.json
   ```
   Look at the `sources` array. A `{ "type": "local" }`-only manifest is a
   self-contained project; an entry with `type: git | http | oci` (or an
   `extends` array) means the repo pulls **real upstream config** → **from-manifest**.
   Also note `.agentsrc.lock` next to it: its presence means layers were already
   resolved on some machine.

## Resolve to a path

| Condition | Path |
|---|---|
| User has a shared/team `da` home-config **git URL** to adopt | **from-home** |
| `.agentsrc.json` exists in the repo with non-local `sources` / `extends` | **from-manifest** |
| No home, no manifest (empty `~/.agents`, no `.agentsrc.json`) | **fresh** |

## When ambiguous — ask ONE question

If the probes do not clearly select a path, ask the user the single
disambiguating question and route on the answer:

- **No home + repo has no manifest:**
  > "Do you have a shared/team `da` config URL to adopt (a published `~/.agents`),
  > or should I scaffold a fresh setup from scratch?"
  - URL given → **from-home**
  - "fresh" / no URL → **fresh**

- **Home exists but you're unsure the repo is set up:**
  > "Does this repo already have a committed `.agentsrc.json`, or is it a new
  > project I should bind?"
  - Has manifest → **from-manifest**
  - New project → **fresh** (skip `da init`; the home is already there — go
    straight to `da add <project-path>`)

Do not run any write command until the path is selected.
