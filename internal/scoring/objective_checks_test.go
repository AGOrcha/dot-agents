package scoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeToolUseTurn writes one Claude-shaped JSONL line carrying one tool_use
// content item, used to construct objective-check transcript fixtures.
func writeToolUseTurn(t *testing.T, dir, file string, ts time.Time, toolName string, input any) {
	t.Helper()
	inputBytes, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	line := map[string]any{
		"type":      "assistant",
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"name":  toolName,
					"input": json.RawMessage(inputBytes),
				},
			},
		},
	}
	encoded, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal turn: %v", err)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return parsed
}

// --- filesUnder10 -----------------------------------------------------------

func TestFilesUnder10(t *testing.T) {
	tests := []struct {
		filesChanged int
		wantPresent  bool
		wantScore    float64
	}{
		{0, false, 0},    // no diff stats -> absent
		{1, true, 1.0},   // within
		{10, true, 1.0},  // boundary
		{11, true, 0.0},  // over
		{100, true, 0.0}, // way over
	}
	for _, tt := range tests {
		got := filesUnder10(IterationRecord{FilesChanged: tt.filesChanged})
		if got.Present != tt.wantPresent {
			t.Errorf("filesUnder10(%d).Present = %v, want %v", tt.filesChanged, got.Present, tt.wantPresent)
		}
		if got.Present && got.SubScore != tt.wantScore {
			t.Errorf("filesUnder10(%d).SubScore = %g, want %g", tt.filesChanged, got.SubScore, tt.wantScore)
		}
	}
}

// --- objectiveFromScan ------------------------------------------------------

func TestObjectiveFromScan(t *testing.T) {
	tests := []struct {
		name        string
		matched     bool
		any         bool
		wantPresent bool
		wantScore   float64
	}{
		{"no coverage -> absent", false, false, false, 0},
		{"coverage no match -> 0.0", false, true, true, 0.0},
		{"matched -> 1.0", true, true, true, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := objectiveFromScan(tt.matched, tt.any, "yes", "no")
			if got.Present != tt.wantPresent {
				t.Errorf("Present = %v, want %v", got.Present, tt.wantPresent)
			}
			if got.Present && got.SubScore != tt.wantScore {
				t.Errorf("SubScore = %g, want %g", got.SubScore, tt.wantScore)
			}
		})
	}
}

// --- transcript scans -------------------------------------------------------

func TestRanCliCommand(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}

	// In-window Bash call -> matched.
	writeToolUseTurn(t, dir, "a.jsonl",
		parseTime(t, "2026-05-01T12:00:00Z"),
		"Bash", map[string]string{"command": "ls -la"})

	got := ranCliCommand(window, []string{dir})
	if !got.Present || got.SubScore != 1.0 {
		t.Errorf("ranCliCommand with Bash in window = %+v, want present 1.0", got)
	}
}

func TestRanCliCommandNoCallInWindow(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}

	// A turn in the window, but it's a Read, not a Bash -> coverage, no match.
	writeToolUseTurn(t, dir, "a.jsonl",
		parseTime(t, "2026-05-01T12:00:00Z"),
		"Read", map[string]string{"file_path": "/tmp/x"})

	got := ranCliCommand(window, []string{dir})
	if !got.Present || got.SubScore != 0.0 {
		t.Errorf("ranCliCommand with no Bash in window = %+v, want present 0.0", got)
	}
}

func TestRanCliCommandWindowEmpty(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}

	// Turn is outside the window -> no coverage -> absent.
	writeToolUseTurn(t, dir, "a.jsonl",
		parseTime(t, "2026-04-15T12:00:00Z"),
		"Bash", map[string]string{"command": "ls"})

	got := ranCliCommand(window, []string{dir})
	if got.Present {
		t.Errorf("ranCliCommand outside window = %+v, want absent", got)
	}
}

func TestRanCliCommandNoTranscriptDirs(t *testing.T) {
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	if got := ranCliCommand(window, nil); got.Present {
		t.Errorf("ranCliCommand with no transcript dirs = %+v, want absent", got)
	}
}

func TestReadLoopState(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}

	writeToolUseTurn(t, dir, "match.jsonl",
		parseTime(t, "2026-05-01T10:00:00Z"),
		"Read", map[string]string{"file_path": "/repo/.agents/active/loop-state.md"})
	writeToolUseTurn(t, dir, "noise.jsonl",
		parseTime(t, "2026-05-01T11:00:00Z"),
		"Read", map[string]string{"file_path": "/repo/README.md"})

	got := readLoopState(window, []string{dir})
	if !got.Present || got.SubScore != 1.0 {
		t.Errorf("readLoopState with loop-state.md read = %+v, want present 1.0", got)
	}
}

func TestReadLoopStateNoMatch(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	writeToolUseTurn(t, dir, "a.jsonl",
		parseTime(t, "2026-05-01T10:00:00Z"),
		"Read", map[string]string{"file_path": "/repo/README.md"})

	got := readLoopState(window, []string{dir})
	if !got.Present || got.SubScore != 0.0 {
		t.Errorf("readLoopState without loop-state.md = %+v, want present 0.0", got)
	}
}

func TestCommittedAfterTests(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	writeToolUseTurn(t, dir, "test.jsonl",
		parseTime(t, "2026-05-01T10:00:00Z"),
		"Bash", map[string]string{"command": "go test ./internal/scoring/ -cover"})

	got := committedAfterTests(window, []string{dir})
	if !got.Present || got.SubScore != 1.0 {
		t.Errorf("committedAfterTests with `go test` = %+v, want present 1.0", got)
	}
}

func TestCommittedAfterTestsNoMatch(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	writeToolUseTurn(t, dir, "a.jsonl",
		parseTime(t, "2026-05-01T10:00:00Z"),
		"Bash", map[string]string{"command": "echo hello"})

	got := committedAfterTests(window, []string{dir})
	if !got.Present || got.SubScore != 0.0 {
		t.Errorf("committedAfterTests without test command = %+v, want present 0.0", got)
	}
}

// --- malformed input handling ----------------------------------------------

func TestScanIgnoresMalformedLines(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	// One malformed JSON line, one valid line lacking a timestamp, one valid
	// match — only the match should be visible.
	path := filepath.Join(dir, "messy.jsonl")
	contents := "not json\n" +
		`{"type":"system","message":{"role":"system"}}` + "\n" + // no timestamp
		`{"type":"assistant","timestamp":"2026-05-01T10:00:00Z","message":{"content":[{"type":"text"}]}}` + "\n" + // turn with no tool_use
		`{"type":"assistant","timestamp":"2026-05-01T11:00:00Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := ranCliCommand(window, []string{dir})
	if !got.Present || got.SubScore != 1.0 {
		t.Errorf("ranCliCommand against messy file = %+v, want present 1.0", got)
	}
}

func TestScanIgnoresBadToolUseInput(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	// A Read tool_use whose input is a non-object — Unmarshal fails inside the
	// matcher, which must return false rather than panic.
	path := filepath.Join(dir, "bad.jsonl")
	contents := `{"type":"assistant","timestamp":"2026-05-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Read","input":"not an object"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := readLoopState(window, []string{dir})
	if !got.Present || got.SubScore != 0.0 {
		t.Errorf("readLoopState against bad input = %+v, want present 0.0 (no match, but covered)", got)
	}

	got2 := committedAfterTests(window, []string{dir})
	if !got2.Present || got2.SubScore != 0.0 {
		t.Errorf("committedAfterTests against bad input = %+v, want present 0.0", got2)
	}
}

func TestScanMissingDir(t *testing.T) {
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	// A non-existent directory simply contributes no turns, no error.
	got := ranCliCommand(window, []string{"/nonexistent/path"})
	if got.Present {
		t.Errorf("ranCliCommand on missing dir = %+v, want absent", got)
	}
}

func TestScanSkipsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	// A *directory* with a .jsonl extension matches the glob but cannot be
	// read as a file — the scanner must skip it rather than crash.
	if err := os.Mkdir(filepath.Join(dir, "asdirectory.jsonl"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := ranCliCommand(window, []string{dir})
	if got.Present {
		t.Errorf("ranCliCommand with unreadable entry = %+v, want absent", got)
	}
}

// --- integration ------------------------------------------------------------

func TestExtractIterationObjectives(t *testing.T) {
	dir := t.TempDir()
	window := IterationWindow{
		Start: parseTime(t, "2026-05-01T00:00:00Z"),
		End:   parseTime(t, "2026-05-02T00:00:00Z"),
	}
	writeToolUseTurn(t, dir, "1.jsonl",
		parseTime(t, "2026-05-01T10:00:00Z"),
		"Bash", map[string]string{"command": "go test ./..."})
	writeToolUseTurn(t, dir, "2.jsonl",
		parseTime(t, "2026-05-01T10:05:00Z"),
		"Read", map[string]string{"file_path": "/repo/.agents/active/loop-state.md"})

	rec := IterationRecord{FilesChanged: 7}
	got := ExtractIterationObjectives(rec, window, dir)

	if !got.RanCliCommand.Present || got.RanCliCommand.SubScore != 1.0 {
		t.Errorf("RanCliCommand = %+v, want present 1.0", got.RanCliCommand)
	}
	if !got.CommittedAfterTests.Present || got.CommittedAfterTests.SubScore != 1.0 {
		t.Errorf("CommittedAfterTests = %+v, want present 1.0", got.CommittedAfterTests)
	}
	if !got.ReadLoopState.Present || got.ReadLoopState.SubScore != 1.0 {
		t.Errorf("ReadLoopState = %+v, want present 1.0", got.ReadLoopState)
	}
	if !got.FilesUnder10.Present || got.FilesUnder10.SubScore != 1.0 {
		t.Errorf("FilesUnder10 = %+v, want present 1.0", got.FilesUnder10)
	}
}
