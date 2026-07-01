package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// restoreSeams snapshots the package filesystem/clock/marshal seams and returns
// a function that restores them, so a test that injects a fault cannot leak it
// into the next test.
func restoreSeams(t *testing.T) {
	t.Helper()
	origRead, origStat, origRename, origNow, origAppend, origMarshal, origOpen :=
		readFile, statFunc, renameFunc, timeNow, appendLine, marshal, openAppend
	t.Cleanup(func() {
		readFile, statFunc, renameFunc, timeNow, appendLine, marshal, openAppend =
			origRead, origStat, origRename, origNow, origAppend, origMarshal, origOpen
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
	wantPrev, err := hashRecord(r1)
	if err != nil {
		t.Fatal(err)
	}
	if r2.PrevHash != wantPrev {
		t.Errorf("second record prev_hash = %q, want %q", r2.PrevHash, wantPrev)
	}

	// File is exactly two JSON lines, append-only (record 1 unchanged).
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
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

func TestAppendChainHashError(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	if _, err := l.Append(sampleEvent(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	// With a prior record present, the next append must hash it first; force
	// the marshal seam to fail to hit that branch.
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

func TestVerifyDetectsTamper(t *testing.T) {
	restoreSeams(t)
	l := tempLog(t)
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := l.Append(sampleEvent(ts.Add(time.Duration(i) * time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
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

func TestVerifyGenesisBreak(t *testing.T) {
	res, err := verifyRecords([]Record{{PrevHash: "not-genesis", Action: ActionLabelSubmit}})
	if err != nil {
		t.Fatal(err)
	}
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

func TestVerifyHashError(t *testing.T) {
	restoreSeams(t)
	// First record's prev_hash is genesis, so the i==0 prev check passes; then
	// hashRecord runs and the marshal seam fault surfaces.
	marshal = func(any) ([]byte, error) { return nil, errors.New("nope") }
	if _, err := verifyRecords([]Record{{PrevHash: GenesisPrevHash}}); !errors.Is(err, ErrMarshal) {
		t.Fatalf("got %v, want ErrMarshal", err)
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
