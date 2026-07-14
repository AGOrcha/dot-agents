## Claude Code /usage significantly overestimates output tokens

**Source**: https://devforth.io/insights/claude-code-usage-significantly-overestimates-output-tokens/
**Author**: Ivan Borshchov, CEO & System Architect @ Devforth
**Date**: 2026-06-27
**Method**: WebFetch
**Word count**: ~1,200 words (structured extract; body served via WebFetch summary model)

---

### Summary

Claude Code's local token stats sum raw transcript rows rather than distinct API calls, significantly overcounting output tokens. On their real dataset (CC 2.1.195): `/usage` reported 5.1M Opus output tokens vs 1.88M deduplicated actual — 1,045 exact-duplicate groups spanning 3,042 events carrying 3.3M phantom output tokens. The decisive evidence is a live mitmproxy HTTP capture: one Sonnet response emitted two tool calls in a single stream with `output_tokens: 101`; the local transcript split it into two rows each claiming 101 (raw sum 202, actual API charge 101). Mechanism: streaming cumulative usage snapshots (`message_start`/`message_delta`) are copied onto one row per content block (thinking/text/tool_use), all sharing one `requestId` and one cumulative `usage` object. Scope is explicitly narrow: local stats pipeline only — no claim that Anthropic's server-side billing is wrong.

### Relevance to dot-agents

The strongest ground-truth corroboration yet for the transcript-analysis plan's CC token normalization (pareto erratum #2 → evidence-rubric clause): the HTTP capture proves the API charges once per `requestId`, validating dedup-by-requestId as matching real billing — not just internal consistency. Independently confirms our own re-derivation (2026-07-12 red-team RT-2: naive/deduped output overcount 3.02× on primary session files; last-entry = max in 100% of 33,194 dup groups) via a completely different method (traffic capture vs log statistics). Note the complementary claim directions: Gille (gille.ai) argues the *field values* undercount vs API (streaming placeholders); devforth proves *raw summation* overcounts (duplicate rows). Both resolve to the same operational rule — never sum raw CC usage rows; dedup by requestId, last entry wins.

### Body (structured extract)

**Key numbers (their dataset, Claude Code 2.1.195):**
- `/usage` report for Opus 4.8: 5.1M output tokens; deduplicated actual (their LetMeCode tool): 1.88M.
- Exact-duplicate detection: `identical_groups: 1045`, `identical_events: 3042`, `identical_output_delta: 3,300,770` — duplicates with identical `(requestId, message.id, all_usage_fields)` tuples.

**Mechanism (three contributing factors):**
1. Duplicate JSONL entries with identical metadata — same `requestId`, same `message.id`, same cumulative `usage` values across multiple rows (thinking blocks, text blocks, tool_use blocks).
2. Streaming snapshot duplication — the API sends cumulative usage snapshots in `message_start`/`message_delta`; the local client copies these cumulative totals onto one row per content block.
3. Multi-block responses stored as separate rows — a single API response containing two tool calls becomes two local rows, each inheriting the same final `usage` count.

**Ground-truth evidence (mitmproxy capture of `/v1/messages`):**
- One Sonnet response emitted two tool calls in a single HTTP stream with `output_tokens: 101`; the local transcript split it into two rows each claiming `output_tokens: 101`. Raw summation: 202; actual API charge: 101.
- Multiple tool calls within a single assistant turn share one `requestId` and receive one cumulative `usage` object — a new real inference call gets a new `requestId`.
- `~/.claude/stats-cache.json` matches simple addition of all `message.usage.output_tokens` fields, no deduplication.

**Fields involved:** `message.usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}`, top-level `requestId`, `message.id`.

**Explicit non-claims:** not claiming Anthropic's server-side billing is incorrect, nor that invoices use this method, nor that all dedup heuristics are automatically valid. Narrow claim: "On Claude Code 2.1.195, the local stats pipeline appears to overcount output tokens by a lot in this real-world dataset."

**Reproduction:** (1) compare `~/.claude/stats-cache.json` vs raw JSONL sums; (2) search exact duplicate usage signatures; (3) capture live HTTPS traffic via their tooling (github.com/devforth/claude-proxy-mitm).

**No fix proposed** beyond the operational rule: "any local-audit tool should not treat every local row as a separate API generation event."

### Key Quotes

> "On Claude Code 2.1.195, the local stats pipeline appears to overcount output tokens by a lot in this real-world dataset."

> "any local-audit tool should not treat every local row as a separate API generation event"

### Extraction Notes

Fetched 2026-07-12 via WebFetch; the body above is a faithful structured extract (WebFetch summarizes through a small model — exact numbers, field names, and both quotes were explicitly demanded and cross-checked for internal consistency; for byte-verbatim prose consult the URL). Complements gille-claude-code-jsonl-undercount-tokens.md (same substrate, different claim direction) and the 2026-07-12 independent re-derivation in the transcript-analysis plan's red-team review.
