package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// restoreSeams snapshots the package filesystem/clock/marshal seams and returns
// a function that restores them, so a test that injects a fault cannot leak into
// the next test.
func restoreSeams(t *testing.T) {
	t.Helper()
	origRead, origStat, origRename, origNow := readFile, statFunc, renameFunc, timeNow
	origAppend, origMarshal, origOpen := appendLine, marshal, openAppend
	origLock, origHead := acquireFileLock, writeHeadFile
	t.Cleanup(func() {
		readFile, statFunc, renameFunc, timeNow = origRead, origStat, origRename, origNow
		appendLine, marshal, openAppend = origAppend, origMarshal, origOpen
		acquireFileLock, writeHeadFile = origLock, origHead
	})
}

func tempLog(t *testing.T) *Log {
	t.Helper()
	return Open(filepath.Join(t.TempDir(), "review", DefaultLogName))
}

// sampleEvent returns a valid label-submit event stamped at ts.
func sampleEvent(ts time.Time) Event {
	return Event{
		Actor:     "reviewer@example.com",
		Role:      "reviewer",
		Action:    ActionLabelSubmit,
		Target:    "iteration/7",
		AfterHash: "deadbeef",
		RequestID: "req-1",
		Now:       ts,
	}
}

// failFile is an appendFile whose Write and/or Close fail on demand.
type failFile struct {
	writeErr error
	closeErr error
	closed   bool
}

func (f *failFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *failFile) Close() error {
	f.closed = true
	return f.closeErr
}

func TestAppendUsesRealClockDefault(t *testing.T) {
	restoreSeams(t)
	// Do not override timeNow: exercise its default body.
	before := time.Now().UTC().Add(-time.Second)
	l := tempLog(t)
	r, err := l.Append(sampleEvent(time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Ts.Before(before) || r.Ts.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("default clock ts out of range: %v", r.Ts)
	}
}

func TestWriteLineOpenError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("open")
	openAppend = func(string) (appendFile, error) { return nil, boom }
	if err := writeLine("x", []byte("l")); !errors.Is(err, boom) {
		t.Fatalf("got %v, want open", err)
	}
}

func TestWriteLineWriteError(t *testing.T) {
	restoreSeams(t)
	ff := &failFile{writeErr: errors.New("wr")}
	openAppend = func(string) (appendFile, error) { return ff, nil }
	if err := writeLine("x", []byte("l")); err == nil || !strings.Contains(err.Error(), "write log") {
		t.Fatalf("got %v, want write log", err)
	}
	if !ff.closed {
		t.Error("file must be closed after a write error")
	}
}

func TestWriteLineCloseError(t *testing.T) {
	restoreSeams(t)
	openAppend = func(string) (appendFile, error) { return &failFile{closeErr: errors.New("cl")}, nil }
	if err := writeLine("x", []byte("l")); err == nil || !strings.Contains(err.Error(), "close log") {
		t.Fatalf("got %v, want close log", err)
	}
}

func TestOpenAppendMkdirError(t *testing.T) {
	restoreSeams(t)
	// Parent path element is a regular file, so MkdirAll under it fails.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openAppend(filepath.Join(blocker, "sub", DefaultLogName)); err == nil {
		t.Fatal("expected mkdir error under a file")
	}
}

func TestOpenAppendOpenError(t *testing.T) {
	restoreSeams(t)
	// Path is an existing directory: OpenFile for writing fails.
	dir := t.TempDir()
	if _, err := openAppend(dir); err == nil {
		t.Fatal("expected open error on a directory path")
	}
}

func TestOpenAppendRoundTrip(t *testing.T) {
	restoreSeams(t)
	// Exercise the real openAppend success path and confirm append semantics.
	path := filepath.Join(t.TempDir(), "review", DefaultLogName)
	if err := writeLine(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeLine(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\ntwo\n" {
		t.Fatalf("append semantics broken: %q", data)
	}
}

func TestActionValid(t *testing.T) {
	for _, a := range []Action{
		ActionLabelSubmit, ActionLabelEdit, ActionUserCreate,
		ActionUserDelete, ActionRoleChange,
	} {
		if !a.Valid() {
			t.Errorf("expected %q valid", a)
		}
	}
	if Action("bogus").Valid() {
		t.Error("bogus action should be invalid")
	}
}

func TestEventValidate(t *testing.T) {
	base := sampleEvent(time.Now())
	tests := []struct {
		name string
		mut  func(e *Event)
		want error
	}{
		{"ok", func(*Event) {}, nil},
		{"empty actor", func(e *Event) { e.Actor = "  " }, ErrEmptyActor},
		{"empty target", func(e *Event) { e.Target = "" }, ErrEmptyTarget},
		{"bad action", func(e *Event) { e.Action = "nope" }, ErrInvalidAction},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.mut(&e)
			err := e.validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAppendCreatesGenesisAndChains(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	ts := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)

	r1, err := l.Append(sampleEvent(ts))
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if r1.PrevHash != GenesisPrevHash {
		t.Errorf("first record prev_hash = %q, want genesis", r1.PrevHash)
	}
	if r1.SchemaVersion != SchemaVersion {
		t.Errorf("schema version = %q, want %q", r1.SchemaVersion, SchemaVersion)
	}

	e2 := sampleEvent(ts.Add(time.Minute))
	e2.Action = ActionLabelEdit
	r2, err := l.Append(e2)
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	// File is exactly two JSON lines, append-only (record 1 unchanged), and
	// record 2's prev_hash is the SHA-256 of record 1's exact stored bytes.
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if wantPrev := hashBytes([]byte(lines[0])); r2.PrevHash != wantPrev {
		t.Errorf("second record prev_hash = %q, want raw-line hash %q", r2.PrevHash, wantPrev)
	}
	var got Record
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.PrevHash != GenesisPrevHash || got.Action != ActionLabelSubmit {
		t.Errorf("line 1 mutated: %+v", got)
	}
}

func TestAppendDefaultsTimestamp(t *testing.T) {
	restoreSeams(t)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	timeNow = func() time.Time { return fixed }
	l := tempLog(t)
	e := sampleEvent(time.Time{}) // zero Now -> timeNow()
	r, err := l.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Ts.Equal(fixed) {
		t.Errorf("ts = %v, want %v", r.Ts, fixed)
	}
}

func TestAppendValidationError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	_, err := l.Append(Event{Action: ActionLabelSubmit, Target: "x"})
	if !errors.Is(err, ErrEmptyActor) {
		t.Fatalf("got %v, want ErrEmptyActor", err)
	}
	if _, statErr := os.Stat(l.Path()); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("invalid event must not create the log file")
	}
}

func TestAppendWriteError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("boom")
	appendLine = func(string, []byte) error { return boom }
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
}

func TestAppendReadError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("read boom")
	readFile = func(string) ([]byte, error) { return nil, boom }
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); !errors.Is(err, boom) {
		t.Fatalf("got %v, want read boom", err)
	}
}

func TestAppendGenesisMarshalError(t *testing.T) {
	restoreSeams(t)
	marshal = func(any) ([]byte, error) { return nil, errors.New("nope") }
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); !errors.Is(err, ErrMarshal) {
		t.Fatalf("got %v, want ErrMarshal", err)
	}
}

func TestAppendMarshalErrorWithPriorRecords(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	// Chaining onto a prior record hashes its raw stored bytes (infallible);
	// the only marshal in Append is for the NEW record's line.
	marshal = func(any) ([]byte, error) { return nil, errors.New("nope") }
	if _, err := l.Append(sampleEvent(time.Now().UTC())); !errors.Is(err, ErrMarshal) {
		t.Fatalf("got %v, want ErrMarshal", err)
	}
}

func TestRecordsMissingFile(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	recs, err := l.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 records, got %d", len(recs))
	}
}

func TestRecordsReadError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("io")
	readFile = func(string) ([]byte, error) { return nil, boom }
	if _, err := tempLog(t).Records(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want io", err)
	}
}

func TestRecordsMalformedLine(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if err := appendLine(l.Path(), []byte(`{"actor":"a",`)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Records(); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line 1 parse error, got %v", err)
	}
}

func TestRecordsSkipsBlankLines(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	// Append a stray blank line; it must be ignored, not parsed.
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n   \n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	recs, err := l.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	var r Record
	if err := decode([]byte(`{"actor":"a","surprise":1}`), &r); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestDecodeRejectsTrailingData(t *testing.T) {
	var r Record
	for _, raw := range []string{
		`{"actor":"a"}{"garbage":true}`, // second object on the line
		`{"actor":"a"}junk`,             // raw bytes after the object
		`{"actor":"a"} x`,               // interior space then junk
	} {
		if err := decode([]byte(raw), &r); !errors.Is(err, ErrTrailingData) {
			t.Errorf("decode(%q) = %v, want ErrTrailingData", raw, err)
		}
	}
	// A clean object still decodes.
	if err := decode([]byte(`{"actor":"a"}`), &r); err != nil {
		t.Errorf("clean line rejected: %v", err)
	}
}

func TestVerifyDetectsTrailingJunkTamper(t *testing.T) {
	// Tamper-evidence hole: junk appended after a mid-chain record's JSON on
	// the SAME line changes the file, but the chain hashes canonical
	// re-marshaled bytes, so neither parsing nor Verify caught it before
	// decode enforced full-line consumption.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	lines[1] += `{"garbage":true}`
	writeLogLines(t, l, lines)

	if _, err := l.Records(); !errors.Is(err, ErrTrailingData) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("Records() must reject trailing junk on line 2, got %v", err)
	}
	if _, err := l.Verify(); !errors.Is(err, ErrTrailingData) {
		t.Fatalf("Verify() must fail on trailing junk, got %v", err)
	}
}

func TestVerifyEmptyAndValidChain(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Count != 0 {
		t.Fatalf("empty log: %+v", res)
	}

	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := l.Append(sampleEvent(ts.Add(time.Duration(i) * time.Minute))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	res, err = l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Count != 5 || res.BrokenAt != 0 {
		t.Fatalf("valid chain: %+v", res)
	}
}

// seedLog appends n valid records stamped in the same year and returns the log.
func seedLog(t *testing.T, l *Log, n int) {
	t.Helper()
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		if _, err := l.Append(sampleEvent(ts.Add(time.Duration(i) * time.Minute))); err != nil {
			t.Fatalf("seed append %d: %v", i, err)
		}
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	// A MIDDLE (non-tail) record: the chain stage catches it because the
	// altered record's hash no longer matches its successor's prev_hash. Tail
	// records are covered separately by the head-anchor tests below.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 5)
	// Flip content in record 3 (index 2): rewrite its actor. The line stays
	// valid JSON, so parsing succeeds but the chain hash no longer matches.
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var r Record
	if err := json.Unmarshal([]byte(lines[2]), &r); err != nil {
		t.Fatal(err)
	}
	r.Actor = "attacker@example.com"
	tampered, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	lines[2] = string(tampered)
	if err := os.WriteFile(l.Path(), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("verify should fail on tampered log")
	}
	// Record 3's altered content changes its hash, so the stored prev_hash in
	// record 4 no longer matches: the break surfaces at record 4 and the reason
	// names record 3 as altered.
	if res.BrokenAt != 4 {
		t.Errorf("BrokenAt = %d, want 4", res.BrokenAt)
	}
	if !strings.Contains(res.Reason, "record 3") {
		t.Errorf("reason should implicate record 3: %q", res.Reason)
	}
}

// readLogLines returns the active log's non-empty lines.
func readLogLines(t *testing.T, l *Log) []string {
	t.Helper()
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// writeLogLines overwrites the active log with the given lines (head untouched),
// simulating an out-of-band editor.
func writeLogLines(t *testing.T, l *Log, lines []string) {
	t.Helper()
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(l.Path(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAppendKeepsChainIntact(t *testing.T) {
	// The core correctness fix: with read-prev+append serialized under the
	// in-process mutex AND the inter-process file lock, N concurrent appenders
	// cannot fork the chain. Run under `-race` for it to be meaningful.
	restoreSeams(t)
	l := tempLog(t)
	const n = 25
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := l.Append(sampleEvent(base.Add(time.Duration(i) * time.Second))); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}
	recs, err := l.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != n {
		t.Fatalf("want %d records after concurrent appends, got %d", n, len(recs))
	}
	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("chain broke under concurrent appends: %+v", res)
	}
}

func TestAppendLockError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("lock")
	acquireFileLock = func(string) (func() error, error) { return nil, boom }
	if _, err := tempLog(t).Append(sampleEvent(time.Now().UTC())); !errors.Is(err, boom) {
		t.Fatalf("got %v, want lock", err)
	}
}

func TestAppendReleasesLock(t *testing.T) {
	restoreSeams(t)
	released := false
	acquireFileLock = func(string) (func() error, error) {
		return func() error { released = true; return nil }, nil
	}
	if _, err := tempLog(t).Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Error("Append must release the file lock")
	}
}

func TestAppendWriteHeadError(t *testing.T) {
	// A failed head write AFTER the record landed = torn append. The error
	// surfaces, but the state must be benign (at-least-once semantics): the
	// record is durable, Verify flags TornAppend rather than tamper, and the
	// next successful Append heals the anchor.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 2) // healthy anchored prefix
	origHead := writeHeadFile
	boom := errors.New("head")
	writeHeadFile = func(string, []byte) error { return boom }
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err == nil || !strings.Contains(err.Error(), "write head") {
		t.Fatalf("got %v, want write head", err)
	}

	// The record already committed; the anchor is one behind.
	recs, err := l.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("record must be durable despite head failure: got %d records", len(recs))
	}
	head, ok, err := l.readHead()
	if err != nil || !ok || head.Count != 2 {
		t.Fatalf("head should be stale at 2: %+v ok=%v err=%v", head, ok, err)
	}

	// Verify: torn append, NOT tamper.
	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.TornAppend {
		t.Fatalf("want OK+TornAppend, got %+v", res)
	}
	if !strings.Contains(res.Reason, "interrupted") {
		t.Errorf("reason should explain the interruption: %q", res.Reason)
	}

	// The next successful Append heals the anchor.
	writeHeadFile = origHead
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	res, err = l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.TornAppend {
		t.Fatalf("append should heal the torn head, got %+v", res)
	}
	if res.Count != 4 {
		t.Fatalf("want 4 records post-heal, got %d", res.Count)
	}
}

func TestRepairHeadHealsTornAppend(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 2)
	origHead := writeHeadFile
	boom := errors.New("head")
	writeHeadFile = func(string, []byte) error { return boom }
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err == nil {
		t.Fatal("expected torn append")
	}
	writeHeadFile = origHead

	res, err := l.RepairHead()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.TornAppend || res.Count != 3 {
		t.Fatalf("repair should yield clean state: %+v", res)
	}
	res, err = l.Verify()
	if err != nil || !res.OK || res.TornAppend {
		t.Fatalf("post-repair verify: %+v %v", res, err)
	}
}

func TestRepairHeadTornFirstAppend(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	origHead := writeHeadFile
	boom := errors.New("head")
	writeHeadFile = func(string, []byte) error { return boom }
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err == nil {
		t.Fatal("expected torn first append")
	}
	writeHeadFile = origHead

	// One record, no anchor at all: still the torn shape.
	res, err := l.Verify()
	if err != nil || !res.OK || !res.TornAppend {
		t.Fatalf("torn first append not flagged: %+v %v", res, err)
	}
	res, err = l.RepairHead()
	if err != nil || !res.OK || res.TornAppend || res.Count != 1 {
		t.Fatalf("repair of torn first append: %+v %v", res, err)
	}
}

func TestRepairHeadNoopWhenClean(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 2)
	res, err := l.RepairHead()
	if err != nil || !res.OK || res.TornAppend {
		t.Fatalf("clean log repair should be a no-op: %+v %v", res, err)
	}
}

func TestRepairHeadRefusesTamper(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	writeLogLines(t, l, lines[:1]) // drop two records: NOT the torn shape

	res, err := l.RepairHead()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatalf("repair must not paper over tamper: %+v", res)
	}
	// The stale (tampered-state) anchor must be untouched.
	head, ok, err := l.readHead()
	if err != nil || !ok || head.Count != 3 {
		t.Fatalf("anchor must be unchanged after refused repair: %+v ok=%v err=%v", head, ok, err)
	}
}

func TestRepairHeadLockError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("lock")
	acquireFileLock = func(string) (func() error, error) { return nil, boom }
	if _, err := tempLog(t).RepairHead(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want lock", err)
	}
}

func TestRepairHeadVerifyError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("io")
	readFile = func(string) ([]byte, error) { return nil, boom }
	if _, err := tempLog(t).RepairHead(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want io", err)
	}
}

func TestRepairHeadWriteError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	boom := errors.New("head")
	writeHeadFile = func(string, []byte) error { return boom }
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err == nil {
		t.Fatal("expected torn append")
	}
	// Keep the head write failing: repair itself hits the write-error branch.
	if _, err := l.RepairHead(); err == nil || !strings.Contains(err.Error(), "write head") {
		t.Fatalf("got %v, want write head", err)
	}
}

func TestAppendWritesHeadAnchor(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	head, ok, err := l.readHead()
	if err != nil || !ok {
		t.Fatalf("head anchor missing after append: ok=%v err=%v", ok, err)
	}
	if head.Count != 1 || len(head.TailHash) != 64 {
		t.Fatalf("unexpected head anchor: %+v", head)
	}
}

func TestVerifyDetectsTailModification(t *testing.T) {
	// The gap the cross-brain gate flagged: modifying the LAST record passed the
	// old chain-only Verify. The head anchor now catches it.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	var last Record
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil {
		t.Fatal(err)
	}
	last.Actor = "attacker@example.com" // prev_hash unchanged -> chain still passes
	tampered, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	lines[2] = string(tampered)
	writeLogLines(t, l, lines)

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.BrokenAt != 3 {
		t.Fatalf("tail modification not caught: %+v", res)
	}
	if !strings.Contains(res.Reason, "modified") {
		t.Errorf("reason should mention modification: %q", res.Reason)
	}
}

func TestVerifyDetectsTailTruncation(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	writeLogLines(t, l, lines[:2]) // drop the last record; head still says count=3

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Reason, "count") {
		t.Fatalf("tail truncation not caught: %+v", res)
	}
}

func TestVerifyDetectsForgedAppend(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	// Chain onto the exact stored bytes of line 3, as an attacker would.
	prev := hashBytes([]byte(readLogLines(t, l)[2]))
	// A correctly-chained forged record: the chain accepts it, but the head
	// anchor (still count=3) does not.
	forged := Record{
		SchemaVersion: SchemaVersion,
		Ts:            time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
		Actor:         "attacker@example.com",
		Role:          "admin",
		Action:        ActionRoleChange,
		Target:        "user/victim",
		PrevHash:      prev,
	}
	line, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	lines := append(readLogLines(t, l), string(line))
	writeLogLines(t, l, lines)

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	// A single correctly-chained out-of-band record is byte-for-byte
	// indistinguishable from an interrupted append, so it is surfaced as
	// TornAppend for operator review rather than silently accepted — and
	// rather than hard-failed, which would false-alarm on every real torn
	// append. TWO or more forged records do hard-fail (see the head-anchor
	// table test).
	if !res.TornAppend || res.Reason == "" {
		t.Fatalf("forged single append must surface as TornAppend for review: %+v", res)
	}
}

func TestVerifyHeadMissing(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 2)
	if err := os.Remove(headPathFor(l.Path())); err != nil {
		t.Fatal(err)
	}
	// Two records with no anchor at all is NOT the one-record-ahead torn
	// shape — it stays a hard failure.
	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.TornAppend || !strings.Contains(res.Reason, "does not match") {
		t.Fatalf("removed head anchor not caught: %+v", res)
	}
}

func TestVerifyHeadOrphaned(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 1)
	writeLogLines(t, l, nil) // empty the log but leave the head
	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Reason, "no records") {
		t.Fatalf("orphaned head not caught: %+v", res)
	}
}

func TestVerifyHeadParseError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 1)
	if err := os.WriteFile(headPathFor(l.Path()), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Verify(); err == nil || !strings.Contains(err.Error(), "parse head") {
		t.Fatalf("got %v, want parse head", err)
	}
}

func TestVerifyHeadReadError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 1)
	boom := errors.New("head io")
	readFile = func(p string) ([]byte, error) {
		if strings.HasSuffix(p, headSuffix) {
			return nil, boom
		}
		return os.ReadFile(p)
	}
	if _, err := l.Verify(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want head io", err)
	}
}

func TestVerifyHeadAnchorDirect(t *testing.T) {
	// verifyHeadAnchor attests raw stored line bytes; the table drives it with
	// synthetic lines directly.
	lineA := []byte(`{"line":"a"}`)
	lineB := []byte(`{"line":"b"}`)
	hashA := hashBytes(lineA)
	tests := []struct {
		name     string
		raws     [][]byte
		head     headAnchor
		hasHead  bool
		wantOK   bool
		wantTorn bool
	}{
		{"empty no head", nil, headAnchor{}, false, true, false},
		{"empty with head", nil, headAnchor{Count: 1, TailHash: "x"}, true, false, false},
		{"one record no head is torn first append", [][]byte{lineA}, headAnchor{}, false, true, true},
		{"two records no head is tamper", [][]byte{lineA, lineB}, headAnchor{}, false, false, false},
		{"head ahead of log", [][]byte{lineA}, headAnchor{Count: 2, TailHash: hashA}, true, false, false},
		{"tail hash mismatch", [][]byte{lineA}, headAnchor{Count: 1, TailHash: "nope"}, true, false, false},
		{"all good", [][]byte{lineA}, headAnchor{Count: 1, TailHash: hashA}, true, true, false},
		{"one clean ahead is torn", [][]byte{lineA, lineB}, headAnchor{Count: 1, TailHash: hashA}, true, true, true},
		{"one ahead but anchored record modified", [][]byte{lineA, lineB}, headAnchor{Count: 1, TailHash: "wrong"}, true, false, false},
		{"two ahead is tamper", [][]byte{lineA, lineB}, headAnchor{Count: 0, TailHash: ""}, true, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := verifyHeadAnchor(tc.raws, tc.head, tc.hasHead)
			if res.OK != tc.wantOK || res.TornAppend != tc.wantTorn {
				t.Fatalf("OK=%v torn=%v, want OK=%v torn=%v (%+v)",
					res.OK, res.TornAppend, tc.wantOK, tc.wantTorn, res)
			}
		})
	}
}

func TestRotateHeadMovesAnchor(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(sampleEvent(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	// The 2025 chain's head anchor followed it into the archive.
	if _, err := os.Stat(archiveBase(l.Path()) + ".2025.jsonl" + headSuffix); err != nil {
		t.Fatalf("archived head anchor missing: %v", err)
	}
	// The active file's fresh chain re-verifies with its own anchor.
	res, err := l.Verify()
	if err != nil || !res.OK {
		t.Fatalf("post-rotation verify: %+v %v", res, err)
	}
}

func TestRotateHeadNoHeadIsNoop(t *testing.T) {
	restoreSeams(t)
	if err := tempLog(t).rotateHead("/some/dest.jsonl"); err != nil {
		t.Fatalf("no-head rotate should be a no-op, got %v", err)
	}
}

func TestRotateHeadStatError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("stat")
	statFunc = func(string) (os.FileInfo, error) { return nil, boom }
	if err := tempLog(t).rotateHead("/dest.jsonl"); !errors.Is(err, boom) {
		t.Fatalf("got %v, want stat", err)
	}
}

func TestRotateHeadRenameError(t *testing.T) {
	restoreSeams(t)
	statFunc = func(string) (os.FileInfo, error) { return nil, nil } // head "exists"
	boom := errors.New("rename")
	renameFunc = func(string, string) error { return boom }
	if err := tempLog(t).rotateHead("/dest.jsonl"); !errors.Is(err, boom) {
		t.Fatalf("got %v, want rename", err)
	}
}

func TestMaybeRotateHeadRenameError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	statFunc = func(p string) (os.FileInfo, error) {
		if strings.HasSuffix(p, headSuffix) {
			return nil, nil // head sidecar "exists"
		}
		return nil, os.ErrNotExist // archive slot is free
	}
	boom := errors.New("head rename")
	renameFunc = func(src, _ string) error {
		if strings.HasSuffix(src, headSuffix) {
			return boom // the log rename succeeds; the head rename fails
		}
		return nil
	}
	recs := []Record{{Ts: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}}
	if _, err := l.maybeRotate(recs, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, boom) {
		t.Fatalf("got %v, want head rename", err)
	}
}

func TestHeadPathFor(t *testing.T) {
	if got := headPathFor("/x/audit.log.jsonl"); got != "/x/audit.log.jsonl.head" {
		t.Errorf("headPathFor = %q", got)
	}
}

func TestVerifyGenesisBreak(t *testing.T) {
	recs := []Record{{PrevHash: "not-genesis", Action: ActionLabelSubmit}}
	res := verifyRecords(recs, [][]byte{[]byte(`{"x":1}`)})
	if res.OK || res.BrokenAt != 1 {
		t.Fatalf("expected genesis break at 1: %+v", res)
	}
	if !strings.Contains(res.Reason, "genesis") {
		t.Errorf("reason %q should mention genesis", res.Reason)
	}
}

func TestVerifyReadError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("io")
	readFile = func(string) ([]byte, error) { return nil, boom }
	if _, err := tempLog(t).Verify(); !errors.Is(err, boom) {
		t.Fatalf("got %v, want io", err)
	}
}

// semanticRewrite re-serializes a stored JSON line via a map round-trip: Go
// maps marshal keys alphabetically while the Record struct writes them in
// declaration order, so the output has IDENTICAL semantics but different
// bytes. Both properties are asserted (byte-different, decode-equal) so the
// fixture proves what the raw-bytes attestation tests rely on.
func semanticRewrite(t *testing.T, line string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) == line {
		t.Fatal("rewrite produced identical bytes; fixture cannot prove anything")
	}
	var orig, rew Record
	if err := decode([]byte(line), &orig); err != nil {
		t.Fatal(err)
	}
	if err := decode(out, &rew); err != nil {
		t.Fatalf("rewritten line must still decode: %v", err)
	}
	if orig != rew {
		t.Fatal("fixture broke semantics; test invalid")
	}
	return string(out)
}

func TestVerifyDetectsSemanticRewriteMidChain(t *testing.T) {
	// The raw-bytes attestation guarantee: rewriting a stored line with
	// semantically-identical but byte-different JSON must break verification,
	// because the chain attests the exact stored bytes, not the JSON meaning.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	lines[1] = semanticRewrite(t, lines[1])
	writeLogLines(t, l, lines)

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.BrokenAt != 3 {
		t.Fatalf("semantic rewrite of record 2 not caught: %+v", res)
	}
	if !strings.Contains(res.Reason, "record 2") {
		t.Errorf("reason should implicate record 2: %q", res.Reason)
	}
}

func TestVerifyDetectsSemanticRewriteTail(t *testing.T) {
	// Same guarantee at the tail: the head anchor attests the tail line's
	// exact bytes, so a semantics-preserving rewrite of the last record fails.
	restoreSeams(t)
	l := tempLog(t)
	seedLog(t, l, 3)
	lines := readLogLines(t, l)
	lines[2] = semanticRewrite(t, lines[2])
	writeLogLines(t, l, lines)

	res, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Reason, "modified") {
		t.Fatalf("semantic rewrite of the tail not caught: %+v", res)
	}
}

func TestYearlyRotation(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	// Appending in the next year rotates the 2025 file to a dated archive and
	// starts a fresh genesis chain.
	r, err := l.Append(sampleEvent(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if r.PrevHash != GenesisPrevHash {
		t.Errorf("post-rotation record should be genesis, got prev %q", r.PrevHash)
	}
	archive := archiveBase(l.Path()) + ".2025.jsonl"
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("expected archive %s: %v", archive, err)
	}
	recs, err := l.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("active file should hold 1 record post-rotation, got %d", len(recs))
	}
	res, err := l.Verify()
	if err != nil || !res.OK {
		t.Fatalf("post-rotation verify: %+v %v", res, err)
	}
}

func TestSizeRotationAndArchiveCollision(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t).WithSizeCap(1) // any existing byte trips the cap
	yr := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.Append(sampleEvent(yr)); err != nil {
		t.Fatal(err)
	}
	// Second append: same year but file exceeds the 1-byte cap -> size rotation
	// into audit.log.2026.jsonl.
	if _, err := l.Append(sampleEvent(yr.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	first := archiveBase(l.Path()) + ".2026.jsonl"
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("expected first archive: %v", err)
	}
	// Third append: rotates again in the same year; the .2026.jsonl name is
	// taken so it must fall through to the numbered variant.
	if _, err := l.Append(sampleEvent(yr.Add(2 * time.Minute))); err != nil {
		t.Fatal(err)
	}
	second := archiveBase(l.Path()) + ".2026.1.jsonl"
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("expected numbered archive %s: %v", second, err)
	}
}

func TestShouldRotateEmpty(t *testing.T) {
	restoreSeams(t)
	do, err := tempLog(t).shouldRotate(nil, time.Now().UTC())
	if err != nil || do {
		t.Fatalf("empty log must not rotate: %v %v", do, err)
	}
}

func TestShouldRotateStatError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("stat")
	statFunc = func(string) (os.FileInfo, error) { return nil, boom }
	l := tempLog(t).WithSizeCap(1)
	recs := []Record{{Ts: time.Now().UTC()}}
	if _, err := l.shouldRotate(recs, time.Now().UTC()); !errors.Is(err, boom) {
		t.Fatalf("got %v, want stat", err)
	}
}

func TestShouldRotateSizeCapDisabled(t *testing.T) {
	restoreSeams(t)
	// Non-positive cap disables size rotation; same-year record does not rotate
	// even though the file exists.
	statFunc = func(string) (os.FileInfo, error) { t.Fatal("stat must not be called"); return nil, nil }
	l := tempLog(t).WithSizeCap(0)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	do, err := l.shouldRotate([]Record{{Ts: now}}, now)
	if err != nil || do {
		t.Fatalf("size-disabled same-year must not rotate: %v %v", do, err)
	}
}

func TestMaybeRotateRenameError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("rename")
	renameFunc = func(string, string) error { return boom }
	statFunc = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	l := tempLog(t)
	recs := []Record{{Ts: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}}
	if _, err := l.maybeRotate(recs, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, boom) {
		t.Fatalf("got %v, want rename", err)
	}
}

func TestAppendRotationError(t *testing.T) {
	restoreSeams(t)
	// A prior-year record forces rotation; a stat fault while choosing the
	// archive name surfaces through maybeRotate into Append.
	old := Record{SchemaVersion: SchemaVersion, Ts: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Actor: "a", Action: ActionLabelSubmit, Target: "t", PrevHash: GenesisPrevHash}
	line, err := marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	readFile = func(string) ([]byte, error) { return append(line, '\n'), nil }
	boom := errors.New("stat boom")
	statFunc = func(string) (os.FileInfo, error) { return nil, boom }
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))); !errors.Is(err, boom) {
		t.Fatalf("got %v, want stat boom", err)
	}
}

func TestParseRecordsScannerError(t *testing.T) {
	restoreSeams(t)
	// A single line larger than the scanner's token cap (8 MiB) with no
	// newline trips bufio.ErrTooLong via sc.Err().
	big := make([]byte, 9*1024*1024)
	for i := range big {
		big[i] = 'x'
	}
	readFile = func(string) ([]byte, error) { return big, nil }
	if _, err := tempLog(t).Records(); err == nil || !strings.Contains(err.Error(), "scan log") {
		t.Fatalf("expected scan error, got %v", err)
	}
}

func TestNextArchivePathStatError(t *testing.T) {
	restoreSeams(t)
	boom := errors.New("stat")
	statFunc = func(string) (os.FileInfo, error) { return nil, boom }
	if _, err := tempLog(t).nextArchivePath(2025); !errors.Is(err, boom) {
		t.Fatalf("got %v, want stat", err)
	}
}

func TestNextArchivePathNumberedStatError(t *testing.T) {
	restoreSeams(t)
	// First candidate exists (nil err), numbered candidate stat fails.
	calls := 0
	statFunc = func(string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, nil // .2025.jsonl "exists"
		}
		return nil, errors.New("stat")
	}
	if _, err := tempLog(t).nextArchivePath(2025); err == nil {
		t.Fatal("expected stat error on numbered candidate")
	}
}

func TestArchiveBaseNonJsonl(t *testing.T) {
	if got := archiveBase("/x/review/audit.log"); got != "/x/review/audit" {
		t.Errorf("non-jsonl base = %q", got)
	}
	if got := archiveBase("/x/review/audit.log.jsonl"); got != "/x/review/audit.log" {
		t.Errorf("jsonl base = %q", got)
	}
}

func TestWithSizeCapPreservesPath(t *testing.T) {
	l := Open("/some/path/audit.log.jsonl")
	l2 := l.WithSizeCap(42)
	if l2.Path() != l.Path() || l2.sizeCap != 42 {
		t.Fatalf("WithSizeCap lost state: %q %d", l2.Path(), l2.sizeCap)
	}
}
