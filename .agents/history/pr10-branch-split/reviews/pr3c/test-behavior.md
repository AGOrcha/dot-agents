# Test + Behavior Review — pr3c/kg (PR#18)

_Lens 2 of 3 (tests + code), general-purpose reviewer, read-only.
Range: `pr3b-rebased..HEAD`._

## Verdict
**approve**

## Critical findings
None.

## Major findings
None.

## Minor findings / nits
- **Defensive `t.Skip("dependency layout mismatch — skip")`
  (bridge_fault_test.go:92, 1003)** fires when `GetNode` returns nil/err right
  after a successful `UpsertNode`. A silent skip there would mask a real
  graphstore upsert→get regression rather than fail. Defensible fixture guard;
  consider `t.Fatalf` instead. graphstore-adjacent (pr3b territory).
- **`assertNeighborYieldsNothing` merges two assertions** — pre-refactor
  `all_neighbors_seen` used two `t.Errorf` with distinct messages; helper folds
  into one `done || len != 0`. Coverage identical; diagnostics marginally
  coarser. Acceptable.
- POSIX-only `/dev/null` skips carry a clear rationale pointing to equivalent
  non-POSIX coverage. Correct (CI is POSIX).

## Notes
1. **Bug fix `f06ab87` — correct, root-cause, properly pinned.** Old
   `persistReweavedNote` called `updateGraphNote(...,"")` and returned on
   success, making the body-preserving fallback dead code and silently wiping
   bodies on every reweave. Fix removes the empty-body happy path, reads the
   existing body off disk (`parseGraphNote`), passes it through.
   `TestPersistReweavedNote_PreservesBody` is a true regression test (asserts
   broken link removed AND body survives); curation E2E re-pins it
   independently (`curationStepReweave`). Two-site coverage.
2. **Curation E2E refactor (`d39c6cb`) — semantically equivalent.** Verified
   `bf3fde8` vs HEAD: identical bodies moved into 13 named `curationStep*` fns,
   ordered slice + `if !t.Run(...){return}` preserves order and
   abort-on-first-failure; `st.now` set/read flow matches original.
   Production-realistic (real runKG* against real KG_HOME + SQLite warm layer).
3. **Bridge fault tests** — all three no-result branches (seen / kind-filter /
   missing-neighbor) remain distinct subtests through
   `assertNeighborYieldsNothing` (`err==nil && !done && len==0`), identical to
   pre-refactor.
4. **kg dispatch test** — byte-identical coverage; only closures hoisted to
   package-level `findKGSub`/`dispatchKG*`.
5. **Coverage realism** — `seams.go` IO-fault vars reach genuine production
   error branches (otherwise unreachable); CRG tests use fake stubs in a temp
   `.venv/bin` + `t.Chdir`, deterministic. Would catch real `da kg`
   regressions.
6. **CRG skip behavior** — correct; fault tests provision their own fake CRG,
   no real-CRG dependency; `crg_backed_wrappers` isolates PATH deliberately.
7. **`wiring_test.go` S8193** — identical behavior; only drops the unused
   `got` binding.
