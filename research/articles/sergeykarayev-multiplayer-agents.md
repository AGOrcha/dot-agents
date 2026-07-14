## Your Agents Should Be Multiplayer

**Source**: https://x.com/sergeykarayev/status/2072768568077516819
**Author**: Sergey Karayev (@sergeykarayev), Superconductor
**Date**: 2026-07-02 (3:45 PM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~600 words
**Engagement**: 6 replies, 4 reposts, 45 likes, 61 bookmarks, 5.2K views

---

### Summary

Two people each asked separate AIs that knew nothing of each other, forcing the humans to act as couriers. Agents should be multiplayer: on the Superconductor team every coding-agent session lives in the cloud and any teammate can join it. Concrete wins: code review by joining the session (ask the agent "why'd you name it this way?" instead of the author), full-context handoffs (what's done, what's left, what the agent already tried), around-the-clock shift work across time zones, and team-level learning of agent best practices. Closing bet: the agentic company runs on shared cloud sessions — "more like Slack threads, less like terminal sessions."

### Relevance to dot-agents

Validates the orchestrator+workers+session-handoff-journal direction and the 'shared coordination backend > isolated-worktree status' lesson. The code-review-by-joining-the-session and handoff-with-what-the-agent-already-tried points are exactly what the session-handoff journal (episodic KG view) and merge-back artifacts are for; "the model is no longer the bottleneck. Coordination is" is the one-line thesis of the full-loop orchestration runtime.

### Body

Recently, my wife and I sat down to plan an upcoming trip. Naturally, we each asked an AI. Trouble was, I had my chat and she had hers, and they knew nothing about each other. So we served as couriers between chatbots: her idea pasted into my chat, my hotel booking screenshotted into hers, the itinerary reconciled by hand in a Google Doc.

I bring this up because your team probably works the same unfortunate way: each person in their own chat or coding agent session, with precious little shared.

I've been building software with the same set of people for over a decade. In the past year, we all got a superpower: coding agents that can do extremely impressive things. But each one (Claude Code, Codex, Cursor, etc.) was built for a single player. That's fine and dandy if you're vibe-coding your own little app. It's just you and Claude, and it's absolutely magical.

But put that same agent on a team and the magic fades quite a bit. The model is no longer the bottleneck. Coordination is. You don't know who's working on what. You can't see that an agent already tried the approach you're about to attempt, and abandoned it. You spend an hour re-deriving context that a teammate has, because it's trapped in their private chat.

Now let me tell you of a better way. On the Superconductor team, every coding agent session is in the cloud, open to anyone else on the team to join. What this enabled was transformative.

Code review improved first. My teammate reviews my work by joining the session I built it in. The session holds the full history of decisions, including the dead ends. Instead of Slacking me "why'd you name it this way?" she asks the agent. She gets her answer, and I never waste time answering. She also doesn't have to check out the branch locally — the live app preview in the cloud sandbox does the job.

Handoffs became easy. If I have to pass a feature to a teammate, he picks it up with full context: what's done, what's left, what the agent already tried. Nothing is trapped on my laptop, and the ball keeps rolling. And since agents don't sleep, a team spread across time zones can push forward around the clock. The old "shift" model of work might be making sense again.

Perhaps most importantly, we are continually learning best practices from each other. Every knowledge worker is facing the same question right now ("what's the best way to use these things?") and mostly figuring it out alone. Your teammates are quickly going to teach you more than any blog post.

And the utility of multiplayer AI goes beyond code. We now have an agent sitting in all of our team and customer meetings, reading the live transcript and taking actions while we talk. Another agent regularly checks on our progress against our stated goals and messages people as needed for clarifications.

My bet is that the agentic company of the future runs on shared cloud sessions. More like Slack threads, less like terminal sessions. The teams that pull ahead will place their agents where everyone can collaborate with them, instead of handing each person a genie with a lamp to hide it in.

### Key Quotes

> "The model is no longer the bottleneck. Coordination is."

> "You can't see that an agent already tried the approach you're about to attempt, and abandoned it."

> "The session holds the full history of decisions, including the dead ends."

> "More like Slack threads, less like terminal sessions."

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). Body verbatim and complete.
