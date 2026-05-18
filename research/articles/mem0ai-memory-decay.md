## mem0 — Why Agent Memory Needs a Sense of Time: Recency-Aware Ranking with Memory Decay

**Source**: https://x.com/mem0ai/status/2052770549307498535
**Author**: mem0 (@mem0ai) — Verified
**Date**: 2026-05-08
**Method**: Playwright
**Word count**: ~900 words
**Engagement**: 7 replies, 12 reposts, 60 likes, 79 bookmarks, 5K views

---

### Summary

Mem0 introduces Memory Decay: a per-project toggle that applies recency-aware ranking to agent memory search. Every memory tracks its last 20 access timestamps; at search time, recently-accessed memories get a scaling boost (up to 1.5×) and idle memories get dampened (floor 0.3×) — a 5× spread. Nothing is deleted or hidden; stale memories still surface when genuinely relevant, just ranked lower. This is a search-time concern only; no reindexing required. Roadmap: category-aware weighting and per-project auto-tuning.

---

### Body

Most agent memory systems are built to remember. But long-running agents need something harder: knowing what still matters. Memory without time becomes noise.

**AI memory has a freshness problem.** The longer an AI app runs, the more it remembers. A breakfast order from this morning, a project from last week, and a preference from six months ago can all sit in memory with the same weight. For short demos, fine. For long-running agents, it gets noisy.

**Memory Decay is the fix.** It's a soft re-rank, not a filter. Scaling factor range: 0.3× (stale) to 1.5× (recently accessed) — a 5× spread. Strong matches still win even when old. Nothing gets hidden, nothing deleted.

**How it works:**
- Each memory tracks up to its last 20 access timestamps
- At search time, the candidate pool widens slightly so the scaling factor has room to reorder before truncation
- Public score stays clamped to [0, 1] so existing API contracts hold
- Reinforcement runs fire-and-forget on a bounded executor; search latency unchanged
- For memories predating the toggle, last-update timestamp serves as fallback — fair starting point on first search, then accumulates access history naturally

**How to enable:** Per-project toggle via dashboard or SDK:
```python
client.project.update(decay=True)
client.search("what does the user want for breakfast?", user_id="alice")
```

**Where it helps:**
- Coding agents: keep current sprint context on top, not details from old side projects
- Personal assistants: prioritize recent behavior, not one-off events from a year ago
- Support bots: surface recent tickets first, not resolved issues from last year

**Roadmap:**
- Category-aware weighting — facts tagged `health` carry more weight than `misc`; important categories shouldn't be dampened the same way as noise
- Per-project auto-tuning — Mem0 learns aggressively to scale based on actual access patterns; fixed band replaced by one that fits each workload

---

### Key Quotes

> "Memory without time becomes noise."

> "History stays in memory. The present gets ranked correctly."

> "Nothing your customer ever wrote disappears."
