## Claude Code's Token Counts Are Wrong — 100x Off in the JSONL Logs

**Source**: https://gille.ai/en/blog/claude-code-jsonl-logs-undercount-tokens/
**Author**: Magnus Gille
**Date**: 2026-02-24
**Method**: WebFetch
**Word count**: ~1350 words

---

### Summary

Gille shows that Claude Code's JSONL conversation logs are unusable for token accounting: `usage.input_tokens` is a streaming placeholder (75% of entries are 0 or 1, never finalized), producing 100–174x undercounts on input and 10–17x on output, while the cache fields (`cache_read_input_tokens`, `cache_creation_input_tokens`) match the accurate statusbar totals at ~1x. Output is additionally off because JSONL records only visible output and excludes thinking tokens, which the API bills as output. He verified via an independent dedup parser, a statusbar-payload validation harness, and exact-token reconciliation against the Anthropic billing dashboard, and filed it as claude-code issue #28197.

### Relevance to dot-agents

Direct hit on the transcript-analysis pipeline: any token accounting built on Claude Code session JSONL `usage` fields will be 1–2 orders of magnitude low on input/output unless it (a) dedups by `requestId`, (b) treats input/output fields as placeholders, and (c) trusts only the cache fields. Per-iteration evidence and cost attribution in our pipeline should either source from statusline `context_window` cumulative totals or explicitly flag JSONL-derived input/output numbers as lower bounds. Note the finding is marked preliminary by the author.

### Body

> **PRELIMINARY RESULT — PENDING REVIEW** This post documents findings that are still being reviewed. The data and methodology are published openly, but treat the conclusions as preliminary until independent verification is complete.

Every tool that reads Claude Code's JSONL conversation logs for token accounting is working with bad data. The `usage.input_tokens` field is a streaming placeholder — 75% of entries are 0 or 1, never updated to the real value. The result: input tokens undercounted by 100–174x, output tokens by 10–17x.

I discovered this while building an [energy monitor](https://github.com/Magnus-Gille/claude-code-energy-monitor) for Claude Code. The monitor reads the statusbar's `context_window` totals (the accurate source), but I wanted to cross-check against JSONL to validate my numbers. What I found instead was that the JSONL data is essentially unusable for token accounting — and every community tool that relies on it, including [ccusage](https://github.com/ryoppippi/ccusage), is affected.

I've [filed this as a feature request](https://github.com/anthropics/claude-code/issues/28197) with Anthropic. Here's the full investigation.

#### The data

Claude Code has two separate data paths for token usage. **JSONL conversation logs** are written to disk (`~/.claude/projects/*/`) during streaming — these are what tools like ccusage read. **Statusbar context** is a JSON payload piped to [statusline scripts](https://docs.anthropic.com/en/docs/claude-code/status-bar) on every status update, containing cumulative totals that Claude Code maintains internally from finalized API responses. Same API calls, same process, two different recording mechanisms with very different fidelity.

I use Claude Code on a MAX subscription during normal workdays — not API credits. I compared JSONL token sums (deduplicated by `requestId`) against statusbar cumulative totals across two full days of Opus 4.6 usage.

**Feb 20 — heavy day, 20 sessions, 1,365 unique API requests:**

| Metric | JSONL (dedup) | Statusbar | Ratio |
|--------|---------------|-----------|-------|
| Input tokens | 41,444 | 7,199,162 | **174x** |
| Output tokens | 183,829 | 3,208,365 | **17x** |
| Cache read | 104,353,324 | 114,798,863 | **1.1x** |
| Cache creation | 3,170,696 | 2,717,775 | **0.9x** |

**Feb 24 — moderate day, 12 sessions, 1,228 unique requests:**

| Metric | JSONL (dedup) | Statusbar | Ratio |
|--------|---------------|-----------|-------|
| Input tokens | 11,758 | 1,193,366 | **102x** |
| Output tokens | 69,449 | 748,337 | **11x** |
| Cache read | 74,254,777 | 67,710,877 | **0.9x** |
| Cache creation | 2,817,739 | 2,003,545 | **0.7x** |

Look at the cache columns. Cache read and cache creation match at roughly 1x across both days. This is the critical cross-check: **both data sources are observing the same set of API calls.** The discrepancy isn't about missing log entries — it's about what values are recorded for each entry.

Input tokens are 100–174x off. Output tokens are 10–17x off. But cache metrics are fine.

##### The smoking gun: 75% placeholders

I built an [independent JSONL parser](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/sum_jsonl.py) (zero dependencies, ~200 lines of Python) to inspect the raw data. The distribution of `usage.input_tokens` values tells the story:

**75% of all JSONL entries have `usage.input_tokens` of 0 or 1.** These are streaming placeholder values. Claude Code writes JSONL entries during streaming, when the input token count hasn't been finalized yet. The placeholder is never updated after the request completes.

The JSONL also contains streaming duplicates: the same `requestId` appears 2–10 times with identical placeholder values. Across the two days I analyzed, 51–55% of all entries were duplicates.

#### Root cause: streaming architecture

Claude Code's JSONL logging happens during streaming, not after request completion. Here's the sequence:

1. **Request starts** → JSONL entry created with `usage.input_tokens = 1` (placeholder)
2. **Tokens stream in** → Additional JSONL entries for the same `requestId`, with `usage.output_tokens` incrementing
3. **Request completes** → `usage.input_tokens` is never updated to the final value

Cache fields (`cache_read_input_tokens`, `cache_creation_input_tokens`) appear to be set correctly from the start — they're available in the initial API response before streaming begins, which explains why they match at ~1x while input tokens don't.

The streaming duplicates compound the problem. A single request generates 2–10 JSONL entries with the same `requestId`. Any parser that doesn't deduplicate will multiply the placeholder errors.

##### Which fields are reliable vs. placeholders

| JSONL field | Status | Notes |
|-------------|--------|-------|
| `usage.input_tokens` | **Placeholder** | Usually 0 or 1. Never updated. |
| `usage.output_tokens` | **Partial** | Also affected by streaming placeholders (per-session ratios range from 3x to 701x) and excludes thinking tokens. |
| `usage.cache_read_input_tokens` | Accurate | Matches statusbar at ~1x. |
| `usage.cache_creation_input_tokens` | Accurate | Matches statusbar at ~1x. |

#### The output gap: thinking tokens

Even if `usage.input_tokens` were fixed, output tokens would still be 10–17x off. Two factors compound here. First, `usage.output_tokens` is also affected by streaming placeholders — not as severely as input, but per-session ratios range from 3x to 701x, showing real variability. Second, JSONL's `usage.output_tokens` excludes extended thinking (chain-of-thought) tokens, while the statusbar's `total_output_tokens` includes them.

Anthropic's [adaptive thinking documentation](https://docs.anthropic.com/en/docs/build-with-claude/adaptive-thinking) classifies thinking as output tokens for billing: "Tokens used during thinking (output tokens)." There is no separate `thinking_tokens` field in the API `usage` object — thinking is counted within `output_tokens` at the API level.

But JSONL records only the visible output. For Opus, where roughly 60–70% of output tokens are thinking, the thinking exclusion alone accounts for ~3x. Combined with the streaming placeholder effect on output (~3–6x), the total 10–17x gap is expected.

I confirmed this with a [validation harness](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/analyze_tokens.py) that logged raw statusbar payloads across 31 API calls: the statusbar's `total_output_tokens` matched the API's `usage.output_tokens` at a 1.0x ratio — both include thinking. JSONL just records something different.

#### Impact

To put the discrepancy in concrete terms: **ccusage reported 225k total tokens for a day where the statusbar recorded 10.4M.** That's input + output combined — a 46x gap. Any JSONL-based monitoring tool will underreport by 1–2 orders of magnitude.

This isn't a bug in ccusage or any other tool — they correctly sum what's in the JSONL. The data they're reading is the problem. If you're making decisions about which model to use, how to optimize your workflow, or how much compute your team is consuming based on JSONL-derived numbers, those decisions are based on data that's 100x too low on input and 10x too low on output.

#### The fix

What Anthropic could do:

1. **Write final `usage` values.** After a request completes, update the JSONL entry — or append a final entry — with the real `input_tokens` and `output_tokens` (including thinking).

2. **Deduplicate streaming entries.** Either log only the final state per request, or add a `"final": true` marker so parsers know which entry to use.

A simpler alternative: a separate append-only usage log with one line per completed request:

```
{"ts":1740000000,"requestId":"req_...","model":"opus","input_tokens":1234,"output_tokens":567,"cache_read":50000,"cache_creation":800}
```

**Current workaround:** Read the statusbar's `context_window` totals via a custom [statusline script](https://github.com/Magnus-Gille/claude-code-energy-monitor). This gives accurate session-level aggregates, but not per-call breakdowns.

#### How I verified this

The conclusions rest on three independent lines of evidence:

1. **Independent JSONL parser** ([sum_jsonl.py](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/sum_jsonl.py)): Zero dependencies, reads raw JSONL, deduplicates by `requestId`. Reproduced the same pattern across two full days of data.

2. **Validation harness** ([analyze_tokens.py](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/analyze_tokens.py)): Logged every raw statusbar JSON payload across 31 API calls in 3 concurrent sessions. Confirmed `total_input_tokens` excludes cache (no double-counting), `total_output_tokens` includes thinking (1.0x ratio to API), and `current_usage.input_tokens` is always 1 (placeholder).

3. **API billing reconciliation** ([FINDINGS.md](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/FINDINGS.md)): 4 direct Anthropic API calls with a personal key. All four token categories (fresh input, output, cache read, cache write) matched the billing dashboard CSV to the exact token. Cost: $0.01.

The full evidence chain is documented in [FINDINGS.md](https://github.com/Magnus-Gille/claude-code-energy-monitor/blob/master/FINDINGS.md). All tools are in the [repo](https://github.com/Magnus-Gille/claude-code-energy-monitor) — MIT licensed, stdlib-only Python.

---

_This finding came out of building a [real-time energy monitor](https://github.com/Magnus-Gille/claude-code-energy-monitor) for Claude Code. The monitor reads statusbar totals (the accurate source) and estimates compute energy per session and per day. If you're curious about how much energy your AI-assisted coding actually consumes, check it out._

_Filed as [GitHub issue #28197](https://github.com/anthropics/claude-code/issues/28197)._

### Key Quotes

> "Every tool that reads Claude Code's JSONL conversation logs for token accounting is working with bad data."

> "75% of all JSONL entries have `usage.input_tokens` of 0 or 1. These are streaming placeholder values."

> "This isn't a bug in ccusage or any other tool — they correctly sum what's in the JSONL. The data they're reading is the problem."

> "ccusage reported 225k total tokens for a day where the statusbar recorded 10.4M."

### Extraction Notes

The URL supplied had a typo in the slug (`jsonl-1ogs`, digit one for the letter l) which resolved to the site homepage. Correct slug confirmed from the blog index: `claude-code-jsonl-logs-undercount-tokens`. Post carries an explicit "PRELIMINARY RESULT — PENDING REVIEW" banner. Companion code documented in `gille-claude-code-energy-monitor-scripts.md`.
