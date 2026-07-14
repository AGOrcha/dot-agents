# What I Learnt After Running Loops for 1 Month

**Author:** Jason Zhou (@jasonzhou1993) — SuperDesign
**Source:** https://x.com/jasonzhou1993/status/2075179471951614381
**Published:** July 9, 2026
**Engagement:** 70.9K views · 283 likes · 616 bookmarks
**Open-source:** Loopany platform (https://github.com/superdesigndev/loopany-platform) · verifier-setup skill (https://github.com/AI-Builder-Club/skills)

---

## Relevance to dot-agents

**[OVERLAP-SHARPEN]** (eval Part L.4). The fullest external articulation of our own loop stack:
contract ≈ active.loop.md + rules/ + boundaries; state+logs ≈ workflow state + iteration-log;
/verify ≈ verifier profiles + `verify` skill + PR evidence; orchestrator+executor+verifier ≈ the
ISP role split; **evolve ≈ our fold-back → lesson → skill distillation** (= J.5's "system loop")
— the sharpest name yet for that machinery. The boundary/fence ("ship on its own vs ask a human,
the exact line") corroborates `loop-discipline-stop-hooks` and the delivery-gate stop-before-push
rule. The concrete Goal/Boundaries/SOP/State/Logs contract templates are citable external
templates for our loop docs. Parked lead: "gate cheaply so empty runs cost nothing" is the
cost-aware trigger the autonomous runtime wants (blocked on the scheduler daemon — lesson
`agents-lack-autonomous-timers`). Practitioner-report grade; open-sourced tooling raises
credibility above the growth-account tier.

Last week I wrote about loop engineering: the shift from prompting an agent to complete a task, toward designing systems where the agent decides what to work on, executes, verifies, and improves over time. Anyone can wrap an agent in a `while true` and call it a loop. That is the easy 5%. The real work is the guardrails that let you walk away from it.

## The anatomy of a good loop

Every loop we run has the same four parts.

### 1. The loop contract

One markdown file, injected into the agent every time a run fires. The constitution of the loop. It holds:
- **the Goal** — what winning looks like, and whether there's even a finish line
- **the Boundaries** — what it can do freely, what it must never do, and the line between what it can ship on its own vs what needs a human
- **the SOP** — the steps it follows each run

People underinvest in the boundary section, and it's the one that decides whether you can walk away. Example boundary slice from the **Error Sweep** loop:

```
- Fix only when the root cause is clear AND the fix is low-risk. Anything risky or large: open a PR and flag a human. Do not merge it yourself.
- Make the smallest fix that works. One PR per fix.
- Never open a new PR while a previous Error Sweep PR is still unmerged.
- Never copy credentials, tokens, or user data into a report or a PR.
```

None of that is about *how* to fix a bug. It's four sentences drawing a fence. Inside the fence the agent ships on its own. Outside it, a human decides.

Contract + state + logs normally live in one md file:

```markdown
# <loop name> — contract
## Goal
What winning looks like. Is there a finish line, or does this run forever as a monitor?
## Boundaries
- Free to do: ...
- Never do: ...
- Ship on its own vs ask a human: <the exact line>
## SOP (each run)
1. Read state + logs.
2. Gather what changed. Pick the single most worthwhile thing to do.
3. Do it (or hand it to an executor).
4. Verify. Record what happened. Report.
## Current understanding
...Current state of the loop run
## Logs
...Logs of past runs
```

### 2. State + logs

A loop that forgets everything between runs is a cron job with extra steps. Memory splits in two:
- **State** is the durable picture: the backlog, current hypothesis, shipped experiments needing follow-up. Read at the top of every run, kept small on purpose.
- **Logs** are the append-only record of what happened, run by run.

Example — the state Error Sweep keeps so it doesn't re-investigate:

```
## Skip these fingerprints (known noise or upstream, not ours)
- ResizeObserver loop limit exceeded
- Stripe.js network blip on /checkout
## Fixed, still watching
- null-team-on-login (019edc8a) — fixed in #1027, confirm it stops firing in prod
```

State also holds what the loop *learned by running* — a working hypothesis plus habits from getting things wrong. The CRM loop carries lines like: "users who hit the credit wall in their first week reply about 3x more than everyone else — draft those first" and "before drafting, check what the user actually built, not the label on their profile." None came from the original contract; the loop earned it run by run. That's why a good loop is worth more in month three than in week one — its state absorbs everything it has seen.

### 3. The /verify

Pre-requisite to any loop delivering high-stakes work (real production code, messaging real customers). The verify process must be easy + produce evidence a human can easily review. For engineering tasks:
- `dev-local.sh` script to easily start local dev + remote sandbox env (like crabbox)
- Tooling for the agent to self-drive the app like a real user (e.g. Playwright-CLI)
- a `/verify` skill containing the SOP for the test
- a place to upload screenshot + video evidence to attach to the PR (they upload to GitHub release assets)

A PR from a loop doesn't arrive as "trust me, it works." It arrives with a video of the thing working — approve in seconds by looking at behaviour, not reading the diff and hoping. A verifier is the deciding factor in whether a loop is even a good idea.

### 4. The trigger

What wakes the loop up. Three shapes, and picking the right one is half the cost model:
- **Continuous for-loop** (ralph-loop / `/goal`): agent runs in a for-loop until a condition is met. Right for bounded pushes ("keep going until the test suite is green"), wasteful as a permanent fixture. Best when there's an instant feedback loop and a clear spec (most bug-fixing work).
- **Time based**: a schedule fires the loop (hourly, 6am daily). Error Sweep, React Doctor, doc maintainer, CRM all run on cron.
- **Workflow / event based**: runs only when there's something to run on (new email, incident, PR lands). Can combine with time-based — a tick every hour checks for new tickets; if some, trigger the agent; if not, log & silence. A good way to manage loop cost.

## Orchestrator + Executor + Verifier

Once a loop touches anything non-trivial, it splits into three roles:
- **Orchestrator** finds the work: wakes on schedule, gathers signals, looks at what changed, picks the single most worthwhile thing this run, hands it off. Its job is not to do the task.
- **Executor** does the actual work in an isolated space (for code, a fresh git worktree off main, so it never pollutes your checkout or another loop's run).
- **Verifier** independently confirms the executor's work and produces glanceable evidence.

Not all loops need all three. The 3-layer shape is what complex, code-shipping loops grow into. Plenty of good loops are just the orchestrator doing the whole thing.

## Evolve Loop — build anti-fragile loops

From Taleb's *Antifragile*: fragile systems fear volatility; robust systems survive and recover; antifragile systems gain from it. Pointed at loops: when a run fails, where does the lesson go? A lesson has three destinations in rising order of abstraction (logs → state conventions → the loop's own scripts/skills/contract). Everyone has logs, and they only get longer. Distilling experience into rules is what makes a loop antifragile.

So **evolve** became a separate run role: an agent session reads the last dozen runs' logs, results, and cost, and asks *where are we repeating mistakes? Which runs were wasted? Which boundary is too loose, which too tight?* Its output isn't product code — it's changes to the loop itself: loop contract, state conventions, trigger mechanism, scripts for repeating deterministic steps, skills, the dashboard humans look at. It's a loop to improve the loop itself.

## Real loops running SuperDesign

**Doc maintainer loop** (1 layer) — once a week keeps docs honest. Orchestrator reads the diff, checks the docs, opens a PR if something drifted. No separate executor/verifier because the task is low-blast-radius. Build the 1-layer version first, feel the specific pain, then add exactly the layer that fixes it. Template:

```markdown
# doc-maintainer — contract
## Goal
The README, setup guides, and examples always match what the code actually ships. Zero drift found = a successful run, not a wasted one.
## Boundaries
- Ship on its own: open ONE pull request with the fixes. That's it.
- Never: rewrite accurate docs to look busy, touch anything outside the docs, stack a 2nd PR while last week's is still open.
## SOP (each run)
1. Read the diff: every commit + PR since the last sweep (cursor is in state).
2. Compare README, setup guides, examples, runbooks against what the code ships NOW.
3. Verify for real: run the commands, check the links, try the examples. Never trust memory.
4. Drift found → smallest fix, fresh worktree, one PR explaining what drifted and why. Nothing stale → clean stop.
5. Move the cursor, log the run.
## State
- last-sweep cursor (commit hash)
- open PR, if any
## Logs
- one dated line per run: drift count + PR link, zero included
```

**Bug hunter / Error Sweep loop** (3 layers) — runs every morning. *(orchestrator)* pulls last 24h of production errors (script collects from posthog/LLM log/server logs), ranks by occurrences × users, skips known-noise fingerprints from state, picks the single most impactful new exception, pulls the de-minified stack trace, finds root cause. *(executor)* if clear + low-risk, fixes in a fresh worktree and ships the PR; if risky/large, stops and flags a human. *(verifier)* the fix gets proven before it counts, and the loop keeps watching that fingerprint on later runs to confirm the error stopped firing. "A fix that doesn't move the real number isn't a fix."

**React Doctor** — same shape aimed at code health: `npx react-doctor` scan, picks the single most severe issue, fixes in isolated worktree, verifies, one PR. Reports a health score as a daily metric. Guardrail: "If a previous React Doctor PR is still open and unmerged, don't open another one today." That rule is the difference between a helpful loop and one that buries you in 30 PRs. A loop has to respect your review bandwidth, not just its own throughput.

**Support triage loop** — hourly against Intercom. Guiding principle: every support ticket is a free window into a product gap. Four moves: (1) pull the window — everything since last run + follow-ups due; the window widens over silent hours so a missed fire never loses tickets; bucket by last speaker. (2) investigate before replying — root-cause against real data (account, sessions, error logs, billing) first; half the time the user's description isn't the problem. (3) fan out to up to three outputs — a reply now, a signal filed to the knowledge base (product gap written once so growth/eng loops act later), and a spawned fix agent when it's a real bug. (4) write back, set follow-up dates, sleep. Boundary: replies are tiered — routine factual answers ship on their own; refunds/angry/non-English go out only after human approval.

**CRM lifecycle loop** — highest-value, never touches code; fullest 3-layer shape pointed at people. Scripts pull data first (deterministic pre-stage, no LLM on data pulls); users who replied exit the loop entirely (a live conversation belongs to a human). Orchestrator proposes *segments*, not one-off picks. Each segment spawns an executor sub-agent that reads what the user actually built + every past exchange, then drafts personal email (never blind). A verifier fact-checks every claim against data, then applies voice + anti-slop rules. Sends are tiered by risk — autonomy is *earned per segment* on proven reply rates, not granted to the loop. Friction found becomes a signal other loops read.

## Checklist for a good loop

- [ ] Loop contract file: goal, SOP, output rules, read at every run
- [ ] Boundary and constraints: what it ships on its own vs asks a human; no-op is a valid run
- [ ] State + logs: memory across runs, so it never re-does work
- [ ] Cheap verifier: proof with evidence; if none exists, keep a human in the loop
- [ ] Isolated execution: fresh worktree or own sandbox per run
- [ ] Cost-effective trigger: gate script or event, empty runs cost nothing
- [ ] Loop evolve cycle: review run history, fold mechanical work into scripts/skills
- [ ] Small scope: split the loop until "just let it run" feels comfortable
