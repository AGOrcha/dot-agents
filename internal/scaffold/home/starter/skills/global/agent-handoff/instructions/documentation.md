# Documentation

What to document before handing off.

## Update Active Plans

- Mark completed items in `.agents/active/` plan files
- Add notes on any items that changed scope or approach during implementation
- If a plan is fully complete, archive it to `.agents/history/{plan-name}/`

## Write Implementation Results

- Write results to `.agents/history/{plan-name}/impl-results.{n}.md` following the convention in CLAUDE.md
- Include what was built, key decisions made, and any trade-offs
- Condense when more than 5 result files exist (merge into aggregate)

## Leave TODO Comments

- For incomplete work, leave `TODO` comments in the code with enough context to resume
- Format: `TODO: [description of what needs to happen and why it was left incomplete]`
- Reference the relevant `.agents/active/` plan if one exists

## Update Task Lists and Issue Trackers

- Update any issue trackers or task boards with current status
- Close or resolve issues that were completed
- Add comments to open issues with progress notes
