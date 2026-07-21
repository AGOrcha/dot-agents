package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

var testNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func TestOutboxDrainDeletesOnlySuccessesAndQuarantinesFailures(t *testing.T) {
	project := t.TempDir()
	paths := []string{
		writeOutboxFixture(t, project, 1, nil),
		writeOutboxFixture(t, project, 2, nil),
		writeOutboxFixture(t, project, 3, nil),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope ingestEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(envelope.Events) != 3 {
			t.Errorf("event count = %d", len(envelope.Events))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":1,"deduped":0,"rejected":[{"index":1,"code":"invalid_event","message":"permanent","retryable":false},{"index":2,"code":"storage_unavailable","message":"d1 busy","retryable":true}]}`))
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{Explicit: true})
	if err == nil {
		t.Fatal("explicit sync should fail when files are retained or quarantined")
	}
	if report.Accepted != 1 || report.Deduped != 0 || report.Retained != 1 || report.Quarantined != 1 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("accepted file was not deleted: %v", err)
	}
	rejected := filepath.Join(filepath.Dir(paths[1]), "quarantine", filepath.Base(paths[1])+".rejected")
	if _, err := os.Stat(rejected); err != nil {
		t.Fatalf("rejected file not quarantined: %v", err)
	}
	if _, err := os.Stat(rejected + ".reason.json"); err != nil {
		t.Fatalf("rejection reason missing: %v", err)
	}
	retained, err := parseOutboxFile(paths[2], outboxID(3))
	if err != nil {
		t.Fatalf("retained file invalid after retry rewrite: %v", err)
	}
	if retained.Attempts != 1 || retained.LastError == nil || *retained.LastError != "d1 busy" {
		t.Fatalf("retry metadata = %#v", retained)
	}
}

func TestOutboxCorruptionIsQuarantinedAndOtherFilesContinue(t *testing.T) {
	project := t.TempDir()
	corrupt := writeOutboxFixture(t, project, 1, func(record *outboxRecord) {
		record.Event.SchemaHash = strings.Repeat("0", 64)
	})
	valid := writeOutboxFixture(t, project, 2, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accepted":1,"deduped":0,"rejected":[]}`))
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{Explicit: true})
	if err == nil {
		t.Fatal("quarantine must make explicit sync nonzero")
	}
	if report.Accepted != 1 || report.Quarantined != 1 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(valid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("valid file was not drained: %v", err)
	}
	quarantined := filepath.Join(filepath.Dir(corrupt), "quarantine", filepath.Base(corrupt)+".corrupt")
	if _, err := os.Stat(quarantined); err != nil {
		t.Fatalf("corrupt file not quarantined: %v", err)
	}
}

func TestOutboxRetentionPrunesOnlyExpiredQuarantineAndOrphanTemps(t *testing.T) {
	project := t.TempDir()
	ready := writeOutboxFixture(t, project, 1, nil)
	dir := outboxDir(project)
	orphan := filepath.Join(dir, ".019b2774-2a00-7a00-8000-000000000099.tmp")
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	quarantineDir := filepath.Join(dir, "quarantine")
	if err := os.MkdirAll(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	expired := filepath.Join(quarantineDir, "expired.corrupt")
	if err := os.WriteFile(expired, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := testNow.Add(-31 * 24 * time.Hour)
	for _, path := range []string{ready, orphan, expired} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	var report SyncReport
	if err := pruneOutbox(dir, testNow, &report); err != nil {
		t.Fatalf("pruneOutbox: %v", err)
	}
	if report.Pruned != 2 {
		t.Fatalf("pruned = %d, want orphan temp + expired quarantine", report.Pruned)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("valid pending file received an age-based expiry: %v", err)
	}
	for _, path := range []string{orphan, expired} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expired path still exists %s: %v", path, err)
		}
	}
}

func TestNonCanonicalReadyFilenameIsCorrupt(t *testing.T) {
	project := t.TempDir()
	dir := outboxDir(project)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "not-a-uuid.obs-v1.json"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var report SyncReport
	files, err := loadReadyFiles(dir, testNow, true, &report)
	if err != nil {
		t.Fatalf("loadReadyFiles: %v", err)
	}
	if len(files) != 0 || report.Quarantined != 1 {
		t.Fatalf("files=%d report=%#v", len(files), report)
	}
	if _, err := os.Stat(filepath.Join(dir, "quarantine", name+".corrupt")); err != nil {
		t.Fatalf("noncanonical ready file not quarantined: %v", err)
	}
}

func TestOutboxAuthFailureRetainsAllAndStops(t *testing.T) {
	project := t.TempDir()
	for i := 1; i <= 101; i++ {
		writeOutboxFixture(t, project, i, nil)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{Explicit: true})
	if err == nil {
		t.Fatal("401 must fail explicit sync")
	}
	if calls.Load() != 1 {
		t.Fatalf("requests after auth failure = %d, want 1", calls.Load())
	}
	if report.Retained != 101 {
		t.Fatalf("retained = %d, want 101", report.Retained)
	}
}

func TestOutboxBatchLimitAndFilenameOrder(t *testing.T) {
	project := t.TempDir()
	for i := 101; i >= 1; i-- {
		writeOutboxFixture(t, project, i, nil)
	}
	var batchSizes []int
	var iterations []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope ingestEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		batchSizes = append(batchSizes, len(envelope.Events))
		for _, event := range envelope.Events {
			iterations = append(iterations, event.Iteration)
		}
		_, _ = fmt.Fprintf(w, `{"accepted":%d,"deduped":0,"rejected":[]}`, len(envelope.Events))
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{Explicit: true})
	if err != nil {
		t.Fatalf("syncProject: %v (%#v)", err, report)
	}
	if fmt.Sprint(batchSizes) != "[100 1]" {
		t.Fatalf("batch sizes = %v", batchSizes)
	}
	for i, iteration := range iterations {
		if iteration != i+1 {
			t.Fatalf("iteration order at %d = %d", i, iteration)
		}
	}
}

func TestOutboxTransientFailureRewritesRetryAndBestEffortDoesNotError(t *testing.T) {
	project := t.TempDir()
	path := writeOutboxFixture(t, project, 1, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{})
	if err != nil {
		t.Fatalf("hook-driven sync returned error: %v", err)
	}
	if report.Retained != 1 {
		t.Fatalf("report = %#v", report)
	}
	record, err := parseOutboxFile(path, outboxID(1))
	if err != nil {
		t.Fatalf("parse retained file: %v", err)
	}
	if record.Attempts != 1 || record.NextAttemptAt != testNow.Format(time.RFC3339) {
		t.Fatalf("Retry-After: 0 did not take precedence: attempts=%d next=%s", record.Attempts, record.NextAttemptAt)
	}
}

func TestParsedResponseRequiredBeforeDeletion(t *testing.T) {
	project := t.TempDir()
	path := writeOutboxFixture(t, project, 1, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer server.Close()

	report, err := syncProject(context.Background(), project, testDeps(t, server), syncOptions{Explicit: true})
	if err == nil || report.Retained != 1 {
		t.Fatalf("invalid 200 report=%#v err=%v", report, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file deleted without complete parsed 200: %v", err)
	}
}

func TestComputeEventHashAndEnvelopeBuild(t *testing.T) {
	event := fixtureEvent(7)
	hash, err := computeEventHash(event)
	if err != nil {
		t.Fatalf("computeEventHash: %v", err)
	}
	if len(hash) != 64 || hash == strings.Repeat("0", 64) {
		t.Fatalf("hash = %q", hash)
	}
	event.SchemaHash = hash
	batch := []queuedFile{{record: outboxRecord{ProjectID: "github.com/AGOrcha/dot-agents", Client: fixtureClient(), Event: event}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got ingestEnvelope
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if got.SchemaVersion != 1 || got.ProjectID != "github.com/AGOrcha/dot-agents" || len(got.Events) != 1 || got.Events[0].SchemaHash != hash {
			t.Fatalf("envelope = %#v", got)
		}
		_, _ = w.Write([]byte(`{"accepted":1,"deduped":0,"rejected":[]}`))
	}))
	defer server.Close()
	if _, _, status, err := postBatch(context.Background(), server.Client(), server.URL, nil, batch); err != nil || status != 200 {
		t.Fatalf("postBatch status=%d err=%v", status, err)
	}
}

func testDeps(t *testing.T, server *httptest.Server) Deps {
	t.Helper()
	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	return (Deps{
		Version: "test",
		loadConfig: func(string) (*cfg.AgentsRC, error) {
			return configuredRC(server.URL, false), nil
		},
		newResolver: func() credentialResolver { return &countingResolver{err: errors.New("must not resolve")} },
		httpClient:  server.Client(),
		now:         func() time.Time { return testNow },
	}).withDefaults()
}

func writeOutboxFixture(t *testing.T, project string, sequence int, mutate func(*outboxRecord)) string {
	t.Helper()
	dir := outboxDir(project)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := outboxID(sequence)
	event := fixtureEvent(sequence)
	hash, err := computeEventHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.SchemaHash = hash
	record := outboxRecord{
		OutboxVersion: 1,
		ID:            id,
		QueuedAt:      testNow.Format(time.RFC3339),
		Attempts:      0,
		NextAttemptAt: testNow.Format(time.RFC3339),
		ProjectID:     "github.com/AGOrcha/dot-agents",
		Client:        fixtureClient(),
		Event:         event,
	}
	if mutate != nil {
		mutate(&record)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".obs-v1.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func outboxID(sequence int) string {
	return fmt.Sprintf("019b2774-2a00-7a00-8000-%012x", sequence)
}

func fixtureClient() clientInfo {
	return clientInfo{DAVersion: "test", HostOS: "darwin", AgentRuntime: "claude-code"}
}

func fixtureEvent(iteration int) ingestEvent {
	payload := json.RawMessage(fmt.Sprintf(`{"schema_version":2,"iteration":%d,"wave":"plan","task_id":"task","checkpoint_at":"2026-07-20T11:59:00Z"}`, iteration))
	return ingestEvent{
		Kind:         "iteration.checkpointed",
		OccurredAt:   "2026-07-20T11:59:00Z",
		PlanID:       "plan",
		TaskID:       "task",
		Iteration:    iteration,
		Payload:      payload,
		ScoreSidecar: json.RawMessage("null"),
	}
}

func TestCanonicalJSONRFC8785ThresholdsAndOrdering(t *testing.T) {
	raw := []byte(`{"\ud83d\ude00":1,"\ufffd":2,"small":0.000001,"tiny":0.0000001,"large":1e21,"negativeZero":-0}`)
	got, err := canonicalJSON(raw)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	want := `{"large":1e+21,"negativeZero":0,"small":0.000001,"tiny":1e-7,"😀":1,"�":2}`
	// Keys are UTF-16 sorted, so the supplementary character sorts before U+FFFD.
	if string(got) != want {
		t.Fatalf("canonical JSON:\n got %s\nwant %s", got, want)
	}
}
