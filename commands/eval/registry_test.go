package eval

import (
	"errors"
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

// openWarmReader is the production seam: point KG_HOME at a temp dir and prove
// it opens a real SQLite-backed reader (OpenSQLite creates the parent + db) and
// hands back a working closer.
func TestOpenWarmReaderOpensRealStore(t *testing.T) {
	t.Setenv("KG_HOME", t.TempDir())
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
