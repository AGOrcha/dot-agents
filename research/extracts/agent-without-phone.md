# Extract: How I Run an AI Agent Without Touching My Phone (@madebydia / Diana Park)

Source: research/articles/agent-without-phone.md
(Tagged "related concepts to draw on later" — adjacent UX/interface piece.)

## Key claims
- The interface, not the agent, is the problem. The value of an interface is the *social signal* it sends, not just throughput.
- Three tiers of command surface: **fast capture** (Watch dictation -> Poke -> iMessage -> Hermes agent), **slow capture** (smart pen / notebook for thoughts needing reflection, piped in later), **no capture** (scheduled/cron automation that never requires a command).
- Automation's real value isn't saving task-minutes — it **removes the decision point before the task** ("cognitive load stops existing"). "The best interface is no interface."

## Techniques
- Route async/recurring work to push-to-you channels (cron jobs, scheduled self-emails, Shortcuts). Batch synchronous interaction into fixed windows. Pick capture surfaces that "don't look like scrolling."
- Concrete personal-agent stack: Hermes (agent) + Poke (iMessage executor) + inq smart pen + Apple Watch.

## What's novel
- Reframes agent UX around attention/presence rather than efficiency; "design for what they see."

## Mapping to our work
- **session-handoff journal / episodic view:** "Slow capture -> route into the pipeline later" is a human analogue of our deferred episodic capture: jot now, the system files it into the right project/view asynchronously. Validates an *ingest-now, classify-later* path into the KG rather than requiring structured input at capture time.
- **No-capture / scheduled work:** Maps to our cron/scheduled-agent and background-worker direction (r3-background-worker-service). The "remove the decision point" framing is a clean articulation of why proactive KG-driven surfacing (e.g. stale-note alerts, eligible-task push) beats pull-only `da kg query`.
- **Weakest tie of the five:** This is an interface/attention essay, not a KG/storage piece. Its concrete contribution to our design is the *capture-tier model* (fast/slow/none) as a lens on how work and knowledge enter the system — worth keeping as adjacent inspiration, not a direct validation/challenge to the KG contracts.
- **Idea (soft):** A "capture tier" attribute on episodic-view ingest (synchronous command vs. deferred note vs. autonomous/scheduled) could help the operational view reason about which knowledge arrived with human intent vs. was machine-generated.

## Caveats
- Personal essay; no metrics or technical contracts. Adjacency to our KG work is thematic, not structural.
