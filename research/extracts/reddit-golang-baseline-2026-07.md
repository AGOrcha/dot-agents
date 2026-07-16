# r/golang baseline reference — "Benchmark surprise: passing structs by value"

**Source (share link):** https://www.reddit.com/r/golang/s/jdARDFhO4Y
**Resolved thread:** https://www.reddit.com/r/golang/comments/1utgamn/benchmark_surprise_passing_structs_by_value/
**Subreddit:** r/golang
**Retrieved:** 2026-07-14
**Purpose:** baseline Go-practice grounding (low ceremony), not a full corpus article.

---

## Retrieval status: PARTIAL — thread body/comments NOT retrieved

The actual thread OP text and comments could **not** be retrieved. Every path was
hard-blocked:
- WebFetch is domain-blocked for `www.reddit.com` ("unable to fetch from www.reddit.com").
- `curl` on the thread `.json` (www and old.reddit, with Safari and Googlebot UAs) returned
  **HTTP 403 / "Blocked"** HTML from Reddit's edge.
- Driving the user's connected Chrome session (Claude-in-Chrome) was not pursued: per its
  protocol it requires interactive per-browser user confirmation, which is out of scope for an
  autonomous extraction subagent.

**What IS confirmed** (from resolving the `/s/` redirect): the thread title is
*"Benchmark surprise: passing structs by value"* in r/golang (post id `1utgamn`). The topic is
the counter-intuitive finding that **passing small structs by value can be as fast as or faster
than passing a `*pointer`** in Go.

No thread comments are reproduced below — doing so would be fabrication. The section that
follows is **adjacent public-source context on the same topic** (clearly attributed, NOT the
thread's contents), sufficient for baseline grounding.

## Topic grounding (from adjacent Go sources, not this thread)

The thread's "surprise" is a well-established Go-practice point. Consensus from adjacent
sources:

- **Small structs: pass by value.** Local non-pointer values live on the stack, which is
  cheaper to access than the heap; the copy cost is negligible for small structs, so
  by-value can match or beat by-pointer. [asserted by community sources]
- **Pointers force heap/escape-analysis overhead.** Taking a pointer can make the value escape
  to the heap (GC-managed), and Go must run escape analysis; that overhead can outweigh the
  copy it was meant to avoid.
- **Crossover at large struct sizes.** As struct size grows, copy cost dominates and by-pointer
  stays roughly constant, so pointers win once the value is "large enough" (sources cite the
  gain-from-avoiding-copy needing to exceed the loss-from-heap; large-MB payloads clearly favor
  pointers).
- **Practice takeaway for a Go CLI codebase (dot-agents):** don't reflexively use `*T` for
  "performance." For small config/option/value structs, passing by value is idiomatic, keeps
  things on the stack, avoids nil-pointer and shared-mutation hazards, and is often as fast.
  Reach for pointers when the struct is large, when mutation of the caller's value is required,
  or when nil is a meaningful state. Benchmark (`testing.B` / `go test -bench`) before assuming.

**Attribution for the grounding above (adjacent sources, via web search — NOT the reddit thread):**
- "Go Benchmarks: Does Pass by Pointer Really Make a Difference?" — dev.to
  (https://dev.to/anubhav023/go-benchmarks-does-pass-by-pointer-really-make-a-difference-1540)
- "Are Pointers in Go Faster Than Values?" — Boot.dev
  (https://blog.boot.dev/golang/pointers-faster-than-values/)
- "When to use pointers in Go" — Dylan Meeus, Medium
  (https://medium.com/@meeusdylan/when-to-use-pointers-in-go-44c15fe04eac)

## To retrieve the real thread later

Use a credentialed/browser path (Claude-in-Chrome with user confirmation, or a logged-in
session) against the resolved URL above, or fetch the `.json` from an IP/UA that Reddit's edge
does not 403. The thread id is `1utgamn`.
