package kg

// Seam-driven fault-injection tests for commands/kg. Each test builds a
// fakeKGIO that overrides exactly one operation to return errSeam, then
// passes that fake into the unit under test. This is the interface-DI
// replacement for the legacy var osMkdirAll = os.MkdirAll-style func-var
// seams formerly defined in seams.go (see docs/TEST_SEAMS.md and the
// seam-interface-di-migration plan / pr40-artifacts atomic convergence).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ── loadKGConfig: ReadFile error path (non-NotExist) ──────────────────────────

func TestLoadKGConfig_ReadFileError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if _, err := loadKGConfig(fake); !errors.Is(err, errSeam) {
		t.Fatalf("expected seam error, got %v", err)
	}
}

// ── SaveKGConfig: mkdir error ─────────────────────────────────────────────────

func TestSaveKGConfig_MkdirError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	if err := saveKGConfigIO(fake, &KGConfig{SchemaVersion: 1, Name: "x"}); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

// ── SaveKGConfig: write error ─────────────────────────────────────────────────

func TestSaveKGConfig_WriteError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	if err := saveKGConfigIO(fake, &KGConfig{SchemaVersion: 1, Name: "x"}); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── appendLogEntry: OpenFile error ────────────────────────────────────────────

func TestAppendLogEntry_OpenFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{openFile: func(string, int, fs.FileMode) (*os.File, error) { return nil, errSeam }}
	if err := appendLogEntry(fake, home, "x"); !errors.Is(err, errSeam) {
		t.Fatalf("expected open seam error, got %v", err)
	}
}

// ── readLogEntries: non-NotExist ReadFile error ───────────────────────────────

func TestReadLogEntries_ReadFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if _, err := readLogEntries(fake, home, 0); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

// ── updateIndex: ReadFile error (non-NotExist) ────────────────────────────────

func TestUpdateIndex_ReadFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	note := &GraphNote{ID: "x", Type: "entity", Title: "t", Summary: "s"}
	if err := updateIndex(fake, home, note); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

// ── updateIndex: WriteFile error ──────────────────────────────────────────────

func TestUpdateIndex_WriteFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	note := &GraphNote{ID: "x", Type: "entity", Title: "t", Summary: "s"}
	if err := updateIndex(fake, home, note); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── writeGraphHealth: mkdir error ─────────────────────────────────────────────

func TestWriteGraphHealth_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	if err := writeGraphHealth(fake, home, GraphHealth{}); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

// ── writeGraphHealth: marshal error ───────────────────────────────────────────

func TestWriteGraphHealth_MarshalError(t *testing.T) {
	home := newTempKG(t)
	fake := withMarshalIndentError(t)
	if err := writeGraphHealth(fake, home, GraphHealth{}); !errors.Is(err, errSeam) {
		t.Fatalf("expected marshal seam error, got %v", err)
	}
}

// ── writeGraphHealth: write error ─────────────────────────────────────────────

func TestWriteGraphHealth_WriteError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	if err := writeGraphHealth(fake, home, GraphHealth{}); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── runKGSetup error branches ─────────────────────────────────────────────────

func TestRunKGSetup_MkdirError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

// TestRunKGSetup_SaveConfigError forces SaveKGConfig (line ~539) to fail.
// Real mkdir succeeds for setup dirs; the config write seam fails only on
// config.yaml.
func TestRunKGSetup_SaveConfigError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == "config.yaml" {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected save-config seam error, got %v", err)
	}
}

// TestRunKGSetup_WriteHealthError forces writeGraphHealth to fail (line ~562).
func TestRunKGSetup_WriteHealthError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == "graph-health.json" {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected write-health seam error, got %v", err)
	}
}

// TestRunKGSetup_BridgeContractError forces writeBridgeContract to fail
// (line ~567).
func TestRunKGSetup_BridgeContractError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == "bridge-contract.yaml" {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected bridge-contract seam error, got %v", err)
	}
}

// TestRunKGSetup_ManifestError forces saveManifest to fail (line ~573).
func TestRunKGSetup_ManifestError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == "manifest.json" {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected manifest seam error, got %v", err)
	}
}

// TestRunKGSetup_AppendLogError forces the final appendLogEntry to fail
// (line ~585) by injecting an OpenFile failure for log.md.
func TestRunKGSetup_AppendLogError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		openFile: func(path string, flag int, perm fs.FileMode) (*os.File, error) {
			if filepath.Base(path) == kgLogFileName {
				return nil, errSeam
			}
			return os.OpenFile(path, flag, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected append-log seam error, got %v", err)
	}
}

func TestRunKGSetup_WriteIndexError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == kgIndexFileName {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected write-index seam error, got %v", err)
	}
}

func TestRunKGSetup_WriteLogError(t *testing.T) {
	newTempKG(t)
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if filepath.Base(path) == kgLogFileName {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	err := runKGSetup(fake)
	if err == nil || !errors.Is(err, errSeam) {
		t.Fatalf("expected write-log seam error, got %v", err)
	}
}

// ── runKGHealth: MarshalIndent error ──────────────────────────────────────────

func TestRunKGHealth_MarshalError(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fake := withMarshalIndentError(t)
	deps := testDeps()
	deps.IO = fake
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", true, "")
	err := runKGHealth(deps, cmd)
	if !errors.Is(err, errSeam) {
		t.Fatalf("expected marshal seam error, got %v", err)
	}
}

// ── recordRawSource: mkdir error ─────────────────────────────────────────────

func TestRecordRawSource_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	err := recordRawSource(fake, home, RawSource{ID: "x", Title: "t", SourceType: "markdown"}, []byte("body"))
	if !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

func TestRecordRawSource_WriteError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	err := recordRawSource(fake, home, RawSource{ID: "x", Title: "t", SourceType: "markdown"}, []byte("body"))
	if !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── moveToImported: mkdir + rename error paths ────────────────────────────────

func TestMoveToImported_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	if err := moveToImported(fake, home, "x"); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

func TestMoveToImported_RenameError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{rename: func(string, string) error { return errSeam }}
	if err := moveToImported(fake, home, "x"); !errors.Is(err, errSeam) {
		t.Fatalf("expected rename seam error, got %v", err)
	}
}

// ── createGraphNote: mkdir & write error paths ────────────────────────────────

func TestCreateGraphNote_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(fake, home, note, ""); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

func TestCreateGraphNote_WriteError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(fake, home, note, ""); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── updateGraphNote: read error path ──────────────────────────────────────────

func TestUpdateGraphNote_ReadError(t *testing.T) {
	home := newTempKG(t)
	// Seed a real note we can later try to update.
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(testIO(), home, note, "body"); err != nil {
		t.Fatalf("seed createGraphNote: %v", err)
	}
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if err := updateGraphNote(fake, home, note, "body2"); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

func TestUpdateGraphNote_WriteError(t *testing.T) {
	home := newTempKG(t)
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(testIO(), home, note, "body"); err != nil {
		t.Fatalf("seed createGraphNote: %v", err)
	}
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	if err := updateGraphNote(fake, home, note, "body2"); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── writeBridgeContract: mkdir & write error paths ────────────────────────────

func TestWriteBridgeContract_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	if err := writeBridgeContract(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

func TestWriteBridgeContract_WriteError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	if err := writeBridgeContract(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── saveManifest error paths ──────────────────────────────────────────────────

func TestSaveManifest_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	m := &IntegrityManifest{SchemaVersion: 1, Notes: map[string]IntegrityManifestEntry{}}
	if err := saveManifest(fake, home, m); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

func TestSaveManifest_MarshalError(t *testing.T) {
	home := newTempKG(t)
	fake := withMarshalIndentError(t)
	m := &IntegrityManifest{SchemaVersion: 1, Notes: map[string]IntegrityManifestEntry{}}
	if err := saveManifest(fake, home, m); !errors.Is(err, errSeam) {
		t.Fatalf("expected marshal seam error, got %v", err)
	}
}

func TestSaveManifest_WriteError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{writeFile: func(string, []byte, fs.FileMode) error { return errSeam }}
	m := &IntegrityManifest{SchemaVersion: 1, Notes: map[string]IntegrityManifestEntry{}}
	if err := saveManifest(fake, home, m); !errors.Is(err, errSeam) {
		t.Fatalf("expected write seam error, got %v", err)
	}
}

// ── loadManifest: non-NotExist read error ─────────────────────────────────────

func TestLoadManifest_ReadError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if _, err := loadManifest(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

// TestUpdateManifest_LoadError covers the load-error return in updateManifest.
func TestUpdateManifest_LoadError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if err := updateManifest(fake, home, "note-x", "body"); !errors.Is(err, errSeam) {
		t.Fatalf("expected load seam error, got %v", err)
	}
}

// TestWarmNotesInDir_ReadDirError covers the early-return on a ReadDir error
// (counts as "not counted as skips" — returns 0,0).
func TestWarmNotesInDir_ReadDirError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	fake := &fakeKGIO{readDir: func(string) ([]os.DirEntry, error) { return nil, errSeam }}
	indexed, skipped := warmNotesInDir(fake, store, filepath.Join(home, "notes", "entities"), nil)
	if indexed != 0 || skipped != 0 {
		t.Errorf("expected (0,0) on readdir error, got (%d,%d)", indexed, skipped)
	}
}

// TestWarmNotesInDir_ReadFileError exercises the per-file readfile-error
// skip branch by failing the file read seam after a real directory has been
// populated.
func TestWarmNotesInDir_ReadFileError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Seed one entity note.
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(testIO(), home, note, "body"); err != nil {
		t.Fatalf("create note: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	indexed, skipped := warmNotesInDir(fake, store, filepath.Join(home, "notes", "entities"), nil)
	if skipped == 0 || indexed != 0 {
		t.Errorf("expected only skipped (>=1) on read-file error, got (%d,%d)", indexed, skipped)
	}
}

// ── runKGCompact: mkdir error ─────────────────────────────────────────────────

func TestRunKGCompact_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	if err := runKGCompact(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected mkdir seam error, got %v", err)
	}
}

// ── writeLintReport: silent-error branches (no return value to assert on,
// just ensure the swallowed branches execute without panicking) ──────────────

func TestWriteLintReport_MkdirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{mkdirAll: func(string, fs.FileMode) error { return errSeam }}
	// Should not panic and should return without writing the report.
	writeLintReport(fake, home, &LintReport{Timestamp: "2026-01-01T00:00:00Z"})
}

func TestWriteLintReport_MarshalError(t *testing.T) {
	home := newTempKG(t)
	fake := withMarshalIndentError(t)
	writeLintReport(fake, home, &LintReport{Timestamp: "2026-01-01T00:00:00Z"})
}

// ── persistReweavedNote: read error (silent) ──────────────────────────────────

func TestPersistReweavedNote_ReadError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	persistReweavedNote(fake, home, "missing", &GraphNote{Type: "entity"})
}

// ── readIndex / readGraphHealth: non-NotExist ReadFile error ──────────────────

func TestReadIndex_ReadFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if _, err := readIndex(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

func TestReadGraphHealth_ReadFileError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readFile: func(string) ([]byte, error) { return nil, errSeam }}
	if _, err := readGraphHealth(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected read seam error, got %v", err)
	}
}

// ── tallyGraphNoteDir: ReadDir error (not NotExist) ───────────────────────────

func TestTallyGraphNoteDir_ReadDirError(t *testing.T) {
	fake := &fakeKGIO{readDir: func(string) ([]os.DirEntry, error) { return nil, errSeam }}
	h := &GraphHealth{}
	if err := tallyGraphNoteDir(fake, "/tmp/anywhere", "entities", h); !errors.Is(err, errSeam) {
		t.Fatalf("expected readdir seam error, got %v", err)
	}
}

// ── listPendingRawSources: ReadDir error (not NotExist) ───────────────────────

func TestListPendingRawSources_ReadDirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readDir: func(string) ([]os.DirEntry, error) { return nil, errSeam }}
	if _, err := listPendingRawSources(fake, home); !errors.Is(err, errSeam) {
		t.Fatalf("expected readdir seam error, got %v", err)
	}
}

// ── walkNoteFiles: ReadDir error on notes dir ─────────────────────────────────

func TestWalkNoteFiles_ReadDirError(t *testing.T) {
	home := newTempKG(t)
	fake := &fakeKGIO{readDir: func(string) ([]os.DirEntry, error) { return nil, errSeam }}
	err := walkNoteFiles(fake, home, func(string, fs.DirEntry) error { return nil })
	if !errors.Is(err, errSeam) {
		t.Fatalf("expected readdir seam error, got %v", err)
	}
}

// TestWalkNoteFilesIn_FnError exercises the fn-returns-err branch in
// walkNoteFilesIn (line ~717).
func TestWalkNoteFilesIn_FnError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Create a note file in entities so the fn callback fires.
	note := &GraphNote{SchemaVersion: 1, ID: "e1", Type: "entity", Title: "t",
		Summary: "s", Status: "draft", CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := createGraphNote(testIO(), home, note, "body"); err != nil {
		t.Fatalf("create note: %v", err)
	}
	err := walkNoteFiles(testIO(), home, func(string, fs.DirEntry) error { return errSeam })
	if !errors.Is(err, errSeam) {
		t.Fatalf("expected fn-propagated seam error, got %v", err)
	}
}

// ── ingestEntityNotes / ingestDecisionNotes: createGraphNote error branch ─────

// TestIngestEntityNotes_CreateError fault-injects a write failure so
// createGraphNote fails inside ingestEntityNotes, exercising the
// result.Warnings append branch.
func TestIngestEntityNotes_CreateError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Allow setup writes already complete; from now on fail entity writes.
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if strings.Contains(path, filepath.Join("notes", "entities")) {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	src := RawSource{ID: "x", Title: "t"}
	srcNote := &GraphNote{ID: "src-x", Type: "source"}
	result := &IngestResult{SourceID: "x"}
	// Body contains backticked entities so extractEntities produces at least one.
	ingestEntityNotes(fake, home, src, srcNote, "Refer to `Alpha` and `Beta`.", "2026-01-01T00:00:00Z", result)
	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning from create-graph-note error")
	}
}

func TestIngestDecisionNotes_CreateError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fake := &fakeKGIO{
		writeFile: func(path string, data []byte, perm fs.FileMode) error {
			if strings.Contains(path, filepath.Join("notes", "decisions")) {
				return errSeam
			}
			return os.WriteFile(path, data, perm)
		},
	}
	src := RawSource{ID: "x", Title: "t"}
	srcNote := &GraphNote{ID: "src-x", Type: "source"}
	result := &IngestResult{SourceID: "x"}
	// Decision-shaped sentence triggers extractDecisions (matches "decided").
	ingestDecisionNotes(fake, home, src, srcNote, "We decided to adopt the new schema.", "2026-01-01T00:00:00Z", result)
	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning from create-graph-note error")
	}
}

// TestStdKGIO_DefaultMarshalIndent verifies that the production stdKGIO's
// MarshalIndent behaves identically to encoding/json's stdlib helper. This
// replaces the legacy TestJSONMarshalIndentSeam_DefaultDelegatesToStdlib
// that asserted the old package-level var defaulted to the stdlib.
func TestStdKGIO_DefaultMarshalIndent(t *testing.T) {
	io := stdKGIO{}
	got, err := io.MarshalIndent(map[string]int{"a": 1}, "", "  ")
	if err != nil {
		t.Fatalf("stdKGIO.MarshalIndent: %v", err)
	}
	if string(got) != "{\n  \"a\": 1\n}" {
		t.Errorf("seam default diverges from stdlib: %s", string(got))
	}
}
