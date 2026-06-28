package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeriveTrustedRepoID covers the FORK-1 hybrid / R12 ambiguity guard: a
// trustworthy single-origin repo yields its canonical repo_id, while any
// ambiguous / multi-remote / non-git topology yields no trusted key so the
// caller falls back to the logical id.
func TestDeriveTrustedRepoID(t *testing.T) {
	tests := []struct {
		name      string
		remotes   map[string][]string
		err       error
		wantID    string
		wantAmbig bool
	}{
		{
			name:    "unambiguous single origin",
			remotes: map[string][]string{"origin": {"git@github.com:acme/repo.git"}},
			wantID:  "github.com/acme/repo",
		},
		{
			name: "second remote with same identity is not ambiguous",
			remotes: map[string][]string{
				"origin": {"git@github.com:acme/repo.git"},
				"backup": {"https://github.com/acme/repo.git"},
			},
			wantID: "github.com/acme/repo",
		},
		{
			// The real AGOrcha case: origin=NikashPrakash fork vs org=AGOrcha.
			name: "divergent second remote is ambiguous (AGOrcha origin-vs-org)",
			remotes: map[string][]string{
				"origin": {"git@github.com:NikashPrakash/dot-agents.git"},
				"org":    {"git@github.com:AGOrcha/dot-agents.git"},
			},
			wantAmbig: true,
		},
		{
			name:      "origin with multiple URLs is ambiguous",
			remotes:   map[string][]string{"origin": {"git@github.com:acme/repo.git", "https://gitlab.com/acme/repo.git"}},
			wantAmbig: true,
		},
		{
			name:    "no origin remote falls back (not ambiguous)",
			remotes: map[string][]string{"upstream": {"git@github.com:acme/repo.git"}},
		},
		{
			name:    "no remotes at all falls back",
			remotes: map[string][]string{},
		},
		{
			name: "non-git path falls back",
			err:  errors.New("not a git repo"),
		},
		{
			name:    "unparseable origin URL falls back",
			remotes: map[string][]string{"origin": {"::::not a url::::"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := gitRemoteAllURLs
			gitRemoteAllURLs = func(string) (map[string][]string, error) {
				return tt.remotes, tt.err
			}
			defer func() { gitRemoteAllURLs = orig }()

			gotID, gotAmbig := DeriveTrustedRepoID("/whatever")
			if gotID != tt.wantID {
				t.Errorf("repoID = %q, want %q", gotID, tt.wantID)
			}
			if gotAmbig != tt.wantAmbig {
				t.Errorf("ambiguous = %v, want %v", gotAmbig, tt.wantAmbig)
			}
		})
	}
}

// TestAddProjectRecordsTrustedRepoIDOnly asserts AddProject records a repo_id in
// the synced identity registry for an unambiguous origin, and records NONE for
// an ambiguous one (R12) — falling back to the logical id (the map key).
func TestAddProjectRecordsTrustedRepoIDOnly(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())

	orig := gitRemoteAllURLs
	defer func() { gitRemoteAllURLs = orig }()

	gitRemoteAllURLs = func(string) (map[string][]string, error) {
		return map[string][]string{"origin": {"git@github.com:acme/clean.git"}}, nil
	}
	cfg := newEmptyConfig()
	cfg.AddProject("clean", "/p/clean")
	if got := cfg.ProjectRepoID("clean"); got != "github.com/acme/clean" {
		t.Errorf("clean repo_id = %q, want github.com/acme/clean", got)
	}

	gitRemoteAllURLs = func(string) (map[string][]string, error) {
		return map[string][]string{
			"origin": {"git@github.com:NikashPrakash/dot-agents.git"},
			"org":    {"git@github.com:AGOrcha/dot-agents.git"},
		}, nil
	}
	cfg.AddProject("ambig", "/p/ambig")
	if got := cfg.ProjectRepoID("ambig"); got != "" {
		t.Errorf("ambiguous project must record NO repo_id, got %q", got)
	}
	if !cfg.IsProjectKnown("ambig") {
		t.Error("ambiguous project must still be in the identity registry (logical-id fallback)")
	}
}

// TestSaveSplitsIdentityFromBinding asserts the synced config.json carries the
// identity (repo_id) but NEVER the absolute path, and the path lives only in the
// machine-local binding table under local/ (defects 1 & 2, R5/R7).
func TestSaveSplitsIdentityFromBinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	orig := gitRemoteAllURLs
	gitRemoteAllURLs = func(string) (map[string][]string, error) {
		return map[string][]string{"origin": {"git@github.com:acme/repo.git"}}, nil
	}
	defer func() { gitRemoteAllURLs = orig }()

	cfg := newEmptyConfig()
	cfg.AddProject("svc", filepath.FromSlash("/abs/machine/path/svc"))
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfgRaw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgRaw), "github.com/acme/repo") {
		t.Errorf("synced config.json should carry the portable repo_id:\n%s", cfgRaw)
	}
	if strings.Contains(string(cfgRaw), filepath.FromSlash("/abs/machine/path/svc")) {
		t.Errorf("synced config.json leaked an absolute path:\n%s", cfgRaw)
	}

	bindRaw, err := os.ReadFile(filepath.Join(home, "local", "bindings.json"))
	if err != nil {
		t.Fatalf("expected machine-local binding table: %v", err)
	}
	if !strings.Contains(string(bindRaw), filepath.FromSlash("/abs/machine/path/svc")) {
		t.Errorf("binding table should hold the absolute path:\n%s", bindRaw)
	}
}

// TestLoadMigratesLegacyV1Config asserts a legacy v1 config.json (paths inline)
// is migrated in memory on Load (UpgradeNeeded, paths folded into the binding
// table) without writing anything, and that the next Save persists the split
// shape so the path no longer appears in the synced config.json (R9, FORK-5).
func TestLoadMigratesLegacyV1Config(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	legacy := `{
  "version": 1,
  "projects": {
    "legacy": {"path": "/old/machine/legacy", "added": "2024-01-02T03:04:05Z"}
  }
}`
	cfgPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfgPath, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.UpgradeNeeded() {
		t.Error("legacy v1 config should set UpgradeNeeded")
	}
	if got := cfg.GetProjectPath("legacy"); got != filepath.Clean("/old/machine/legacy") {
		t.Errorf("legacy path not folded into binding table: %q", got)
	}
	// Load must be pure: the on-disk config is untouched until a mutating Save.
	after, _ := os.ReadFile(cfgPath)
	if string(after) != legacy {
		t.Error("Load mutated config.json on disk (must stay pure)")
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	migrated, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(migrated), "/old/machine/legacy") {
		t.Errorf("v2 config.json still carries the legacy path:\n%s", migrated)
	}
	var shape struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(migrated, &shape)
	if shape.Version != configSchemaVersion {
		t.Errorf("migrated config version = %d, want %d", shape.Version, configSchemaVersion)
	}
	if cfg.UpgradeNeeded() {
		t.Error("UpgradeNeeded should clear after a persisting Save")
	}
}

// TestSyncedIdentitySurvivesMissingBinding models machine B: it has the synced
// config.json (identity only) but no machine-local binding table. The project
// list must still be visible and the project reported as known-but-unbound
// rather than disappearing (defect 3 / BLOCKER 1, R4).
func TestSyncedIdentitySurvivesMissingBinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	synced := `{
  "version": 2,
  "projects": {"svc": {"repo_id": "github.com/acme/repo"}}
}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(synced), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsProjectKnown("svc") {
		t.Error("machine B should see the synced project identity")
	}
	if cfg.IsProjectBound("svc") {
		t.Error("machine B should have no local binding yet")
	}
	if got := cfg.GetProjectPath("svc"); got != "" {
		t.Errorf("unbound project path should be empty, got %q", got)
	}
	if cfg.ProjectRepoID("svc") != "github.com/acme/repo" {
		t.Error("portable repo_id should survive the trip to machine B")
	}

	// Rebinding on machine B must not touch the synced identity registry (R5).
	cfg.BindProject("svc", filepath.FromSlash("/machine-b/svc"))
	if got := cfg.GetProjectPath("svc"); got != filepath.Clean("/machine-b/svc") {
		t.Errorf("rebind failed: %q", got)
	}
	if cfg.ProjectRepoID("svc") != "github.com/acme/repo" {
		t.Error("BindProject must not mutate the identity registry")
	}
}

// TestLoadBindings_MalformedReturnsError covers the binding-table parse-error
// branch: a corrupt local/bindings.json must surface an error, not be silently
// dropped.
func TestLoadBindings_MalformedReturnsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "local", "bindings.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("expected parse error for malformed binding table")
	}
}

// TestLoadBindings_ReadErrorNotNotExist covers the non-NotExist read-error
// branch: a bindings.json that is a directory yields a read error.
func TestLoadBindings_ReadErrorNotNotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local", "bindings.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBindings(); err == nil {
		t.Error("expected read error when bindings.json is a directory")
	}
}

// TestLoadBindings_NilBindingsMap covers the f.Bindings==nil branch: a binding
// file with no bindings object yields an empty (non-nil) map.
func TestLoadBindings_NilBindingsMap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "local", "bindings.json"), []byte(`{"version":2}`), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := loadBindings()
	if err != nil {
		t.Fatalf("loadBindings: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil map, got %v", got)
	}
}

// TestLoadEmptyConfigStillLoadsBindings covers the missing-config.json branch
// where the binding table exists (e.g. a partially-materialized machine).
func TestLoadEmptyConfigStillLoadsBindings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local"), 0755); err != nil {
		t.Fatal(err)
	}
	bt := `{"version":2,"bindings":{"svc":{"path":"/b/svc","added":"2024-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(home, "local", "bindings.json"), []byte(bt), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.GetProjectPath("svc"); got != filepath.Clean("/b/svc") {
		t.Errorf("binding not loaded when config.json absent: %q", got)
	}
}

// TestLoadEmptyConfigBindingError covers the missing-config.json path's binding
// load-error branch (bindings.json is a directory).
func TestLoadEmptyConfigBindingError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local", "bindings.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("expected binding read error on empty-config path")
	}
}

// TestSaveBindings_MkdirError covers Save's second error branch: config.json
// writes fine but the local/ binding dir cannot be created because local is a
// file.
func TestSaveBindings_MkdirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "local"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newEmptyConfig()
	if err := cfg.Save(); err == nil {
		t.Error("expected binding-table mkdir error when local/ is a file")
	}
}

// TestSaveAtomicity_BindingFailurePreservesPath proves Fix 2: when the binding
// write fails mid-Save, the legacy path-bearing config.json on disk is left
// UNTOUCHED (recoverable), not overwritten with the path-free shape — so the
// project path is never lost between the two files. The binding table is written
// first precisely so a failure cannot strand the path.
func TestSaveAtomicity_BindingFailurePreservesPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// A legacy v1 config.json carrying the path INLINE — the recoverable shape.
	legacy := `{
  "version": 1,
  "projects": {"svc": {"path": "/old/machine/svc", "added": "2024-01-02T03:04:05Z"}}
}`
	cfgPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfgPath, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	// Force saveBindings to fail: make ~/.agents/local a FILE so its MkdirAll
	// errors. (We do not call Load here, so this does not interfere with reads.)
	if err := os.WriteFile(filepath.Join(home, "local"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newEmptyConfig()
	cfg.Projects["svc"] = Project{RepoID: "github.com/acme/svc"}
	cfg.BindProject("svc", "/old/machine/svc")

	if err := cfg.Save(); err == nil {
		t.Fatal("expected Save to fail when the binding table cannot be written")
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != legacy {
		t.Errorf("config.json was mutated despite a failed Save (path may be lost):\n%s", after)
	}
	if !strings.Contains(string(after), "/old/machine/svc") {
		t.Errorf("legacy path was lost from config.json:\n%s", after)
	}
}

// TestBindingTableNotInConfigJSON guards that the unexported binding map can
// never be marshaled into config.json even when populated.
func TestBindingTableNotInConfigJSON(t *testing.T) {
	cfg := newEmptyConfig()
	cfg.bindings["x"] = Binding{Path: "/secret/abs/path", Added: time.Now()}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/secret/abs/path") {
		t.Errorf("config.json marshal leaked a binding path:\n%s", data)
	}
}
