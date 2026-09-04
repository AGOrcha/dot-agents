package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
)

// writeBridgeConsumerLockfile writes a .agentsrc.lock at dir declaring one
// materialized view ("bridged") that depends on the crg-bridge mirror — a
// live §11.4-criterion-4 consumer.
func writeBridgeConsumerLockfile(t *testing.T, dir string) {
	t.Helper()
	lf := lockfile.New()
	lf.Activate("compliance", "sha256:src", "sha256:schema", time.Now())
	lf.Adapters["compliance"].MaterializedViews = map[string]*lockfile.View{
		"bridged": {
			ViewStatus: lockfile.StatusReady,
			DependsOn:  []lockfile.ViewDependency{{Adapter: graphstore.BridgeAdapterName}},
		},
	}
	if err := lockfile.Save(config.AgentsLockPath(dir), lf); err != nil {
		t.Fatalf("write bridge-consumer lockfile: %v", err)
	}
}

// writeCleanAdapterLockfile writes a .agentsrc.lock at dir with an activated
// adapter that has zero bridge dependencies — the "clean" bucket.
func writeCleanAdapterLockfile(t *testing.T, dir string) {
	t.Helper()
	lf := lockfile.New()
	lf.Activate("crg", "sha256:src", "sha256:schema", time.Now())
	lf.Adapters["crg"].MaterializedViews = map[string]*lockfile.View{
		"pure": {
			ViewStatus: lockfile.StatusReady,
			DependsOn:  []lockfile.ViewDependency{{Adapter: "crg"}},
		},
	}
	if err := lockfile.Save(config.AgentsLockPath(dir), lf); err != nil {
		t.Fatalf("write clean adapter lockfile: %v", err)
	}
}

func TestDriftBridgeConsumerPhase_NotAKGRepo(t *testing.T) {
	dir := t.TempDir() // no .agentsrc.lock at all
	report := &RepoDriftReport{}
	driftBridgeConsumerPhase(report, ManagedProject{Name: "fresh", Path: dir})
	if report.BridgeConsumerStatus != bridgeConsumerStatusNotAKGRepo {
		t.Errorf("status = %q, want %q", report.BridgeConsumerStatus, bridgeConsumerStatusNotAKGRepo)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("not_a_kg_repo must not warn, got %v", report.Warnings)
	}
}

func TestDriftBridgeConsumerPhase_Clean(t *testing.T) {
	dir := t.TempDir()
	writeCleanAdapterLockfile(t, dir)
	report := &RepoDriftReport{}
	driftBridgeConsumerPhase(report, ManagedProject{Name: "clean-proj", Path: dir})
	if report.BridgeConsumerStatus != bridgeConsumerStatusClean {
		t.Errorf("status = %q, want %q", report.BridgeConsumerStatus, bridgeConsumerStatusClean)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("clean must not warn, got %v", report.Warnings)
	}
	if len(report.BridgeConsumers) != 0 {
		t.Errorf("clean must have zero consumers, got %v", report.BridgeConsumers)
	}
}

func TestDriftBridgeConsumerPhase_ConsumersFound(t *testing.T) {
	dir := t.TempDir()
	writeBridgeConsumerLockfile(t, dir)
	report := &RepoDriftReport{}
	driftBridgeConsumerPhase(report, ManagedProject{Name: "bridged-proj", Path: dir})
	if report.BridgeConsumerStatus != bridgeConsumerStatusConsumersFound {
		t.Errorf("status = %q, want %q", report.BridgeConsumerStatus, bridgeConsumerStatusConsumersFound)
	}
	if len(report.BridgeConsumers) != 1 || report.BridgeConsumers[0].View != "bridged" {
		t.Errorf("expected one consumer view=bridged, got %v", report.BridgeConsumers)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "§11.4 criterion 4 not met") {
		t.Errorf("expected criterion-4 warning, got %v", report.Warnings)
	}
}

func TestDriftBridgeConsumerPhase_UnreadableLockfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(config.AgentsLockPath(dir), []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}
	report := &RepoDriftReport{}
	driftBridgeConsumerPhase(report, ManagedProject{Name: "corrupt", Path: dir})
	if report.BridgeConsumerStatus != bridgeConsumerStatusError {
		t.Errorf("status = %q, want %q", report.BridgeConsumerStatus, bridgeConsumerStatusError)
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "could not read adapter lockfile") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lockfile-unreadable warning, got %v", report.Warnings)
	}
}

// TestDetectRepoDrift_BridgeConsumersFlipsStatusToWarn verifies the
// end-to-end wiring: a repo that is otherwise perfectly healthy (fresh
// checkpoint, canonical plan structure) still reports overall status=warn
// once a live crg-bridge consumer is present, because driftBridgeConsumerPhase
// appends a warning like every other phase.
func TestDetectRepoDrift_BridgeConsumersFlipsStatusToWarn(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	writeBridgeConsumerLockfile(t, dir)

	project := ManagedProject{Name: "bridge-warn-proj", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if report.BridgeConsumerStatus != bridgeConsumerStatusConsumersFound {
		t.Fatalf("expected consumers_found, got %q", report.BridgeConsumerStatus)
	}
	if report.Status != "warn" {
		t.Errorf("expected overall status=warn from bridge-consumer finding, got %q (warnings=%v)", report.Status, report.Warnings)
	}
}

func TestAggregateDrift_BridgeSweepBuckets(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "a"}, Status: "warn", BridgeConsumerStatus: bridgeConsumerStatusConsumersFound},
		{Project: ManagedProject{Name: "b"}, Status: "healthy", BridgeConsumerStatus: bridgeConsumerStatusClean},
		{Project: ManagedProject{Name: "c"}, Status: "healthy", BridgeConsumerStatus: bridgeConsumerStatusNotAKGRepo},
		{Project: ManagedProject{Name: "d"}, Status: "warn", BridgeConsumerStatus: bridgeConsumerStatusError},
		{Project: ManagedProject{Name: "e"}, Status: "unreachable"}, // never ran the phase
	}
	agg := aggregateDrift(reports)
	if len(agg.BridgeSweep.ConsumersFoundRepos) != 1 || agg.BridgeSweep.ConsumersFoundRepos[0] != "a" {
		t.Errorf("ConsumersFoundRepos = %v, want [a]", agg.BridgeSweep.ConsumersFoundRepos)
	}
	if len(agg.BridgeSweep.CleanRepos) != 1 || agg.BridgeSweep.CleanRepos[0] != "b" {
		t.Errorf("CleanRepos = %v, want [b]", agg.BridgeSweep.CleanRepos)
	}
	if len(agg.BridgeSweep.NotAKGRepoRepos) != 1 || agg.BridgeSweep.NotAKGRepoRepos[0] != "c" {
		t.Errorf("NotAKGRepoRepos = %v, want [c]", agg.BridgeSweep.NotAKGRepoRepos)
	}
	if len(agg.BridgeSweep.ErrorRepos) != 1 || agg.BridgeSweep.ErrorRepos[0] != "d" {
		t.Errorf("ErrorRepos = %v, want [d]", agg.BridgeSweep.ErrorRepos)
	}
}

func TestAggregateDrift_BridgeSweepEmptyBucketsNeverNil(t *testing.T) {
	agg := aggregateDrift(nil)
	if agg.BridgeSweep.ConsumersFoundRepos == nil || agg.BridgeSweep.CleanRepos == nil || agg.BridgeSweep.NotAKGRepoRepos == nil {
		t.Errorf("bridge sweep bucket slices must be [] not null for JSON: %+v", agg.BridgeSweep)
	}
}

func TestRenderBridgeSweepSummary_ConsumersFound(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderBridgeSweepSummary(BridgeSweepSummary{
			ConsumersFoundRepos: []string{"proj-a"},
			CleanRepos:          []string{"proj-b"},
		})
		return nil
	})
	if !strings.Contains(out, "consumers found") || !strings.Contains(out, "proj-a") {
		t.Errorf("expected consumers-found line naming proj-a, got %s", out)
	}
}

func TestRenderBridgeSweepSummary_AllClean(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderBridgeSweepSummary(BridgeSweepSummary{
			CleanRepos:      []string{"proj-b"},
			NotAKGRepoRepos: []string{"proj-c"},
		})
		return nil
	})
	if !strings.Contains(out, "criterion 4 satisfied") {
		t.Errorf("expected criterion-4-satisfied line when no consumers found, got %s", out)
	}
}

func TestRenderBridgeSweepSummary_ErrorReposListed(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderBridgeSweepSummary(BridgeSweepSummary{
			CleanRepos: []string{"proj-b"},
			ErrorRepos: []string{"proj-broken"},
		})
		return nil
	})
	if !strings.Contains(out, "errors: 1") || !strings.Contains(out, "proj-broken") {
		t.Errorf("expected errors line naming proj-broken, got %s", out)
	}
}

func TestRenderBridgeSweepSummary_NothingCheckedIsSilent(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderBridgeSweepSummary(BridgeSweepSummary{})
		return nil
	})
	if strings.Contains(out, "criterion 4") {
		t.Errorf("expected no output when nothing was evaluated, got %s", out)
	}
}

// TestRunWorkflowDriftWithLister_PathFlagBypassesRegistry drives --path: no
// managed projects registered at all (registry lister would return empty),
// yet the current-directory check still runs because --path never calls the
// lister.
func TestRunWorkflowDriftWithLister_PathFlagBypassesRegistry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	target := t.TempDir()
	writeBridgeConsumerLockfile(t, target)

	cmd := newDriftTestCommand("", 7, 30)
	cmd.Flags().String("path", target, "")
	neverCalled := func() ([]ManagedProject, error) {
		t.Fatal("lister should not be called when --path is set")
		return nil, nil
	}
	out, err := captureCovStdout(t, func() error {
		return runWorkflowDriftWithLister(cmd, neverCalled)
	})
	if err != nil {
		t.Fatalf("runWorkflowDriftWithLister --path: %v", err)
	}
	if !strings.Contains(out, "consumers found") {
		t.Errorf("expected bridge-consumer finding surfaced via --path, got %s", out)
	}
}

// TestRunWorkflowDriftWithLister_PathFlagJSON verifies the --path JSON shape
// carries bridge_consumer_status through to reports[0], the field the
// crg-bridge-consumer-audit.sh script parses.
func TestRunWorkflowDriftWithLister_PathFlagJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	target := t.TempDir()
	writeCleanAdapterLockfile(t, target)

	cmd := newDriftTestCommand("", 7, 30)
	cmd.Flags().String("path", target, "")
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out, err := captureCovStdout(t, func() error {
		return runWorkflowDriftWithLister(cmd, loadManagedProjects)
	})
	if err != nil {
		t.Fatalf("runWorkflowDriftWithLister --path --json: %v", err)
	}
	if !strings.Contains(out, `"bridge_consumer_status": "clean"`) {
		t.Errorf("expected bridge_consumer_status=clean in JSON, got %s", out)
	}
}

// TestRunWorkflowDriftWithLister_PathFlagRelative drives the relative-path
// arm of filepath.Abs inside --path handling — the real-world invocation
// shape (`da workflow drift --path . --json`) that
// scripts/crg-bridge-consumer-audit.sh uses from the repo root.
func TestRunWorkflowDriftWithLister_PathFlagRelative(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	target := t.TempDir()
	writeBridgeConsumerLockfile(t, target)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cmd := newDriftTestCommand("", 7, 30)
	cmd.Flags().String("path", ".", "")
	out, runErr := captureCovStdout(t, func() error {
		return runWorkflowDriftWithLister(cmd, loadManagedProjects)
	})
	if runErr != nil {
		t.Fatalf("runWorkflowDriftWithLister --path .: %v", runErr)
	}
	if !strings.Contains(out, "consumers found") {
		t.Errorf("expected bridge-consumer finding via relative --path, got %s", out)
	}
}

func TestResolveProjectNameForPath_FallsBackToDirBase(t *testing.T) {
	dir := t.TempDir()
	name := resolveProjectNameForPath(dir)
	if name != filepath.Base(dir) {
		t.Errorf("name = %q, want dir base %q", name, filepath.Base(dir))
	}
}

func TestResolveProjectNameForPath_UsesAgentsRCProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".agentsrc.json"), []byte(`{"project":"custom-name"}`), 0644); err != nil {
		t.Fatal(err)
	}
	name := resolveProjectNameForPath(dir)
	if name != "custom-name" {
		t.Errorf("name = %q, want custom-name", name)
	}
}
