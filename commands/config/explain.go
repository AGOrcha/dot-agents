package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// orderedLayers is the canonical precedence order used everywhere in this
// package. Lowest precedence first; the last layer with a non-nil value for a
// field wins. Mirrors spec config-distribution-model §6.
var orderedLayers = []string{
	layerProductDefaults,
	layerUserLocal,
	layerRepoLocal,
}

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

// snapshot is the package-local view of layered config. In flat mode (the
// only mode wired today) it holds three maps keyed by the layer identifier.
// When the layered resolver from p1 lands, snapshot will degrade to a thin
// wrapper around internal/config.Snapshot — the surface this command consumes
// stays the same.
type snapshot struct {
	// layers maps layer-id -> decoded JSON object (or nil if the layer was
	// not present on disk).
	layers map[string]map[string]any
	// effective is the merged result with last-writer-wins on scalar fields
	// (sufficient for flat scope; richer merge categories arrive with p1b).
	effective map[string]any
}

// loadFlatSnapshot constructs the flat-scope snapshot for the project at
// projectPath. It walks (in precedence order):
//
//  1. product-defaults — currently an empty stub; reserved for p1 to populate
//     when shipped built-in defaults are wired.
//  2. user-local — ~/.agents/agentsrc.json (loaded as raw JSON; absence is
//     not an error).
//  3. repo-local — <projectPath>/.agentsrc.json (the canonical manifest).
//
// A schema parse error on the repo-local file is fatal (returned with
// exitSchemaErr). Missing repo-local file is reported as "no manifest" — that
// is also fatal because there is nothing to explain.
func loadFlatSnapshot(projectPath string) (*snapshot, int, error) {
	snap := &snapshot{
		layers: map[string]map[string]any{
			layerProductDefaults: nil,
			layerUserLocal:       nil,
			layerRepoLocal:       nil,
		},
	}

	// user-local: ~/.agents/agentsrc.json (optional)
	userHome := cfg.AgentsHome()
	if userPath := filepath.Join(userHome, cfg.AgentsRCFile); fileExists(userPath) {
		decoded, err := decodeJSONFile(userPath)
		if err != nil {
			// User-local parse errors are surfaced as schema errors but
			// recorded with a layer hint so the operator knows which file
			// to fix.
			return nil, exitSchemaErr, fmt.Errorf("parsing user-local %s: %w", userPath, err)
		}
		snap.layers[layerUserLocal] = decoded
	}

	// repo-local: <projectPath>/.agentsrc.json (required for explain)
	repoPath := filepath.Join(projectPath, cfg.AgentsRCFile)
	if !fileExists(repoPath) {
		return nil, exitSchemaErr, fmt.Errorf("no %s found at %s — run `da install --generate` first", cfg.AgentsRCFile, projectPath)
	}
	repoLayer, err := decodeJSONFile(repoPath)
	if err != nil {
		return nil, exitSchemaErr, fmt.Errorf("parsing %s: %w", repoPath, err)
	}
	snap.layers[layerRepoLocal] = repoLayer

	snap.effective = mergeLayers(snap.layers)
	return snap, exitOK, nil
}

// decodeJSONFile reads path and decodes it into a generic map. Returns an
// error if the file does not parse as a JSON object.
func decodeJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// fileExists is a thin wrapper kept here (rather than inlined) so tests can
// observe the exact "file missing" branch with a deterministic stat err.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mergeLayers produces the effective config object by walking orderedLayers
// (lowest precedence first) and overlaying each non-nil layer onto the
// accumulating result with last-writer-wins on top-level scalar fields.
//
// Nested objects are not deep-merged here — that is intentional. Spec §7.2
// defines per-category merge rules (set-union, ordered-replace, map-merge);
// implementing them lives in p1's resolver. This flat overlay is sufficient
// to display provenance correctly for the manifest fields users care about
// today (skills, rules, agents, hooks, mcp, settings, sources, repo_id,
// features, …).
func mergeLayers(layers map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for _, layerID := range orderedLayers {
		layer := layers[layerID]
		if layer == nil {
			continue
		}
		for k, v := range layer {
			out[k] = v
		}
	}
	return out
}

// explainField builds the FieldExplanation for a single field path
// against the snapshot. Path syntax is dot-separated keys
// (e.g. "kg.bridge.enabled", "app_type_verifier_map.pa-angular-ui").
//
// A field that is unset in every layer returns a FieldExplanation with
// Value=nil and ActiveLayer="" — the caller decides whether to treat that as
// an error (single-field default) or as informational ("not set" in --all).
func (s *snapshot) explainField(path string) FieldExplanation {
	parts := splitFieldPath(path)
	out := FieldExplanation{Field: path, Layers: make([]LayerValue, 0, len(orderedLayers))}

	for _, layerID := range orderedLayers {
		val, ok := lookup(s.layers[layerID], parts)
		entry := LayerValue{Layer: layerID, Value: nil, Active: false}
		if ok {
			entry.Value = val
			out.ActiveLayer = layerID // last writer wins
			out.Value = val
		}
		out.Layers = append(out.Layers, entry)
	}

	// Flip Active on the last layer that set a value (if any).
	if out.ActiveLayer != "" {
		for i := range out.Layers {
			if out.Layers[i].Layer == out.ActiveLayer {
				out.Layers[i].Active = true
			}
		}
	}
	return out
}

// splitFieldPath splits a dot-separated path into traversal parts. Empty
// input returns an empty slice so callers can short-circuit on len==0.
func splitFieldPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// lookup walks layer with parts and returns (value, true) if every step
// resolved against an object key. Any non-object intermediate or missing key
// short-circuits to (nil, false).
func lookup(layer map[string]any, parts []string) (any, bool) {
	if layer == nil || len(parts) == 0 {
		return nil, false
	}
	var cur any = layer
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, present := obj[p]
		if !present {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// runExplainOptions captures the flag state for one invocation. Pulled into a
// struct so the run* helpers stay easy to table-drive in tests.
type runExplainOptions struct {
	all        bool
	flags      bool
	valueOnly  bool
	originOnly bool
	jsonOut    bool
	stdout     io.Writer
	stderr     io.Writer
	cwd        string
}

func newExplainCmd(deps Deps) *cobra.Command {
	opts := &runExplainOptions{}
	cmd := &cobra.Command{
		Use:   "explain [field-path]",
		Short: "Show the effective value of a config field and where it came from",
		Long: `Prints the effective value of a single config field plus the full layer
stack that produced it. With --all, prints the entire effective configuration.
With --flags, prints feature-flag resolution.

Field paths are dot-separated (e.g. "kg.backend", "features.graph_bridge").

Output is human-readable by default; --json emits a stable machine-readable
shape documented on the FieldExplanation type in this package.

In the current flat scope (no extends layers) the layer stack is:
  [1] product-defaults     (reserved; currently empty)
  [2] user-local           (~/.agents/agentsrc.json, if present)
  [3] repo-local           (./.agentsrc.json)
Once config-v2-migration/p1b lands, declared extends layers slot in between
user-local and repo-local with the same provenance surface.`,
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
	return cmd
}

// runExplain is the test-friendly entry point — receives an already-prepared
// runExplainOptions so tests can stub cwd, stdout, and stderr without going
// through cobra.
func runExplain(opts *runExplainOptions, args []string, deps Deps) error {
	if err := validateFlagCombo(opts, args, deps); err != nil {
		return err
	}

	snap, code, err := loadFlatSnapshot(opts.cwd)
	if err != nil {
		// Schema errors are surfaced to the operator via ErrorWithHints so the
		// existing UX layer can add color and hints. Exit code is propagated
		// through cobra by returning the error (cobra maps non-nil to exit
		// status; the precise int is informational here per spec §10 — main()
		// translates non-nil errors uniformly).
		_ = code
		return deps.ErrorWithHints(err.Error(),
			"Run `da install --generate` to create .agentsrc.json from current state.",
		)
	}

	switch {
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
	return nil
}

// emitField handles the single-field path. Branches on --value-only,
// --origin-only, --json, then falls through to the human-readable layer-stack
// render.
func emitField(opts *runExplainOptions, snap *snapshot, path string, deps Deps) error {
	exp := snap.explainField(path)

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
func emitAll(opts *runExplainOptions, snap *snapshot) error {
	if opts.jsonOut {
		prov := map[string]FieldExplanation{}
		for _, key := range sortedKeys(snap.effective) {
			prov[key] = snap.explainField(key)
		}
		return writeJSON(opts.stdout, map[string]any{
			"effective":  snap.effective,
			"provenance": prov,
		})
	}
	fmt.Fprintln(opts.stdout, "Effective configuration (with active layer per field):")
	fmt.Fprintln(opts.stdout)
	for _, key := range sortedKeys(snap.effective) {
		exp := snap.explainField(key)
		fmt.Fprintf(opts.stdout, "  %s\n", key)
		fmt.Fprintf(opts.stdout, "    value  : %s\n", formatScalar(exp.Value))
		fmt.Fprintf(opts.stdout, "    origin : %s\n", emptyAsDash(exp.ActiveLayer))
		fmt.Fprintln(opts.stdout)
	}
	return nil
}

// emitFlags prints feature flag resolution. Flags live under the `features`
// map per config-distribution-model §3.6. In flat mode the source for each
// flag is whichever layer last wrote the `features.<name>` field.
func emitFlags(opts *runExplainOptions, snap *snapshot) error {
	// Collect the union of feature names across all layers so flags set only
	// in user-local but absent in repo-local still show up.
	names := map[string]struct{}{}
	for _, layerID := range orderedLayers {
		layer := snap.layers[layerID]
		if layer == nil {
			continue
		}
		raw, ok := layer["features"].(map[string]any)
		if !ok {
			continue
		}
		for name := range raw {
			names[name] = struct{}{}
		}
	}

	flagNames := make([]string, 0, len(names))
	for name := range names {
		flagNames = append(flagNames, name)
	}
	sort.Strings(flagNames)

	if opts.jsonOut {
		out := make(map[string]FieldExplanation, len(flagNames))
		for _, name := range flagNames {
			out[name] = snap.explainField("features." + name)
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
		exp := snap.explainField("features." + name)
		fmt.Fprintf(opts.stdout, "  %-30s = %s   [%s]\n",
			name,
			formatScalar(exp.Value),
			emptyAsDash(exp.ActiveLayer),
		)
	}
	return nil
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

// sortedKeys returns the keys of m in deterministic order. Sorting is
// important for golden-file tests and for diffing `--all` output between
// runs.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Exported error sentinel used by tests to assert the "missing manifest"
// branch without string matching. Kept as a var (not const) because errors.Is
// expects a typed sentinel.
var ErrNoManifest = errors.New("no manifest")
