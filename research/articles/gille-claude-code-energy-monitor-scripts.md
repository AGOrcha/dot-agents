## claude-code-energy-monitor — analyze_tokens.py & sum_jsonl.py (reference)

**Source**: https://github.com/Magnus-Gille/claude-code-energy-monitor
**Author**: Magnus Gille (Magnus-Gille)
**Date**: Unknown (repo active as of 2026-02; companion post dated 2026-02-24)
**Method**: WebFetch
**Word count**: ~1400 words (reference doc; scripts total ~200–400 lines Python)

---

### Summary

Companion code to Gille's "Claude Code's Token Counts Are Wrong" post. `sum_jsonl.py` is an independent, zero-dependency parser that sums token usage from Claude Code's JSONL conversation logs (with `requestId` dedup and placeholder detection) and compares against statusline history. `analyze_tokens.py` is a validation harness that replays statusline debug payloads to settle two semantics questions: whether `total_input_tokens` double-counts cache creation (it does not) and whether `total_output_tokens` includes thinking tokens (it does). The parent repo is a statusline script that tracks token totals and order-of-magnitude energy estimates per session/day.

### Relevance to dot-agents

This is the reference implementation for exactly the JSONL parsing our transcript-analysis pipeline does: which `message.usage` fields to read, why `requestId` dedup (last-entry-wins) is mandatory, how to detect placeholder values (<=1), and which fields are trustworthy (cache read/creation) versus placeholder (input) or partial (output, excludes thinking). The Q1/Q2 delta methodology in analyze_tokens.py is a reusable pattern for validating any token-accounting source against another.

### Body

#### Repo context (README)

`claude-code-energy-monitor` is a statusline script (`statusline.py`, stdlib-only Python 3, macOS/Linux/WSL) that displays the active model, context utilization, API-quota consumption over rolling windows, and daily/weekly/monthly token totals with energy estimates, distinguishing cached from fresh tokens. State accumulates in a locked JSON file at `~/.claude/statusline_daily.json`, archived to a history file at midnight. Energy is an explicit order-of-magnitude proxy ("±3x uncertainty") with per-token constants:

- Fresh input: 390 mWh / 1k tokens
- Output (decode): 1,400 mWh / 1k tokens
- Cached reads: 15 mWh / 1k tokens
- Cache writes: 490 mWh / 1k tokens

README framing: "The energy numbers shown in the statusbar are order-of-magnitude estimates, not measurements." Token counting is validated against actual API payloads (see FINDINGS.md — 4 direct API calls reconciled to the billing CSV to the exact token). Companion utilities in the repo: `codex_status.py` (same idea for Codex CLI rollout logs), `stepcount.py` (shareable usage summaries), `codex_with_summary.py` (wrapper printing a summary after Codex exits).

#### sum_jsonl.py — independent JSONL token summer

Module docstring (verbatim):

```python
"""Independent JSONL token summer — no ccusage dependency.

Reads Claude Code's raw JSONL conversation logs and sums token usage
from assistant messages. Includes subagent files.

Usage:
  python3 sum_jsonl.py                    # today
  python3 sum_jsonl.py 2026-02-20         # specific date
  python3 sum_jsonl.py 2026-02-20 --all   # all dates in that project dir

Compares against statusline_history.jsonl / statusline_daily.json
if available.
"""
```

**File discovery.** Recursively globs every `*.jsonl` under `~/.claude/projects/`, skipping dotfiles; subagent transcripts are included and identified by path (`"subagents" in str(f)`):

```python
PROJECTS_DIR = CLAUDE_DIR / "projects"

def find_jsonl_files():
    """Find all session and subagent JSONL files."""
    files = []
    if not PROJECTS_DIR.exists():
        return files
    for jsonl in PROJECTS_DIR.rglob("*.jsonl"):
        # Skip non-conversation files
        if jsonl.name.startswith("."):
            continue
        files.append(jsonl)
    return files
```

**Entry filtering and field access.** Only `type == "assistant"` entries carry usage. Fields read per entry (verbatim):

```python
if entry.get("type") != "assistant":
    continue

msg = entry.get("message", {})
usage = msg.get("usage")

timestamp = entry.get("timestamp", "")
session_id = entry.get("sessionId", "unknown")
is_subagent = entry.get("isSidechain", False)
request_id = entry.get("requestId", "")

"input_tokens": usage.get("input_tokens", 0),
"output_tokens": usage.get("output_tokens", 0),
"cache_read": usage.get("cache_read_input_tokens", 0),
"cache_creation": usage.get("cache_creation_input_tokens", 0),
```

So the JSONL schema it depends on: top-level `type`, `timestamp`, `sessionId`, `isSidechain`, `requestId`; nested `message.usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}`.

**Dedup — the load-bearing step.** Streaming writes 2–10 entries per API request with the same `requestId`; the parser keeps the last entry per request, then sums over the dedup map:

```python
by_request = {}
for m in msgs:
    rid = m["request_id"]
    if rid:
        by_request[rid] = m  # last entry wins

dedup_in = sum(m["input_tokens"] for m in by_request.values())
```

**Placeholder detection.** Values <= 1 are treated as streaming placeholders:

```python
placeholder_input = sum(1 for m in by_request.values()
                        if m["input_tokens"] <= 1)
placeholder_output = sum(1 for m in by_request.values()
                         if m["output_tokens"] <= 1)
```

**Statusline comparison.** After aggregation, dedup sums are compared against `statusline_history.jsonl` / `statusline_daily.json` totals as ratios (`in_ratio = sl_in / dedup_in if dedup_in > 0 else float("inf")`), with diagnostic output naming the two known gap causes (verbatim strings):

```python
print(f"    Input {in_ratio:.0f}x   => JSONL usage.input_tokens are mostly placeholders (1)")
print(f"    Output {out_ratio:.1f}x => Gap likely = thinking tokens + missing intermediate calls")
```

#### analyze_tokens.py — statusline-semantics validation harness

Module docstring (verbatim):

```python
"""Analyze statusline debug logs to validate token-counting semantics.

Answers two key questions:
  1. Does total_input_tokens include cache_creation_input_tokens? (double-counting?)
  2. Does total_output_tokens include thinking tokens?

Usage:
  1. Enable debug logging:  export ENERGY_DEBUG=1
  2. Use Claude Code normally for a session (a few exchanges is enough)
  3. Run:  python3 analyze_tokens.py

The script reads ~/.claude/statusline_debug.jsonl and prints a report.
"""
```

**Input.** `DEBUG_FILE = Path.home() / ".claude" / "statusline_debug.jsonl"` — raw statusline JSON payloads captured when the statusline script runs with `ENERGY_DEBUG=1`.

**Payload fields read.** Each debug entry wraps the raw statusline payload; the harness reads the cumulative totals from `context_window` and the per-call breakdown from `context_window.current_usage`:

```python
raw = e.get("raw", {})
sid = raw.get("session_id", "unknown")        # session grouping

ctx = raw.get("context_window", {})
cu = ctx.get("current_usage") or {}
total_in = ctx.get("total_input_tokens", 0)
total_out = ctx.get("total_output_tokens", 0)

call = {
    "cu_input": cu.get("input_tokens", None),
    "cu_output": cu.get("output_tokens", None),
    "cu_cache_read": cu.get("cache_read_input_tokens", 0),
    "cu_cache_create": cu.get("cache_creation_input_tokens", 0),
}
```

**API-call detection.** Entries are grouped by `session_id` and sorted by timestamp; a new API call is inferred whenever the cumulative input total increases, and the call's true cost is the delta of the cumulative counters:

```python
if total_in > prev_total_in:
    call_num += 1
    delta_in = total_in - prev_total_in
    delta_out = total_out - prev_total_out
```

**Q1 — does `total_input_tokens` double-count cache creation?** For each detected call, compare the cumulative input delta against `current_usage.input_tokens` alone vs input + cache_creation, with tolerance `tol = max(delta * 0.05, 10)`:

```python
if abs(delta - cu_in) <= tol:
    match_fresh += 1
elif abs(delta - (cu_in + cu_cc)) <= tol:
    match_plus_cc += 1
else:
    match_neither += 1
```

Finding (per the blog post): deltas match fresh input alone — no double-counting of cache creation.

**Q2 — does `total_output_tokens` include thinking tokens?** Filter to finalized calls (`cu_output > 10 and delta_out > 10`, cu_output not None), then compute the ratio of cumulative-output delta to the per-call `current_usage.output_tokens`:

```python
ratios = [c["delta_out"] / c["cu_output"] for c in finalized_output]
avg_ratio = sum(ratios) / len(ratios)
```

Interpretation band: `0.95 <= avg_ratio <= 1.05` means the statusline's cumulative output matches the API's `usage.output_tokens` — i.e. both include thinking tokens (Anthropic bills thinking as output; there is no separate thinking_tokens field). The blog reports 1.0x across 31 calls. A caveat noted by the harness: `current_usage.input_tokens` in the statusline payload is itself always 1 (placeholder), which is why Q1 relies on deltas of the cumulative totals rather than the per-call field.

#### Token-accounting methodology (synthesis)

1. **Two sources, different fidelity.** JSONL conversation logs are written during streaming and never finalized; statusline `context_window` totals are maintained internally from finalized API responses and reconcile with API billing.
2. **JSONL is usable only with dedup + field discipline.** Dedup by `requestId` (last wins); trust `cache_read_input_tokens` / `cache_creation_input_tokens`; treat `input_tokens` as placeholder (<=1 in ~75% of entries) and `output_tokens` as a lower bound (streaming-partial, excludes thinking).
3. **Cross-source validation by deltas.** When a source only exposes cumulative counters, detect calls by counter increase and validate semantics by comparing deltas against a per-call breakdown, with a relative+absolute tolerance (5% or 10 tokens).
4. **Pricing/energy constants** live in the statusline script, not these two analysis scripts: mWh-per-1k-token constants above; no dollar pricing table in the analysis scripts themselves.

### Key Quotes

> "Independent JSONL token summer — no ccusage dependency."

> "1. Does total_input_tokens include cache_creation_input_tokens? (double-counting?) 2. Does total_output_tokens include thinking tokens?"

> "The energy numbers shown in the statusbar are order-of-magnitude estimates, not measurements."

### Extraction Notes

Body is a structured reference (per task brief), not verbatim prose. Code excerpts were extracted via WebFetch prompts against the raw files with instructions to quote verbatim; field-name strings and constants were requested exactly, but line-level completeness is not guaranteed (WebFetch summarizes through an intermediary model — for byte-exact needs, pull the raw files: https://raw.githubusercontent.com/Magnus-Gille/claude-code-energy-monitor/master/analyze_tokens.py and .../sum_jsonl.py). The repo also contains FINDINGS.md (full evidence chain, API billing reconciliation) which was referenced but not extracted. MIT licensed, stdlib-only.
