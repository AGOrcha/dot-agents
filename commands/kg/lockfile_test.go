package kg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/kg/lockfile"
	"github.com/NikashPrakash/dot-agents/internal/kg/registry"
)

func TestLockfilePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KG_HOME", dir)
	want := filepath.Join(dir, "self", "adapters.lock.yaml")
	if got := lockfilePath(); got != want {
		t.Fatalf("lockfilePath() = %q, want %q", got, want)
	}
}

func TestBuiltinRegistryHasNone(t *testing.T) {
	reg, err := builtinRegistry()
	if err != nil {
		t.Fatalf("builtinRegistry: %v", err)
	}
	names := reg.Names()
	if len(names) != 1 || names[0] != "none" {
		t.Fatalf("builtinRegistry names = %v, want [none]", names)
	}
}

func TestResolveBackend(t *testing.T) {
	a, err := resolveBackend("dotagents-builtin:graph/none@^1.0")
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if a.Name() != "none" {
		t.Fatalf("resolved name = %q, want none", a.Name())
	}
	if _, err := resolveBackend("dotagents-builtin:graph/missing@^1.0"); err == nil {
		t.Fatal("resolveBackend missing want error")
	}
}

func TestResolveBackendNoneEndToEnd(t *testing.T) {
	// The hard test from TASKS.yaml: a profile selecting the none backend
	// resolves and runs a no-op impact radius.
	a, err := resolveBackend("dotagents-builtin:graph/none@^1.0")
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	res, err := a.ImpactRadius(impactReq("x", "y"))
	if err != nil {
		t.Fatalf("ImpactRadius: %v", err)
	}
	if len(res.IDs) != 2 || res.IDs[0] != "x" || res.IDs[1] != "y" {
		t.Fatalf("none impact radius = %v, want [x y]", res.IDs)
	}
}

func TestRunLockfileShowEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	var buf bytes.Buffer
	if err := runLockfileShow(stdKGIO{}, &buf, path, "", false); err != nil {
		t.Fatalf("runLockfileShow: %v", err)
	}
	if !strings.Contains(buf.String(), "No adapters activated") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunLockfileShowText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("none", "sha256:src", "sha256:schema", time.Now())
	lf.Adapters["none"].MaterializedViews = map[string]*lockfile.View{
		"v1": {ViewStatus: lockfile.StatusReady},
	}
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileShow(stdKGIO{}, &buf, path, "", false); err != nil {
		t.Fatalf("runLockfileShow: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"none", "sha256:src", "sha256:schema", "view v1", "ready"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunLockfileShowJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("none", "sha256:src", "sha256:schema", time.Now())
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileShow(stdKGIO{}, &buf, path, "", true); err != nil {
		t.Fatalf("runLockfileShow json: %v", err)
	}
	var res lockfileShowResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if _, ok := res.Adapters["none"]; !ok {
		t.Fatalf("json result missing none: %s", buf.String())
	}
}

func TestRunLockfileShowAdapterFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("none", "s", "d", time.Now())
	lf.Activate("other", "s", "d", time.Now())
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileShow(stdKGIO{}, &buf, path, "none", false); err != nil {
		t.Fatalf("runLockfileShow filter: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "other") {
		t.Fatalf("filter leaked other adapter:\n%s", out)
	}
	// Unknown adapter -> error.
	if err := runLockfileShow(stdKGIO{}, &bytes.Buffer{}, path, "missing", false); err == nil {
		t.Fatal("runLockfileShow unknown adapter want error")
	}
}

func TestRunLockfileShowLoadError(t *testing.T) {
	// Loading a directory as the lockfile path errors.
	if err := runLockfileShow(stdKGIO{}, &bytes.Buffer{}, t.TempDir(), "", false); err == nil {
		t.Fatal("runLockfileShow on a directory want error")
	}
}

func TestRunLockfileReconcileNoChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("none", "s", "d", time.Now())
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileReconcile(&buf, path, false); err != nil {
		t.Fatalf("runLockfileReconcile: %v", err)
	}
	if !strings.Contains(buf.String(), "no changes") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunLockfileReconcileWithChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("comp", "s", "d", time.Now())
	lf.Adapters["comp"].MaterializedViews = map[string]*lockfile.View{
		"v": {ViewStatus: lockfile.StatusReady, ViewDigest: "sha256:v"},
	}
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileReconcile(&buf, path, false); err != nil {
		t.Fatalf("runLockfileReconcile: %v", err)
	}
	if !strings.Contains(buf.String(), "comp/v") || !strings.Contains(buf.String(), "pending-rebuild") {
		t.Fatalf("output = %q", buf.String())
	}
	// State must have been persisted.
	reloaded, err := lockfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Adapters["comp"].MaterializedViews["v"].ViewStatus != lockfile.StatusPendingRebuild {
		t.Fatal("reconcile change not persisted")
	}
}

func TestRunLockfileReconcileJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("comp", "s", "d", time.Now())
	lf.Adapters["comp"].MaterializedViews = map[string]*lockfile.View{
		"v": {ViewStatus: lockfile.StatusReady, ViewDigest: "sha256:v"},
	}
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLockfileReconcile(&buf, path, true); err != nil {
		t.Fatalf("runLockfileReconcile json: %v", err)
	}
	var res reconcileResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(res.Changes) != 1 {
		t.Fatalf("json changes = %v, want 1", res.Changes)
	}
}

func TestRunLockfileReconcileLoadError(t *testing.T) {
	if err := runLockfileReconcile(&bytes.Buffer{}, t.TempDir(), false); err == nil {
		t.Fatal("runLockfileReconcile on a directory want error")
	}
}

func TestRunLockfileReconcileSaveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	lf := lockfile.New()
	lf.Activate("comp", "s", "d", time.Now())
	lf.Adapters["comp"].MaterializedViews = map[string]*lockfile.View{
		"v": {ViewStatus: lockfile.StatusReady, ViewDigest: "sha256:v"},
	}
	if err := lockfile.Save(path, lf); err != nil {
		t.Fatal(err)
	}
	// Make the parent dir read-only so the atomic Save (which writes a temp
	// file into it) fails after Reconcile produces a change.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := runLockfileReconcile(&bytes.Buffer{}, path, false)
	if err == nil {
		t.Skip("filesystem permitted write to read-only dir (running as root?)")
	}
}

func TestBuiltinRegistryError(t *testing.T) {
	orig := registerBuiltins
	t.Cleanup(func() { registerBuiltins = orig })
	registerBuiltins = func(*registry.Registry) error { return errReg }
	if _, err := builtinRegistry(); err == nil {
		t.Fatal("builtinRegistry want error when registration fails")
	}
	if _, err := resolveBackend("none"); err == nil {
		t.Fatal("resolveBackend want error when registration fails")
	}
}

var errReg = errorString("register failed")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestNewLockfileCmdWiring(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KG_HOME", dir)
	deps := Deps{ExampleBlock: func(lines ...string) string { return "" }}
	cmd := newLockfileCmd(deps)
	if cmd.Use != "lockfile" {
		t.Fatalf("cmd.Use = %q", cmd.Use)
	}
	sub := map[string]bool{}
	for _, c := range cmd.Commands() {
		sub[c.Name()] = true
	}
	if !sub["show"] || !sub["reconcile"] {
		t.Fatalf("missing subcommands: %v", sub)
	}

	// Execute `show` through the cobra tree against an empty KG_HOME.
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute show: %v", err)
	}
	if !strings.Contains(out.String(), "No adapters activated") {
		t.Fatalf("show output = %q", out.String())
	}

	// Execute `reconcile`.
	out.Reset()
	cmd.SetArgs([]string{"reconcile"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute reconcile: %v", err)
	}
	if !strings.Contains(out.String(), "no changes") {
		t.Fatalf("reconcile output = %q", out.String())
	}
}

func TestLockfileCmdRegisteredOnKG(t *testing.T) {
	deps := Deps{ExampleBlock: func(lines ...string) string { return "" }}
	root := NewKGCmd(deps)
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "lockfile" {
			found = true
		}
	}
	if !found {
		t.Fatal("lockfile command not registered on kg")
	}
}

// impactReq builds an ImpactRequest from ids for the end-to-end test.
func impactReq(ids ...string) registry.ImpactRequest {
	return registry.ImpactRequest{ChangedIDs: ids}
}
