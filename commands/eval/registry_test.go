package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
	gogen "github.com/AGOrcha/dot-agents/internal/eval/gen/golang"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

func TestKGHomeHonoursEnv(t *testing.T) {
	t.Setenv("KG_HOME", "/custom/kg")
	if got := kgHome(); got != "/custom/kg" {
		t.Errorf("kgHome with KG_HOME = %q, want /custom/kg", got)
	}
}

func TestKGHomeFallsBackToUserHome(t *testing.T) {
	t.Setenv("KG_HOME", "")
	got := kgHome()
	if got == "" || !strings.HasSuffix(filepath.ToSlash(got), "knowledge-graph") {
		t.Errorf("kgHome fallback = %q, want a .../knowledge-graph path", got)
	}
}

func TestWarmDBPath(t *testing.T) {
	t.Setenv("KG_HOME", "/custom/kg")
	want := filepath.Join("/custom/kg", "ops", "graphstore.db")
	if got := warmDBPath(); got != want {
		t.Errorf("warmDBPath = %q, want %q", got, want)
	}
}

// openWarmReader is the production seam: with a real store already built at the
// warm path, it opens a SQLite-backed reader and hands back a working closer.
func TestOpenWarmReaderOpensRealStore(t *testing.T) {
	t.Setenv("KG_HOME", t.TempDir())
	buildWarmStore(t)

	reader, closeFn, err := openWarmReader()
	if err != nil {
		t.Fatalf("openWarmReader: %v", err)
	}
	if reader == nil {
		t.Fatal("openWarmReader returned a nil reader")
	}
	if closeFn == nil {
		t.Fatal("openWarmReader returned a nil closer")
	}
	// The reader is usable and the closer releases it.
	if _, err := reader.GetAllFiles(); err != nil {
		t.Errorf("GetAllFiles on warm reader: %v", err)
	}
	closeReader(closeFn)
}

// gen/run are read paths: an absent warm store is an operator error, not a
// reason to create one. openWarmReader must fail cleanly with a build-the-graph
// message AND leave the filesystem untouched (no empty graphstore.db, no ops/).
func TestOpenWarmReaderMissingDBFails(t *testing.T) {
	kgHomeDir := t.TempDir()
	t.Setenv("KG_HOME", kgHomeDir)

	reader, closeFn, err := openWarmReader()
	if err == nil {
		closeReader(closeFn)
		t.Fatal("openWarmReader against an absent store should fail")
	}
	if reader != nil || closeFn != nil {
		t.Fatal("openWarmReader must return no reader/closer on the absent-store error")
	}
	if !strings.Contains(err.Error(), "code graph not built") {
		t.Errorf("absent-store error not actionable: %v", err)
	}
	// The read path created nothing: no graphstore.db and no ops/ directory.
	if _, statErr := os.Stat(warmDBPath()); !os.IsNotExist(statErr) {
		t.Errorf("openWarmReader created a store on the read path: stat err = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(kgHomeDir, "ops")); !os.IsNotExist(statErr) {
		t.Error("openWarmReader created the ops/ directory on the read path")
	}
}

// A store-open failure that is NOT os.ErrNotExist (e.g. a corrupt store)
// surfaces as the wrapped open error, NOT the build-the-graph message. The error
// is injected through the openWarmStore seam so the test does not depend on
// OS-specific filesystem error semantics (Unix ENOTDIR vs Windows
// ERROR_PATH_NOT_FOUND classify differently).
func TestOpenWarmReaderNonNotExistError(t *testing.T) {
	swapOpenWarmStore(t, func(string) (*graphstore.SQLiteStore, error) {
		return nil, errors.New("graphstore: db is corrupt")
	})
	reader, closeFn, err := openWarmReader()
	if err == nil {
		closeReader(closeFn)
		t.Fatal("openWarmReader should surface a non-not-exist open error")
	}
	if reader != nil || closeFn != nil {
		t.Fatal("openWarmReader must return no reader/closer on the open error")
	}
	if strings.Contains(err.Error(), "code graph not built") {
		t.Errorf("a non-not-exist open error must not be reported as an unbuilt graph: %v", err)
	}
	if !strings.Contains(err.Error(), "open code graph") {
		t.Errorf("open error lost its command context: %v", err)
	}
}

// An os.ErrNotExist from the store open (however the OS spells it) maps to the
// build-the-graph message. Injected through the seam so the mapping is asserted
// independently of the OS-specific not-exist errno.
func TestOpenWarmReaderMapsNotExist(t *testing.T) {
	swapOpenWarmStore(t, func(string) (*graphstore.SQLiteStore, error) {
		return nil, fmt.Errorf("graphstore: db does not exist: %w", os.ErrNotExist)
	})
	_, _, err := openWarmReader()
	if err == nil || !strings.Contains(err.Error(), "code graph not built") {
		t.Fatalf("os.ErrNotExist should map to the build-the-graph message, got %v", err)
	}
}

// buildWarmStore materialises a real (empty-schema) warm SQLite store at the
// resolved warm path so openWarmReader's exists-then-open path is exercised.
func buildWarmStore(t *testing.T) {
	t.Helper()
	store, err := graphstore.OpenSQLite(warmDBPath())
	if err != nil {
		t.Fatalf("build warm store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close warm store: %v", err)
	}
}

func TestBuildRegistryRegistersEveryLanguage(t *testing.T) {
	reg, err := buildRegistry(fixtureReader())
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	for _, lang := range []evalcore.Language{evalcore.LanguageGo, evalcore.LanguagePython, evalcore.LanguageTypeScript} {
		if _, ok := reg.Lookup(lang); !ok {
			t.Errorf("registry missing generator for %q", lang)
		}
	}
}

func TestBuildRegistryNilReaderErrors(t *testing.T) {
	if _, err := buildRegistry(nil); err == nil {
		t.Fatal("buildRegistry(nil): expected error from kgquery.New")
	}
}

// A duplicate profile forces the per-language gencore.Register error branch.
func TestBuildRegistryDuplicateProfileErrors(t *testing.T) {
	swapLanguageProfiles(t, []gencore.Profile{gogen.Profile, gogen.Profile})
	if _, err := buildRegistry(fixtureReader()); err == nil {
		t.Fatal("buildRegistry with duplicate profile: expected registration error")
	}
}

func TestKGRegistrySuccess(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	reg, closeFn, err := kgRegistry()
	if err != nil {
		t.Fatalf("kgRegistry: %v", err)
	}
	defer closeReader(closeFn)
	if _, ok := reg.Lookup(evalcore.LanguageGo); !ok {
		t.Error("kgRegistry did not register the go generator")
	}
}

func TestKGRegistryOpenError(t *testing.T) {
	swapOpenReader(t, func() (graphstore.CodeGraphReader, func() error, error) {
		return nil, nil, errors.New("boom")
	})
	if _, _, err := kgRegistry(); err == nil {
		t.Fatal("kgRegistry: expected open error")
	}
}

// openReader succeeds but hands back a nil reader → buildRegistry fails and the
// closer must still run (covers the kgRegistry build-error cleanup branch).
func TestKGRegistryBuildErrorClosesReader(t *testing.T) {
	closed := false
	swapOpenReader(t, func() (graphstore.CodeGraphReader, func() error, error) {
		return nil, func() error { closed = true; return nil }, nil
	})
	if _, _, err := kgRegistry(); err == nil {
		t.Fatal("kgRegistry: expected build error from nil reader")
	}
	if !closed {
		t.Error("kgRegistry build error did not release the reader")
	}
}

func TestCloseReaderToleratesNil(t *testing.T) {
	closeReader(nil) // must not panic
	closed := false
	closeReader(func() error { closed = true; return nil })
	if !closed {
		t.Error("closeReader did not invoke a non-nil closer")
	}
}

func TestValidateLanguage(t *testing.T) {
	if err := validateLanguage(""); err == nil {
		t.Error("empty language should error")
	}
	if err := validateLanguage("ruby"); err == nil {
		t.Error("invalid language should error")
	}
	if err := validateLanguage(evalcore.LanguageGo); err != nil {
		t.Errorf("go should validate: %v", err)
	}
}

func TestValidateDifficulty(t *testing.T) {
	// Empty is allowed — it means "generator's choice".
	if err := validateDifficulty(""); err != nil {
		t.Errorf("empty difficulty should be allowed: %v", err)
	}
	for _, d := range []evalcore.Difficulty{evalcore.DifficultyEasy, evalcore.DifficultyMedium, evalcore.DifficultyHard} {
		if err := validateDifficulty(d); err != nil {
			t.Errorf("%q should validate: %v", d, err)
		}
	}
	err := validateDifficulty("bogus")
	if err == nil {
		t.Fatal("invalid difficulty should error")
	}
	if !strings.Contains(err.Error(), `invalid difficulty "bogus"`) ||
		!strings.Contains(err.Error(), "easy, medium, or hard") {
		t.Errorf("difficulty error not actionable: %v", err)
	}
}
