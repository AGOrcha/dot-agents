# Proposal: Python Bindings for dot-agents (compiled embed, JSON dispatch core)

**Status:** draft
**Created:** 2026-06-11
**Scope:** project-local (dot-agents)
**Related:** `[[api-conventions]]`, payout `luma-prototype-sprint` plan (first consumer: Luma voice agent harness)

## §1 Problem

The Luma voice-agent harness (payout `client-ai-poc/pi-client`, Python 3.13 on
laptop today, Raspberry Pi 5 at the bench) wants to use dot-agents commands
**natively** at runtime: per-journey-stage skill-file/config injection (the
`journey-stage-context` task), skill-registry reads, and eventually
workflow/KG queries from inside the agent loop. More broadly, any non-Go
consumer today has exactly one option: shell out to the `da` binary.

Subprocess works but has real costs in an interactive voice loop:

- **Per-call spawn latency** — tens of ms per invocation on an M4, worse on a
  Pi 5 over SD-card I/O. The Luma latency budget treats 50ms as material
  (human turn-gap bar ≈200ms).
- **No in-process state** — every call re-loads config/registry; no warm
  caches across calls.
- **Awkward composition** — streaming/iterative use means parsing stdout
  framing instead of calling a function.

Long-term, the Luma agent harness itself likely moves to Go (it does not use
the OpenAI Python SDK's features — the POC already speaks the Realtime API as
raw WebSocket JSON events — so the wire protocol can be ported directly, or
the official `openai-go` SDK used if its Realtime support checks out;
**verify before committing**). But the short-term / alternate route is Python
calling `da` natively, and other future consumers will want the same without
adopting Go.

## §2 Options

| Option | What | Pros | Cons |
| --- | --- | --- | --- |
| A. Subprocess `da --json` (status quo) | spawn per call | zero work, always in sync | spawn latency, no warm state, stdout framing |
| B. **c-shared embed + thin Python package** | `go build -buildmode=c-shared` → `libda.{dylib,so}` exporting a tiny C ABI; Python wraps with `cffi`/`ctypes` | in-proc (<~ms calls after init), warm caches, pythonic API, works macOS arm64 + Linux arm64 (Pi 5) | cgo build matrix, ABI discipline, GIL care |
| C. gopy-generated bindings | auto-generate per-package bindings | "free" API surface | fragile with modules/generics, huge surface to maintain, poor control |
| D. Daemon + local IPC (unix socket JSON-RPC) | long-running `da` service; clients in any language | amortizes spawn, language-agnostic, no cgo | daemon lifecycle/packaging, still IPC-hop latency, new failure modes |

## §3 Recommendation (phased)

**Phase 0 — JSON dispatch core (prerequisite, benefits everything):** factor a
single internal entrypoint `dispatch(commandPath, jsonArgs) -> jsonResult` that
the CLI command tree already routes through (or is refactored to route
through). This is the one stable boundary; CLI, embed, and any future daemon
(option D) all sit on it, so they cannot drift apart.

**Phase 1 — c-shared embed (option B):** export a minimal C ABI over the
dispatch core:

```c
// returns malloc'd JSON string; caller frees via da_free
char* da_call(const char* command_path, const char* json_args);
void  da_free(char* p);
const char* da_version(void);
```

**Phase 2 — Python package `dotagents`:** thin `cffi` wrapper, pythonic
surface:

```python
import dotagents
da = dotagents.Client(repo="/path/to/repo")   # loads libda once
out = da.call("kg.impact", {"file": "internal/realtime/hub.go"})
skills = da.call("skills.show", {"name": "drinks-stage"})
```

Blocking calls run fine under the GIL (release it around `da_call`); async
consumers wrap in `run_in_executor`. Ship platform wheels for
`macosx_arm64` and `manylinux_aarch64` (Pi 5) first; others on demand.

**Phase 3 (optional):** publish wheels to an internal index; consider option D
later for multi-process sharing — it reuses the same dispatch core.

## §4 First consumer / proving ground

Luma `journey-stage-context`: on stage transition (drinks → appetizers → …)
the harness pulls the stage's skill file + tool subset through
`da.call("skills.show", ...)`-style reads and injects into the realtime
session. Acceptance numbers come from the Luma latency harness (per-turn
JSONL): embed call p50 must be <5ms warm, vs subprocess baseline measured on
both M4 and Pi 5.

## §5 Non-goals

- Not exposing the full Go API surface — the ABI is `dispatch` only.
- Not rewriting any dot-agents logic in Python — the package is a façade.
- Not a daemon (option D) in this proposal; the dispatch core just keeps that
  door open.

## §6 Risks / open questions

- cgo cross-compile matrix (darwin/arm64, linux/arm64) in CI — build cost and
  toolchain pinning.
- ABI/version skew between `libda` and repo state — `da_version()` handshake +
  same-version check in the Python wrapper.
- Commands that expect a TTY/interactive confirm must hard-fail through
  dispatch with a structured error (`--yes` semantics only).
- Which command namespaces are embed-safe first? Propose: `skills.*`, `kg.*`
  (read-only) before any `workflow.*` mutation surface.

## §7 Acceptance

1. `dispatch` core in place; CLI behavior unchanged (existing CLI tests pass).
2. `libda` builds on darwin/arm64 + linux/arm64; `dotagents` package round-trips
   `kg.impact` and a skills read against a real repo from Python.
3. Warm-call p50 <5ms on M4 (measured), subprocess comparison documented.
4. Luma `journey-stage-context` consumes a stage skill file through the
   bindings on the Pi 5 bench.
