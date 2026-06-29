package kg

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/spf13/cobra"
)

// TestMain isolates the per-repo journal under a throwaway XDG_STATE_HOME for the
// whole package run. The kg runners now append typed journal events (p3b), so
// without this every existing kg test that drives ingest/link/maintain/warm/
// build/update/postprocess/sync would write events.log into the developer's real
// ~/.local/state. Mirrors p3a's commands/workflow TestMain isolation.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kg-journal-state-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_STATE_HOME", dir)
	// Neuter the real append path for the whole package: every kg runner resolves
	// the SAME repoPath (crgRepoRoot of the dot-agents repo), so the journal's
	// interprocess events.log lock would serialize the package's many parallel
	// runner tests into a multi-minute stall. The wiring (build → NewEvent →
	// journalEmit) still executes; only the disk write + lock is replaced by a
	// no-op. Capture-based tests override journalEmit with their own recorder.
	journalEmit = func(string, journal.Envelope) error { return nil }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// captureJournal swaps journalEmit for an in-memory recorder restored via
// t.Cleanup. The returned pointer accumulates every appended envelope so a test
// can assert the typed event a command produced without reading the log file.
func captureJournal(t *testing.T) *[]journal.Envelope {
	t.Helper()
	prev := journalEmit
	var got []journal.Envelope
	journalEmit = func(_ string, e journal.Envelope) error {
		got = append(got, e)
		return nil
	}
	t.Cleanup(func() { journalEmit = prev })
	return &got
}

// findEvent returns the first captured envelope for command, or fails.
func findEvent(t *testing.T, got []journal.Envelope, command string) journal.Envelope {
	t.Helper()
	for _, e := range got {
		if e.Command == command {
			return e
		}
	}
	t.Fatalf("no journal event for %q in %d captured events", command, len(got))
	return journal.Envelope{}
}

// ── journal_emit.go unit coverage ──────────────────────────────────────────────

func TestEmitKGSuccess_AppendsTypedEvent(t *testing.T) {
	got := captureJournal(t)
	emitKGSuccess("/repo", journal.CmdKGLinkAdd,
		&journal.KGContentDeltaInput{Operation: "link add", Targets: []string{"n", "s"}},
		&journal.KGContentDeltaObserved{Counts: map[string]int{"links_added": 1}, IDs: []string{"7"}})
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	e := (*got)[0]
	if e.Command != journal.CmdKGLinkAdd || e.EventType != journal.EventDurableDelta {
		t.Fatalf("unexpected envelope: %+v", e)
	}
	if e.Actor != journal.ActorMain {
		t.Fatalf("actor = %q, want main", e.Actor)
	}
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil || obs.Counts["links_added"] != 1 {
		t.Fatalf("observed decode: %v %+v", err, obs)
	}
}

func TestEmitKGSuccess_BlankRepoSkips(t *testing.T) {
	got := captureJournal(t)
	emitKGSuccess("", journal.CmdKGWarm, &journal.KGContentDeltaInput{Operation: "warm"}, &journal.KGContentDeltaObserved{})
	if len(*got) != 0 {
		t.Fatalf("blank repoPath should skip emission, got %d", len(*got))
	}
}

func TestEmitKGSuccess_BuildErrorWarnsNotFatal(t *testing.T) {
	got := captureJournal(t)
	// An unregistered command makes NewEvent fail; the helper must warn and not
	// append (and must never panic / propagate).
	emitKGSuccess("/repo", "kg not-a-command", nil, nil)
	if len(*got) != 0 {
		t.Fatalf("build error should not append, got %d", len(*got))
	}
}

func TestEmitKGEvent_AppendErrorWarnsNotFatal(t *testing.T) {
	prev := journalEmit
	journalEmit = func(string, journal.Envelope) error { return errors.New("disk full") }
	t.Cleanup(func() { journalEmit = prev })
	// Must not panic or propagate — proves a journal append failure cannot fail
	// the underlying command.
	emitKGSuccess("/repo", journal.CmdKGWarm,
		&journal.KGContentDeltaInput{Operation: "warm"},
		&journal.KGContentDeltaObserved{Counts: map[string]int{"indexed": 1}})
}

func TestEmitKGFailure_DropsObserved(t *testing.T) {
	got := captureJournal(t)
	emitKGFailure("/repo", journal.CmdKGIngest, &journal.KGIngestInput{File: "x.md"})
	e := findEvent(t, *got, journal.CmdKGIngest)
	if e.EventType != journal.EventFailed {
		t.Fatalf("event_type = %q, want failed", e.EventType)
	}
	if e.Observed != nil {
		t.Fatalf("failed event must drop observed, got %s", string(e.Observed))
	}
}

func TestJournalKG_BranchesOnOK(t *testing.T) {
	got := captureJournal(t)
	in := &journal.KGContentDeltaInput{Operation: "warm"}
	journalKG("/repo", journal.CmdKGWarm, in, &journal.KGContentDeltaObserved{Counts: map[string]int{"indexed": 1}}, true)
	journalKG("/repo", journal.CmdKGWarm, in, &journal.KGContentDeltaObserved{Counts: map[string]int{"indexed": 1}}, false)
	if len(*got) != 2 {
		t.Fatalf("want 2 events, got %d", len(*got))
	}
	if (*got)[0].EventType != journal.EventDurableDelta {
		t.Fatalf("ok=true should be durable_delta, got %q", (*got)[0].EventType)
	}
	if (*got)[1].EventType != journal.EventFailed {
		t.Fatalf("ok=false should be failed, got %q", (*got)[1].EventType)
	}
}

func TestJournalActor_IsMain(t *testing.T) {
	if journalActor() != journal.ActorMain {
		t.Fatalf("journalActor = %q, want main", journalActor())
	}
}

func TestSetDecisionGraphCounts(t *testing.T) {
	// nil status leaves the counts unset (omitted from the record).
	obs := &journal.KGDecisionObserved{}
	setDecisionGraphCounts(obs, nil)
	if obs.Nodes != nil || obs.Edges != nil || obs.Files != nil {
		t.Fatalf("nil status must leave counts unset, got %+v", obs)
	}
	setDecisionGraphCounts(obs, &graphstore.CRGStatus{Nodes: 3, Edges: 5, Files: 2})
	if obs.Nodes == nil || *obs.Nodes != 3 || obs.Edges == nil || *obs.Edges != 5 || obs.Files == nil || *obs.Files != 2 {
		t.Fatalf("counts not copied: %+v", obs)
	}
}

// ── per-command wiring: typed events from the real runners ─────────────────────

// TestRunKGIngest_RecordsCountsAndIDsNotBodies is the canonical "KG records
// counts/ids, never bodies" (D4) assertion: the ingest event carries note counts
// and ids but the marshaled envelope never contains the source body text.
func TestRunKGIngest_RecordsCountsAndIDsNotBodies(t *testing.T) {
	home, _ := curationKG(t)
	const secretBody = "We decided to use Cobra for the CLI surface."
	writeRawInbox(t, home, "design-doc", "Design Doc", secretBody+"\n- Should adopt Cobra commands\n")

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGIngest(testDeps(), newIngestCmd(true, false, "", "markdown"), nil); err != nil {
			t.Fatalf("runKGIngest: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGIngest)
	if e.EventType != journal.EventDurableDelta {
		t.Fatalf("event_type = %q, want durable_delta", e.EventType)
	}
	var obs journal.KGIngestObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.NotesCreated == 0 {
		t.Fatalf("expected notes_created > 0, got %+v", obs)
	}
	if len(obs.NoteIDs) == 0 {
		t.Fatalf("expected note_ids recorded, got %+v", obs)
	}
	// The note ids are recorded; the note BODY must never be.
	line, err := e.MarshalLine()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "CLI surface") {
		t.Fatalf("journal event leaked the note body:\n%s", line)
	}
	var in journal.KGIngestInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if !in.All {
		t.Fatalf("input should record --all, got %+v", in)
	}
}

func TestRunKGLinkAdd_RecordsCount(t *testing.T) {
	setupKGWithNotes(t)
	captureStdout(t, func() { _ = runKGWarm(newKGWarmCmdForTest(), nil) })

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGLinkAdd(testDeps(), newKGLinkAddCmdForTest("mentions"), []string{"dec-use-cobra", "commands::NewKGCmd"}); err != nil {
			t.Fatalf("runKGLinkAdd: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGLinkAdd)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["links_added"] != 1 || len(obs.IDs) != 1 {
		t.Fatalf("observed = %+v, want links_added=1 + one id", obs)
	}
	var in journal.KGContentDeltaInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.Operation != "link add" || len(in.Targets) != 2 {
		t.Fatalf("input = %+v", in)
	}
}

func TestRunKGLinkRemove_RecordsCount(t *testing.T) {
	home := setupKGWithNotes(t)
	captureStdout(t, func() { _ = runKGWarm(newKGWarmCmdForTest(), nil) })
	captureStdout(t, func() {
		if err := runKGLinkAdd(testDeps(), newKGLinkAddCmdForTest("mentions"), []string{"dec-use-cobra", "commands::NewKGCmd"}); err != nil {
			t.Fatalf("seed link: %v", err)
		}
	})
	store, _ := openKGStore(home)
	links, _ := store.GetLinksForNote("dec-use-cobra")
	store.Close()
	if len(links) != 1 {
		t.Fatalf("expected one seeded link, got %d", len(links))
	}
	linkID := strconv.FormatInt(links[0].ID, 10)

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGLinkRemove(testDeps(), &cobra.Command{}, []string{linkID}); err != nil {
			t.Fatalf("runKGLinkRemove: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGLinkRemove)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["links_removed"] != 1 || len(obs.IDs) != 1 || obs.IDs[0] != linkID {
		t.Fatalf("observed = %+v, want links_removed=1 + id %q", obs, linkID)
	}
}

func TestRunKGReweave_RecordsCounts(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	_ = createGraphNote(testIO(), home, &GraphNote{
		SchemaVersion: 1, ID: "dec-reweave", Type: "decision", Title: "T", Summary: "S",
		Status: "active", Links: []string{"does-not-exist"}, CreatedAt: now, UpdatedAt: now,
	}, "body")

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGReweave(testIO(), home); err != nil {
			t.Fatalf("runKGReweave: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGMaintainReweave)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["links_removed"] != 1 {
		t.Fatalf("observed = %+v, want links_removed=1", obs)
	}
	if len(obs.IDs) != 1 || obs.IDs[0] != "dec-reweave" {
		t.Fatalf("observed ids = %v, want [dec-reweave]", obs.IDs)
	}
}

func TestRunKGMarkStale_RecordsCounts(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	oldTime := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_ = createGraphNote(testIO(), home, &GraphNote{
		SchemaVersion: 1, ID: "ent-stale", Type: "entity", Title: "Old", Summary: "S",
		Status: "active", CreatedAt: oldTime, UpdatedAt: oldTime,
	}, "body")

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGMarkStale(testIO(), home, 90*24*time.Hour); err != nil {
			t.Fatalf("runKGMarkStale: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGMaintainStale)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["marked_stale"] != 1 || len(obs.IDs) != 1 || obs.IDs[0] != "ent-stale" {
		t.Fatalf("observed = %+v, want marked_stale=1 + [ent-stale]", obs)
	}
}

func TestRunKGCompact_RecordsCounts(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	_ = createGraphNote(testIO(), home, &GraphNote{
		SchemaVersion: 1, ID: "dec-archived", Type: "decision", Title: "Old", Summary: "S",
		Status: "archived", CreatedAt: now, UpdatedAt: now,
	}, "body")

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGCompact(testIO(), home); err != nil {
			t.Fatalf("runKGCompact: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGMaintainCompact)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["archived"] != 1 || len(obs.IDs) != 1 || obs.IDs[0] != "dec-archived" {
		t.Fatalf("observed = %+v, want archived=1 + [dec-archived]", obs)
	}
}

func TestRunKGWarm_RecordsCounts(t *testing.T) {
	setupKGWithNotes(t) // 5 notes
	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGWarm(newKGWarmCmdForTest(), nil); err != nil {
			t.Fatalf("runKGWarm: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGWarm)
	var obs journal.KGContentDeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Counts["indexed"] != 5 {
		t.Fatalf("observed = %+v, want indexed=5", obs)
	}
}

// TestRunKGUpdate_EmitsDecisionEvent proves a KG decision command records the
// outcome + graph counts (KGDecisionObserved) — never node/edge bodies (D4).
func TestRunKGUpdate_EmitsDecisionEvent(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	commitFile(t, repo, "a.txt", "one\n", "init")
	if err := os.WriteFile(repo+"/a.txt", []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(t, repo, "commit", "-am", "edit"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	writeFakeCRGBinary(t, repo, "exit 0")
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.txt", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "HEAD~1", "")
	cmd.Flags().Bool("skip-flows", true, "")
	cmd.Flags().Bool("skip-postprocess", true, "")
	cmd.Flags().Bool("json", false, "")

	got := captureJournal(t)
	captureStdout(t, func() {
		if err := runKGUpdate(cmd, nil); err != nil {
			t.Fatalf("runKGUpdate: %v", err)
		}
	})

	e := findEvent(t, *got, journal.CmdKGUpdate)
	if e.EventType != journal.EventDurableDelta {
		t.Fatalf("event_type = %q", e.EventType)
	}
	var obs journal.KGDecisionObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Outcome == "" {
		t.Fatalf("decision outcome empty: %+v", obs)
	}
	var in journal.KGDecisionInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.Repo != repo || in.Base != "HEAD~1" {
		t.Fatalf("input = %+v", in)
	}
}
