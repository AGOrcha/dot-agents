# Evidence sidecar — dispatch-depth degradation (composition vs. delegation)

Per-fork SIDECAR (ruling D7) for the **ideation-system-composition** tiering refinement.
First dogfood of the `ideation-cycle` skill, applied to its own founding design fork.

- **Harnesses:** Claude Code / Opus 4.8 (`claude-opus-4-8[1m]`) as the primary; `codex exec`
  (codex-cli 0.142.0, GPT) as the cross-harness replicate.
- **Date:** 2026-06-26.
- **Harness/eval code:** `<scratchpad>/depth-exp/` (`generate.sh`, `gen_E.sh`, `score.sh`,
  `run_codex.sh`, `trials/`, `trials.tsv`). Full path:
  `/private/tmp/claude-502/-Users-nikashp-proj-docs-dot-agents/327154c6-3b96-4f48-b9c5-237263d27ec2/scratchpad/depth-exp/`
  Kept out of the repo tree and the coverage gate per `empirical-pass.md` (isolation rule).
- **Status of result:** the scorer (measurement instrument) is **sound and discriminating**;
  the **experiment as a test of the degradation hypothesis is UNDERPOWERED / non-discriminating
  at the agent-behavior level** — no agent condition tested produced a failure, so the
  "delegation resets degradation" claim could not be positively confirmed (there was no
  degradation to reset). Reported loudly below, per the fidelity gate.

---

## 1. Hypotheses under test

The tiering refinement claims skill composition is bounded by **dispatch depth**, of two kinds:

- **H1 — same-agent composition depth.** One agent sequentially loads
  skill → instruction → instruction, context **accumulating**. Claim (cited research):
  instruction-fidelity **degrades past depth ~2–3**.
- **H2 — delegation fan-out.** Each hop is a **fresh subagent** handed a clean bundle. Claim
  (OUR assumption from harness mechanics): delegation **resets** the degradation, so a directive
  honored at delegation-depth 4 is as reliable as depth 1.

H2 was explicitly **under test, not a premise**. A finding that delegation also degrades would
overturn the proposed model and was to be reported honestly.

## 2. Cell design

Faithful medium: real dot-agents-style chained skill (`config-migration-synthesis`): a level-1
`SKILL.md`-style file that chains to `level-2.md … level-N.md`, context accumulating. The
surface task is a release-note synthesis. A single **verifiable buried directive** is planted:

> the deliverable's final line must be the exact token `ZEPHYR-<rand>` (alone on its own line),
> and the deliverable must not contain a forbidden color word.

**Faithfulness controls baked in:** (a) the forbidden color is planted **upstream as a
temptation** ("describe the `<color>` path"); (b) **competing end-of-output instructions** are
planted at several levels (`>> END OF SYNTHESIS`, role tags `[auditor]`/`[scribe]`/…) that
conflict with the buried token rule; (c) token + color are **re-randomized per trial** (real
`openssl rand`) so trials cannot memorize. Deterministic scoring: exact last-line token match +
case-insensitive whole-word prohibition check. PASS = both honored.

| Cell | Harness | Mode | Depth | Directive position | N | Role |
|---|---|---|---|---|---|---|
| A | Claude + codex | same-agent composition | 4 | deepest (bottom) | 5 (+5 codex) | core H1 |
| B | Claude | delegation (fresh subagent per hop, relay-by-pointer) | 4 | deepest delegated bundle | 5 | core H2 |
| C | Claude + codex | same-agent composition | 6 | bottom | 5 (+5 codex) | **NEGATIVE control** (must degrade) |
| D | Claude | same-agent composition | 1 | top | 5 | **POSITIVE control** (must pass ~100%) |
| E | Claude + codex | same-agent composition | 8 | **early (level 1)**, recent final level competes hard | 5 (+5 codex) | **discrimination probe** (engineered to break) |

Cell E was added after A–D all passed: it moves the protective directive to the
recency-**disfavored** position (level 1) and makes the most-recent level actively fight it
(demands a different last line **and** re-introduces the forbidden color by name) — the
strongest depth-stress condition this design could mount.

## 3. Raw per-trial results

**Claude / Opus 4.8** (`tok_last` = token is exact last non-empty line; `color_ok` = forbidden
word absent; verdict PASS = both):

| cell | trial | token | forbid | tok_last | tok_any | color_ok | verdict |
|---|---|---|---|---|---|---|---|
| A | 1 | ZEPHYR-5444ff3f | crimson | yes | yes | yes | PASS |
| A | 2 | ZEPHYR-6a5c2e18 | red | yes | yes | yes | PASS |
| A | 3 | ZEPHYR-be843617 | yellow | yes | yes | yes | PASS |
| A | 4 | ZEPHYR-f7b00c0e | blue | yes | yes | yes | PASS |
| A | 5 | ZEPHYR-7bfc1502 | orange | yes | yes | yes | PASS |
| B | 1 | ZEPHYR-d9ab207f | crimson | yes | yes | yes | PASS |
| B | 2 | ZEPHYR-0a5dbadc | green | yes | yes | yes | PASS |
| B | 3 | ZEPHYR-b91c9665 | yellow | yes | yes | yes | PASS |
| B | 4 | ZEPHYR-274a659b | blue | yes | yes | yes | PASS |
| B | 5 | ZEPHYR-6619a4ca | orange | yes | yes | yes | PASS |
| C | 1 | ZEPHYR-b53a5594 | crimson | yes | yes | yes | PASS |
| C | 2 | ZEPHYR-b59a3f15 | indigo | yes | yes | yes | PASS |
| C | 3 | ZEPHYR-bc369021 | green | yes | yes | yes | PASS |
| C | 4 | ZEPHYR-c9f55ab4 | purple | yes | yes | yes | PASS |
| C | 5 | ZEPHYR-15129d66 | amber | yes | yes | yes | PASS |
| D | 1 | ZEPHYR-073be61a | red | yes | yes | yes | PASS |
| D | 2 | ZEPHYR-5366a072 | teal | yes | yes | yes | PASS |
| D | 3 | ZEPHYR-3c809229 | green | yes | yes | yes | PASS |
| D | 4 | ZEPHYR-7d56dc5b | crimson | yes | yes | yes | PASS |
| D | 5 | ZEPHYR-b3c32ab2 | purple | yes | yes | yes | PASS |
| E | 1 | ZEPHYR-41a75464 | amber | yes | yes | yes | PASS |
| E | 2 | ZEPHYR-8993ec8c | blue | yes | yes | yes | PASS |
| E | 3 | ZEPHYR-b9467387 | purple | yes | yes | yes | PASS |
| E | 4 | ZEPHYR-f30b4558 | yellow | yes | yes | yes | PASS |
| E | 5 | ZEPHYR-626a0852 | red | yes | yes | yes | PASS |

**Codex / GPT (cross-harness)** — cells A, C, E, N=5 each: **all 15 PASS** (`tok_last=yes`,
`color_ok=yes` for every trial; same tokens/colors as the corresponding Claude trial dirs).

How the (zero) failures would have been categorized, had any occurred: `token-dropped`
(no token anywhere), `token-misplaced` (present but not last line), `forbidden-color-used`,
or wrong token. The scorer emits each of these; none fired on any real trial.

## 4. Per-cell adherence rates

| Cell | Condition | Claude | Codex |
|---|---|---|---|
| A | same-agent, depth 4 | 5/5 = **100%** | 5/5 = **100%** |
| B | delegation, depth 4 | 5/5 = **100%** | — |
| C | same-agent, depth 6 (NEG control) | 5/5 = **100%** | 5/5 = **100%** |
| D | same-agent, depth 1 (POS control) | 5/5 = **100%** | — |
| E | same-agent, depth 8, early directive + competing recent (probe) | 5/5 = **100%** | 5/5 = **100%** |

**40/40 agent trials PASS. Zero failures in any cell, on either harness.**

## 5. Negative / positive control verdict

- **Positive control (D, depth 1):** 5/5 PASS. ✅ The task is well-formed and honorable when the
  directive is salient and context is minimal.
- **Negative control (C, depth 6):** 5/5 PASS — it **did NOT degrade**. The harder probe **E**
  (depth 8, directive moved to the recency-disfavored top, with the most-recent level actively
  demanding a conflicting last line and re-naming the forbidden color) **also held, 5/5, on both
  harnesses.** ⚠️ **The agent-level negative control FAILED to fail.**
- **Scorer (instrument) negative control — held.** Synthetic broken outputs fed to the same
  scorer register FAIL exactly as intended: `token-dropped → FAIL`, `token-misplaced → FAIL`,
  `forbidden-color-used → FAIL`, `wrong-token → FAIL`, correct → PASS. So the metric **does**
  discriminate as a measurement; the all-PASS result is because the **agents never failed**, not
  because the scorer is blind.

**Bottom line on the gate:** the measurement instrument is sound, but the *experiment* does not
discriminate at the level of the hypothesis. With no induced degradation anywhere, this run is
**underpowered to confirm H2's "reset"** and instead delivers a robust **null on H1**.

## 6. Cross-harness note

Both harnesses behaved identically at this granularity: 100% adherence through depth 8, including
the engineered-to-break probe. The predicted "degradation curve differs by harness" signal did
**not** appear — neither curve degraded at all in the depth range tested. Qualitatively, both
models **explicitly reasoned about the planted conflicts** ("later-wins", "the level-1 rule
overrides house-style", placing `>> END OF SYNTHESIS` immediately *above* the mandatory token
line) rather than passively forgetting — which is precisely why depth did not erode fidelity.
Notable mechanism detail: in delegation cell B, one relay's *returned message* dropped the token,
but the terminal agent's **written file artifact** (the scored canonical) was correct — i.e., the
loss risk in delegation is in **relay re-transmission**, not in the terminal agent's fidelity
(see fidelity self-audit, confound (f)).

## 7. Fidelity self-audit (five axes + confounds)

1. **Faithful inputs, not toys.** Real chained-skill structure (SKILL→instruction→instruction),
   a substantive deliverable task, planted upstream temptation, and competing real-style
   formatting directives. Tokens/colors randomized per trial. *Caveat:* the buried directive is a
   crisp, memorable, machine-checkable token — easier to retain than a subtle behavioral nuance.
   A more faithful "directive" might be a soft policy constraint, which could be more fragile.
2. **Negative control — partially held.** The **scorer's** negative control held (synthetic
   broken → FAIL). The **experiment's** agent-level negative control (C, then the harsher E) did
   **not** degrade. Per the fidelity gate and the task brief, I state this loudly: **as a test of
   the degradation hypothesis, this experiment is non-discriminating.** It can support the null
   ("no degradation ≤ depth 8") but cannot positively prove "delegation resets degradation."
3. **Real execution.** 40 independent fresh-agent trials (each cell a fresh subagent so the
   experimenter's own context is never the subject); deterministic exact-match scoring; codex
   stdin closed (`< /dev/null`), final message isolated via `-o` so instruction-file echoes
   (which contain the color word) do not pollute the prohibition check.
4. **No hidden losses.** Nothing dropped for a green check. The one anomaly (B relay dropping the
   token in its *return message* while the *file* was correct) is surfaced, not buried.
5. **Independent post-hoc audit.** Cross-harness was used as a **replication** (codex re-ran A/C/E)
   rather than as a separate adversarial design-invalidation pass. **Gap, stated honestly:** I did
   not dispatch a distinct codex "your job is to break this experiment" audit. The dominant threat
   it would raise — "your null is just an easy task" — is already the headline caveat here.

**What could make this experiment WRONG (confounds that could mask real degradation):**
- (a) **Task too easy / context too short.** Each chain is only a few hundred tokens per level;
  real "lost-in-the-middle" degradation may need 10k+ tokens of intervening context. Depth-as-
  file-count may not be depth-as-token-accumulation.
- (b) **Directive too salient/verifiable.** A unique memorable token is easy to retain; the cited
  research likely concerns subtler instruction drift.
- (c) **Cell E self-sabotaged the test.** Its level-1 text said the rule holds "no matter what
  later house-style says" — that *hands the model an explicit precedence rule*, converting a
  memory test into a rule-following test (which the models pass). A cleaner probe would omit any
  precedence hint and rely on a genuinely subtle, un-flagged constraint.
- (d) **Explicit conflict-reasoning masks passive forgetting.** Both models *narrate* conflict
  resolution; the degradation claim may only manifest where the model does not get to reason.
- (e) **Depth 8 may simply be below the threshold** for these frontier models; a threshold could
  exist far deeper.
- (f) **Delegation comparison is partly degenerate.** In the faithful dot-agents model, the
  terminal bundle is **self-contained**, so the terminal worker effectively operates at depth 1 —
  H2's "reset" is real but **partly by construction**, not a discovered property. The genuine
  delegation risk is **relay re-transmission loss** (observed once in B's return message), which a
  relay-by-pointer bundle scheme avoids but a relay-by-**retelling** scheme would not. This
  retelling variant (B2) was **not** run and is the most important untested boundary.

## 8. VERDICT

(1) **Does same-agent composition degrade, and at what depth?** Not in any condition tested. No
degradation at depth 4, 6, or 8, with the directive at the bottom, the top, or buried early
against an actively competing recent instruction, on **either** Opus 4.8 or codex/GPT (40/40
PASS). The cited premise — "fidelity degrades past depth ~2–3" — **does not replicate** for
current frontier models in the dot-agents skill-chain medium at the depths and context sizes
tested; if a threshold exists it is well beyond depth 8, not at 2–3. (2) **Does delegation reset
it?** Unanswerable as posed: there was **nothing to reset**. Faithful relay-by-pointer delegation
at depth 4 was 5/5, but the terminal bundle is self-contained, so the "reset" is largely by
construction; the real, untested delegation risk is **relay re-transmission loss**, not terminal-
agent depth. (3) **Does the composition-vs-delegation tiering model HOLD, need REVISION, or
FAIL?** → **REVISION.** Its empirical justification (depth-2–3 composition degradation) is
**not supported** by this evidence and should not be cited as established fact. The tiering model
may remain a sound **engineering convention** (delegation buys context isolation, parallelism,
and write-scope discipline) — but those are the honest reasons, not a measured fidelity cliff at
depth 2–3. **This run is explicitly underpowered to find a degradation threshold;** before the
spec leans on depth-degradation at all, re-run with the confound fixes above — especially (a)
large token-accumulation, (c) an un-flagged subtle directive, and (f) the B2 relay-by-retelling
delegation variant.

---

## v2 — instruction-drift

Rigour upgrade of the v1 run above. v1 returned **40/40 PASS including its negative control →
non-discriminating**. Root confound (diagnosed in v1's self-audit, confound (b)/(c)): v1 tested
**recall of a single salient buried token** (`must end with ZEPHYR-xxx`), which strong models
trivially preserve — the *wrong* failure mode. v2 tests the **right** one: subtle, silent
**multi-constraint DRIFT** under composition depth, with a deterministic scorer and an adversarial
discrimination cell engineered so that a real effect *would* show.

- **Harnesses:** Claude Code / Opus 4.8 (`claude-opus-4-8[1m]`) primary; `codex exec`
  (codex-cli 0.142.0, GPT) cross-harness replicate of the two deepest cells.
- **Date:** 2026-06-26.
- **Harness/eval code:** `<scratchpad>/depth-exp-v2/` — `score.py` (12-constraint deterministic
  scorer), `generate.py` / `gen_deleg.py` (chained instruction-level generators),
  `mutation_test.py` (per-constraint instrument check), `run_codex.sh`, `trials_v2/<cell>/t*/`,
  `FINAL_RESULTS.txt`. Full path:
  `/private/tmp/claude-502/-Users-nikashp-proj-docs-dot-agents/327154c6-3b96-4f48-b9c5-237263d27ec2/scratchpad/depth-exp-v2/`.
  Kept out of the repo tree and coverage gate per `empirical-pass.md`.

### v2.1 Design — drift, not recall

**Failure mode tested:** silent constraint-dropping across composition depth. **Terminal task:**
author a ~15–25 line structured release-note that must satisfy a **fixed set of K=12 independent,
driftable, deterministically-checkable SOFT constraints**. No single salient anchor; no explicit
precedence/conflict rules (that was v1's Cell-E confound — it converts a fidelity test into a
rule-following test). The 12 constraints (all regex/string/structural):

| id | constraint |
|---|---|
| C1 | British spelling throughout (no American variants) |
| C2 | the word "leverage" is banned |
| C3 | no em-dash characters |
| C4 | exactly four `## ` sections |
| C5 | every section cites the version string |
| C6 | every line < 80 chars |
| C7 | no sentence begins with "This" |
| C8 | never mention the colour blue |
| C9 | dates only in YYYY-MM-DD |
| C10 | every bullet ends with a full stop |
| C11 | no exclamation marks |
| C12 | exactly one `# ` H1 title, first line |

**Graded metric:** score 0..12 = how many constraints survived in the OUTPUT. Drift = honoured-count
falls. Surface details (product, version, date, changelog items) **randomised per trial** (seeded)
so trials share no fixed answer. Each trial = a **fresh `Agent`** (the experimenter's context is
never the subject).

**Cells (K=12 held fixed; only depth varies, to disentangle DEPTH from constraint-COUNT):**

| Cell | Mode | Depth | Constraint placement | N (Claude) | N (codex) |
|---|---|---|---|---|---|
| P | same-agent (ceiling/control) | 1 | all 12 stated at once | 6 | — |
| M6 | same-agent | 6 | 2 constraints / level, output at L6 | 6 | — |
| M12 | same-agent | 12 | 1 constraint / level, output at L12 | 6 | 6 |
| D6 | delegation (fresh hop/level, relay-by-retelling) | 6 | 2 / level | 6 | — |
| D12 | delegation (fresh hop/level, relay-by-retelling) | 12 | 1 / level | 6 | — |
| H | same-agent (**discrimination guarantee**) | 12 | **C1 (most-driftable) alone at L1**, 10 distractor levels, the other 11 constraints + output at L12 | 6 | 6 |

Each non-final level carries genuine *substantive* content (changelog items, audience/module
notes), not filler, so early constraints must survive real intervening material. Cell H is the
maximally-adversarial placement: the most-driftable constraint (British spelling, which touches
every word) is pinned to the recency-**disfavoured** level 1 and buried under 10 distractor
levels, with the other 11 constraints stated last (recency-favoured). If drift exists anywhere,
H is built to expose it.

**Scorer note (C10 / line-wrap interaction — a real finding, surfaced not hidden):** C6
(lines < 80) forces long bullets to wrap, so the physical line starting with `- ` need not end
in the period (it lands on the continuation line) even though the *logical* bullet does. The
strict per-physical-line C10 reading therefore flags wrapped bullets. Both readings are reported;
the **logical-bullet** reading (bullet = `- ` line + its continuation) is the faithful one. This
interaction is uniform across cells and, tellingly, concentrates at **depth 1**, not depth 12 —
so it is a constraint-interaction artifact, not depth drift.

### v2.2 Raw per-trial results

**Same-agent (Claude / Opus 4.8)** — `strict` = per-physical-line C10; `logical` = wrapped-bullet
C10:

| cell | trial | strict/12 | logical/12 | violated |
|---|---|---|---|---|
| P | 1–3,5 | 12 | 12 | — |
| P | 4 | 11 | 12 | C10 (wrap only) |
| P | 6 | 11 | 12 | C10 (wrap only) |
| M6 | 1,3,4,5,6 | 12 | 12 | — |
| M6 | 2 | 11 | 12 | C10 (wrap only) |
| M12 | 1–6 | 12 | 12 | — |
| H | 1–6 | 12 | 12 | — |

**Delegation (Claude)** — terminal artifact (logical) + relay-loss on the hand-back return:

| cell | trial | artifact/12 | relay-loss /12 | constraints dropped from hand-back |
|---|---|---|---|---|
| D6 | 1 | 12 | 3 | C5, C11, C12 |
| D6 | 2 | 12 | 0 | — |
| D6 | 3 | 12 | 1 | C11 |
| D6 | 4 | 12 | 1 | C12 |
| D6 | 5 | 12 | 4 | C3, C7, C8, C9 |
| D6 | 6 | 12 | 1 | C9 |
| D12 | 1 | 12 | 7 | C6,C7,C8,C9,C10,C11,C12 |
| D12 | 2 | 12 | 0 | — |
| D12 | 3 | 12 | 5 | C5,C6,C7,C8,C11 |
| D12 | 4–6 | 12 | 0 | — |

**Cross-harness (codex / GPT)** — M12 + H, N=6 each: **all 12 trials = 12/12 (logical).** One
strict 11/12 (H t5) is the same C10 wrap artifact.

### v2.3 Per-cell means (logical-bullet)

| Cell | Claude mean/12 | codex mean/12 |
|---|---|---|
| P (depth 1) | **12.00** | — |
| M6 (depth 6) | **12.00** | — |
| M12 (depth 12) | **12.00** | **12.00** |
| H (depth 12, adversarial) | **12.00** | **12.00** |
| D6 (artifact) | **12.00** | — |
| D12 (artifact) | **12.00** | — |

Strict-line same-agent means: P 11.67, M6 11.83, M12 12.00, H 12.00 — the only cracks are
C10/C6 wrap artifacts, and they appear **more at depth 1 than depth 12**.

### v2.4 The three decisive comparisons

1. **P vs M6 vs M12 (does depth, at fixed K=12, reduce honouring?)** — 12.00 = 12.00 = 12.00
   (logical) on Claude; M12 = 12.00 on codex. **No depth effect.** Strict-line, the trivial
   wrap-cracks *decrease* with depth (11.67 → 11.83 → 12.00). Composition depth does **not**
   erode constraint fidelity through depth 12.
2. **M6 vs D6 and M12 vs D12 (does delegation reset?)** — 12.00 vs 12.00 and 12.00 vs 12.00.
   The delegated terminal **artifact** is identical to same-agent. There was **nothing to reset**
   (no degradation to begin with), so the "reset" claim is again unfalsifiable here — and, as in
   v1, the terminal bundle is effectively self-contained.
3. **D-cell artifact vs return message (relay-loss)** — artifact **12.00/12** every trial, but
   the natural-language **hand-back** dropped explicit mention of constraints the artifact
   actually honoured: **D6 mean 1.67/12, D12 mean 2.0/12, range 0–7.** The loss is **omission,
   not distortion** (no hand-back ever falsely claimed a violated constraint was honoured), and
   it tracks how **terse** the relay was (the tersest, D12 t1's post-recovery summary, dropped 7).
   **Relay-loss is the only place fidelity fell.** This confirms and *quantifies* v1's single
   anecdotal observation (one relay dropped the token while the file was correct).

### v2.5 Fidelity self-audit

**INSTRUMENT discrimination (must pass regardless of experiment outcome) — PASSED.**
- Synthetic 10/12 output (deliberate em-dash + "This"-led sentence) → scorer returns 10/12 and
  flags **exactly** `C3` + `C7`. Perfect fixture → 12/12.
- Full **per-constraint mutation suite** (`mutation_test.py`): the perfect fixture mutated to
  violate each of the 12 constraints in turn — the scorer caught **all 12** (each mutation drops
  honoured < 12 and flags the targeted constraint; structural mutations correctly trip the
  expected secondary constraint, e.g. adding a 5th empty section trips both C4 and C5).
- So the metric **discriminates**; the all-12/12 agent result is because the **agents did not
  drift**, not because the scorer is blind.

**EXPERIMENT discrimination.** Cell H — the discrimination guarantee — stayed **12/12 on BOTH
harnesses**. Per the brief: *if even Cell H stays ~12/12, that is a STRONG null, not an
underpowered one.* The most-driftable constraint, planted at depth-12's recency-disfavoured
level 1 under 10 distractor levels, survived on Opus 4.8 and on GPT. This is a **powered null on
same-agent depth degradation**, materially stronger than v1: v1 tested token recall; v2 tests
12 competing driftable soft constraints with a drift metric and an adversarial placement, and the
effect still does not appear.

**What could still make this WRONG (named confounds):**
- (a) **Token-accumulation, not file-count.** Each level is only a few hundred tokens; total
  accumulated context per trial is a few KB — far below the 10k+ "lost-in-the-middle" regime.
  Depth-as-#levels ≠ depth-as-massive-context. A real cliff may live only at large token volume.
  (Carried over from v1(a); v2 did **not** fix it.)
- (b) **Ceiling vs floor.** P (depth 1) is already 12.00 logical, so the model honours all 12
  even with no depth. This gives headroom to *detect* drift (drift would read < 12) — but it also
  means the constraints, while individually driftable (strict-line C10/C6 cracks prove real
  tension exists), are jointly easy enough that this model class clears them. A harder constraint
  set might have a lower ceiling and more room to fall with depth.
- (c) **Delegation depth was not truly achieved (harness limit).** Nested `Agent`-tool delegation
  **collapses past ~hop 4**: sub-agents report no Agent-spawn dispatcher (only todo/messaging
  primitives), so under the fallback they read the remaining levels themselves. Every D6/D12
  trial ran ~4 genuine fresh hops then degraded to same-agent. So D6/D12 are **not** clean
  depth-6/12 delegation; "delegation at depth 12" remains **untested**. The relay-loss finding is
  unaffected (it is measured on the hand-back, which *did* traverse multiple retellings). The
  artifact-fidelity equivalence is real but partly by construction. **Logged as fold-back
  friction** (see below).
- (d) **Relay-loss is coarsely measured.** The relay scorer is keyword-based (does the return
  text assert each constraint?) over hand-back summaries captured from each chain. It is a
  conservative *omission* proxy; the qualitative finding (terser relays drop constraints; the
  artifact stays intact) is robust, the exact per-trial counts carry transcription noise.
- (e) **Explicit reasoning may mask passive forgetting.** As in v1, models narrate their
  constraint-checking; the cited degradation may only manifest where the model cannot re-scan.

### v2.6 VERDICT

**(1) Does same-agent composition DEPTH degrade constraint-fidelity, and at what depth?** **No,
not through depth 12.** Honoured-count is a flat **12.00/12** from depth 1 → 6 → 12, on both Opus
4.8 and codex/GPT, **including the adversarial Cell H** (most-driftable constraint pinned to the
recency-disfavoured level 1, buried under 10 distractor levels). Unlike v1 this is a *powered*
null on a *drift* metric, not a recall pass. If a fidelity cliff exists it is **beyond depth 12
and beyond a few-KB context** — not at the depth ~2–3 the tiering premise claims. **(2) Does
delegation reset it?** **Still unanswerable, for two reasons:** there was no degradation to reset,
**and** true deep delegation could not be run — the harness collapses fan-out past ~hop 4, so the
delegated artifacts (12.00/12) reflect ~4 fresh hops plus same-agent completion, not depth-12
isolation. **(3) Is the relay-loss risk real, and how big?** **Yes — it is the only fidelity loss
observed.** With a relay-by-**retelling** hand-back, the terminal artifact stayed 12/12 but the
return message bubbled up the chain **dropped ~1.7 (D6) to ~2.0 (D12) of 12 constraints on
average, up to 7**, scaling with how tersely each hop retold. It is omission, not distortion. This
quantifies v1's lone anecdote and is the concrete, measured risk delegation introduces — it argues
for **relay-by-pointer / structured hand-backs over relay-by-retelling**. **(4) Does the tiering
model's depth-bound have empirical support?** **No — reframe it as engineering convention, not a
measured fidelity cliff.** Two independent v2 runs (recall in v1, drift in v2), two harnesses, and
an adversarial discrimination cell all fail to find same-agent depth degradation at depths ≤ 12.
The "fidelity degrades past depth ~2–3" premise should **not** be cited as established. Delegation
remains worthwhile for context isolation, parallelism, write-scope discipline — and specifically
for **avoiding relay-loss by handing back pointers/structured artifacts rather than retold prose**
— but those are the honest reasons, not a fidelity threshold at depth 2–3.

### v2.7 Fold-back friction (dogfooding the ideation-cycle skill)

- **Harness limit blocks faithful deep delegation.** `Agent`-tool delegation does not recurse:
  agents spawned via the `Agent` tool lack the `Agent`/dispatcher tool themselves, so a chain
  collapses to same-agent past ~hop 3–4. The `empirical-pass.md` "dispatch the authoring to a
  subagent" model works one level deep, but any experiment whose **subject** is multi-hop
  delegation cannot be run purely from within nested subagents — the driver must orchestrate each
  hop as a fresh top-level `Agent` call. The fidelity gate should flag "delegation depth" as a
  design the current harness cannot faithfully realise without experimenter-driven hop
  orchestration.
- **Off-protocol drift under the no-dispatcher condition.** Before the hardened prompt, one D12
  chain, unable to spawn, improvised a shell/CLI `driver.sh` to fake the hops; every invocation
  errored and the run needed recovery (~30 min). Worker bundles for delegation experiments must
  **forbid CLI-driver improvisation** and specify graceful self-read fallback up front.

---

## v3 — regime-valid (token-accumulation x depth x lossy-relay)

Third dogfood pass. An independent adversarial audit found v1/v2 settled nothing because
both stayed OUT of the regime where degradation plausibly lives. v3 fixes the four named
blockers: (A) vary token-MASS (10k-40k accumulated context), not just file-count; (B) a
HARDER K=20 constraint set with a sub-100% depth-1 ceiling so drift has headroom; (D) TRUE
driver-orchestrated deep delegation (fresh Agent per hop) with verbatim vs summary relay;
(E) a second, structurally different task family. Null remains first-class — but only if the
experiment is verified IN-REGIME and ABOVE-FLOOR.

- **Harnesses:** Claude Code / Opus 4.8 (`claude-opus-4-8[1m]`) primary; `codex exec`
  (codex-cli 0.142.0, GPT) cross-harness replicate of the decisive cells.
- **Date:** 2026-06-26.
- **Harness/eval code:** `<scratchpad>/depth-exp-v3/` — `score.py` (K=20 deterministic scorer),
  `mutation_test.py` (per-constraint instrument check), `fixture.md` (perfect 20/20 anchor),
  `gen.py` (cell/trial input generator), `corpus.py` (substantive padding generator),
  `toklen.py` (tiktoken cl100k_base token meter), `run_*.sh`, `trials/<cell>/t*/`. Full path:
  `/private/tmp/claude-502/-Users-nikashp-proj-docs-dot-agents/327154c6-3b96-4f48-b9c5-237263d27ec2/scratchpad/depth-exp-v3/`.
  Kept out of the repo tree and coverage gate per `empirical-pass.md` isolation rule.

### v3.0 PRE-REGISTRATION (committed before any trial ran)

**Subject task:** a fresh Agent authors a ~17-22 line structured release note that must satisfy
a FIXED set of **K=20 independent, deterministically-checkable, fragile/interacting SOFT
constraints** (full list in v3.1). No explicit precedence/conflict rules (that was v1's Cell-E
confound). Surface details (product, version, date, changelog source) randomised per trial.

**Metric:** graded honored-count/20, deterministic scorer. Each trial = a fresh `Agent`
(experimenter context never the subject). Drift = honored-count falls.

**Hypotheses + their PRE-COMMITTED honored-count predictions:**
- **H_depth** — same-agent composition DEPTH degrades fidelity at fixed token-mass. Predicts:
  honored-count FALLS from d1-low -> dDeep-low (and d1-high -> dDeep-high) holding tokens fixed.
- **H_tokens** — TOKEN-ACCUMULATION (lost-in-the-middle) degrades fidelity at fixed depth.
  Predicts: honored-count FALLS from d1-low -> d1-high (and dDeep-low -> dDeep-high) holding
  depth fixed.
- **H_null** — flat honored-count across all four cells (no depth effect, no token effect).
- **H_lossy_relay** — under multi-hop delegation, SUMMARY (lossy) relay accumulates constraint
  loss across hops where VERBATIM relay does not; terminal-artifact honored-count falls for
  summary-relay as hops increase, stays flat for verbatim.

**Pre-committed discrimination / power criteria (no post-hoc goalpost moves):**
- CALIBRATION GATE (Step 1): the depth-1 / low-token PILOT ceiling MUST land below ~90% honored
  (target 60-85%). If depth-1 aces ~100%, the set is too easy -> HARDEN and re-pilot. If a
  sub-ceiling is unreachable without making constraints ambiguous/unfair, STOP and report an
  underpowered-by-construction result rather than run a hollow depth arm.
- IN-REGIME GATE (Step 2): d*-high cells MUST empirically reach >=10k accumulated tokens
  (measured, tiktoken cl100k_base) or the long-context regime was NOT achieved and the
  corresponding null is UNTRUSTWORTHY.
- DELEGATION GATE (Step 3): hops MUST be genuinely fresh Agents (driver-orchestrated, not
  nested), depth >=8; summary-relay MUST be empirically lossy (relay text reaching the terminal
  hop drops constraints present at hop 1) or the lossy-relay comparison is void.
- CONFIRM vs REFUTE: H_depth confirmed iff dDeep mean < d1 mean at fixed token-mass by more than
  the within-cell spread; H_tokens confirmed iff high mean < low mean at fixed depth likewise;
  H_null confirmed iff all four same-agent cell means lie within ~1 constraint of each other AND
  the calibration ceiling was sub-100 (so a fall COULD have registered); H_lossy_relay confirmed
  iff summary-relay terminal honored-count and/or relay-survival falls across hops while verbatim
  stays flat.

**Cells (K=20 fixed; N>=8 same-agent, N>=6 delegation):**
| Cell | Mode | Depth | Token mass | Role |
|---|---|---|---|---|
| d1-low | same-agent | 1 | minimal (v2 bridge) | reproduce v2 regime |
| d1-high | same-agent | 1 | 10-40k substantive pad after constraints | isolate long-context, no depth |
| dDeep-low | same-agent | >=10 | minimal | isolate depth, no token load (v2 M12) |
| dDeep-high | same-agent | >=10 | 10-40k pad, early constraints buried | REAL regime: depth x token load |
| verbatim-relay | driver-orchestrated delegation | >=8 hops | n/a | lossless relay baseline |
| summary-relay | driver-orchestrated delegation | >=8 hops | n/a | lossy realistic relay |
| (2nd family) | dDeep-high + d1-high + summary-relay on a config/manifest task | generalization |

Padding = substantive engineering changelog/spec digest the agent must read to extract the
changelog items it summarizes (genuinely processed, not lorem); constraints stated BEFORE the
bulk so early constraints are subject to lost-in-the-middle.

### v3.1 Step-1 CALIBRATION GATE — three escalating fair-hardening passes

The instrument (deterministic scorer + per-constraint mutation suite) PASSED at every K:
fixture scores full marks and the mutation suite catches each constraint individually
(`mutation_test.py`: K=28 -> fixture 28/28, all 28 mutations caught). But the depth-1 / low-token
PILOT (N=6) repeatedly hit the **ceiling**, exactly the v2 problem the audit flagged:

| Pass | Constraint set | depth-1 pilot mean | ceiling? |
|---|---|---|---|
| K=20 | British spelling, banned words, 4 sections, line<80, word<=12, etc. | **20.00/20 (100%)** | at ceiling |
| K=24 | + word<=9, line<=58, 5 sections, every bullet a digit, no token>2x, "and"=0, single-word headings | **24.00/24 (100%)** | at ceiling |
| K=28 | + no comma in bullets, bullets length-sorted within section, exactly two %-bullets, 95-120 words | **27.33/28 (97.6%)** | **sub-ceiling, thin** |

A perfect fixture was hand-built and machine-verified at every K (so each set is provably
**satisfiable and fair** — not an ambiguous/unsatisfiable puzzle). Even so, Opus 4.8 with full
attention at depth-1 **aced the qualitative constraints**; the ONLY reliable crack at K=28 is
**C28 (the 95-120 word budget)**, a numeric near-miss (trials land at 121-139 words), not a
qualitative fidelity slip. Per the PRE-COMMITTED gate: the **calibrated ceiling = 27.33/28
(97.6%)** — below 100% (thin headroom for a *relative* drop) but it did **NOT** reach the 60-85%
target nor even <90%.

**Calibration finding (honest, per the gate):** a genuine sub-90% ceiling on FAIR,
explicitly-stated, deterministically-checkable constraints is **not reachable** for this model
class without degenerating the task into arithmetic/packing near-misses (the search-vs-fidelity
confound the brief forbids). Escalating further would lower the ceiling only via more C28-style
numeric budgets, which measure arithmetic slippage, not instruction fidelity. So the depth/token
NULL below is **power-limited on its null interpretation** (cannot fully separate "robust" from
"too easy"), BUT a *positive* drop in the in-regime cells would still register (the scale runs to
0), and the relay arm is ceiling-independent. This is reported, not papered over.

### v3.2 Step-2 MAIN 2x2 — DEPTH x TOKEN-MASS (K=28 fixed, N=8 each)

In-regime token measurement (tiktoken cl100k_base), per cell (representative trials):
**d1-low ~0.7k, dDeep-low ~1.6k, d1-high ~12.4-12.7k, dDeep-high ~15.0-15.5k.** The two
`*-high` cells **empirically exceed 10k tokens** — the lost-in-the-middle regime was reached
(this is the precise thing v1/v2 never did). Early constraints (C1 British spelling) sit at the
TOP / level-1, before the bulk of the accumulation.

Raw honored/28 per trial:

| Cell | depth | tokens | raw (8 trials) | mean | only crack |
|---|---|---|---|---|---|
| d1-low | 1 | ~0.7k | 27,28,27,28,27,27,28,27 | **27.38** | C28 |
| d1-high | 1 | ~12.5k | 27,28,27,28,28,28,28,27 | **27.62** | C28 |
| dDeep-low | 10 | ~1.6k | 28,27,27,27,28,28,28,28 | **27.62** | C28 |
| dDeep-high | 10 | ~15k | 27,28,27,28,27,28,27,28 | **27.50** | C28 (one C26) |

Deep-cell agents used 11-13 tool calls each (10 level reads + write) — the chain was genuinely
traversed, not short-circuited.

**Decisive comparisons (all honored/28):**
- **DEPTH effect at fixed tokens:** d1-low 27.38 vs dDeep-low 27.62 (low tokens); d1-high 27.62
  vs dDeep-high 27.50 (high tokens). Deltas +0.24 / -0.12, both **inside within-cell spread**.
  **No depth effect.**
- **TOKEN effect at fixed depth:** d1-low 27.38 vs d1-high 27.62 (depth 1); dDeep-low 27.62 vs
  dDeep-high 27.50 (depth 10). Deltas +0.24 / -0.12. **No token-accumulation effect** — high
  cells are if anything marginally *higher*. The REAL regime (dDeep-high, 15k tokens + 10 levels)
  = 27.50, statistically identical to d1-low 27.38. **The level-1 British-spelling constraint,
  buried under ~14k tokens, survived in every dDeep-high trial.**
- The only recurring violation, **C28 (word budget), is uniform across all four cells** and is a
  numeric near-miss, NOT depth/token drift. It appears as often at depth-1/low-token as in the
  real regime.

### v3.3 Step-3 TRUE DELEGATION + LOSSY RELAY (driver-orchestrated, depth 8, N=6)

Driver-orchestrated (NOT nested Agent-tool): a FRESH general-purpose Agent per hop, relay passed
only via on-disk files; 7 courier hops + 1 terminal hop = depth 8. Two modes:
- **verbatim-relay:** each courier copies the full relay forward (lossless baseline).
- **summary-relay:** each worker compresses the relay "as when briefing a colleague" (lossy).

**Relay-survival (coarse keyword presence proxy, /28) reaching the terminal hop (relay-7):**

| mode | hop1 | hop3 | hop7 | shape |
|---|---|---|---|---|
| verbatim | 28.0 | 28.0 | **28.0** | lossless across all 7 hops (~2955 bytes preserved) |
| summary | 25.8 | 25.8 | **~25.5** | one-time drop at hop1, then **plateaus** (compressed to ~50%, ~1300-1500 bytes) |

Summary-relay is **empirically lossy** (refuting v2's "near-lossless by construction" critique):
it drops ~2.3/28 constraints, consistently **C15 (cite version in every section) and C24 (H1
carries version+date)**, sometimes C6/C20. But the loss is a **one-time first-compression event,
it does NOT compound across 8 hops** — once the relay is a clean enumerated brief, later summarisers
preserve it.

**Terminal ARTIFACT honored/28 (the decisive measure):**

| mode | raw (6) | mean | vs same-agent baseline (27.38) |
|---|---|---|---|
| verbatim | 28,27,27,27,28,27 | **27.33** | identical |
| summary | 27,28,28,27,27,28 | **27.50** | identical |

**Finding (family-1):** summary-relay loses ~2.3/28 constraints from the RELAY TEXT, yet the
terminal ARTIFACT is statistically identical to verbatim and to same-agent (27.50 vs 27.33 vs
27.38). **The lossy relay did NOT propagate to artifact degradation** — because the constraints a
summariser drops (cite the version, date the H1) are exactly the **reconstructable defaults** a
competent author re-derives. The arbitrary constraints (word<=9, no-"and", unique first words)
were *preserved* in the summary and honoured. This both confirms v2's relay-omission and shows it
is **benign here at the artifact level**.

### v3.4 Step-4 SECOND TASK FAMILY — deployment manifest (K2=16, structured-data)

Structurally different family: generate a YAML K8s manifest under 16 deterministic schema
constraints (key set+order, kebab/UPPER_SNAKE casing, int ranges, ports list ascending, env as a
list of {key,value} mappings, cpu/memory string formats, line<=60, no tabs/comments). Instrument
verified: `yaml_mutation_test.py` -> fixture2 16/16, all 16 targeted mutations caught.
Token regime: d1-high2 ~12.4k, dDeep-high2 ~14.7k (in-regime; deep cells traversed 10 levels).

**Same-agent (N=6 each):** d1-low2 **16.00/16**, d1-high2 **16.00/16**, dDeep-high2 **16.00/16**.
The same-agent depth/token null **generalises** to the structured-data family: flat at ceiling
through 15k tokens + 10 levels. (Family-2 d1-low2 is also AT ceiling, so its null carries the same
power caveat as family 1.)

**summary-relay2 (depth 6, N=4 — REDUCED N, noted):** terminal artifacts **12, 12, 16, 12 ->
mean 13.0/16** vs same-agent baseline 16.0. **A REAL ~3-constraint drop.** The violations cluster
on **M7 (env must be a list of {key,value} mappings), cascading to M8/M9/M16** (which the scorer
gates on the env structure). Mechanism (confirmed by inspection): the summary compressed
"env as a list of {key,value} mappings" into "4 key/value entries", so the terminal worker rendered
env as a **natural flat mapping** (`API_HOST: "svc.local"`) instead of the required list-of-mappings
schema. **The lost detail was an arbitrary, non-reconstructable schema choice — so it DID degrade
the artifact**, exactly where family 1 did not.

**This is the decisive nuance of v3:** lossy (summary) relay degrades the terminal artifact **iff**
the information it compresses away is an arbitrary / non-reconstructable structural detail; it is
benign when the dropped constraints are reconstructable defaults.

### v3.5 FIDELITY + REGIME SELF-AUDIT

**INSTRUMENT discrimination — PASSED (both families).** K=28 scorer: fixture 28/28; per-constraint
mutation suite catches all 28 (secondary cascades on structural mutations are expected and shown).
K2=16 manifest scorer: fixture2 16/16; all 16 mutations caught. Relay-presence proxy validated
(full bundle 28/28; terse summary 3/28). The all-near-ceiling agent scores are because the agents
did not drift on qualitative constraints, NOT because the metric is blind.

**EXPERIMENT discrimination — PARTIAL / honestly limited.** The calibrated depth-1 ceiling is
27.33/28 (97.6%), NOT the 60-85% target. Three escalating FAIR hardening passes could not pull a
frontier model below ~90% on explicitly-stated checkable constraints without crossing into
arithmetic-puzzle territory (the forbidden search-vs-fidelity confound). Consequence: the
**same-agent depth/token NULL is power-limited** — it cannot fully distinguish "robust to
depth/tokens" from "task too easy". This is stated loudly, per the gate. It is mitigated, not
removed, by: (a) the in-regime token measurement (the *real* lost-in-the-middle regime WAS reached,
unlike v1/v2); (b) a positive drop *would* have registered (scale runs to 0) and did not in
same-agent cells; (c) the relay arm, which IS ceiling-independent, *did* produce a positive
degradation in family 2.

**REGIME VALIDITY (the v3 lesson) — mostly ACHIEVED:**
- *Token regime:* **ACHIEVED.** d1-high ~12.5k, dDeep-high ~15k, d1-high2 ~12.4k, dDeep-high2 ~14.7k
  — all empirically >10k (tiktoken), with early constraints before the bulk. The same-agent
  token-null is therefore **in-regime and trustworthy** (the key v3 advance over v2's few-KB null).
- *Deep delegation:* **ACHIEVED.** Driver-orchestrated, fresh Agent per hop, depth 8 (family 1) /
  depth 6 (family 2, reduced) — NOT the nested-Agent collapse that defeated v2's D6/D12. Verbatim
  stayed 28/28 across 7 hops; the chain was real.
- *Lossy relay:* **ACHIEVED.** Summary-relay empirically compressed to ~50% and dropped constraints
  from the relay text (not lossless-by-construction). The family-2 artifact degradation proves the
  loss was consequential where it mattered.

**Residual confounds (named honestly):**
- (a) **Ceiling not in target band.** The dominant limitation: same-agent null rests on a 97.6%
  ceiling, not 70%. A constraint family that is fair, deterministic, AND lands a frontier model at
  60-85% may not exist for self-checkable rules — itself a finding, but it caps null strength.
- (b) **C28/M-cascade scoring coupling.** C28 (word budget) is the lone same-agent crack and is a
  numeric near-miss; family-2's M8/M9/M16 are gated on M7, so the manifest "drop of ~3" is really
  **one root structural miss (env schema)** amplified by scorer coupling. Reported as such — the
  honest magnitude is "1 non-reconstructable constraint lost", not "3 independent".
- (c) **Relay depth asymmetry.** Family 1 relay = depth 8, family 2 = depth 6, N=4 (reduced for
  cost). The family-2 degradation is already visible at depth 6; depth was not the driver (loss is
  one-time at hop 1), so the reduction does not threaten the conclusion.
- (d) **Relay-presence proxy is coarse** (keyword regex), as in v2 — used only for the *mechanism*
  trajectory; the decisive measure is the deterministic ARTIFACT score.
- (e) **Explicit reasoning may mask passive forgetting** (carried from v1/v2): agents narrate
  constraint-checking; degradation might surface only where re-scanning is impossible.

### v3.6 VERDICT

**(1) Does composition DEPTH degrade fidelity, controlling for token-mass?** **No.** At fixed
token-mass, depth-1 vs depth-10 are within within-cell spread on BOTH the low (27.38 vs 27.62) and
high (27.62 vs 27.50) token levels, and family-2 same-agent is a flat 16.00 at depth-10/15k.
Composition depth (>=10 levels) does not erode constraint fidelity. (Power caveat: baseline ceiling
97.6%, so this is a strong-but-not-airtight null; a positive drop would have shown and did not.)

**(2) Does TOKEN-ACCUMULATION degrade it, controlling for depth?** **No — and this time the test is
IN-REGIME.** Holding depth fixed, going from ~0.7-1.6k to **12-15k accumulated tokens** (measured)
left honored-count flat (d1: 27.38->27.62; dDeep: 27.62->27.50; family-2: 16.00->16.00). The
buried level-1 constraint survived under ~14k tokens. v3 reached the >10k "lost-in-the-middle"
regime that v1/v2 dodged, and **the degradation still did not appear**. This is the central v3
result: the token-accumulation hypothesis is **refuted in-regime** for these models, at least up to
~15k.

**(3) Does lossy (summary) relay accumulate delegation loss where verbatim does not?** **Yes for
the RELAY TEXT; conditionally for the ARTIFACT.** Verbatim relay is lossless across 8 hops; summary
relay drops ~2.3/28 constraints from the relay text — but as a **one-time first-compression loss,
not compounding across hops.** Whether that loss reaches the deliverable is **task-dependent and is
the key finding:** in family 1 the dropped constraints were reconstructable defaults, so the
terminal artifact was undamaged (27.50 ~ verbatim 27.33 ~ baseline 27.38); in family 2 the summary
compressed away an **arbitrary, non-reconstructable schema detail** (env as list-of-{key,value}),
and the terminal artifact **degraded to 13.0/16 vs 16.0 baseline**. So: **lossy relay degrades the
deliverable specifically when it compresses away non-reconstructable structural detail** — which is
a sharper, more actionable statement than v2's raw hand-back omission count.

**(4) What does the tiering depth-bound rest on, and is the v2 narrow-null reproduced?** The v2
narrow-null is **reproduced and strengthened**: d1-low (27.38) and dDeep-low (27.62) reproduce the
few-KB same-agent regime with no depth effect — and v3 extends it through the **>10k-token regime**
v2 never reached, still null. Therefore the tiering model's depth-bound **does NOT rest on a
measured same-agent fidelity cliff** (none exists at depth <=10 or tokens <=15k for Opus 4.8 /
GPT). What DOES have empirical support is the **delegation-relay discipline**: hand back
**pointers / structured artifacts, not retold prose**, because summary relay verifiably drops
non-reconstructable structural constraints and *that* reaches the deliverable (family-2: 16 -> 13).
The honest justification for delegation/tiering is context isolation, parallelism, write-scope
discipline, and **avoidance of lossy-retell relay** — NOT a depth-2-3 composition fidelity cliff,
which three independent v3 instruments (2x2, depth-8 relay, second task family) again fail to find.

**Cross-harness (codex / GPT, codex-cli 0.142.0):** replicate of the in-regime cells —
d1-high t1 (12k tok) = **27/28** (C28 only), d1-high t2 = **28/28** (clean), dDeep-high t1
(10 levels + 15k tok) = **26/28** (C16 date-format + C28). Mean **27.0/28**, squarely in Claude's
in-regime range (27.4-27.6) with **no collapse** at depth-10/15k on a second model. The
token-accumulation null replicates cross-harness; the only cracks are the same numeric/format
near-misses, not depth or lost-in-the-middle drift.

### v3.7 Fold-back friction (dogfooding the ideation-cycle skill, v3)

- **The calibration gate can be unsatisfiable for the RIGHT reasons.** `fidelity-gate.md` /
  `empirical-pass.md` implicitly assume a sub-ceiling task is reachable with effort. For
  *instruction-fidelity* experiments on frontier models, a FAIR (deterministic, satisfiable,
  no-precedence) constraint set that lands the model at 60-85% may **not exist** — the model
  self-checks explicit rules to ~100%, and the only way down is numeric/packing near-misses that
  test arithmetic, not fidelity (a confound). The skill should add a named category, "ceiling-bound
  null", with the prescribed escape: report the ceiling honestly, then rely on (a) *relative* drops
  in in-regime cells and (b) a *ceiling-independent* arm (here, lossy relay) rather than forcing a
  sub-ceiling. Three escalating hardening passes is "reasonable iteration"; do not manufacture a
  ceiling by unfair puzzling.
- **Deep delegation REQUIRES driver-orchestration (confirmed fix for v2's blocker D).** Nested
  Agent-tool delegation collapses past ~hop 4. Spawning a FRESH top-level Agent per hop and passing
  the relay via on-disk files (pointer/relay-N.txt) achieved true depth-8 delegation with verbatim
  staying lossless across all hops. The skill's "dispatch the authoring to a subagent" model should
  state explicitly that **multi-hop delegation experiments must be driver-orchestrated hop-by-hop**,
  not delegated recursively.
- **Hand-templated per-hop prompts are error-prone at scale** (~150 Agent calls here; two path
  typos slipped in, though the subject agents self-recovered). A generated driver (emit the N
  per-hop prompts from the trial manifest) would remove transcription risk — worth a small harness
  helper in the skill's experiment scaffold.
- **Measure the ARTIFACT, not just the hand-back.** v2 measured relay-omission on the return
  message and stopped there. v3's decisive result needed scoring the terminal **artifact** — the
  omission only matters if it reaches the deliverable, and whether it does is task-dependent
  (reconstructable vs arbitrary). The fidelity gate should require "score the thing that ships,"
  not the intermediate relay, as the primary metric.
