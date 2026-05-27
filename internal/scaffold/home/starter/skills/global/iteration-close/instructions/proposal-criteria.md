# Proposal Creation Criteria

At the end of each iteration, scan CLI traces and observations for **worthy candidates** — findings that should flow back into the canonical config rather than staying locked in loop-state forever.

## What Qualifies

A worthy candidate is a finding that:
- Will recur in future iterations or future sessions
- Can be captured as a concrete change to a file in `~/.agents/`
- Is specific enough to write as a diff (not "improve UX generally")

### High-value candidate types

| Finding type | Proposal type | Target path example |
|---|---|---|
| New gotcha found while running a command | `skill` / `modify` | `skills/dot-agents/iteration-close/instructions/gotchas.md` |
| Repeated pattern that belongs in a rule | `rule` / `modify` | `rules/global/rules.mdc` or `rules/dot-agents/agents.md` |
| Project-specific orientation gap | `rule` / `modify` | `rules/payout/agents.md` |
| Missing or wrong hook behavior | `hook` / `modify` | `settings/dot-agents/claude-code.json` |
| Skill workflow step that's wrong or incomplete | `skill` / `modify` | `skills/dot-agents/<skill>/instructions/workflow.md` |

### Does NOT qualify
- Implementation bugs in the CLI itself → write a plan item instead
- Large loop prompt refactors → do those as a direct edit to the repo-local `.loop.md` file
- One-off debugging findings with no reuse value → leave in `## CLI Observations` only
- Speculative improvements → require evidence from at least one trace before proposing

## How to Create a Proposal

Use the proposal/review loop: create the canonical proposal artifact under `~/.agents/proposals/`, then inspect/apply it with `da review`.

**Option 1: Direct YAML write** to `~/.agents/proposals/<id>.yaml`:

```yaml
schema_version: 1
id: "20260411T210000-kg-warm-gotcha"
status: pending
type: skill
action: modify
target: skills/dot-agents/iteration-close/instructions/gotchas.md
rationale: |
  kg warm silently succeeds with 0 notes when KG_HOME is uninitialized.
  Observed in iteration 5 — operators see "success" but KG never actually warm.
content: |
  # Gotchas: Iteration Close
  
  ... full updated file content ...
created_at: "2026-04-11T21:00:00Z"
created_by: loop-agent
reviewed_at: ""
review_reason: ""
```

**Option 2: Use the `propose.sh` helper script**:
```bash
# The script is at: .agents/skills/iteration-close/scripts/propose.sh
# Or via canonical path: ~/.agents/skills/dot-agents/iteration-close/scripts/propose.sh

~/.agents/skills/dot-agents/iteration-close/scripts/propose.sh \
  --type skill \
  --action modify \
  --target "skills/dot-agents/iteration-close/instructions/gotchas.md" \
  --rationale "kg warm silent success when KG_HOME uninitialized" \
  --content-file /tmp/updated-gotchas.md
```

## Important: `modify` action replaces the entire file

When action=`modify`, `da review approve` **overwrites the entire target file** with `content`. Always:
1. Read the current file first: `cat ~/.agents/<target>`
2. Write the updated full content (add your new gotcha to the existing ones, don't just write the new gotcha alone)
3. Put the full updated file in `content:`

## Proposal cadence

- At most **one proposal per iteration** — don't flood the queue with speculative changes
- Batch multiple small gotchas into one `modify` proposal for a single file when possible
- Check `workflow health` after creating a proposal to confirm `proposals: N` increased

## Checking pending proposals

At session start (or anytime):
```bash
da review                    # list all pending proposals
da review show <id>          # see full content of one proposal
da review approve <id>       # apply it — writes content to ~/.agents/<target>
da review reject <id>        # dismiss it with a reason
```
