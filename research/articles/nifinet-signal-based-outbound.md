## How to Build a Signal-Based Outbound Engine on Codex

**Source**: https://x.com/nifinet/status/2072704249663004773
**Author**: Nicolas Finet (@nifinet), Sortlist
**Date**: 2026-07-02 (11:29 AM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~1,900 words (prose; embedded code/command blocks not carried by extraction — see notes)
**Engagement**: 9 replies, 21 reposts, 290 likes, 1.3K bookmarks, 152.7K views

---

### Summary

Turned an outbound routine into something an agent runs: watch the market, wait for a company to move, decide if the movement is a real signal, then write from that signal. Architecture: one shared JSON record (schema-validated) in the middle, six `codex exec` jobs around it — sense, score, write, check, log, learn — chained by cron each morning into a human-reviewed approval queue. Cheap model routing for normalization, strong routing for judgment/reputation steps; file-based non-interactive loop (no MCP, no approval bypass flags in unattended runs); weekly "learn" step exists to make the engine more selective (a cut list of triggers/phrases/score rules), not more active.

### Relevance to dot-agents

A concrete template (watch -> validate-signal -> decide -> draft) for the dot-agents agent-run X outreach engine once the X app is approved. The full body adds directly reusable structure: schema-per-stage with a shared record ("the prompt will drift; the schema keeps the next step usable") is the same contract discipline as delegation bundles; AGENTS.md as inherited house rules; outputs/ vs memory/ separation ("outputs are what happened today, memory is what the system should remember tomorrow") mirrors iter-log vs lessons; per-stage model routing matches the worker-model-selection policy.

### Body

I spent the last few weeks turning our outbound routine into something an agent can run.

The routine: watch the market, wait for a company to move, decide if the movement is real, write from that trigger, and keep a record of what happened.

At Sortlist, timing decides whether outbound feels useful or rude. A company rarely wakes up and searches for a vendor from nowhere. Something happened first: a role opened, a round closed, a team changed, a problem became visible.

Outbound should start there.

I shared the first version as an AI GTM Brain on Claude Code. Then I ported the same system to GLM-5.2. Both ran the same sequence: sense the market, score the account, write from the signal, check the message, log the outcome, learn from the reply.

The Codex version has one advantage: it can run from the ChatGPT/Codex plan you already pay for.

No OpenAI API key for the reasoning step. You sign in once, keep the workflow in a folder, and run each job with codex exec.

codex exec is the useful part. It takes a file, writes a structured file, and gives the next step something predictable to read. Cron runs the same chain every morning.

The daily output is a shortlist: who moved, why now, what changed, what to say, and what happened last time.

By the time a company searches, the budget already moved.

Run the first version by hand. Take 20 accounts, write one real signal for each, score them, draft the message, review it, and log the result. Once the routine is clear, Codex can run the repetitive parts.

The loop: one shared record in the middle. Six Codex jobs around it.

Six Codex jobs, one shared record: sense, score, write, check, log, learn.

**1. Install Codex and sign in with ChatGPT**

Install Codex [command block not captured]. Then log in [command block not captured].

Pick ChatGPT sign-in. Codex also supports API-key auth. The build below runs the reasoning loop from the paid-plan path.

If you are on a remote machine: [command block not captured]

Codex caches credentials locally, often in ~/.codex/auth.json or your OS credential store. Treat that file like a password. Keep it out of repos, tickets, and shared docs.

You may still need external keys for data sources: CRM, job boards, funding feeds, website intent, or any signal provider you already use. Codex runs the routine. The data still comes from your stack.

That separation is important.

The first version should not depend on a perfect data warehouse. Export a CSV from your CRM, drop in a job-board scrape, or paste ten funding announcements into a file. The agent can work with boring inputs as long as the evidence is concrete.

**2. Create the repo**

Start with files.

The folder stays boring. Keep it visible. When a recommendation is bad, you want to see the input, the prompt, the schema, and the output.

The repo: inputs, schemas, prompts, outputs, memory, logs, scripts, AGENTS.md.

This is also why I keep memory/ separate from outputs/. Outputs are what happened today. Memory is what the system should remember tomorrow. Mixing them is how teams end up with a folder full of impressive JSON and no actual learning.

**3. Add the house rules**

Create AGENTS.md.

This file saves you from repeating the same standards in every prompt. It also makes weak outputs easier to debug.

If Codex approves a vague message later, you now have a place to fix the rule once instead of editing six prompts. Add your own constraints here: forbidden industries, tone rules, segments you never contact, claims sales cannot make.

The agent should inherit the operating standard before it touches a lead.

**4. Define the shared record**

The shared record is the engine.

Create schemas/account.schema.json. [schema block not captured]

The prompt will drift. The schema keeps the next step usable.

The fields are intentionally plain. signals stores what happened. score stores how much we care. why_now is the sentence a human should be able to read in the morning. status keeps the account from bouncing around the workflow forever.

**5. Sense**

The first job turns raw inputs into clean signals.

This is where I would be strict. The sense step is allowed to throw rows away. A weak signal that survives here wastes time in every later step. I would rather miss a few accounts than fill the queue with fake urgency.

Your raw input can be a CSV. Create schemas/signals.schema.json and prompts/01_sense.md, then run it. [blocks not captured]

I would start with gpt-5.4-mini here. Normalization is cheap judgment.

After this run, open outputs/01_signals.json before you continue. If the evidence field sounds like a summary instead of a fact, fix the input. Do not let the next steps polish weak evidence.

**6. Score**

Scoring answers one question: would I ask a human seller to spend time on this account today?

The score is a priority filter. You are teaching Codex what your team already does in its head: recent evidence beats old evidence, a buying trigger beats a vanity signal, and one strong reason beats five weak ones.

Create schemas/scores.schema.json and prompts/02_score.md, then run it. [blocks not captured]

A funding round can be useful. It can also mean nothing. A new job post can mean budget, or backfill. A competitor like can mean interest, or an intern clicked something.

Score for attention. Vanity signals go to the bottom.

The useful habit here is to read only the why_now field. If that sentence does not make you want to open the account, the score is too high. I do this because numbers can hide bad judgment. A sentence exposes it.

**7. Write**

Only draft for accounts above the threshold.

I keep the threshold at 70 in the first version because I want fewer drafts. You can lower it later. Early on, the goal is 5 messages you would actually send. The 100-message version can wait.

Create schemas/messages.schema.json and prompts/03_write.md, then run it. [blocks not captured]

Use the stronger routing here. Bad drafting is the first place a prospect sees your automation.

No real trigger, no message.

The draft should feel almost constrained. That is good. The signal gives the message its shape. If Codex starts writing about the company being "innovative" or "growing fast," the source evidence is too thin or the prompt is too loose.

**8. Check**

Now add the reputation gate.

I separate writing and checking because the same step that writes the message will often defend it. The check step has a different job. It only needs to be slightly suspicious.

Create schemas/checks.schema.json and prompts/04_check.md, then run it. [blocks not captured]

Do not send automatically at the beginning. Put approved messages in a morning queue. A human can review 20 good messages in less than 10 minutes.

That approval queue is the compromise I like. Codex removes the blank-page work, and the human keeps the final standard. If the queue feels bad, you fix the system. If the queue feels good, you can decide later which parts deserve more autonomy.

**9. Log**

The log is where the routine gets better.

Most outbound teams log the outcome and lose the reason. Won, lost, no reply. Fine for reporting. Almost useless for the next message. The reason is what should feed the next run.

Create inputs/outcomes.csv, schemas/log.schema.json, and prompts/05_log.md, then run it. [blocks not captured]

Every send, reply, ignore, meeting, and bad fit goes back into memory. Without that, the workflow never learns.

Write the ugly reasons too. "Wrong person." "No budget until Q4." "Already solved with internal team." Those are more useful than a clean lost status. The agent cannot learn from politeness.

**10. Learn**

Once a week, ask Codex to read the outcomes.

This step is where the engine stops being a daily script and starts becoming a feedback loop. I do not want Codex to celebrate activity. I want it to tell me which signals deserve less trust next week.

Create schemas/learning.schema.json and prompts/06_learn.md, then run it. [blocks not captured]

I care about this step most. Every week, the engine should become more selective.

The best weekly output is usually a cut list: triggers to retire, phrases to ban, score rules that were too generous. Outbound improves when the system says no more often.

**11. Run the morning loop**

Create scripts/run_daily.sh, then add cron. [blocks not captured]

By 7 a.m., outputs/04_checks.json is your queue.

Morning workflow: open the queue, approve, edit, or kill, log the result, let the next run learn.

The first week, run this manually before adding cron. You want to watch where it breaks. Maybe the sense step accepts weak rows, the score step overrates funding, or the writer gets too polite. Fix those before the machine starts showing up every morning.

**12. The Codex gotcha**

One Codex detail is easy to miss.

codex exec is built for non-interactive work: read files, run shell commands, call scripts, write outputs, return structured data.

It should not be waiting for approval prompts at 6:15 a.m.

Keep the scheduled loop on plain files, shell, HTTP calls, and provider APIs where needed. Use MCP in interactive work, where a human can answer prompts.

Codex has flags that bypass approvals and sandboxing. I would keep those out of a GTM routine unless the environment is isolated.

The practical fix is boring: make the scheduled loop file-based. Let Codex read files, run scripts, and write JSON. Keep anything that requires a human decision outside the unattended run.

**13. Routing**

With ChatGPT sign-in, routing is about speed, quality, and usage discipline.

Do not overthink this in the first week. Smaller routing for clean-up work. Stronger routing for judgment. If the output is bad, fix the prompt and evidence before blaming the name in the -m flag.

My default: use lighter routing for normalization and first-pass classification. Use stronger routing when judgment or reputation risk enters the routine.

Routing: cheap models sort, strong models judge.

If your Codex install shows different names, run [command not captured], then update the script. The available names move faster than outbound fundamentals.

**14. Start smaller than you want**

Start with three: [list block not captured — sense, score, write per surrounding context]

Run those by hand for one week, then automate.

The first version should feel almost too simple. Simple gets used.

We are building the same logic into the agent we are shipping at Sortlist. It learns the buyer, watches the signals, drafts the LinkedIn or email message with the reason attached, and waits for approval before anything sends.

If you want the Codex repo, reply and I will send it.

The search is the last step. Your outbound engine should be there before it happens.

### Key Quotes

> "The prompt will drift. The schema keeps the next step usable."

> "Outputs are what happened today. Memory is what the system should remember tomorrow."

> "I separate writing and checking because the same step that writes the message will often defend it."

> "Outbound improves when the system says no more often."

> "Routing: cheap models sort, strong models judge."

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). The article embeds its code/command/schema blocks as images or rich embeds that X's DOM text extraction does not carry — those are marked `[block not captured]` in place; all prose is verbatim and complete. Author offers the actual Codex repo by reply.
