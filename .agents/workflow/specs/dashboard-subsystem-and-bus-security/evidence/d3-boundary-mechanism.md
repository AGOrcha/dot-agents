# Evidence sidecar — D3 slim-runtime boundary mechanism

Fork owner: spec `dashboard-subsystem-and-bus-security` D3 · plan task `t3-boundary-mechanism-decision`.
Resolved via ideation-cycle (2026-07-18). This sidecar is linked from the spec D3 decision;
it is NOT inlined into the spec body.

## The fork

Given D1/D2 (dashboard is a `da` subsystem over one shared core) and D3's goal (a slim,
separately-deployable prod runtime whose import graph excludes the fat agent command
surface), **which boundary mechanism** produces that slim runtime?

- Sub-fork A (empirically-testable): does a **dedicated minimal entrypoint** actually
  isolate the runtime import graph from the fat surface?
- Sub-fork B (judgment-call): **entrypoint vs build-tag vs separate-module** — which
  mechanism, given all three can in principle isolate, at what maintenance/enforcement
  trade-off?

## Classification

- Sub-fork A → empirically-testable (software/infra measurement). Evidence form:
  `go list -deps` import-graph breadth + `go build` binary size. Negative control: the
  fat `cmd/da` binary (must exhibit the large graph the runtime must shed).
- Sub-fork B → judgment-call (trade-off across maintenance cost and enforcement strength;
  no single run decides it) → cross-brain trade-off pass, informed by A's data.

## Pre-registration (sub-fork A)

- **Hypothesis:** a dedicated minimal entrypoint importing only the dashboard core yields a
  runtime whose import graph excludes the fat command surface (commands/*, internal/kg,
  internal/graphstore, internal/adapters) and is materially smaller than the fat binary.
- **Prediction (pre-committed):** entrypoint binary < 1/2 the fat binary; entrypoint pulls
  none of {commands/kg, commands/workflow, commands/worktree, commands/config, commands/eval,
  internal/kg, internal/graphstore, internal/adapters}.
- **Negative control:** the fat `cmd/da` — must show the large graph + all fat packages
  reachable (proves the measurement can see the surface the runtime must exclude).
- **Discrimination/power:** binary-size delta and per-package reachability are direct,
  non-ceilinged signals (36 MB vs 11 MB leaves ample headroom; not a saturated instrument).

## Measurement (real toolchain, GOFLAGS=-buildvcs=false, origin/master @ 039bbb55)

| Target | internal pkgs (`AGOrcha/dot-agents/*`) | total deps | binary size |
|---|---|---|---|
| Negative control — fat `cmd/da` | 55 | 493 | 36.0 MB |
| Mechanism 2 — dedicated entrypoint (`cmd/da-dashboard`, exists today) | 10 | 211 | 11.1 MB |

Per-package exclusion (mechanism 2): `commands/{kg,workflow,worktree,config,eval}`,
`internal/{kg,graphstore,adapters}` are ALL excluded from the entrypoint's dep graph.
Result: **-69% binary, -82% internal packages, -57% total deps**; fat surface fully shed.

## Fidelity self-audit (5 checks × 4 discrimination levels)

1. Faithful inputs — real `cmd/da` / `cmd/da-dashboard`, real module graph, real builds. ✓
2. Negative control — the fat binary is the real control; it exhibits the large graph the
   runtime must shed, and the measurement DID see all fat packages in it. ✓
3. Real execution — real `go list -deps` + `go build`. ✓
4. No hidden losses — the entrypoint's 10 retained internal pkgs are `internal/dashboard/*`
   + their transitive deps (reported, not hidden). ✓
5. Independent cross-harness audit (GATE 2) — PENDING codex adversarial pass (below).

Discrimination: (1) instrument catches the difference (clear size/pkg delta); (2) effect
can occur (per-package exclusion verified); (3) regime — CAVEAT: binary size + package
count are a **proxy** for "attack surface"; true network-reachable surface is narrower and
not directly measured here; (4) power — 36 vs 11 MB, no ceiling.

## Preliminary conclusion (scoped)

- Sub-fork A: SETTLED — a dedicated minimal entrypoint achieves the isolation goal
  empirically (the current `cmd/da-dashboard` already demonstrates it).
- Sub-fork B: recommended default = **dedicated entrypoint + an import-guard** (cement the
  measured boundary at near-zero maintenance cost); build-tag adds tag-discipline fragility
  for no measured gain; separate-module adds submodule maintenance for enforcement an
  import-guard already provides. PENDING cross-brain trade-off pass.

## Faithful-runtime re-measurement (self-driven confound close)

The current `cmd/da-dashboard` is the STANDALONE dashboard (fswatch source) — not the
real D3 target, which is the R3-mounted runtime = `internal/service` hosting the
`internal/dashboard` mount. Re-measured that faithful target directly:

| Target | internal pkgs | total deps | fat surface excluded? |
|---|---|---|---|
| `internal/service/...` + `internal/dashboard/...` | 16 | 220 | YES — all of commands/{kg,workflow,worktree,config,eval} + internal/{kg,graphstore,adapters} excluded |

It pulls only internal/{service(7), dashboard(5), scoring, review, fsops, agentslock}. So
isolation holds for the FAITHFUL prod runtime, not just the toy standalone — the primary
confound (is the standalone representative?) is closed: even service+dashboard sheds the
fat surface. This strengthens sub-fork A.

## GATE 2 — cross-harness audit verdict (GPT, cross-family)

Codex-exec was quota-blocked (`resource_exhausted` ×2); the gate was instead run via the
available cross-family GPT model (`completion(model="slow")`) — a genuinely different model
family than the Claude author, satisfying the cross-harness requirement. Owner-ratified
2026-07-18.

**Sub-fork A — NOT-SOUND as an "attack-surface settled" claim; SOUND only as "static import
isolation demonstrated provisionally."** Decisive flaws the gate raised:
- package-count + binary-size are an invalid proxy for attack surface — one small package
  (HTTP router, deserializer, template engine, reflection, cgo, known-CVE dep) can dominate
  10 inert packages;
- `internal/service`+`internal/dashboard` UNDER-COUNTS the real fully-wired release `main`
  (adds config load, credential/keyring, TLS, auth middleware, concrete KG scoring);
- `go list -deps` misses subprocesses (python bridge), runtime-loaded assets/plugins, and
  network reachability;
- no isolation exists while production still ships the fat `cmd/da` artifact.
- **Decisive fix:** build the ACTUAL fully-wired slim release entrypoint and measure ITS
  transitive closure at real release `GOOS`/`GOARCH`/`CGO`/tags (`go list -deps -json`); make
  that exact target the CI import-policy subject; network attack surface still needs a
  separate route/data-flow security review.

**Sub-fork B — mechanism ranking SURVIVES, amended.** (1) dedicated entrypoint + an
ALLOWLIST/layer policy over the FULL transitive closure of the exact production build (NOT a
denylist or source grep), run for every supported build config, PLUS split `internal/service`
into slim-core vs fat-wiring so fat adapters never enter the shared core; (2) separate module
only if independent dependency governance / release ownership becomes a hard requirement;
(3) build-tags only as a localized implementation-selection escape hatch. R3 note: a guard
over the exact slim entrypoint's closure DOES hold even though `internal/service` is shared by
both the fat `da service` path and the slim runtime — provided the layering split exists.

**Ratified decision (owner-accepted):** dedicated release entrypoint + full-closure allowlist
import-guard (per build config) + `internal/service` slim-core/fat-wiring split. D3's isolation
claim is scoped to **"static import isolation, provisional"** — NOT "attack surface reduced."
**Residual open axes** (folded into the spec Open Questions): (a) measure the real fully-wired
release artifact's transitive closure (plan task t4a); (b) a separate SSE/bus network
attack-surface route/data-flow review — package metrics cannot settle it.
