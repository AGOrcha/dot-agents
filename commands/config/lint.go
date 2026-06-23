package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/schemas"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

var (
	agentsRCSchemaCompiled    *jsonschema.Schema
	agentsRCSchemaCompileOnce sync.Once
	agentsRCSchemaCompileErr  error
)

// compiledAgentsRCSchema compiles the embedded AgentsRC schema once and caches
// the result, mirroring the compile pattern used by the workflow schema gates
// (NewCompiler → AddResource → Compile against a stable resource URL).
func compiledAgentsRCSchema() (*jsonschema.Schema, error) {
	agentsRCSchemaCompileOnce.Do(func() {
		var doc any
		if err := json.Unmarshal(schemas.AgentsRCSchemaJSON(), &doc); err != nil {
			agentsRCSchemaCompileErr = fmt.Errorf("parsing embedded agentsrc.schema.json: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const url = "https://dot-agents.dev/schemas/agentsrc.schema.json"
		if err := c.AddResource(url, doc); err != nil {
			agentsRCSchemaCompileErr = fmt.Errorf("registering agentsrc schema: %w", err)
			return
		}
		agentsRCSchemaCompiled, agentsRCSchemaCompileErr = c.Compile(url)
	})
	return agentsRCSchemaCompiled, agentsRCSchemaCompileErr
}

// lintPass / lintFail / lintSkip are the per-file lint result statuses. skip is
// used for declared remote layers whose bytes are not locally readable without a
// fetch (run `da config sync` then lint), so they never silently fail.
const (
	lintPass = "pass"
	lintFail = "fail"
	lintSkip = "skip"
)

// LintResult is one validated file in the lint report.
type LintResult struct {
	// File is the layer's display label: "repo-local" for the project manifest,
	// or the layer ref ("acme:org/base.json") for a declared extends layer.
	File string `json:"file"`
	// Path is the absolute path that was read (omitted for skipped remote layers
	// with no local path).
	Path   string `json:"path,omitempty"`
	Status string `json:"status"` // pass | fail | skip
	// Detail carries the validation error on fail, or a short reason on skip.
	Detail string `json:"detail,omitempty"`
}

// LintReport is the stable JSON shape emitted by `da config lint --json`. OK is
// false iff any layer failed validation; skipped layers do not flip OK.
type LintReport struct {
	OK      bool         `json:"ok"`
	Results []LintResult `json:"results"`
}

// runLintOptions captures one invocation's state. The shared stdout/stderr/
// cwd/json surface comes from the embedded runContext, so the run path stays
// table-drivable without cobra.
type runLintOptions struct {
	runContext
}

func newLintCmd(deps Deps) *cobra.Command {
	opts := &runLintOptions{}
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate declared layer files against the AgentsRC layer schema",
		Long: `Validate the repo-local .agentsrc.json and each declared ` + "`extends`" + ` layer
file against the canonical AgentsRC layer schema (schemas/agentsrc.schema.json,
spec config-distribution-model §13.1, §14).

For each layer it reports pass/fail with the structured validation error on
failure. Local-source layers are read directly from disk; remote-source layers
that are not locally readable without a fetch are skipped (run ` + "`da config sync`" + `
first, then re-run lint).

Exits non-zero if any layer is invalid. Skipped layers do not fail the command.`,
		Example: exampleBlock(
			"  da config lint",
			"  da config lint --json",
		),
		Args: deps.ExactArgsWithHints(0, "`da config lint` takes no arguments; use --json for machine-readable output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			return runLint(opts, deps)
		},
	}
	return cmd
}

// runLint is the test-friendly entry point. It builds the report, renders it
// (JSON or human), and returns a non-nil error iff any layer failed validation
// so cobra maps it to a non-zero exit.
func runLint(opts *runLintOptions, deps Deps) error {
	report, err := buildLintReport(opts.cwd)
	if err != nil {
		return deps.ErrorWithHints("config lint could not run: " + err.Error())
	}

	if opts.jsonOut {
		if werr := writeJSON(opts.stdout, report); werr != nil {
			return werr
		}
	} else {
		printLintHuman(opts.stdout, report)
	}

	if !report.OK {
		return deps.ErrorWithHints("config lint found one or more invalid layers",
			"Fix the reported schema violations in the named layer file(s) and re-run.",
		)
	}
	return nil
}

// buildLintReport validates the repo-local manifest plus every declared extends
// layer that is locally readable. A missing/unparseable repo manifest is a
// terminal error (there are no layers to lint without it). Each subsequent layer
// is validated independently so the report shows every failure at once rather
// than stopping at the first.
func buildLintReport(projectPath string) (LintReport, error) {
	sch, err := compiledAgentsRCSchema()
	if err != nil {
		return LintReport{}, err
	}

	report := LintReport{OK: true, Results: []LintResult{}}

	// 1. Repo-local manifest.
	repoPath := filepath.Join(projectPath, cfg.AgentsRCFile)
	report.Results = append(report.Results, lintOneFile(sch, "repo-local", repoPath))

	// 2. Each declared extends layer.
	rc, err := cfg.LoadAgentsRC(projectPath)
	if err != nil {
		// The repo-local result above already records the parse failure; surface
		// only that, without a second decode error.
		report.OK = reduceLintOK(report.Results)
		return report, nil
	}
	sources := map[string]cfg.Source{}
	for _, s := range rc.Sources {
		if s.ID != "" {
			sources[s.ID] = s
		}
	}
	for _, ext := range rc.Extends {
		report.Results = append(report.Results, lintExtendsLayer(sch, ext, sources))
	}

	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].File < report.Results[j].File
	})
	report.OK = reduceLintOK(report.Results)
	return report, nil
}

// lintExtendsLayer validates one declared extends layer. Local-source layers are
// read directly from disk at source.Path/layer-path (the same path localFetcher
// reads). Remote layers (git/http/oci) are not reachable without a fetch from
// this command, so they are reported as skipped — never as a false pass/fail.
func lintExtendsLayer(sch *jsonschema.Schema, ext cfg.LayerRef, sources map[string]cfg.Source) LintResult {
	parts, err := cfg.ParseLayerRef(ext.Ref)
	if err != nil {
		return LintResult{File: ext.Ref, Status: lintFail, Detail: "invalid layer ref: " + err.Error()}
	}
	src, ok := sources[parts.SourceID]
	if !ok {
		return LintResult{File: ext.Ref, Status: lintFail, Detail: fmt.Sprintf("source %q not declared in %s", parts.SourceID, cfg.AgentsRCFile)}
	}
	if src.Type != "local" {
		return LintResult{File: ext.Ref, Status: lintSkip, Detail: src.Type + " layer; not locally readable without a fetch (run `da config sync` then lint)"}
	}
	base := src.Path
	if base == "" {
		base = src.URL
	}
	path := filepath.Join(base, filepath.FromSlash(parts.LayerPath))
	res := lintOneFile(sch, ext.Ref, path)
	return res
}

// lintOneFile reads path, parses it as JSON, and validates the document against
// the compiled AgentsRC schema. A read or parse failure is a structured fail
// result; a schema violation is a fail with the jsonschema error detail.
func lintOneFile(sch *jsonschema.Schema, label, path string) LintResult {
	res := LintResult{File: label, Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		res.Status = lintFail
		if os.IsNotExist(err) {
			res.Detail = "file not found: " + path
		} else {
			res.Detail = "could not read: " + err.Error()
		}
		return res
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		res.Status = lintFail
		res.Detail = "invalid JSON: " + err.Error()
		return res
	}
	if err := sch.Validate(doc); err != nil {
		res.Status = lintFail
		res.Detail = "schema violation: " + err.Error()
		return res
	}
	res.Status = lintPass
	return res
}

// reduceLintOK reports whether every result either passed or was skipped (no
// fail). Skips do not flip OK; only a fail does.
func reduceLintOK(results []LintResult) bool {
	for _, r := range results {
		if r.Status == lintFail {
			return false
		}
	}
	return true
}

// printLintHuman renders the lint outcome as an aligned per-file list with a
// one-line summary footer.
func printLintHuman(w io.Writer, report LintReport) {
	fmt.Fprintln(w, "Config lint (layer schema validation):")
	fmt.Fprintln(w)
	pass, fail, skip := 0, 0, 0
	for _, r := range report.Results {
		switch r.Status {
		case lintPass:
			pass++
		case lintFail:
			fail++
		case lintSkip:
			skip++
		}
		line := fmt.Sprintf("  [%s] %-28s", lintMark(r.Status), r.File)
		if r.Detail != "" {
			line += " " + r.Detail
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d passed, %d failed, %d skipped — %s\n",
		pass, fail, skip, lintOutcome(report.OK))
}

func lintMark(status string) string {
	switch status {
	case lintPass:
		return "ok  "
	case lintFail:
		return "FAIL"
	case lintSkip:
		return "skip"
	default:
		return "?   "
	}
}

func lintOutcome(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}
