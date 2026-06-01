# Gotchas: Skill Architect

Common failure points when building, testing, and shipping skills.

## Design Failures

**Putting rules in SKILL.md instead of instructions/**
SKILL.md should read like a table of contents, not a rulebook. If you find yourself writing "always do X" or "never do Y" in the SKILL.md body, move it to an instructions/ file. The test: SKILL.md should make sense even if you can't see the instructions/ files.

**Writing a description that describes the skill, not a trigger condition**
"This skill helps with PDF operations" is not a trigger condition. "Use when the user needs to extract, fill, convert, or analyze PDF files — especially multi-page documents, form filling, or batch operations" is. The difference: one answers "what is it?" and the other answers "when should I use it?"

**Over-specifying instructions**
Rigid step-by-step instructions make Claude mechanical. Explain the goal and the why; let Claude adapt. If you wrote a 500-line instruction file, most of those lines are probably over-constraining things Claude already handles well.

## Eval Failures

**Launching the viewer after evaluating results yourself**
The human should see the outputs before you form a judgment. Generate the viewer and get results in front of the user immediately after runs complete. Don't summarize or assess first.

**Spawning with-skill runs before baseline runs**
Always spawn both in the same turn. If you run with-skill first, you'll have anchored expectations before seeing the baseline, and the comparison will be biased.

**Grading JSON field names wrong**
The viewer requires exactly `text`, `passed`, and `evidence` in each expectation object. Using `name`/`met`/`details` or any variant will cause the viewer to show empty or broken grades.

**Forgetting to capture timing data from task notifications**
`total_tokens` and `duration_ms` exist only in the subagent completion notification. Once the notification is processed, the data is gone. Save to `timing.json` immediately when each task completes.

**Writing assertions that always pass**
An assertion that passes even for completely wrong output doesn't test anything. Check that each assertion is discriminating — it would fail if the skill hadn't actually done its job.

## Description Optimization Failures

**Using obvious negative eval queries**
"Write a fibonacci function" as a negative test for a PDF skill is too easy — it doesn't verify that the description avoids false triggers on adjacent domains. Negative queries should be near-misses: queries that share vocabulary or intent with the skill but actually need something different.

**Triggering on too-simple queries**
Simple queries ("read this PDF") may not trigger skills even with a perfect description, because Claude handles them directly with basic tools. Eval queries need to be complex enough that Claude would genuinely benefit from consulting a skill.

## Packaging Failures

**Packaging before validating**
Always run `quick_validate.py` first. The packager validates too, but catching errors early is cheaper.

**Renaming a published skill when updating**
If the installed skill is `research-helper`, the output must be `research-helper.skill` — not `research-helper-v2.skill`. Users' installations depend on the name matching.
