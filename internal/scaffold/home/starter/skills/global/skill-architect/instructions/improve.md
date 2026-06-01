# Improve Mode

Iterate on a skill based on eval feedback until the outputs are satisfactory.

## How to Think About Improvements

**1. Generalize from the feedback.** The skill will be used across many different prompts — not just the test cases in front of you. Avoid fiddly, overfitty changes. Instead of adding rigid constraints, try to understand *why* the output wasn't right and update the skill to transmit that understanding. If a stubborn issue won't resolve, try a different approach entirely — new metaphors, different working patterns.

**2. Keep the skill lean.** Remove things that aren't pulling their weight. Read the transcripts, not just final outputs. If the skill is making the agent waste time on unproductive steps, cut the instructions driving that behavior. Less is often more.

**3. Explain the why.** Don't just say what to do — say why. LLMs respond well to reasoning. Instead of `ALWAYS include a summary section`, try explaining what value the summary gives the reader and when it matters. If you find yourself writing `ALWAYS` or `NEVER` in caps, that's a yellow flag — reframe as reasoning instead.

**4. Look for repeated work across test cases.** If multiple test runs independently wrote similar helper scripts or took the same multi-step approach, that's a signal to bundle it as a script. Put it in `scripts/` and reference it from the skill. Every future invocation benefits from the work done once.

## The Iteration Loop

After reviewing eval feedback:

1. Apply improvements to the skill
2. Rerun all test cases into `iteration-<N+1>/`, including baseline runs
3. Launch the reviewer with `--previous-workspace` pointing at the previous iteration
4. Wait for user review, read feedback, improve again

Keep going until:
- The user says they're happy
- All feedback is empty
- No meaningful progress is being made

## What to Look For in Transcripts

Read actual execution transcripts, not just outputs:
- Did the agent follow the skill's instructions or improvise its own approach?
- Did the agent use scripts the skill provided, or reinvent them?
- Were there unnecessary steps the skill accidentally encouraged?
- Did the agent get confused at a specific point?

These patterns reveal whether the skill instructions are clear, not just whether the output was right.

## After Improving

Once the user is satisfied with the content, offer to run `optimize` mode to improve triggering accuracy, then `package` to create a distributable file.
