package scoring

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IterationObjectives carries facts about an iteration checked objectively
// from the agent transcripts, in place of the rubber-stamped self_assessment
// booleans the boolean-effectiveness analysis (iter-66 dogfood) showed carried
// no information.
//
// Each entry is a SignalValue — Present with a 0/1 sub-score for a checked
// fact, Absent when the transcript window had no coverage. These observations
// surface alongside the score as a parallel record of process discipline;
// they do not enter the numeric score directly. The self-report counterparts
// have been removed from the schema, so there is nothing to pair against in
// the integrity track.
type IterationObjectives struct {
	// RanCliCommand: did the agent actually invoke a CLI tool in the window.
	RanCliCommand SignalValue
	// CommittedAfterTests: did a test command run in the window before the
	// iteration's commit.
	CommittedAfterTests SignalValue
	// ReadLoopState: was loop-state.md read in the window.
	ReadLoopState SignalValue
}

// ExtractIterationObjectives runs every objective check for one iteration.
// window scopes the transcript scans (use the backfill's IterationWindow);
// empty transcriptDirs leave the transcript-derived signals absent.
//
// The rec argument is retained for future objective checks that need diff
// stats or other iteration-log facts; the current set is transcript-only.
func ExtractIterationObjectives(_ IterationRecord, window IterationWindow, transcriptDirs ...string) IterationObjectives {
	return IterationObjectives{
		RanCliCommand:       ranCliCommand(window, transcriptDirs),
		ReadLoopState:       readLoopState(window, transcriptDirs),
		CommittedAfterTests: committedAfterTests(window, transcriptDirs),
	}
}

// ranCliCommand returns 1.0 if the window contains any Bash tool_use, 0.0 if
// the window had transcript coverage but no Bash call, absent otherwise.
func ranCliCommand(window IterationWindow, transcriptDirs []string) SignalValue {
	matched, any := scanToolUse(window, transcriptDirs, func(name string, _ []byte) bool {
		return name == "Bash"
	})
	return objectiveFromScan(matched, any, "Bash tool invoked", "no Bash tool invocation in window")
}

// readLoopState returns 1.0 if a Read tool_use targeted a loop-state.md path.
func readLoopState(window IterationWindow, transcriptDirs []string) SignalValue {
	matched, any := scanToolUse(window, transcriptDirs, func(name string, input []byte) bool {
		if name != "Read" {
			return false
		}
		var in struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return false
		}
		return strings.HasSuffix(in.FilePath, "loop-state.md")
	})
	return objectiveFromScan(matched, any, "loop-state.md was read", "loop-state.md not read in window")
}

// testCommandPatterns are command substrings that identify a test run. The set
// is intentionally loose — false positives are far less costly than missing a
// real test invocation in the objective check for committed_after_tests.
var testCommandPatterns = []string{
	"go test", "pytest", "npm test", "yarn test",
	"make test", "cargo test", "rspec", "phpunit",
}

// committedAfterTests returns 1.0 if a Bash command matching a test pattern
// appears in the window — i.e., tests ran during the iteration that produced
// the commit.
func committedAfterTests(window IterationWindow, transcriptDirs []string) SignalValue {
	matched, any := scanToolUse(window, transcriptDirs, func(name string, input []byte) bool {
		if name != "Bash" {
			return false
		}
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return false
		}
		for _, p := range testCommandPatterns {
			if strings.Contains(in.Command, p) {
				return true
			}
		}
		return false
	})
	return objectiveFromScan(matched, any, "test command invoked before commit", "no test command in window")
}

// objectiveFromScan turns a scan result into a SignalValue. matched → 1.0,
// any-but-not-matched → 0.0, no transcript coverage in the window → absent.
// The "any" flag is what lets the rubric distinguish "checked, no match" from
// "couldn't check, no data" — the same first-class-absent principle the rubric
// uses for the score signals.
func objectiveFromScan(matched, any bool, presentDetail, absentDetail string) SignalValue {
	if !any {
		return AbsentSignal("no transcript coverage in window")
	}
	if matched {
		return PresentSignal(1.0, presentDetail)
	}
	return PresentSignal(0.0, absentDetail)
}

// toolUseLine is the focused parse of a Claude JSONL turn for the objective
// checks — just timestamp and the tool_use fields of each content item.
// signal_backfill.go's claudeContentItem stays orthogonal: it reads IsError
// for token-error counting; this reads Name and Input for tool routing.
type toolUseLine struct {
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Content []toolUseContent `json:"content"`
	} `json:"message"`
}

type toolUseContent struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// transcriptScanBufferSize bounds bufio.Scanner — some assistant turns are
// large (a tool result with a big file readback).
const transcriptScanBufferSize = 16 * 1024 * 1024

// scanToolUse walks every *.jsonl under transcriptDirs and, for each turn
// whose timestamp falls in (window.Start, window.End], inspects each tool_use
// content item via match. Returns matched = true on the first match (it
// short-circuits) and any = true if at least one turn fell in the window —
// distinguishing "checked, no match" from "no coverage."
func scanToolUse(window IterationWindow, transcriptDirs []string, match func(name string, input []byte) bool) (matched, any bool) {
	for _, dir := range transcriptDirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		for _, f := range files {
			fileMatched, fileAny := scanToolUseFile(f, window, match)
			if fileAny {
				any = true
			}
			if fileMatched {
				return true, true
			}
		}
	}
	return matched, any
}

// parseToolUseLineInWindow decodes one transcript line; returns (turn, true)
// only when the line is well-formed JSON with a parseable timestamp inside
// (window.Start, window.End]. The boolean lets the caller distinguish
// "skip" from "in-window candidate" without inspecting turn.
func parseToolUseLineInWindow(line []byte, window IterationWindow) (toolUseLine, bool) {
	var turn toolUseLine
	if err := json.Unmarshal(line, &turn); err != nil {
		return turn, false
	}
	t, err := time.Parse(time.RFC3339Nano, turn.Timestamp)
	if err != nil {
		return turn, false
	}
	if !t.After(window.Start) || t.After(window.End) {
		return turn, false
	}
	return turn, true
}

// toolUseTurnMatches reports whether any tool_use content item in this turn
// satisfies match. Safe to call with a nil-Message turn (returns false).
func toolUseTurnMatches(turn toolUseLine, match func(name string, input []byte) bool) bool {
	if turn.Message == nil {
		return false
	}
	for _, item := range turn.Message.Content {
		if item.Type == "tool_use" && match(item.Name, item.Input) {
			return true
		}
	}
	return false
}

// scanToolUseFile is the per-file half of scanToolUse — factored out so a
// short-circuit on match returns from the file cleanly with its handle closed.
func scanToolUseFile(path string, window IterationWindow, match func(name string, input []byte) bool) (matched, any bool) {
	fh, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer fh.Close()

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), transcriptScanBufferSize)
	for sc.Scan() {
		turn, ok := parseToolUseLineInWindow(sc.Bytes(), window)
		if !ok {
			continue
		}
		any = true
		if toolUseTurnMatches(turn, match) {
			return true, true
		}
	}
	return false, any
}
