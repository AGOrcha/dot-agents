// Package harness is the R4 eval driver: a single [Harness.Run] entry point
// that sequences the five eval stages and returns an [EvalRun] aggregating
// every stage's output.
//
// The five stages, in order:
//
//  1. generate — resolve the language's generator from the injected registry
//     and synthesise a versioned eval.TaskSpec from the knowledge graph.
//  2. provision — create an isolated sandbox instance for the run.
//  3. run — invoke the agent runner inside the sandbox workdir.
//  4. verify — run the language verifier's build/test commands over the
//     sandbox workdir.
//  5. score — bridge the completed run into R1: emit the iteration record and
//     persist the rubric score.
//
// The harness is pure orchestration. Every stage's heavy lifting lives behind
// a seam interface injected at construction (an eval.Registry of generators, a
// sandbox.Sandbox, a runner.Runner, and per-language verifier.Verifiers), so
// a test can swap a FakeRunner or a fixture sandbox without touching the
// driver. The driver owns only the sequencing and the failure policy:
//
//   - An infrastructure failure — no generator or verifier for the language, a
//     provision error, an agent launch failure, a verifier step that could not
//     start, a scoring write error, or a sandbox cleanup failure — is surfaced
//     as an error.
//   - A completed-but-wrong outcome is not an error. A failing build or test is
//     recorded in the score: the verifier encodes it in its VerifyResult, which
//     is what drives the rubric's verifier signal. A non-zero agent exit code is
//     likewise not an error — the runner encodes it in its Result, which the
//     harness captures on the returned EvalRun. The exit code itself is not a
//     scoring input: the score is driven by the verify outcome and the agent
//     telemetry (retries, token usage) that flow into the scoringbridge; a run
//     that exits non-zero but still verifies clean scores as a pass.
package harness
