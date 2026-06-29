package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
)

// --- envelope.go -------------------------------------------------------------

func TestEnvelopeMarshalLineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		env  Envelope
	}{
		{
			name: "full durable delta",
			env: Envelope{
				Schema:    Schema,
				Version:   Version,
				TS:        "2026-06-28T00:00:00.000000001Z",
				Seq:       7,
				Actor:     ActorMain,
				Command:   "workflow advance",
				CwdRepo:   "abc123",
				EventType: EventDurableDelta,
				Input:     json.RawMessage(`{"task":"p1"}`),
				Observed:  json.RawMessage(`{"status":"completed"}`),
			},
		},
		{
			name: "input only no observed",
			env: Envelope{
				Schema:    Schema,
				Version:   Version,
				Actor:     ActorLoopWorker,
				Command:   "task update",
				EventType: EventInputOnly,
				Input:     json.RawMessage(`{"changed_fields":["notes"]}`),
			},
		},
		{
			name: "failed event",
			env: Envelope{
				Schema:    Schema,
				Version:   Version,
				Actor:     ActorOrchestrator,
				Command:   "commit",
				EventType: EventFailed,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, err := tc.env.MarshalLine()
			if err != nil {
				t.Fatalf("MarshalLine: %v", err)
			}
			if !bytes.HasSuffix(line, []byte("\n")) {
				t.Fatalf("line not newline-terminated: %q", line)
			}
			if n := bytes.Count(line, []byte("\n")); n != 1 {
				t.Fatalf("line has %d newlines, want exactly 1: %q", n, line)
			}
			var got Envelope
			if err := json.Unmarshal(line, &got); err != nil {
				t.Fatalf("unmarshal round-trip: %v", err)
			}
			if got.Schema != tc.env.Schema || got.Seq != tc.env.Seq ||
				got.Actor != tc.env.Actor || got.Command != tc.env.Command ||
				got.EventType != tc.env.EventType || got.CwdRepo != tc.env.CwdRepo {
				t.Fatalf("scalar drift after round-trip: got %+v want %+v", got, tc.env)
			}
			if !rawEqual(got.Input, tc.env.Input) {
				t.Fatalf("input drift: got %s want %s", got.Input, tc.env.Input)
			}
			if !rawEqual(got.Observed, tc.env.Observed) {
				t.Fatalf("observed drift: got %s want %s", got.Observed, tc.env.Observed)
			}
		})
	}
}

func TestEnvelopeMarshalLineError(t *testing.T) {
	// An invalid json.RawMessage makes the whole struct un-marshalable, so the
	// error branch of MarshalLine is exercised.
	bad := Envelope{Input: json.RawMessage("{not json")}
	if _, err := bad.MarshalLine(); err == nil {
		t.Fatal("MarshalLine on invalid RawMessage: want error, got nil")
	}
}

// --- identity.go -------------------------------------------------------------

func TestFingerprintStableAndDeterministic(t *testing.T) {
	restore := stubTrustedRepoID(t, "")
	defer restore()

	dir := t.TempDir()
	a := Fingerprint(dir)
	b := Fingerprint(dir)
	if a != b {
		t.Fatalf("Fingerprint not stable: %q vs %q", a, b)
	}
	if len(a) != fingerprintLen {
		t.Fatalf("fingerprint len = %d, want %d", len(a), fingerprintLen)
	}
	if other := Fingerprint(t.TempDir()); other == a {
		t.Fatal("distinct paths produced identical fingerprints")
	}
}

func TestFingerprintPrefersTrustedRepoID(t *testing.T) {
	restore := stubTrustedRepoID(t, "github.com/acme/repo")
	defer restore()

	// Two different paths that share the same trusted repo id must collapse to
	// one fingerprint (a repo is one journal regardless of checkout location).
	fpA := Fingerprint(t.TempDir())
	fpB := Fingerprint(t.TempDir())
	if fpA != fpB {
		t.Fatalf("same repo id gave different fingerprints: %q vs %q", fpA, fpB)
	}
	if want := hashKey("id:github.com/acme/repo"); fpA != want {
		t.Fatalf("fingerprint = %q, want id-hash %q", fpA, want)
	}
}

func TestFingerprintPathFallbackDiffersFromIDDomain(t *testing.T) {
	dir := t.TempDir()
	abs := absRepoPath(dir)
	withID := stubTrustedRepoID(t, abs) // id text equals the path text
	idFP := Fingerprint(dir)
	withID()

	pathFP := stubTrustedRepoID(t, "")
	fallback := Fingerprint(dir)
	pathFP()

	if idFP == fallback {
		t.Fatal("id-domain and path-domain hashes collided for identical text")
	}
	if want := hashKey("path:" + abs); fallback != want {
		t.Fatalf("fallback fingerprint = %q, want path-hash %q", fallback, want)
	}
}

func TestAbsRepoPathFallbackOnError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom") }
	defer func() { absFn = orig }()

	if got := absRepoPath("a/b/../b"); got != filepath.Clean("a/b/../b") {
		t.Fatalf("absRepoPath fallback = %q, want %q", got, filepath.Clean("a/b/../b"))
	}
}

func TestResolveIdentityWithStubs(t *testing.T) {
	rt := stubTrustedRepoID(t, "github.com/acme/repo")
	defer rt()
	origURL := originRemoteURL
	originRemoteURL = func(string) string { return "git@github.com:acme/repo.git" }
	defer func() { originRemoteURL = origURL }()

	dir := t.TempDir()
	id := ResolveIdentity(dir)
	if id.Fingerprint != Fingerprint(dir) {
		t.Errorf("Fingerprint = %q, want %q", id.Fingerprint, Fingerprint(dir))
	}
	if id.RepoID != "github.com/acme/repo" {
		t.Errorf("RepoID = %q", id.RepoID)
	}
	if id.RemoteURL != "git@github.com:acme/repo.git" {
		t.Errorf("RemoteURL = %q", id.RemoteURL)
	}
	if id.WorktreePath != absRepoPath(dir) {
		t.Errorf("WorktreePath = %q, want %q", id.WorktreePath, absRepoPath(dir))
	}
}

func TestResolveIdentityRealGitRepo(t *testing.T) {
	// No seam overrides: exercises the default trustedRepoID + originRemoteURL
	// closures against a real go-git checkout with an origin remote.
	dir := initRepoWithOrigin(t, "git@github.com:acme/repo.git")
	id := ResolveIdentity(dir)
	if id.RepoID != "github.com/acme/repo" {
		t.Errorf("RepoID = %q, want github.com/acme/repo", id.RepoID)
	}
	if id.RemoteURL != "git@github.com:acme/repo.git" {
		t.Errorf("RemoteURL = %q", id.RemoteURL)
	}
	if id.Fingerprint != hashKey("id:github.com/acme/repo") {
		t.Errorf("Fingerprint = %q, want id-hash", id.Fingerprint)
	}
}

func TestResolveIdentityAmbiguousOriginFallsBack(t *testing.T) {
	// No seam overrides: a divergent second remote makes the trusted repo id
	// ambiguous, so the default trustedRepoID closure returns "" and the
	// fingerprint falls back to the path domain.
	dir := initRepoWithOrigin(t, "git@github.com:fork/repo.git")
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "org",
		URLs: []string{"git@github.com:canonical/repo.git"},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}

	id := ResolveIdentity(dir)
	if id.RepoID != "" {
		t.Errorf("RepoID = %q, want empty under ambiguous origin", id.RepoID)
	}
	if id.Fingerprint != hashKey("path:"+absRepoPath(dir)) {
		t.Errorf("Fingerprint = %q, want path-domain fallback", id.Fingerprint)
	}
}

func TestResolveIdentityNonGitDir(t *testing.T) {
	// No seam overrides: the default closures take their error branches.
	dir := t.TempDir()
	id := ResolveIdentity(dir)
	if id.RepoID != "" {
		t.Errorf("RepoID = %q, want empty for non-git dir", id.RepoID)
	}
	if id.RemoteURL != "" {
		t.Errorf("RemoteURL = %q, want empty for non-git dir", id.RemoteURL)
	}
	if id.Fingerprint == "" || id.WorktreePath == "" {
		t.Errorf("path-derived fields must still populate: %+v", id)
	}
}

// --- append.go / Emit --------------------------------------------------------

func TestEmitCreatesDirAndAppendsLine(t *testing.T) {
	repo := newJournalRepo(t)
	if err := Emit(repo, Envelope{Actor: ActorMain, Command: "workflow advance", EventType: EventDurableDelta}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if fi, err := os.Stat(RepoDir(repo)); err != nil || !fi.IsDir() {
		t.Fatalf("RepoDir not created: %v", err)
	}
	got := readEvents(t, repo)
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	e := got[0]
	if e.Schema != Schema || e.Version != Version {
		t.Errorf("schema/version not stamped: %+v", e)
	}
	if e.TS == "" || e.Seq == 0 {
		t.Errorf("ts/seq not stamped: ts=%q seq=%d", e.TS, e.Seq)
	}
	if e.CwdRepo != Fingerprint(repo) {
		t.Errorf("CwdRepo = %q, want fingerprint %q", e.CwdRepo, Fingerprint(repo))
	}
}

func TestEmitAppendsNotTruncates(t *testing.T) {
	repo := newJournalRepo(t)
	for i := 0; i < 3; i++ {
		if err := Emit(repo, Envelope{Command: "commit", EventType: EventDurableDelta}); err != nil {
			t.Fatalf("Emit #%d: %v", i, err)
		}
	}
	if got := readEvents(t, repo); len(got) != 3 {
		t.Fatalf("want 3 appended events, got %d", len(got))
	}
}

func TestEmitMonotonicSeq(t *testing.T) {
	repo := newJournalRepo(t)
	for i := 0; i < 5; i++ {
		if err := Emit(repo, Envelope{Command: "checkpoint", EventType: EventDurableDelta}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	events := readEvents(t, repo)
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("seq not strictly increasing: %d then %d", events[i-1].Seq, events[i].Seq)
		}
	}
}

func TestEmitFailedDropsObserved(t *testing.T) {
	repo := newJournalRepo(t)
	err := Emit(repo, Envelope{
		Command:   "commit",
		EventType: EventFailed,
		Observed:  json.RawMessage(`{"should":"be dropped"}`),
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	e := readEvents(t, repo)[0]
	if e.Observed != nil {
		t.Fatalf("failed event kept observed: %s", e.Observed)
	}
}

func TestEmitPreservesCallerTSAndCwdRepo(t *testing.T) {
	repo := newJournalRepo(t)
	err := Emit(repo, Envelope{
		Command:   "fanout",
		EventType: EventInputOnly,
		TS:        "2000-01-01T00:00:00Z",
		CwdRepo:   "explicit-id",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	e := readEvents(t, repo)[0]
	if e.TS != "2000-01-01T00:00:00Z" {
		t.Errorf("TS overwritten: %q", e.TS)
	}
	if e.CwdRepo != "explicit-id" {
		t.Errorf("CwdRepo overwritten: %q", e.CwdRepo)
	}
}

func TestEmitConcurrentNoTornLines(t *testing.T) {
	repo := newJournalRepo(t)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Emit(repo, Envelope{Command: "workflow advance", EventType: EventDurableDelta})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Emit: %v", err)
		}
	}

	events := readEvents(t, repo) // readEvents fails on any malformed/torn line
	if len(events) != n {
		t.Fatalf("want %d intact lines, got %d", n, len(events))
	}
	seqs := map[int64]bool{}
	for _, e := range events {
		if e.Schema != Schema {
			t.Fatalf("line missing schema (torn?): %+v", e)
		}
		if seqs[e.Seq] {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seqs[e.Seq] = true
	}
}

func TestEmitMarshalError(t *testing.T) {
	repo := newJournalRepo(t)
	err := Emit(repo, Envelope{Command: "x", EventType: EventInputOnly, Input: json.RawMessage("{bad")})
	if err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("want marshal error, got %v", err)
	}
}

func TestEmitMkdirError(t *testing.T) {
	repo := newJournalRepo(t)
	// Plant a regular file where the journal root must become a directory, so
	// MkdirAll of the per-repo dir fails.
	if err := os.MkdirAll(stateDir(t), 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	journalRoot := filepath.Join(stateDir(t), "journal")
	if err := os.WriteFile(journalRoot, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant file: %v", err)
	}
	if err := Emit(repo, Envelope{Command: "x", EventType: EventInputOnly}); err == nil ||
		!strings.Contains(err.Error(), "create dir") {
		t.Fatalf("want create dir error, got %v", err)
	}
}

func TestAppendLineOpenError(t *testing.T) {
	repo := newJournalRepo(t)
	if err := os.MkdirAll(RepoDir(repo), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Make events.log a directory so O_WRONLY open fails.
	if err := os.Mkdir(EventsLogPath(repo), 0o700); err != nil {
		t.Fatalf("mkdir events.log: %v", err)
	}
	if err := Emit(repo, Envelope{Command: "x", EventType: EventInputOnly}); err == nil ||
		!strings.Contains(err.Error(), "open") {
		t.Fatalf("want open error, got %v", err)
	}
}

func TestAppendLineLockError(t *testing.T) {
	orig := acquireLock
	acquireLock = func(string) (func() error, error) { return nil, errors.New("lock boom") }
	defer func() { acquireLock = orig }()

	if err := appendLine("/nope/events.log", []byte("x\n")); err == nil ||
		!strings.Contains(err.Error(), "lock") {
		t.Fatalf("want lock error, got %v", err)
	}
}

func TestAppendLineReleaseErrorSurfaced(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.log")
	orig := acquireLock
	acquireLock = func(string) (func() error, error) {
		return func() error { return errors.New("release boom") }, nil
	}
	defer func() { acquireLock = orig }()

	// The write itself succeeds, so the deferred release error becomes the
	// returned error.
	err := appendLine(logPath, []byte("line\n"))
	if err == nil || !strings.Contains(err.Error(), "release lock") {
		t.Fatalf("want release error surfaced, got %v", err)
	}
}

func TestAppendLineWriteErrorSurfaced(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.log")
	orig := openFile
	openFile = func(name string, _ int, _ os.FileMode) (*os.File, error) {
		// Hand back a read-only handle so the subsequent Write fails.
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			return nil, err
		}
		return os.Open(name)
	}
	defer func() { openFile = orig }()

	if err := appendLine(logPath, []byte("line\n")); err == nil ||
		!strings.Contains(err.Error(), "append") {
		t.Fatalf("want write/append error, got %v", err)
	}
}

// --- helpers -----------------------------------------------------------------

// rawEqual compares two json.RawMessage values semantically (nil == empty).
func rawEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return bytes.Equal(a, b)
}

// stubTrustedRepoID overrides the trustedRepoID seam to return id and returns a
// restore func.
func stubTrustedRepoID(t *testing.T, id string) func() {
	t.Helper()
	orig := trustedRepoID
	trustedRepoID = func(string) string { return id }
	return func() { trustedRepoID = orig }
}

// stateDir returns the temp XDG state dir's dot-agents root for the current test.
func stateDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "dot-agents")
}

// newJournalRepo points the journal at a fresh temp XDG state dir and returns a
// non-git repo path to emit against.
func newJournalRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return t.TempDir()
}

// readEvents reads events.log for repo and decodes every line, failing on any
// malformed (torn) line so concurrency tests detect interleaving.
func readEvents(t *testing.T, repo string) []Envelope {
	t.Helper()
	data, err := os.ReadFile(EventsLogPath(repo))
	if err != nil {
		t.Fatalf("read events.log: %v", err)
	}
	var out []Envelope
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e Envelope
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("malformed journal line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// initRepoWithOrigin stands up a real go-git checkout with an origin remote.
func initRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{originURL},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	return dir
}
