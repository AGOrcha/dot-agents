package platform

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// cc_pipeline.go is the Claude-Code dynamic-workflow PipelineProjector: the
// second harness projection of the O1 Layer-2 profile IR (full-loop-craft
// §2/§6). Where the OMP projector renders swarm YAML, Claude-Code workflows are
// JS programs (`.claude/workflows/*.mjs`) evaluated by the workflow harness with
// `agent`/`parallel`/`phase`/`log`/`args` injected as globals and the trailing
// top-level `return` consumed as the run result (the ultracode-wave-engine
// idiom).
//
// Artifact shape (the key contract decision): rather than regenerating whole
// program logic per profile — craft §7's anti-pattern of hand-edited/churning
// emitted artifacts — each emitted `.mjs` is split into two clearly-bannered
// regions:
//
//   - GENERATED MANIFEST: an `export const MANIFEST = {…}` block holding the
//     ONLY spec-dependent data (per-stage model routes, topology, caps, digest).
//     ultracode-wave-engine loads its config as an inline top-level const
//     overridable via `args` — never a sibling JSON file — so the manifest is
//     embedded as a const block, not a separate `.mjs`+JSON pair.
//   - STABLE RUNNER: a fixed template that consumes MANIFEST and never names a
//     concrete model, slug, or count. Its bytes are IDENTICAL across every
//     profile and app_type, so the "program logic" is authored once (here, as a
//     Go const) and only the data manifest is regenerated.
//
// Two artifacts mirror the OMP pair: the per-task inner pipeline
// (profile_resolve → executor → ≤7 verify → ≤4 routine lens → cross-family gate
// → evidence gate, each stage pinned to its resolved model route) and the
// post-barrier reconcile pass. Output is deterministic and carries no wall-clock
// value: the same spec re-emits byte-identical `.mjs`, and the header stamps the
// shared PipelineSpec.Digest so stage_profiles drift is visible without a
// re-resolve.

const (
	// CCPipelineArtifactName is the emitted per-task inner-pipeline workflow.
	CCPipelineArtifactName = "profile-pipeline.mjs"
	// CCReconcileArtifactName is the emitted reconcile-pass workflow.
	CCReconcileArtifactName = "profile-reconcile.mjs"

	// ccStableRunnerMarker is the single banner separating the generated manifest
	// region from the fixed runner template. Everything from this line to EOF is
	// byte-identical across every emitted profile/app_type.
	ccStableRunnerMarker = "// ============================= STABLE RUNNER ============================="
)

// ccStageRoute is one stage's resolved route in the emitted JS manifest.
type ccStageRoute struct {
	Slug   string `json:"slug"`
	Model  string `json:"model"`
	Family string `json:"family"`
}

// ccCaps mirrors the craft §4 cardinality caps into the manifest so the runner
// can log/verify the fan-out shape it was projected under.
type ccCaps struct {
	MaxVerifiers     int `json:"maxVerifiers"`
	MaxRoutineLenses int `json:"maxRoutineLenses"`
}

// ccManifest is the generated stage table embedded as `const MANIFEST` in the
// per-task pipeline `.mjs`. It is the only spec-dependent region of the artifact.
type ccManifest struct {
	AppType         string         `json:"appType"`
	Workspace       string         `json:"workspace"`
	TargetCount     int            `json:"targetCount"`
	ConfigDigest    string         `json:"configDigest"`
	CrossFamilySlug string         `json:"crossFamilySlug"`
	Caps            ccCaps         `json:"caps"`
	Executor        ccStageRoute   `json:"executor"`
	Verifiers       []ccStageRoute `json:"verifiers"`
	RoutineLenses   []ccStageRoute `json:"routineLenses"`
	CrossFamily     *ccStageRoute  `json:"crossFamily"`
}

// ccReconcileManifest is the generated manifest embedded in the reconcile-pass
// `.mjs`: the reconcile stage is app_type-agnostic lifecycle logic, so only the
// workspace and the executor route (plus the shared digest) come from the spec.
type ccReconcileManifest struct {
	Workspace    string `json:"workspace"`
	ConfigDigest string `json:"configDigest"`
	Model        string `json:"model"`
	Family       string `json:"family"`
}

// ccPipelineProjector emits Claude-Code dynamic workflows from a PipelineSpec.
type ccPipelineProjector struct{}

// Platform is the Claude-Code platform id (the canonical harness id used across
// the repo: eval runner, config, and telemetry).
func (ccPipelineProjector) Platform() string { return "claude-code" }

// RuntimeRelDir is the repo-relative directory the Claude-Code workflows are
// emitted into (alongside the hand-authored `.claude/workflows/*.mjs`).
func (ccPipelineProjector) RuntimeRelDir() string { return filepath.Join(".claude", "workflows") }

// Emit renders both Claude-Code workflow artifacts from the spec. An
// app_type-agnostic spec (AppType == "") emits the canonical filenames; a
// specialized spec emits app_type-qualified filenames so it never clobbers the
// canonical skeleton.
func (p ccPipelineProjector) Emit(spec PipelineSpec) ([]PipelineArtifact, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	digest := spec.Digest()
	return []PipelineArtifact{
		{Name: ccArtifactName(CCPipelineArtifactName, spec.AppType), Content: p.emitPipeline(spec, digest)},
		{Name: ccArtifactName(CCReconcileArtifactName, spec.AppType), Content: p.emitReconcile(spec, digest)},
	}, nil
}

// ccArtifactName qualifies a `.mjs` filename with the app_type when the spec is
// specialized (e.g. "profile-pipeline.mjs" + "docs/web" ->
// "profile-pipeline.docs-web.mjs"), leaving the canonical name unchanged for the
// skeleton. A "/" in an app_type is sanitized to "-".
func ccArtifactName(base, appType string) string {
	appType = strings.TrimSpace(appType)
	if appType == "" {
		return base
	}
	safe := strings.ReplaceAll(appType, "/", "-")
	stem := strings.TrimSuffix(base, ".mjs")
	return stem + "." + safe + ".mjs"
}

// ccGeneratedHeader is the two-line JS generation stamp: the do-not-hand-edit
// marker (reusing the shared marker text) and the config digest (no wall-clock).
func ccGeneratedHeader(digest string) string {
	msg := strings.TrimPrefix(generatedHeaderLine, "# ")
	return fmt.Sprintf("// %s\n// config-digest: %s\n", msg, digest)
}

// ccBuildManifest projects the resolved routes/topology into the generated
// manifest. Verifier/lens slices are always non-nil so the JS renders `[]` (not
// `null`), keeping the runner's `.map`/`.length` calls total.
func ccBuildManifest(spec PipelineSpec) ccManifest {
	toRoute := func(r StageRoute) ccStageRoute {
		return ccStageRoute{Slug: r.Slug, Model: r.Model, Family: r.ModelFamily}
	}
	m := ccManifest{
		AppType:         spec.AppType,
		Workspace:       spec.Workspace,
		TargetCount:     spec.TargetCount,
		ConfigDigest:    spec.Digest(),
		CrossFamilySlug: crossFamilyLensSlug,
		Caps:            ccCaps{MaxVerifiers: maxPipelineVerifiers, MaxRoutineLenses: maxPipelineRoutineLenses},
		Executor:        toRoute(spec.Executor),
		Verifiers:       make([]ccStageRoute, 0, len(spec.Verifiers)),
		RoutineLenses:   make([]ccStageRoute, 0, len(spec.RoutineLenses)),
	}
	for _, v := range spec.Verifiers {
		m.Verifiers = append(m.Verifiers, toRoute(v))
	}
	for _, l := range spec.RoutineLenses {
		m.RoutineLenses = append(m.RoutineLenses, toRoute(l))
	}
	if spec.CrossFamily != nil {
		cf := toRoute(*spec.CrossFamily)
		m.CrossFamily = &cf
	}
	return m
}

// emitPipeline renders profile-pipeline.mjs: header + meta + generated manifest
// + stable runner. MarshalIndent of the struct is deterministic (fixed field
// order, no maps), so the same spec re-emits byte-identical content.
func (ccPipelineProjector) emitPipeline(spec PipelineSpec, digest string) string {
	data, _ := json.MarshalIndent(ccBuildManifest(spec), "", "  ")
	var b strings.Builder
	b.WriteString(ccGeneratedHeader(digest))
	b.WriteString(ccPipelineHeaderDoc)
	b.WriteString(ccPipelineMeta)
	b.WriteString("\n")
	b.WriteString(ccManifestBeginBanner)
	b.WriteString("export const MANIFEST = ")
	b.Write(data)
	b.WriteString("\n")
	b.WriteString(ccManifestEndBanner)
	b.WriteString("\n")
	b.WriteString(ccStableRunnerMarker)
	b.WriteString("\n")
	b.WriteString(ccPipelineRunnerBody)
	return b.String()
}

// emitReconcile renders profile-reconcile.mjs, mirroring the OMP reconcile pass:
// only the workspace and the executor route come from the spec; the prose is
// fixed lifecycle driver logic.
func (ccPipelineProjector) emitReconcile(spec PipelineSpec, digest string) string {
	man := ccReconcileManifest{
		Workspace:    spec.Workspace,
		ConfigDigest: digest,
		Model:        spec.Executor.Model,
		Family:       spec.Executor.ModelFamily,
	}
	data, _ := json.MarshalIndent(man, "", "  ")
	var b strings.Builder
	b.WriteString(ccGeneratedHeader(digest))
	b.WriteString(ccReconcileHeaderDoc)
	b.WriteString(ccReconcileMeta)
	b.WriteString("\n")
	b.WriteString(ccManifestBeginBanner)
	b.WriteString("export const MANIFEST = ")
	b.Write(data)
	b.WriteString("\n")
	b.WriteString(ccManifestEndBanner)
	b.WriteString("\n")
	b.WriteString(ccStableRunnerMarker)
	b.WriteString("\n")
	b.WriteString(ccReconcileRunnerBody)
	return b.String()
}

// ---------------------------------------------------------------------------
// Fixed template text. None of the strings below reference a concrete model,
// slug, or count: the runner regions are byte-identical for every profile.
// ---------------------------------------------------------------------------

const ccManifestBeginBanner = "// ========================= GENERATED MANIFEST =========================\n" +
	"// Regenerated from the profile IR on every emit; the ONLY spec-dependent region.\n"

const ccManifestEndBanner = "// ======================= END GENERATED MANIFEST =======================\n"

const ccPipelineHeaderDoc = `//
// Claude Code dynamic-workflow projection of the resolved profile IR
// (stage_profiles + execution_profile topology, full-loop-craft §1/§2/§6). This
// file is a GENERATED build artifact: regenerate it with
// da workflow pipeline emit --platform claude-code rather than hand-editing
// (craft §7 anti-pattern: hand-edited emitted artifacts). The GENERATED MANIFEST
// block below is the only spec-dependent region; the STABLE RUNNER beneath it is
// a fixed template, byte-identical for every profile.
//
// NODE VALIDATION: this is NOT a plain ES module and does not pass a raw
// node --check. Like the authored .claude/workflows/ultracode-wave-engine.mjs it
// ends with a top-level return the harness consumes as the run result after
// wrapping the file in an injected (async) function scope; a raw module rejects
// that return by design. Validate the harness-eval form, never the raw module
// (internal/platform/cc_pipeline_test.go).

`

const ccPipelineMeta = `export const meta = {
  name: 'da-profile-pipeline',
  description:
    'Per-task inner pipeline projected from the resolved profile IR: profile_resolve → executor → verify×N → routine-lens×M → cross-family gate → evidence gate, each stage pinned to its resolved model route. Regenerate via da workflow pipeline emit --platform claude-code; do not hand-edit.',
  phases: [
    { title: 'Resolve' },
    { title: 'Execute' },
    { title: 'Verify' },
    { title: 'Review' },
    { title: 'Gate' },
  ],
}
`

const ccPipelineRunnerBody = `// Everything below consumes MANIFEST and never names a concrete model, slug, or
// count, so it is byte-identical for every emitted profile and app_type. Do not
// hand-edit; re-run da workflow pipeline emit --platform claude-code instead.

const WF_ARGS = typeof args !== 'undefined' && args ? args : {}

// assertCrossFamily enforces full-loop-craft §2 RULE-7: the blocking cross-family
// lens MUST bind by its NAMED slug and route to a model family different from the
// executor's. A same-family or mis-slugged cross lens is a refusal, never a pass.
export function assertCrossFamily(m) {
  const cf = m && m.crossFamily
  if (!cf) return null
  if (!cf.slug || cf.slug !== m.crossFamilySlug) {
    throw new Error('cross-family lens must bind the named slug ' + m.crossFamilySlug + ', got ' + (cf.slug || '(none)'))
  }
  if (cf.family === m.executor.family) {
    throw new Error('cross-family lens family ' + cf.family + ' must differ from executor family ' + m.executor.family + ' (RULE-7)')
  }
  return cf
}

// parseVerdict extracts the outcome token from an agent's unstructured return
// text. Tokens are case-sensitive and the LAST occurrence wins (the agent's
// final word is its verdict). Text naming no token is FAIL-safe 'UNKNOWN': it
// never short-circuits downstream stages but is still reported to the gate.
export function parseVerdict(text) {
  const s = typeof text === 'string' ? text : text == null ? '' : String(text)
  let best = 'UNKNOWN'
  let at = -1
  for (const tok of ['PASS', 'FAIL', 'SKIP', 'APPROVE', 'REJECT']) {
    const i = s.lastIndexOf(tok)
    if (i > at) {
      at = i
      best = tok
    }
  }
  return best
}

// stageSequence materializes the ordered per-task pipeline from the manifest:
// profile_resolve → executor → verify×N → routine-lens×M → cross-family? → gate.
// Each descriptor carries the resolved model/family route the driver pins the
// stage to — the single source of truth for both the live driver and the tests.
export function stageSequence(m) {
  assertCrossFamily(m)
  const seq = []
  seq.push({ kind: 'profile_resolve', phase: 'Resolve', model: m.executor.model, family: m.executor.family })
  seq.push({ kind: 'executor', phase: 'Execute', model: m.executor.model, family: m.executor.family })
  m.verifiers.forEach((r, i) =>
    seq.push({ kind: 'verify', phase: 'Verify', index: i + 1, slug: r.slug, model: r.model, family: r.family })
  )
  m.routineLenses.forEach((r, i) =>
    seq.push({ kind: 'routine', phase: 'Review', index: i + 1, slug: r.slug, model: r.model, family: r.family })
  )
  if (m.crossFamily) {
    seq.push({ kind: 'cross_family', phase: 'Review', slug: m.crossFamily.slug, model: m.crossFamily.model, family: m.crossFamily.family })
  }
  seq.push({ kind: 'gate', phase: 'Gate', model: m.executor.model, family: m.executor.family })
  return seq
}

// stagePrompt renders the harness driver prompt for one stage. The prose is
// driver logic (resolve the concrete prompt at runtime); only the model/family
// route and the task come from the manifest.
export function stagePrompt(st, m, task) {
  const route = 'model=' + st.model + ', family=' + st.family
  switch (st.kind) {
    case 'profile_resolve':
      return 'Resolve the profile for task ' + task + ' in ' + m.workspace + '. Confirm the resolved routes match this manifest (' + route + '), refuse any stage with an empty model or model_family, and require the cross-family lens family to differ from the executor family. Write $COORD/profile.json.'
    case 'executor':
      return 'Implement task ' + task + ' in ' + m.workspace + ' under the delegated write_scope. Confirm executor ' + route + '. Address prior fold-back first, run focused tests through the production path, and emit $COORD/impl.md (DONE|FAIL|SKIP). Never mutate canonical plan/task state.'
    case 'verify':
      return 'Run verifier slot ' + st.index + ' (' + (st.slug || 'runtime-resolved') + ') for task ' + task + '. Require this pass to route ' + route + '. Gate on the prior slot PASS and emit $COORD/verify-' + st.index + '.md (PASS|FAIL|SKIP) read-only.'
    case 'routine':
      return 'Run routine review lens ' + st.index + ' (' + (st.slug || 'runtime-resolved') + ') for task ' + task + '. Require ' + route + '. Review read-only and emit $COORD/review-routine-' + st.index + '.md (APPROVE|REJECT|SKIP).'
    case 'cross_family':
      return 'Run the blocking cross-model-family adversarial lens (named slug ' + st.slug + ') for task ' + task + '. Require ' + route + ' and a family different from the executor. Review read-only, any BLOCKER/HIGH rejects, and emit $COORD/review-cross-family.md (APPROVE|REJECT|SKIP).'
    case 'gate':
      return 'Gate task ' + task + ': read all impl/verify/review evidence. On complete evidence write $COORD/READY.md, otherwise write $COORD/GATE.md with FOLD-BACK and every actionable blocker for the next bounded iteration. Confirm ' + route + '. Never merge, advance, or mutate canonical plan/task state.'
    default:
      return 'Unknown stage ' + st.kind + ' for task ' + task + '.'
  }
}

// stagePromptWithPrior prepends the serialized prior-stage verdicts so every
// stage after profile_resolve — and the evidence gate especially — decides from
// driver-visible state, not only the $COORD files. profile_resolve runs against
// an empty ledger, so it receives the bare prompt.
export function stagePromptWithPrior(st, m, task, verdicts) {
  const base = stagePrompt(st, m, task)
  return Object.keys(verdicts).length ? 'Prior outcomes: ' + JSON.stringify(verdicts) + '\n' + base : base
}

const harnessReady =
  typeof agent === 'function' &&
  typeof parallel === 'function' &&
  typeof phase === 'function' &&
  typeof log === 'function'

if (harnessReady) {
  const task = WF_ARGS.task || '(bundle-selected)'
  const seq = stageSequence(MANIFEST)
  const verdicts = {}
  // runStage runs one agent stage with the accumulated prior outcomes prepended
  // and folds the parsed verdict back into the ledger the next prompt reads.
  const runStage = async (st, opts) =>
    parseVerdict(await agent(stagePromptWithPrior(st, MANIFEST, task, verdicts), opts))
  // Resolve + Execute (sequential): profile_resolve's parsed outcome flows into
  // every subsequent stage prompt via the shared verdicts ledger.
  for (const st of seq.filter((s) => s.kind === 'profile_resolve' || s.kind === 'executor')) {
    phase(st.phase)
    log(st.kind + ': ' + st.model + ' (' + st.family + ')')
    verdicts[st.kind] = await runStage(st, {
      label: st.kind + ':' + task,
      phase: st.phase,
      model: st.model,
      agentType: st.kind === 'executor' ? 'loop-worker' : 'task',
    })
  }
  // Verify slots (sequential — each gates on the prior slot PASS). The first FAIL
  // short-circuits: no further verify slot launches and the remaining slots are
  // recorded SKIPPED:downstream-of-FAIL so the gate sees where verification died.
  phase('Verify')
  let verifyFailed = false
  for (const st of seq.filter((s) => s.kind === 'verify')) {
    if (verifyFailed) {
      verdicts['verify_' + st.index] = 'SKIPPED:downstream-of-FAIL'
      log('verify ' + st.index + '/' + MANIFEST.verifiers.length + ': skipped (downstream of FAIL)')
      continue
    }
    log('verify ' + st.index + '/' + MANIFEST.verifiers.length + ': ' + st.model + ' (' + st.family + ')')
    const v = await runStage(st, {
      label: 'verify:' + task + ':' + st.index,
      phase: 'Verify',
      model: st.model,
    })
    verdicts['verify_' + st.index] = v
    if (v === 'FAIL') verifyFailed = true
  }
  // Routine lenses (parallel — independent read-only reviews, capped ≤4). They
  // review WORKING code, so a verify FAIL skips the whole group.
  const routine = seq.filter((s) => s.kind === 'routine')
  if (routine.length) {
    phase('Review')
    if (verifyFailed) {
      routine.forEach((st) => {
        verdicts['routine_' + st.index] = 'SKIPPED:downstream-of-FAIL'
      })
      log('routine review: skipped (downstream of verify FAIL)')
    } else {
      const rets = await parallel(
        routine.map((st) => () =>
          agent(stagePromptWithPrior(st, MANIFEST, task, verdicts), {
            label: 'review:' + task + ':' + st.index,
            phase: 'Review',
            model: st.model,
          })
        )
      )
      routine.forEach((st, i) => {
        verdicts['routine_' + st.index] = parseVerdict(rets[i])
      })
    }
  }
  // Blocking cross-family gate (sequential, after the routine lenses). Skipped on
  // a verify FAIL; a REJECT is recorded but NEVER skips the evidence gate.
  const cross = seq.find((s) => s.kind === 'cross_family')
  if (cross) {
    phase('Review')
    if (verifyFailed) {
      verdicts.crossFamily = 'SKIPPED:downstream-of-FAIL'
      log('cross-family: skipped (downstream of verify FAIL)')
    } else {
      log('cross-family: ' + cross.model + ' (' + cross.family + ') via slug ' + cross.slug)
      verdicts.crossFamily = await runStage(cross, {
        label: 'review-cross-family:' + task,
        phase: 'Review',
        model: cross.model,
      })
    }
  }
  // Evidence gate — ALWAYS runs (the fold-back writer). Its prompt already
  // carries the serialized verdicts via stagePromptWithPrior, so GATE.md/READY.md
  // decide from driver-visible state incl. any FAIL/REJECT, not only $COORD files.
  const gate = seq.find((s) => s.kind === 'gate')
  phase('Gate')
  log('gate: ' + gate.model + ' (' + gate.family + ')')
  verdicts.gate = await runStage(gate, {
    label: 'gate:' + task,
    phase: 'Gate',
    model: gate.model,
  })
}

return {
  note:
    'Per-task profile pipeline complete (or imported as a module). Re-emit with ' +
    'da workflow pipeline emit --platform claude-code after any stage_profiles change.',
}
`

const ccReconcileHeaderDoc = `//
// Claude Code reconcile-pass projection of the resolved profile IR
// (full-loop-craft §2/§6). Post-wave lifecycle reconciliation is app_type-
// agnostic, so only the workspace and the executor model route come from the
// spec; the prose is fixed driver logic. GENERATED build artifact: regenerate
// with da workflow pipeline emit --platform claude-code; do not hand-edit.
//
// NODE VALIDATION: not a plain ES module — the harness consumes the trailing
// top-level return, which a raw node --check rejects by design. Validate the
// harness-eval form, never the raw module (internal/platform/cc_pipeline_test.go).

`

const ccReconcileMeta = `export const meta = {
  name: 'da-profile-reconcile',
  description:
    'Post-wave reconcile pass projected from the profile IR: serialized da lifecycle and meta-loop reconciliation pinned to the executor model route. Regenerate via da workflow pipeline emit --platform claude-code; do not hand-edit.',
  phases: [{ title: 'Reconcile' }],
}
`

const ccReconcileRunnerBody = `// Everything below consumes MANIFEST and never names a concrete model or count,
// so it is byte-identical for every emitted profile. Do not hand-edit; re-run
// da workflow pipeline emit --platform claude-code instead.

const WF_ARGS = typeof args !== 'undefined' && args ? args : {}

// reconcilePrompt renders the fixed reconcile driver logic. Only the workspace
// and the executor route come from the manifest.
export function reconcilePrompt(m) {
  return 'Reconcile one post-wave barrier in ' + m.workspace + '. Confirm reconciler model=' + m.model + ', family=' + m.family + '. ' +
    'Process each task result in manifest order with the repository-HEAD da binary; never edit PLAN.yaml or TASKS.yaml directly. ' +
    'READY: validate the merge-back and delegation gate, leave the PR owner-held, and run the closeout/advance sequence only on an owner outcome. ' +
    'FOLD-BACK: record idempotent task/plan fold-backs and close out decision=reject so the canonical task blocks and its slot frees. ' +
    'FAILED/UNKNOWN/BLOCKED: record a fold-back with the exit and logs, persist the required failed merge-back, and close out decision=reject; never orphan an in_progress delegation or claim success. ' +
    'Re-run da --json workflow slots and da --json workflow eligible after every canonical write, and emit $WAVE_DIR/RECONCILED only when every result has transitioned or has an explicit owner/external wait reason.'
}

const harnessReady =
  typeof agent === 'function' &&
  typeof phase === 'function' &&
  typeof log === 'function'

if (harnessReady) {
  phase('Reconcile')
  log('reconcile: ' + MANIFEST.model + ' (' + MANIFEST.family + ')')
  await agent(reconcilePrompt(MANIFEST), {
    label: 'reconcile',
    phase: 'Reconcile',
    model: MANIFEST.model,
    agentType: 'task',
  })
}

return {
  note:
    'Reconcile pass complete (or imported as a module). Re-emit with ' +
    'da workflow pipeline emit --platform claude-code after any stage_profiles change.',
}
`
