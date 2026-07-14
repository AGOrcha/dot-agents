## Context rot: the study that proves your million-token context window is lying to you

**Source**: https://x.com/0xCarnagee/status/2076269316912095422 (X-native article, id 2076247668959973376)
**Author**: Carnage (@0xCarnagee)
**Date**: 2026-07-12
**Method**: fxtwitter API (Tier-0 mirror, no login)
**Word count**: ~1900
**Engagement**: 82 likes, 159 bookmarks, 14 reposts, 13 replies, 9 quotes, ~93K views (point-in-time)

---

### Summary

Long-form X article arguing that large context windows degrade with length ("context rot") — a
structural property of autoregressive, unevenly-attentive attention, not a per-model bug. Anchored
on Chroma's mid-2025 controlled study (18 frontier models, 194,000 test calls) and cross-checked
against 2026 models (MRCR v2, RULER). Core claim: effective context is ~50-70% of advertised and
the middle of a long prompt is a "death zone"; the fixes are shorter/structured context,
early compaction, and better retrieval — not bigger windows.

---

### Body

**The demo→production trap (PART 1).** A 1M-window research agent fed ~320K tokens/call looks
flawless in demo, then in production silently cites the wrong quarter/source/attribution —
confidently. Not a prompt bug: "the window is big enough, the model is strong, and the output is
still wrong."

**Needle-in-a-haystack is a trick (PART 2-3).** The benchmark labs brag about is a *lexical* match
(question and needle share words). When Chroma rebuilt it to need one small inference ("which
character has been to Helsinki?" / needle "Yuki lives next to the Kiasma museum") all 18 models
degraded as context grew — same question, same answer, more surrounding tokens → worse. "the model
gets dumber as the window fills, even when the task never changes."

**The numbers (PART 4).** (1) Every model degraded with length; 1M models stopped behaving like 1M
past ~200K, 200K models rolled over by 80-100K. (2) Middle death zone: key fact in positions 5-15
of a 20-doc context drops accuracy 30+ pp ("lost in the middle," worse than 2023 Stanford, not
improved with scale). (3) Length-only floor: even with all distractors stripped, accuracy fell
**7.9% from length alone** — "only a shorter prompt" helps, not better RAG or cleaner context.

**2026 still pays the tax (PART 5).** Effective context lands at 60-70% of advertised across every
model benchmarked; 13 models ship 1M+ windows, none use the full window reliably. MRCR v2 at 1M:
Opus 4.6 holds 76%, Gemini 3 Pro (10M advertised) collapses to 24.5%; **Opus 4.7 REGRESSED to 32%
at 1M** and Anthropic's own system card says keep 4.6 as the fallback for multi-needle retrieval.

**Shuffled beats coherent (PART 6 — breaks the RAG playbook).** Past 32K tokens, across all 18
models, a *shuffled* haystack beat a *coherent* one — "destroying the logic improved performance."
Coherent prose gives attention a plausible alternative story to latch onto; the model can't tell a
coherent summary of junk from a coherent answer, and a clean summary that reads better to a human
reviewer reads better to the model too (which is why it loses). Position barely mattered; length
mattered. Cuts against Anthropic's RAG guide (put retrieved results early).

**Distractors (PART 7).** Same distractors do more damage as the window grows — "not how much
noise you add, it's how much room you give it to spread." And the families **fail in opposite
ways**: GPT models hallucinate (confident and wrong); Claude models refuse/give-up (claim no answer
exists even when it's present). "one lies to you. the other gives up. both get worse as the window
fills, and neither is safe to trust at length."

**Unfixable at the mechanism level (PART 8).** A "repeated words" copy task (repeat "apple" a few
hundred times, swap one for "apples," copy back) failed — models stopped early, over-generated, or
emitted garbage. Because models are autoregressive, every generated token becomes input for the
next: **output tokens become input tokens**, so longer output = more self-rot, and a bigger window
makes it worse.

**RAG is not dead (PART 9).** For 1M models the drop kicks in ~300-400K, long before the ceiling;
RULER puts effective context at ~50-65% of advertised (lower for multi-hop and code). Real working
room: a 200K model ≈ 100-130K, a 1M model ≈ 500-650K. "long context didn't kill retrieval. it
killed lazy retrieval." Retrieve better, don't stop.

**What to do this week (PART 10).**
1. Set a working cap at **25-30% of the advertised window**, in config (1M → ~280K ceiling).
2. Run a **needle-in-the-middle eval on real prompts**: 30 questions, answer at positions 1/5/10/15/20; a >20-pt drop between 5 and 15 means fix retrieval first.
3. Replace one paraphrased/summarized context block with **raw chunks + "Source N" headers**; compare on 50 inputs — messy usually wins.
4. **Compact early, not at the limit**: trigger summarization at 60% of the working cap, keep the last few turns verbatim, re-summarize the summary so it doesn't bloat.
5. Read your agent's context as data — the rot is visible in the inputs.

**The shift (PART 11).** "The window is a ceiling, not a promise... the teams shipping reliable
agents in 2026 aren't the ones with the biggest windows. they're the ones whose effective context
is small, structured, and held under 30% of advertised... The window says a million. build like it
says a few thousand."

---

### Key Quotes

> "same question. same answer. more tokens around it. worse performance. that's context rot in one line: the model gets dumber as the window fills, even when the task never changes."

> "even with perfect retrieval, longer prompts cost you accuracy. better RAG doesn't save you. cleaner context doesn't save you. only a shorter prompt does."

> "across all 18 models, the shuffled haystack beat the coherent one. destroying the logic improved performance... the model can't tell a coherent summary of junk from a coherent answer."

> "GPT models hallucinated, confident and wrong. Claude models did the reverse. faced with ambiguity they'd often refuse and claim no answer exists... one lies to you. the other gives up."

> "output tokens become input tokens... it's choking on what it just said. and a bigger window only makes it worse."

> "set a working cap at 25 to 30 percent of your model's advertised window. put it in config."

---

### Extraction Notes

Full X-native article body recovered via the fxtwitter Tier-0 mirror (no login). All numbers
(Chroma 18 models / 194K calls, MRCR v2 %s, RULER 50-65%, 7.9% length floor, 25-30% cap) are the
author's summary of Chroma/RULER/MRCR studies — treat as [UNVERIFIED] pending the primary Chroma
"context rot" report and the RULER/MRCR sources. Quote-tweeted by 0xCodez (see
`langchain-context-engineering-compaction.md`).
