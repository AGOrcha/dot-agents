package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// Exit codes per spec config-distribution-model §10. Only schemaErrCode is
// reachable today in flat mode; the others are reserved for p1b/p1c when
// layered fetch lands.
const (
	exitOK            = 0
	exitLayerFetchErr = 1
	exitSchemaErr     = 2
	exitAuthErr       = 3
)

// Layer identifiers used in provenance output. These mirror the layer names
// from org-config-resolution §4 (flat slice — extends/packages excluded
// because they belong to p1b).
const (
	layerProductDefaults = "product-defaults"
	layerUserLocal       = "user-local"
	layerRepoLocal       = "repo-local"
)

// LayerValue is one slot in a single field's provenance stack.
//
// Active=true on exactly one entry per field, except when no layer sets the
// field (all entries Active=false, all Value=nil).
type LayerValue struct {
	Layer  string `json:"layer"`
	Value  any    `json:"value"`
	Active bool   `json:"active"`
}

// FieldExplanation is the JSON shape emitted by `da config explain <path> --json`.
// The shape is documented in the proposal at
// `.agents/proposals/config-explain-live-surface.md` and intentionally kept
// stable so scripts can pin against it.
type FieldExplanation struct {
	Field       string       `json:"field"`
	Value       any          `json:"value"`
	ActiveLayer string       `json:"active_layer,omitempty"`
	Layers      []LayerValue `json:"layers"`
}

// fileExists is a thin wrapper kept here (rather than inlined) so tests can
// observe the exact "file missing" branch with a deterministic stat err.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// splitFieldPath splits a dot-separated path into traversal parts. Empty input
// returns an empty slice so callers can short-circuit on len==0.
func splitFieldPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// sortedKeys returns the keys of m in deterministic order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ensureResolved is the auto-lock seam `da config explain` consumes. It is a
// package var so tests can inject a hermetic resolver without touching the
// network or the real lock writer. Production points it at the §7A.5 auto-sync
// entry point config.EnsureResolved: running explain makes one cheap, local,
// clock-free staleness decision and either re-resolves (rewriting the lock —
// the `uv tree` auto-lock behavior) or, when the lock is already fresh, reads
// it back read-only via ResolveLocked. Explain is therefore the single
// effective-config truth surface: it consumes the units lock and auto-locks on
// run, absorbing the config-inspection role status used to carry.
var ensureResolved = cfg.EnsureResolved

// loadSnapshot resolves the effective-config snapshot for the project at
// projectPath through the auto-lock seam. The default EnsureOpts selects the
// no-flag path: a fresh lock is a read-only no-op; a stale (or absent) lock is
// re-resolved and the units lock is rewritten. A missing repo-local manifest is
// fatal — there is nothing to explain — and is surfaced as a schema error.
func loadSnapshot(projectPath string) (*cfg.Snapshot, int, error) {
	res, err := ensureResolved(projectPath, cfg.EnsureOpts{})
	if err != nil {
		return nil, exitSchemaErr, err
	}
	return res.Snapshot, exitOK, nil
}

// resolveLayered is the package-level (test-seam) surface `relevance` and
// `verify` route through to resolve the effective-config Snapshot. It is the
// SAME read-only, offline, units-lock-backed resolution path `da config
// explain` and `workflow app-types` already consume
// (cfg.NewLayeredResolver().ResolveLocked — see appTypeSnapshot in
// commands/workflow/app_types.go), so relevance and verify now see `extends`
// layers too (a team-source's execution_profile / verifier_chain), not just
// the flat product-defaults -> user-local -> repo-local stack the retired
// loadFlatSnapshot used to read.
//
// For a project declaring no `extends`, ResolveLocked degrades to the FLAT
// layer set via its embedded FlatResolver, so a flat-only project resolves
// exactly as it did before this migration — the flat fallback is preserved by
// construction (ResolveLocked's own degrade branch), not re-implemented here.
var resolveLayered = func(projectPath string) (*cfg.Snapshot, error) {
	return cfg.NewLayeredResolver().ResolveLocked(projectPath)
}

// explainField builds the FieldExplanation for a single dot-separated field
// path against the resolved snapshot. It delegates the layer walk to the
// canonical Snapshot.FieldAt so explain and the resolver can never disagree on
// which layer wins, then maps the resolver's FieldProvenance into the stable
// FieldExplanation JSON shape.
//
// A field that is unset in every layer returns a FieldExplanation with
// Value=nil and ActiveLayer="" — the caller decides whether to treat that as an
// error (single-field default) or as informational ("not set" in --all).
func explainField(snap *cfg.Snapshot, path string) FieldExplanation {
	fp := snap.FieldAt(path)
	out := FieldExplanation{
		Field:       path,
		ActiveLayer: fp.ActiveLayer,
		Layers:      make([]LayerValue, 0, len(fp.Layers)),
	}
	for _, lv := range fp.Layers {
		out.Layers = append(out.Layers, LayerValue{Layer: lv.Layer, Value: lv.Value, Active: lv.Active})
		if lv.Active {
			out.Value = lv.Value
		}
	}
	return out
}

// runExplainOptions captures the flag state for one invocation. Pulled into a
// struct so the run* helpers stay easy to table-drive in tests.
type runExplainOptions struct {
	all        bool
	flags      bool
	valueOnly  bool
	originOnly bool
	jsonOut    bool
	// Profile context selectors (unified-config-profiles R6): when any is set,
	// explain resolves the effective profile bundle for that dispatch context
	// through the shared selector-merge engine instead of a single field.
	role    string
	appType string
	stage   string
	harness string
	stdout  io.Writer
	stderr  io.Writer
	cwd     string
}

// profileContextRequested reports whether any profile selector flag was set, in
// which case explain resolves the profile bundle for that context.
func (o *runExplainOptions) profileContextRequested() bool {
	return o.role != "" || o.appType != "" || o.stage != "" || o.harness != ""
}

func newExplainCmd(deps Deps) *cobra.Command {
	opts := &runExplainOptions{}
	cmd := &cobra.Command{
		Use:   "explain [field-path]",
		Short: "Show the effective value of a config field and where it came from",
		Long: `Prints the effective value of a single config field plus the full layer
stack that produced it. With --all, prints the entire effective configuration.
With --flags, prints feature-flag resolution.

explain is the single effective-config truth surface: it AUTO-LOCKS on run (like
` + "`uv tree`" + `). It consumes the committed units lock and, when that lock is
stale or absent, re-resolves and rewrites it before rendering; when the lock is
already fresh it reads it back read-only. da status no longer inspects config —
it reports fleet and link health only.

Field paths are dot-separated (e.g. "kg.backend", "features.graph_bridge").

Output is human-readable by default; --json emits a stable machine-readable
shape documented on the FieldExplanation type in this package.

The layer stack (lowest precedence first) is:
  [1] product-defaults     (built-in defaults)
  [2] user-local           (~/.agents/.agentsrc.json, if present)
  [.] imported extends     (reconstructed from the lock at their locked SHA)
  [n] repo-local           (./.agentsrc.json)`,
		Example: exampleBlock(
			"  da config explain",
			"  da config explain repo_id",
			"  da config explain skills --value-only",
			"  da config explain kg.backend --origin-only",
			"  da config explain --all --json",
		),
		Args: deps.MaximumNArgsWithHints(1, "Pass a dot-separated field path, or use --all / --flags for the full view."),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.stdout = cmd.OutOrStdout()
			opts.stderr = cmd.ErrOrStderr()
			opts.jsonOut = deps.jsonFlag() // honor the global persistent --json
			if opts.cwd == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return deps.ErrorWithHints("could not resolve current directory", err.Error())
				}
				opts.cwd = cwd
			}
			return runExplain(opts, args, deps)
		},
	}
	cmd.Flags().BoolVar(&opts.all, "all", false, "Print the full effective configuration annotated with provenance")
	cmd.Flags().BoolVar(&opts.flags, "flags", false, "Print resolved feature flags (features.*) across all layers")
	cmd.Flags().BoolVar(&opts.valueOnly, "value-only", false, "Print only the effective value (JSON-encoded for non-scalars)")
	cmd.Flags().BoolVar(&opts.originOnly, "origin-only", false, "Print only the winning layer identifier")
	cmd.Flags().StringVar(&opts.role, "role", "", "Resolve the effective profile bundle for this runtime role (profile context)")
	cmd.Flags().StringVar(&opts.appType, "app-type", "", "Resolve the effective profile bundle for this app_type (profile context)")
	cmd.Flags().StringVar(&opts.stage, "stage", "", "Resolve the effective profile bundle for this stage (profile context)")
	cmd.Flags().StringVar(&opts.harness, "harness", "", "Resolve the effective profile bundle for this harness (profile context)")
	return cmd
}

// runExplain is the test-friendly entry point — receives an already-prepared
// runExplainOptions so tests can stub cwd, stdout, and stderr without going
// through cobra.
func runExplain(opts *runExplainOptions, args []string, deps Deps) error {
	if err := validateFlagCombo(opts, args, deps); err != nil {
		return err
	}

	snap, code, err := loadSnapshot(opts.cwd)
	if err != nil {
		// Resolution errors are surfaced to the operator via ErrorWithHints so
		// the existing UX layer can add color and hints. The precise exit int
		// is informational here per spec §10 — main() translates non-nil errors
		// uniformly.
		_ = code
		return deps.ErrorWithHints(err.Error(),
			"Run `da install --generate` to create .agentsrc.json from current state.",
		)
	}

	switch {
	case opts.profileContextRequested():
		return emitProfile(opts, snap, deps)
	case opts.flags:
		return emitFlags(opts, snap)
	case opts.all:
		return emitAll(opts, snap)
	case len(args) == 1:
		return emitField(opts, snap, args[0], deps)
	default:
		// No field path and no --all/--flags — default to --all for a useful
		// "show me what's resolved here" invocation.
		opts.all = true
		return emitAll(opts, snap)
	}
}

// validateFlagCombo guards against flag combinations that have no defined
// semantics. --value-only and --origin-only require a single field path
// (they print one value, not a tree).
func validateFlagCombo(opts *runExplainOptions, args []string, deps Deps) error {
	if opts.valueOnly && opts.originOnly {
		return deps.UsageError("--value-only and --origin-only cannot be combined")
	}
	if (opts.valueOnly || opts.originOnly) && len(args) != 1 {
		return deps.UsageError("--value-only and --origin-only require a single field path")
	}
	if (opts.valueOnly || opts.originOnly) && (opts.all || opts.flags) {
		return deps.UsageError("--value-only and --origin-only cannot be combined with --all or --flags")
	}
	if opts.profileContextRequested() {
		if opts.valueOnly || opts.originOnly || opts.all || opts.flags {
			return deps.UsageError("profile context flags (--role/--app-type/--stage/--harness) cannot be combined with --value-only/--origin-only/--all/--flags")
		}
		if len(args) != 0 {
			return deps.UsageError("profile context flags resolve a bundle, not a single field — drop the field-path argument")
		}
	}
	return nil
}

// emitProfile resolves the effective profile bundle for the requested dispatch
// context through the shared selector-merge engine and renders it: the effective
// bundle, every contributing absolute ref (both shown on a same-scope conflict),
// the binding locks with owning scope, the effective permission map, the policy
// mode (replace vs narrow), and the reproducibility digest (R6).
func emitProfile(opts *runExplainOptions, snap *cfg.Snapshot, deps Deps) error {
	resolved, err := cfg.ResolveProfileContext(snap, opts.role, opts.appType, opts.stage, opts.harness)
	if err != nil {
		return deps.ErrorWithHints("resolving profile context", err.Error())
	}
	if opts.jsonOut {
		return writeJSON(opts.stdout, resolved)
	}
	printProfileHuman(opts.stdout, opts, resolved)
	return nil
}

// printProfileHuman renders the resolved profile bundle in the human view.
func printProfileHuman(w io.Writer, opts *runExplainOptions, r cfg.ResolvedProfile) {
	fmt.Fprintln(w, "Effective profile bundle for context:")
	fmt.Fprintf(w, "  role=%s app_type=%s stage=%s harness=%s\n\n",
		emptyAsDash(opts.role), emptyAsDash(opts.appType), emptyAsDash(opts.stage), emptyAsDash(opts.harness))
	fmt.Fprintf(w, "  bundle     : %s\n", formatScalar(r.Bundle))
	fmt.Fprintf(w, "  digest     : %s\n", emptyAsDash(r.Digest))
	fmt.Fprintf(w, "  policy     : %s", string(r.PolicyMode))
	if r.ReplacedBy != "" {
		fmt.Fprintf(w, " (replaced by %s)", r.ReplacedBy)
	}
	fmt.Fprintln(w)
	printProfileRefs(w, r.Contributing)
	printProfileLocks(w, r.Locks)
	printProfilePermissions(w, r.Permissions)
	printProfileConflicts(w, r.Conflicts)
}

func printProfileRefs(w io.Writer, refs []string) {
	if len(refs) == 0 {
		fmt.Fprintln(w, "  contributing: (none matched)")
		return
	}
	fmt.Fprintln(w, "  contributing refs:")
	for _, ref := range refs {
		fmt.Fprintf(w, "    - %s\n", ref)
	}
}

func printProfileLocks(w io.Writer, locks []cfg.ResolvedLockInfo) {
	if len(locks) == 0 {
		return
	}
	fmt.Fprintln(w, "  binding locks:")
	for _, l := range locks {
		if l.Kind == "value_lock" {
			fmt.Fprintf(w, "    - %s = %s  [value-lock, owner %s]\n", l.Field, formatScalar(l.Value), l.Owner)
			continue
		}
		fmt.Fprintf(w, "    - %s deny %s  [deny-lock, owner %s]\n", l.Field, formatScalar(l.Deny), l.Owner)
	}
}

func printProfilePermissions(w io.Writer, perms map[cfg.AuthorityScope][]string) {
	if perms == nil {
		return
	}
	fmt.Fprintln(w, "  override permissions (allowlist):")
	scopes := make([]string, 0, len(perms))
	for s := range perms {
		scopes = append(scopes, string(s))
	}
	sort.Strings(scopes)
	for _, s := range scopes {
		fmt.Fprintf(w, "    - %s: %s\n", s, formatScalar(perms[cfg.AuthorityScope(s)]))
	}
}

func printProfileConflicts(w io.Writer, conflicts []cfg.ProfileConflict) {
	if len(conflicts) == 0 {
		return
	}
	fmt.Fprintln(w, "  same-scope conflicts (both contributors shown):")
	for _, c := range conflicts {
		fmt.Fprintf(w, "    - %s @%s: winner=%s among %s\n", c.Field, c.Scope, c.Winner, formatScalar(c.Refs))
	}
}

// emitField handles the single-field path. Branches on --value-only,
// --origin-only, --json, then falls through to the human-readable layer-stack
// render.
func emitField(opts *runExplainOptions, snap *cfg.Snapshot, path string, deps Deps) error {
	exp := explainField(snap, path)

	switch {
	case opts.valueOnly:
		if exp.ActiveLayer == "" {
			return deps.ErrorWithHints(fmt.Sprintf("field %q is not set in any layer", path))
		}
		return printScalarOrJSON(opts.stdout, exp.Value)
	case opts.originOnly:
		if exp.ActiveLayer == "" {
			return deps.ErrorWithHints(fmt.Sprintf("field %q is not set in any layer", path))
		}
		fmt.Fprintln(opts.stdout, exp.ActiveLayer)
		return nil
	case opts.jsonOut:
		return writeJSON(opts.stdout, exp)
	default:
		return printFieldHuman(opts.stdout, exp)
	}
}

// emitAll prints the full effective configuration. In JSON mode the shape is
// {"effective": <merged map>, "provenance": {field-path: FieldExplanation, …}}
// so consumers can decide whether to display provenance or just the values.
func emitAll(opts *runExplainOptions, snap *cfg.Snapshot) error {
	effective, err := snap.EffectiveRaw()
	if err != nil {
		return fmt.Errorf("decoding effective config: %w", err)
	}
	if opts.jsonOut {
		prov := map[string]FieldExplanation{}
		for _, key := range snap.FieldNames() {
			prov[key] = explainField(snap, key)
		}
		return writeJSON(opts.stdout, map[string]any{
			"effective":       effective,
			"provenance":      prov,
			"lock_collisions": snap.LockCollisions,
		})
	}
	fmt.Fprintln(opts.stdout, "Effective configuration (with active layer per field):")
	fmt.Fprintln(opts.stdout)
	for _, key := range snap.FieldNames() {
		exp := explainField(snap, key)
		fmt.Fprintf(opts.stdout, "  %s\n", key)
		fmt.Fprintf(opts.stdout, "    value  : %s\n", formatScalar(exp.Value))
		fmt.Fprintf(opts.stdout, "    origin : %s\n", emptyAsDash(exp.ActiveLayer))
		fmt.Fprintln(opts.stdout)
	}
	printLockCollisions(opts.stdout, snap.LockCollisions)
	return nil
}

// printLockCollisions surfaces the §15 D1a authority-pass rejections: each
// lower-scope write a higher-scope value-lock or deny-lock rejected, with the
// attempted value, the winning (locked) value, and the owning scope. Nothing is
// printed when no collision occurred.
func printLockCollisions(w io.Writer, collisions []cfg.LockCollision) {
	if len(collisions) == 0 {
		return
	}
	fmt.Fprintln(w, "Authority-lock rejections (a higher scope's lock won):")
	fmt.Fprintln(w)
	for _, c := range collisions {
		fmt.Fprintf(w, "  %s\n", c.Field)
		fmt.Fprintf(w, "    attempted : %s\n", formatScalar(c.Attempted))
		fmt.Fprintf(w, "    winning   : %s\n", formatScalar(c.Winning))
		fmt.Fprintf(w, "    owner     : %s (%s)\n", c.Owner, c.Kind)
		fmt.Fprintln(w)
	}
}

// emitFlags prints feature flag resolution. Flags live under the `features`
// map per config-distribution-model §3.6. The source for each flag is whichever
// layer last wrote the `features.<name>` field, read off the resolved snapshot.
func emitFlags(opts *runExplainOptions, snap *cfg.Snapshot) error {
	flagNames := featureFlagNames(snap)

	if opts.jsonOut {
		out := make(map[string]FieldExplanation, len(flagNames))
		for _, name := range flagNames {
			out[name] = explainField(snap, "features."+name)
		}
		return writeJSON(opts.stdout, out)
	}

	if len(flagNames) == 0 {
		fmt.Fprintln(opts.stdout, "No feature flags declared in any layer.")
		return nil
	}
	fmt.Fprintln(opts.stdout, "Feature flags (effective value + winning layer):")
	fmt.Fprintln(opts.stdout)
	for _, name := range flagNames {
		exp := explainField(snap, "features."+name)
		fmt.Fprintf(opts.stdout, "  %-30s = %s   [%s]\n",
			name,
			formatScalar(exp.Value),
			emptyAsDash(exp.ActiveLayer),
		)
	}
	return nil
}

// featureFlagNames returns the sorted union of feature-flag names declared by
// any layer in the snapshot. A layer whose `features` field is absent or not an
// object contributes nothing (a non-object features field is ignored rather
// than treated as a flag, matching the historical empty-flags behavior).
func featureFlagNames(snap *cfg.Snapshot) []string {
	names := map[string]struct{}{}
	for _, layer := range snap.Layers {
		raw, ok := layer.Raw["features"].(map[string]any)
		if !ok {
			continue
		}
		for name := range raw {
			names[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// printFieldHuman renders the layer-stack human view matching the example in
// the proposal at .agents/proposals/config-explain-live-surface.md.
func printFieldHuman(w io.Writer, exp FieldExplanation) error {
	fmt.Fprintf(w, "Field:   %s\n", exp.Field)
	fmt.Fprintf(w, "Value:   %s\n", formatScalar(exp.Value))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Layer stack:")
	for i, lv := range exp.Layers {
		marker := ""
		if lv.Active {
			marker = "   <- active"
		}
		valStr := "not set"
		if lv.Value != nil {
			valStr = formatScalar(lv.Value)
		}
		fmt.Fprintf(w, "  [%d] %-22s -> %s%s\n", i+1, lv.Layer, valStr, marker)
	}
	return nil
}

// printScalarOrJSON prints scalar values raw (one line) and complex values as
// compact JSON. Mirrors the --value-only contract: scripts get exactly one
// line out, which is either a primitive literal or a JSON document.
func printScalarOrJSON(w io.Writer, v any) error {
	switch v.(type) {
	case nil:
		fmt.Fprintln(w, "null")
		return nil
	case string, float64, bool:
		fmt.Fprintln(w, formatScalar(v))
		return nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data))
		return nil
	}
}

// formatScalar formats a generic JSON-decoded value for human-readable
// single-line output. Strings are unquoted (so --value-only on a string field
// emits the bare string, friendly for shell substitution); everything else is
// JSON-encoded so arrays and objects round-trip.
func formatScalar(v any) string {
	if v == nil {
		return "not set"
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// emptyAsDash returns "-" for an empty layer id so the human output is
// visually balanced (rather than a hanging colon).
func emptyAsDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// writeJSON emits a pretty-printed JSON document with a trailing newline.
// Used by every --json branch so output stays stable across emit*.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encoding json output: %w", err)
	}
	return nil
}

// Exported error sentinel used by tests to assert the "missing manifest"
// branch without string matching. Kept as a var (not const) because errors.Is
// expects a typed sentinel.
var ErrNoManifest = errors.New("no manifest")
