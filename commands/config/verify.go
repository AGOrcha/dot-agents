package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// runVerifyOptions captures one invocation's state. stdout/stderr/cwd and the
// binary probe are injected so the run path is table-drivable without cobra or
// a real code-review-graph install.
type runVerifyOptions struct {
	jsonOut bool
	stdout  io.Writer
	stderr  io.Writer
	cwd     string
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
			opts.stdout = cmd.OutOrStdout()
			opts.stderr = cmd.ErrOrStderr()
			if opts.cwd == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return deps.ErrorWithHints("could not resolve current directory", err.Error())
				}
				opts.cwd = cwd
			}
			return runVerify(opts, deps)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "Emit machine-readable JSON output")
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

	snap, _, err := loadFlatSnapshot(opts.cwd)
	if err != nil {
		checks = append(checks, VerifyCheck{"manifest", verifyFail, err.Error()})
		return VerifyReport{OK: false, Checks: checks}
	}
	checks = append(checks, VerifyCheck{"manifest", verifyPass, "parsed " + cfg.AgentsRCFile})

	checks = append(checks, verifySources(opts.cwd, snap)...)
	checks = append(checks, verifyLayerLocks(opts.cwd)...)

	probe := opts.crgProbe
	if probe == nil {
		probe = defaultCRGProbe
	}
	if perr := probe(opts.cwd); perr != nil {
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
func verifySources(cwd string, snap *snapshot) []VerifyCheck {
	repo := snap.layers[layerRepoLocal]
	raw, ok := repo["sources"].([]any)
	if !ok || len(raw) == 0 {
		return []VerifyCheck{{"config-layers", verifyPass, "no external layers declared"}}
	}
	referenced := extendsRefSourceIDs(repo)

	checks := make([]VerifyCheck, 0, len(raw))
	for i, item := range raw {
		src, ok := item.(map[string]any)
		if !ok {
			checks = append(checks, VerifyCheck{fmt.Sprintf("source[%d]", i), verifyFail, "source entry is not an object"})
			continue
		}
		typ, _ := src["type"].(string)
		name := sourceLabel(i, src)
		switch typ {
		case "local":
			path, _ := src["path"].(string)
			if path == "" {
				checks = append(checks, VerifyCheck{name, verifyPass, "local repo layer"})
				continue
			}
			abs := path
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(cwd, path)
			}
			if fileExists(abs) {
				checks = append(checks, VerifyCheck{name, verifyPass, "local path present: " + path})
			} else {
				checks = append(checks, VerifyCheck{name, verifyFail, "local path missing: " + path})
			}
		case "git", "http", "oci":
			id, _ := src["id"].(string)
			if _, used := referenced[id]; used && id != "" {
				checks = append(checks, VerifyCheck{name, verifyPass, "remote " + typ + " source; its layers are verified in the locked-layers check below"})
			} else {
				checks = append(checks, VerifyCheck{name, verifyPass, "remote " + typ + " source declared but unused (no `extends` layer references it)"})
			}
		case "":
			checks = append(checks, VerifyCheck{name, verifyFail, "source is missing a type"})
		default:
			checks = append(checks, VerifyCheck{name, verifyWarn, "unknown source type: " + typ})
		}
	}
	return checks
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
		case s.OK() && s.SourceType == "local":
			checks = append(checks, VerifyCheck{name, verifyPass, "locked (local source)"})
		case s.OK():
			checks = append(checks, VerifyCheck{name, verifyPass, "cached at " + abbrevSHA(s.SHA)})
		case s.Optional:
			checks = append(checks, VerifyCheck{name, verifyWarn, s.Problem + " [optional]"})
		default:
			checks = append(checks, VerifyCheck{name, verifyFail, s.Problem})
		}
	}
	return checks
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
