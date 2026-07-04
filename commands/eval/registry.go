package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
	gogen "github.com/AGOrcha/dot-agents/internal/eval/gen/golang"
	pygen "github.com/AGOrcha/dot-agents/internal/eval/gen/python"
	tsgen "github.com/AGOrcha/dot-agents/internal/eval/gen/typescript"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// languageProfiles enumerates the per-language generator profiles the v1 eval
// harness ships. Adding a language is one entry here — the shared gencore
// engine drives them all (see internal/eval/gen/gencore).
var languageProfiles = []gencore.Profile{gogen.Profile, pygen.Profile, tsgen.Profile}

// openReader is the seam that opens a read-only code-graph reader over the warm
// knowledge-graph store. Production wiring is openWarmReader; tests replace it
// with an in-memory fixture reader so the gen/run pipelines are exercised
// without a populated KG (the same fixture-reader pattern the harness and
// generator tests use).
var openReader = openWarmReader

// openWarmReader opens the warm SQLite code graph as a graphstore.CodeGraphReader
// and returns it alongside a closer the caller must invoke when done. The DB
// path mirrors `da kg`'s warm-store layout (<KG_HOME>/ops/graphstore.db).
func openWarmReader() (graphstore.CodeGraphReader, func() error, error) {
	store, err := graphstore.OpenSQLite(warmDBPath())
	if err != nil {
		return nil, nil, fmt.Errorf("eval: open code graph: %w", err)
	}
	return store, store.Close, nil
}

// warmDBPath is the warm code-graph database path under the resolved KG home.
func warmDBPath() string {
	return filepath.Join(kgHome(), "ops", "graphstore.db")
}

// kgHome resolves the knowledge-graph home directory, honouring KG_HOME and
// otherwise falling back to <user-home>/knowledge-graph — the same resolution
// `da kg` uses so eval reads the graph the rest of the toolchain builds.
func kgHome() string {
	if v := os.Getenv("KG_HOME"); v != "" {
		return v
	}
	home, _ := config.UserHomeDir()
	return filepath.Join(home, "knowledge-graph")
}

// kgRegistry opens the warm code-graph reader and builds a generator registry
// over it for every supported language. The returned closer releases the reader
// and must be invoked by the caller (via closeReader) once generation is done.
func kgRegistry() (*evalcore.Registry, func() error, error) {
	reader, closeFn, err := openReader()
	if err != nil {
		return nil, nil, err
	}
	reg, err := buildRegistry(reader)
	if err != nil {
		closeReader(closeFn)
		return nil, nil, err
	}
	return reg, closeFn, nil
}

// buildRegistry constructs a generator registry backed by reader for every
// profile in languageProfiles. Registration only wires the querier through each
// generator (it issues no graph queries), so it succeeds for any non-nil reader.
func buildRegistry(reader graphstore.CodeGraphReader) (*evalcore.Registry, error) {
	q, err := kgquery.New(reader)
	if err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}
	reg := evalcore.NewRegistry()
	for _, p := range languageProfiles {
		if err := gencore.Register(reg, q, p); err != nil {
			return nil, fmt.Errorf("eval: register %s generator: %w", p.Language, err)
		}
	}
	return reg, nil
}

// closeReader invokes a reader closer, tolerating a nil closer (a test seam may
// hand back an already-owned reader with nothing to release). A close error is
// deliberately dropped: it is a best-effort release of a read-only handle after
// the useful work is done.
func closeReader(closeFn func() error) {
	if closeFn != nil {
		_ = closeFn()
	}
}

// validateLanguage rejects an empty or unrecognised language before it reaches
// the registry, so a typo surfaces as an actionable CLI error rather than a
// bare "no generator" lookup miss.
func validateLanguage(lang evalcore.Language) error {
	if lang == "" {
		return fmt.Errorf("eval: --%s is required", languageFlagName)
	}
	if !lang.Valid() {
		return fmt.Errorf("eval: invalid language %q (want go, python, or typescript)", lang)
	}
	return nil
}
