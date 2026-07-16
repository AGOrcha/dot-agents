package config

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	"github.com/AGOrcha/dot-agents/commands/workflow"
	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// Verify check status values. A report is OK unless at least one check is
// verifyFail; verifyWarn is advisory (e.g. an optional integration is absent,
// or a remote layer cannot be confirmed without re-fetching).
const (
	verifyPass = "pass"
	verifyWarn = "warn"
	verifyFail = "fail"
)

// VerifyCheck is one line in the verify report.
type VerifyCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass | warn | fail
	Detail string `json:"detail,omitempty"`
}

// VerifyReport is the stable JSON shape emitted by `da config verify --json`.
// OK is false iff any check failed (warnings do not flip OK), so CI can gate on
// the top-level boolean without parsing individual checks.
type VerifyReport struct {
	OK     bool          `json:"ok"`
	Checks []VerifyCheck `json:"checks"`
}

// runVerifyOptions captures one invocation's state. The shared stdout/stderr/
// cwd/json surface comes from the embedded runContext; the binary probe is
// injected so the run path is table-drivable without cobra or a real
// code-review-graph install.
type runVerifyOptions struct {
	runContext
	// crgProbe reports code-review-graph binary readiness for the project at
	// root; nil err means available. Injected for tests; nil falls back to the
	// real discovery probe.
	crgProbe func(root string) error
}

func newVerifyCmd(deps Deps) *cobra.Command {
	opts := &runVerifyOptions{}
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run offline repo setup contract checks (no layer re-fetch)",
		Long: `Run standalone setup contract checks for this repository without
re-fetching any config layers (spec config-distribution-model §13.1, §16).

Checks performed:
  - manifest      .agentsrc.json is present and parses
  - config-layers each declared source resolves offline (local source paths
                  must exist)
  - locked-layers each declared ` + "`extends`" + ` layer is pinned in .agentsrc.lock,
                  and for remote (git/http/oci) sources its downloaded assets
                  are present in the local cache at the locked SHA — so remote
                  layers are confirmed offline without re-fetching
  - binary        optional integrations are ready (code-review-graph)

Exits non-zero if any check fails. Warnings (optional integration absent, or a
remote layer that cannot be confirmed offline) do not fail the command.

This is intentionally narrower than ` + "`da doctor`" + `, which audits the full
platform link projection; run that for a complete link/health audit.`,
		Example: exampleBlock(
			"  da config verify",
			"  da config verify --json",
		),
		Args: deps.ExactArgsWithHints(0, "`da config verify` takes no arguments; use --json for machine-readable output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			return runVerify(opts, deps)
		},
	}
	return cmd
}

// runVerify is the test-friendly entry point. It builds the report, renders it
// (JSON or human), and returns a non-nil error iff a hard check failed so cobra
// maps the failure to a non-zero exit status.
func runVerify(opts *runVerifyOptions, deps Deps) error {
	report := buildVerifyReport(opts)

	if opts.jsonOut {
		if err := writeJSON(opts.stdout, report); err != nil {
			return err
		}
	} else {
		printVerifyHuman(opts.stdout, report)
	}

	if !report.OK {
		return deps.ErrorWithHints("config verify found one or more failed checks",
			"Run `da install` to (re)project config and set up hooks.",
			"Run `da config sync` to fetch remote layers, then re-run verify.",
		)
	}
	return nil
}

// buildVerifyReport runs every check in order and aggregates them. A manifest
// failure is terminal — without a parseable manifest there are no layers to
// check — so it short-circuits with OK=false.
func buildVerifyReport(opts *runVerifyOptions) VerifyReport {
	checks := make([]VerifyCheck, 0, 4)

	snap, err := resolveLayered(opts.cwd)
	if err != nil {
		checks = append(checks, VerifyCheck{"manifest", verifyFail, err.Error()})
		return VerifyReport{OK: false, Checks: checks}
	}
	checks = append(checks, VerifyCheck{"manifest", verifyPass, "parsed " + cfg.AgentsRCFile})

	checks = append(checks, verifySources(opts.cwd, snap)...)
	checks = append(checks, verifyLayerLocks(opts.cwd)...)
	checks = append(checks, verifyStaleness(opts.cwd)...)
	checks = append(checks, verifyPreconditionPolicies(snap)...)

	probe := opts.crgProbe
	if probe == nil {
		probe = defaultCRGProbe
	}
	if probe(opts.cwd) != nil {
		checks = append(checks, VerifyCheck{"binary:code-review-graph", verifyWarn, "not installed; graph features will degrade"})
	} else {
		checks = append(checks, VerifyCheck{"binary:code-review-graph", verifyPass, "available"})
	}

	ok := true
	for _, c := range checks {
		if c.Status == verifyFail {
			ok = false
		}
	}
	return VerifyReport{OK: ok, Checks: checks}
}

// verifySources checks each declared source in the repo-local manifest. Local
// sources with an explicit path must exist on disk; the implicit local repo
// layer (no path) always passes. Remote sources are reported by whether any
// `extends` layer actually references them — referenced sources are verified in
// detail by the locked-layers check; unreferenced ones are flagged as unused so
// the output never promises a "below" section that isn't there.
func verifySources(cwd string, snap *cfg.Snapshot) []VerifyCheck {
	repo := repoLocalRaw(snap)
	raw, ok := repo["sources"].([]any)
	if !ok || len(raw) == 0 {
		return []VerifyCheck{{"config-layers", verifyPass, "no external layers declared"}}
	}
	referenced := extendsRefSourceIDs(repo)

	checks := make([]VerifyCheck, 0, len(raw))
	for i, item := range raw {
		checks = append(checks, verifyOneSource(i, item, cwd, referenced))
	}
	return checks
}

// repoLocalRaw returns the raw (un-merged) repo-local layer object from a
// resolved Snapshot's layer stack. verifySources reads `sources` from the raw
// repo-local layer — not the merged Effective config — because `sources` is a
// CategoryScalar field declared once, repo-local only (org-config-resolution
// §4); it never merges across layers. Returns nil when snap is nil or carries
// no repo-local layer.
func repoLocalRaw(snap *cfg.Snapshot) map[string]any {
	if snap == nil {
		return nil
	}
	for _, layer := range snap.Layers {
		if layer.ID == cfg.LayerRepoLocal {
			return layer.Raw
		}
	}
	return nil
}

// verifyOneSource verifies a single declared source entry and returns its
// check. Split out of verifySources' loop so the per-type switch does not
// compound the loop's nesting (keeps each function's cognitive complexity low).
func verifyOneSource(i int, item any, cwd string, referenced map[string]struct{}) VerifyCheck {
	src, ok := item.(map[string]any)
	if !ok {
		return VerifyCheck{fmt.Sprintf("source[%d]", i), verifyFail, "source entry is not an object"}
	}
	typ, _ := src["type"].(string)
	name := sourceLabel(i, src)
	switch typ {
	case "local":
		return verifyLocalSource(name, cwd, src)
	case "git", "http", "oci":
		id, _ := src["id"].(string)
		if _, used := referenced[id]; used && id != "" {
			return VerifyCheck{name, verifyPass, "remote " + typ + " source; its layers are verified in the locked-layers check below"}
		}
		return VerifyCheck{name, verifyPass, "remote " + typ + " source declared but unused (no `extends` layer references it)"}
	case "":
		return VerifyCheck{name, verifyFail, "source is missing a type"}
	default:
		return VerifyCheck{name, verifyWarn, "unknown source type: " + typ}
	}
}

// verifyLocalSource verifies a `local`-type source: the implicit repo layer
// (no path) always passes; an explicit path must exist on disk.
func verifyLocalSource(name, cwd string, src map[string]any) VerifyCheck {
	path, _ := src["path"].(string)
	if path == "" {
		return VerifyCheck{name, verifyPass, "local repo layer"}
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, path)
	}
	if fileExists(abs) {
		return VerifyCheck{name, verifyPass, "local path present: " + path}
	}
	return VerifyCheck{name, verifyFail, "local path missing: " + path}
}

// verifyLayerLocks cross-checks each declared `extends` layer against the
// lockfile and the on-disk layer cache (no fetch). For remote (git/http/oci)
// layers this confirms the downloaded assets are present at the SHA the
// lockfile pins; local-source layers just need a lock entry. Returns nil when
// the project declares no extends (nothing to add to the report).
func verifyLayerLocks(cwd string) []VerifyCheck {
	statuses, err := cfg.VerifyLayerLocks(cwd)
	if err != nil {
		return []VerifyCheck{{"locked-layers", verifyWarn, "could not read lockfile/cache: " + err.Error()}}
	}
	if len(statuses) == 0 {
		return nil
	}
	checks := make([]VerifyCheck, 0, len(statuses))
	for _, s := range statuses {
		name := "layer:" + s.Ref
		switch {
		case s.OK():
			checks = append(checks, VerifyCheck{name, verifyPass, s.SourceType + " layer cached at " + abbrevSHA(s.SHA)})
		case s.Optional:
			checks = append(checks, VerifyCheck{name, verifyWarn, s.Problem + " [optional]"})
		default:
			checks = append(checks, VerifyCheck{name, verifyFail, s.Problem})
		}
	}
	return checks
}

// verifyStaleness is the §7A local-scope drift check — the first-class
// inputs_digest contract verify gained when the units-lock model was wired
// (section-7a-units-lock-wiring). It is the whole point of §7A on the primary
// verify path: a flat/local-only project that now carries a lockfile reports its
// tracked inputs_digest instead of "nothing to verify", and a project whose
// local config scopes changed since the last resolve surfaces the drift.
//
// The check is purely content-hash driven (cfg.Staleness compares the recomputed
// inputs_digest, the declared unit set, and recorded unit digests against the
// lock — never a clock), matching the §7A done-criterion "no clock-driven
// staleness anywhere".
//
// Status policy (deliberate):
//   - pass  — the lock is fresh: inputs_digest matches and the declared set is
//     unchanged. Detail shows the tracked inputs_digest so a local-only project
//     proves it is pinned.
//   - warn  — local-scope drift (inputs_digest mismatch or a changed declared
//     set), OR no lockfile/inputs_digest recorded yet. Drift is recoverable by
//     re-resolving, so it is advisory ("run `da config sync`") rather than a
//     hard failure that would block CI on an editable local overlay; it does not
//     flip the report's OK.
//   - warn  — the lock/manifest could not be read (the manifest check above owns
//     the hard failure; a lock read error here is reported but not fatal).
//   - FAIL  — unit-digest-mismatch ONLY (package-artifact-install spec H7): a
//     `kind:artifact` unit whose installed CAS content no longer matches its
//     locked digest — a post-install store tamper, not a benign local-scope
//     edit — surfaces here as a hard failure so `config verify` cannot report
//     OK while the installed content is not what the lock says it is.
//
// H7 wires cfg.Staleness's UnitDigestFunc to the production artifact-store
// integrity resolver (previously nil here, per the spec's own gap callout),
// so this offline/no-refetch check can detect a tamper it previously had no
// way to see.
func verifyStaleness(cwd string) []VerifyCheck {
	const name = "config-staleness"
	res, err := cfg.Staleness(cwd, "", lifecycle.PackagesArtifactDigestResolver(cwd))
	if err != nil {
		return []VerifyCheck{{name, verifyWarn, "could not compute staleness: " + err.Error()}}
	}
	if res.Fresh {
		return []VerifyCheck{{name, verifyPass, "local config in sync (inputs_digest " + abbrevSHA(res.ExpectedInputsDigest) + ")"}}
	}
	// A unit-digest mismatch is ALWAYS a hard failure (review #1): a tampered
	// store must FAIL verify regardless of whether ordinary local-scope drift
	// (inputs/declared-set) is ALSO present. Requiring the reasons list to be
	// EXACTLY [ReasonUnitDigest] let tamper-plus-any-drift downgrade to a warn
	// with OK=true — an integrity bypass. Report the co-occurring drift in the
	// detail, but the check is a failure.
	if reasonsContainUnitDigest(res.Reasons) {
		detail := "an installed packages artifact no longer matches its locked digest (possible store tamper) — run `da install` to re-verify/re-materialize"
		if len(res.Reasons) > 1 {
			detail += " (local config scopes also drifted; re-resolve after re-verifying the store)"
		}
		return []VerifyCheck{{name, verifyFail, detail}}
	}

	recorded := recordedInputsDigest(cwd)
	if recorded == "" {
		return []VerifyCheck{{name, verifyWarn, "no inputs_digest recorded in " + cfg.AgentsLockFile + " — run `da config sync` to create the lock"}}
	}
	return []VerifyCheck{{name, verifyWarn, fmt.Sprintf(
		"local config changed since last resolve (lock %s, now %s) — run `da config sync`",
		abbrevSHA(recorded), abbrevSHA(res.ExpectedInputsDigest))}}
}

// reasonsContainUnitDigest reports whether a unit-digest mismatch is among the
// staleness reasons. A store-integrity failure is a hard FAIL whether or not it
// co-occurs with ordinary local-scope drift (inputs-digest / declared-set) —
// requiring it to be the SOLE reason was an integrity bypass (review #1): a
// tampered store that also had any config edit downgraded to a recoverable
// warn with OK=true.
func reasonsContainUnitDigest(reasons []cfg.StalenessReason) bool {
	for _, r := range reasons {
		if r == cfg.ReasonUnitDigest {
			return true
		}
	}
	return false
}

// recordedInputsDigest reads the inputs_digest the lockfile currently pins, or
// "" when no lock/digest exists, so verifyStaleness can tell "drifted from a
// recorded digest" apart from "never resolved". A read error degrades to "" —
// the staleness check has already reported the actionable warning.
func recordedInputsDigest(cwd string) string {
	lock, err := cfg.ReadUnits(cwd)
	if err != nil {
		return ""
	}
	return lock.InputsDigest
}

// verifyPreconditionPolicies fail-closed-validates the verifier precondition
// policy config (verifier-precondition-policy plan, Slice B5) against the
// resolved layered Snapshot's effective config (snap.Effective) — so a policy
// registry supplied only by an imported `extends` layer (a team-source's
// verifier_chain) is validated exactly like a repo-local one:
//
//   - a StageProfile.precondition_policy naming a key absent from the top-level
//     precondition_policies registry → fail (the resolver degrades such a profile
//     to the built-in default, so a typo silently weakens the gate unless caught
//     here);
//   - a PredicateSpec.signal whose kind no evaluator handles → fail (it would be
//     fail-closed at verify-transition time, but the operator should learn at
//     config time, not when a task is stuck at the gate).
//
// Snapshot resolution already decodes precondition_policies/stage_profiles
// through the typed AgentsRC schema — a malformed shape fails resolveLayered
// itself, before this is ever reached — so there is no decode-error branch
// here. Returns nil when snap is nil, or the project declares no precondition
// policies and no profile references one — there is nothing to add to the
// report.
func verifyPreconditionPolicies(snap *cfg.Snapshot) []VerifyCheck {
	if snap == nil {
		return nil
	}
	rc := &snap.Effective
	if len(rc.PreconditionPolicies) == 0 && !anyProfileNamesPolicy(rc.StageProfiles) {
		return nil
	}
	checks := verifyPolicyReferences(rc)
	checks = append(checks, verifyPolicySignalKinds(rc)...)
	if len(checks) == 0 {
		return []VerifyCheck{{"precondition-policies", verifyPass, "all policy references and signal kinds resolve"}}
	}
	return checks
}

// anyProfileNamesPolicy reports whether any stage profile references a
// precondition policy by name, so verifyPreconditionPolicies still validates a
// dangling reference even when the registry itself is empty.
func anyProfileNamesPolicy(stages map[string]map[string]cfg.StageProfile) bool {
	for _, profiles := range stages {
		for _, p := range profiles {
			if p.PreconditionPolicy != "" {
				return true
			}
		}
	}
	return false
}

// verifyPolicyReferences checks that every StageProfile.precondition_policy
// names a declared key in precondition_policies. Each dangling reference is one
// failed check naming the stage, profile slug, and missing policy. Profiles are
// visited in a stable (stage, slug) order so the report is deterministic.
func verifyPolicyReferences(rc *cfg.AgentsRC) []VerifyCheck {
	var checks []VerifyCheck
	for _, stage := range sortedMapKeys(rc.StageProfiles) {
		profiles := rc.StageProfiles[stage]
		for _, slug := range sortedMapKeys(profiles) {
			name := profiles[slug].PreconditionPolicy
			if name == "" {
				continue
			}
			if _, ok := rc.PreconditionPolicies[name]; !ok {
				checks = append(checks, VerifyCheck{
					fmt.Sprintf("precondition-policy:%s/%s", stage, slug),
					verifyFail,
					fmt.Sprintf("stage profile %q references undeclared precondition policy %q (add it to precondition_policies or fix the name)", stage+"/"+slug, name),
				})
			}
		}
	}
	return checks
}

// verifyPolicySignalKinds checks that every predicate signal across all declared
// policies resolves to a registered evaluator kind (workflow.ValidSignalKind).
// Each unregistered kind is one failed check naming the policy and signal.
// Policies and predicates are visited in a stable order for a deterministic
// report.
func verifyPolicySignalKinds(rc *cfg.AgentsRC) []VerifyCheck {
	var checks []VerifyCheck
	for _, name := range sortedMapKeys(rc.PreconditionPolicies) {
		for _, pred := range rc.PreconditionPolicies[name].Predicates {
			if workflow.ValidSignalKind(pred.Signal) {
				continue
			}
			checks = append(checks, VerifyCheck{
				fmt.Sprintf("precondition-signal:%s", name),
				verifyFail,
				fmt.Sprintf("policy %q predicate names unregistered signal kind %q (no evaluator handles it)", name, pred.Signal),
			})
		}
	}
	return checks
}

// sortedMapKeys returns the keys of any string-keyed map in lexical order, so a
// policy/profile report is deterministic regardless of Go map iteration order.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// abbrevSHA shortens a resolved SHA for the human render without assuming a
// minimum length (git SHAs and content hashes both flow through here).
func abbrevSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// extendsRefSourceIDs returns the set of source ids referenced by the manifest's
// `extends` layers. An extends ref is "source-id:layer-path[@version]", so the
// source id is the segment before the first ':'. Used to tell a remote source
// that is actually consumed from one that is merely declared.
func extendsRefSourceIDs(repo map[string]any) map[string]struct{} {
	ids := map[string]struct{}{}
	raw, ok := repo["extends"].([]any)
	if !ok {
		return ids
	}
	for _, item := range raw {
		var ref string
		switch v := item.(type) {
		case string:
			ref = v
		case map[string]any:
			ref, _ = v["ref"].(string)
		}
		if i := strings.IndexByte(ref, ':'); i > 0 {
			ids[ref[:i]] = struct{}{}
		}
	}
	return ids
}

// sourceLabel names a source check using its stable id when declared, else a
// positional fallback annotated with the type.
func sourceLabel(i int, src map[string]any) string {
	if id, ok := src["id"].(string); ok && id != "" {
		return "source:" + id
	}
	typ, _ := src["type"].(string)
	if typ == "" {
		typ = "?"
	}
	return fmt.Sprintf("source[%d](%s)", i, typ)
}

// defaultCRGProbe reports whether code-review-graph is discoverable for root.
// It wraps the shared discovery used by the kg commands so verify and the
// graph-update degrade path agree on "installed".
func defaultCRGProbe(root string) error {
	_, err := graphstore.DiscoverCRGBin(root)
	return err
}

// printVerifyHuman renders the report as an aligned, scannable check list with
// a one-line summary footer.
func printVerifyHuman(w io.Writer, report VerifyReport) {
	fmt.Fprintln(w, "Config verify (offline contract checks):")
	fmt.Fprintln(w)
	pass, warn, fail := 0, 0, 0
	for _, c := range report.Checks {
		switch c.Status {
		case verifyPass:
			pass++
		case verifyWarn:
			warn++
		case verifyFail:
			fail++
		}
		line := fmt.Sprintf("  [%s] %-28s", verifyMark(c.Status), c.Name)
		if c.Detail != "" {
			line += " " + c.Detail
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d passed, %d warning(s), %d failed — %s\n",
		pass, warn, fail, verifyOutcome(report.OK))
}

// verifyMark maps a status to a fixed-width glyph for the human render.
func verifyMark(status string) string {
	switch status {
	case verifyPass:
		return "ok "
	case verifyWarn:
		return "warn"
	case verifyFail:
		return "FAIL"
	default:
		return "?  "
	}
}

func verifyOutcome(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}
