package scoring

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/execabs"
)

// signal_backfill.go reconstructs token-efficiency and tool-error telemetry the
// iteration log itself never captured.
//
// The iteration log records native session_tokens for only ~3% of entries, but
// every entry carries a 100%-populated commit SHA, and the raw agent transcripts
// survive. The token window for iteration N is (commitTime(N-1), commitTime(N)]:
// a transcript turn whose timestamp falls in that half-open window belongs to
// that iteration. Summing those turns reconstructs the token and cache counts.
//
// The core scanner (scanTranscriptWindow) is a pure function over a directory
// and a time window, unit-tested against small fixture JSONL files. The git
// commit-time resolution and the real ~/.claude / ~/.codex scan live in the
// thin BackfillIterations wrapper, exercised only by a skip-guarded integration
// test.

// transcriptTotals is the raw sum of a transcript-window scan: token counts
// plus tool-call error tallies. It is the pure scanner's output.
type transcriptTotals struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int

	// ToolCalls is the number of tool results observed in the window;
	// ToolErrors is how many of them were flagged as errors.
	ToolCalls  int
	ToolErrors int

	// Turns is the number of token-bearing transcript turns summed. Zero means
	// no transcript covered the window at all.
	Turns int
}

// cacheHitRate is cache_read / (cache_read + cache_creation), or 0 when the
// iteration created no cacheable context.
func (t transcriptTotals) cacheHitRate() float64 {
	denom := t.CacheReadTokens + t.CacheCreationTokens
	if denom <= 0 {
		return 0
	}
	return float64(t.CacheReadTokens) / float64(denom)
}

// toolErrorRate is the fraction of observed tool calls that errored, or 0 when
// no tool calls were observed.
func (t transcriptTotals) toolErrorRate() float64 {
	if t.ToolCalls <= 0 {
		return 0
	}
	return float64(t.ToolErrors) / float64(t.ToolCalls)
}

// BackfillSignals carries the transcript-reconstructed signals for one
// iteration. Both fields are always set; absence is expressed inside the
// SignalValue / present-bool, never by a nil struct.
type BackfillSignals struct {
	// Iteration is the iteration number these signals belong to.
	Iteration int

	// TokenEfficiency is the token_efficiency signal: cache_hit_rate as a
	// sub-score in [0,1]. Present from native session_tokens when the
	// iteration log captured it, otherwise backfilled from the transcript
	// window; absent when no transcript covers the window.
	TokenEfficiency SignalValue

	// ToolErrorRate is the fraction of tool calls in the iteration's window
	// that errored, feeding the assemble slice's correction_pressure signal.
	// ToolErrorRatePresent is false when no tool-call evidence was found — a
	// caller must check it before reading ToolErrorRate.
	ToolErrorRate        float64
	ToolErrorRatePresent bool
}

// --- raw transcript JSONL shapes ------------------------------------------

// claudeUsage is the token block on a Claude assistant turn.
type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// claudeContentItem is one item of a Claude turn's message.content array. Only
// tool_result items matter here — they carry the is_error flag.
type claudeContentItem struct {
	Type    string `json:"type"`
	IsError *bool  `json:"is_error"`
}

// claudeMessage is the message envelope on a Claude transcript line.
type claudeMessage struct {
	Role    string              `json:"role"`
	Usage   *claudeUsage        `json:"usage"`
	Content []claudeContentItem `json:"content"`
}

// claudeLine is one line of a Claude *.jsonl transcript. Tool results may also
// surface as a top-level toolUseResult with an is_error sibling on tool_result
// content items; both are handled by walking message.content.
type claudeLine struct {
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	Message   *claudeMessage `json:"message"`
}

// codexTokenUsage is one usage block inside a Codex token_count payload.
type codexTokenUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// codexInfo is payload.info on a Codex token_count line. It is sometimes null,
// in which case the line carries no usable counts.
type codexInfo struct {
	LastTokenUsage *codexTokenUsage `json:"last_token_usage"`
}

// codexPayload is the payload object on a Codex rollout line.
type codexPayload struct {
	Type string     `json:"type"`
	Info *codexInfo `json:"info"`
}

// codexLine is one line of a Codex rollout *.jsonl transcript.
type codexLine struct {
	Timestamp string        `json:"timestamp"`
	Payload   *codexPayload `json:"payload"`
}

// --- pure scanner ----------------------------------------------------------

// scanTranscriptWindow sums every transcript turn in dir whose timestamp falls
// in the half-open window (start, end]. It is a PURE function: it touches only
// the given directory, never ~/.claude, ~/.codex, or git history, so it is
// fully unit-testable against fixture JSONL files.
//
// Both Claude and Codex line shapes are recognized per line, so a directory may
// mix harnesses. Malformed lines, lines outside the window, and Codex
// token_count lines with a null info block are skipped silently — a transcript
// is a best-effort source, not a schema-checked input.
func scanTranscriptWindow(dir string, start, end time.Time) (transcriptTotals, error) {
	var totals transcriptTotals

	entries, err := os.ReadDir(dir)
	if err != nil {
		return totals, fmt.Errorf("scoring: scan transcript dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := scanTranscriptFile(path, start, end, &totals); err != nil {
			return totals, err
		}
	}
	return totals, nil
}

// scanTranscriptFile folds one JSONL file's in-window turns into totals.
func scanTranscriptFile(path string, start, end time.Time, totals *transcriptTotals) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("scoring: open transcript %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Transcript lines can be large (a tool result embedded inline); raise the
	// scanner's token cap well above the 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		foldTranscriptLine(line, start, end, totals)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scoring: read transcript %s: %w", path, err)
	}
	return nil
}

// foldTranscriptLine folds a single JSONL line into totals if it is an
// in-window Claude or Codex token/tool turn. A line that is neither, is
// malformed, has no timestamp, or falls outside the window is ignored.
func foldTranscriptLine(line []byte, start, end time.Time, totals *transcriptTotals) {
	// Probe the timestamp and shape with one cheap decode shared by both
	// harnesses; a line missing a parseable timestamp cannot be windowed.
	var probe struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return
	}
	ts, ok := parseTranscriptTime(probe.Timestamp)
	if !ok || !inWindow(ts, start, end) {
		return
	}

	switch {
	case len(probe.Payload) > 0:
		foldCodexLine(line, totals)
	case probe.Type == "assistant" || probe.Type == "user":
		foldClaudeLine(line, totals)
	}
}

// foldClaudeLine adds a Claude assistant/user turn's tokens and tool results.
func foldClaudeLine(line []byte, totals *transcriptTotals) {
	var cl claudeLine
	if err := json.Unmarshal(line, &cl); err != nil || cl.Message == nil {
		return
	}
	if u := cl.Message.Usage; u != nil {
		totals.InputTokens += u.InputTokens
		totals.OutputTokens += u.OutputTokens
		totals.CacheReadTokens += u.CacheReadInputTokens
		totals.CacheCreationTokens += u.CacheCreationInputTokens
		totals.Turns++
	}
	for _, item := range cl.Message.Content {
		if item.Type != "tool_result" {
			continue
		}
		totals.ToolCalls++
		if item.IsError != nil && *item.IsError {
			totals.ToolErrors++
		}
	}
}

// foldCodexLine adds a Codex token_count turn's tokens. Codex rollout lines
// carry no reliable per-tool error flag, so they contribute no ToolCalls — see
// the package report. A token_count line with a null info block is skipped.
func foldCodexLine(line []byte, totals *transcriptTotals) {
	var cl codexLine
	if err := json.Unmarshal(line, &cl); err != nil || cl.Payload == nil {
		return
	}
	if cl.Payload.Type != "token_count" || cl.Payload.Info == nil {
		return
	}
	u := cl.Payload.Info.LastTokenUsage
	if u == nil {
		return
	}
	totals.InputTokens += u.InputTokens
	totals.OutputTokens += u.OutputTokens
	// Codex reports a single cached_input_tokens count and no cache-creation
	// figure; treat cached tokens as cache reads and the remaining input as
	// freshly cached context so cacheHitRate stays well-defined.
	totals.CacheReadTokens += u.CachedInputTokens
	if fresh := u.InputTokens - u.CachedInputTokens; fresh > 0 {
		totals.CacheCreationTokens += fresh
	}
	totals.Turns++
}

// parseTranscriptTime parses an RFC3339 transcript timestamp, tolerating the
// millisecond-precision Z form both harnesses emit.
func parseTranscriptTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// inWindow reports whether ts is in the half-open window (start, end]. A zero
// start means the window is open on the left — used for the first iteration,
// which has no predecessor commit.
func inWindow(ts, start, end time.Time) bool {
	if !start.IsZero() && !ts.After(start) {
		return false
	}
	return !ts.After(end)
}

// --- iteration-window backfill ---------------------------------------------

// IterationWindow pairs an iteration with the transcript time window that
// belongs to it: (Start, End]. Start is the predecessor commit's time (zero for
// the first iteration); End is this iteration's commit time.
type IterationWindow struct {
	Iteration int
	Start     time.Time
	End       time.Time
}

// backfillWindow computes the BackfillSignals for one iteration window by
// scanning dir. rec supplies the native session_tokens shortcut: when present,
// its cache_hit_rate is authoritative and the transcript is consulted only for
// the tool-error rate.
func backfillWindow(rec IterationRecord, win IterationWindow, dir string) (BackfillSignals, error) {
	out := BackfillSignals{Iteration: win.Iteration}

	totals, err := scanTranscriptWindow(dir, win.Start, win.End)
	if err != nil {
		return out, err
	}

	switch {
	case rec.SessionTokens != nil:
		out.TokenEfficiency = PresentSignal(
			rec.SessionTokens.CacheHitRate,
			fmt.Sprintf("native session_tokens: cache_hit_rate %.3f", rec.SessionTokens.CacheHitRate),
		)
	case totals.Turns > 0:
		rate := totals.cacheHitRate()
		out.TokenEfficiency = PresentSignal(rate, fmt.Sprintf(
			"backfilled from %d transcript turn(s): cache_hit_rate %.3f", totals.Turns, rate))
	default:
		out.TokenEfficiency = AbsentSignal("no transcript covers the iteration window")
	}

	if totals.ToolCalls > 0 {
		out.ToolErrorRate = totals.toolErrorRate()
		out.ToolErrorRatePresent = true
	}
	return out, nil
}

// BackfillIterations reconstructs token-efficiency and tool-error signals for a
// sorted slice of iteration records. It is the thin, side-effecting wrapper
// around the pure scanner: it resolves each iteration's commit time via git in
// repoDir, derives the per-iteration windows, and scans every transcriptDir.
//
// records MUST be sorted ascending by iteration (LoadIterationLog returns them
// so). The window for iteration N is (commitTime(N-1), commitTime(N)]; the
// first iteration's window is open on the left. An iteration whose commit SHA
// git cannot resolve is reported with both signals absent rather than failing
// the whole batch — squashed and rebased history is expected.
func BackfillIterations(records []IterationRecord, repoDir string, transcriptDirs ...string) ([]BackfillSignals, error) {
	windows := resolveWindows(records, repoDir)

	out := make([]BackfillSignals, 0, len(records))
	for i, rec := range records {
		win, resolved := windows[rec.Iteration]
		if !resolved {
			out = append(out, BackfillSignals{
				Iteration:       rec.Iteration,
				TokenEfficiency: AbsentSignal("commit time unresolvable; cannot window the transcript"),
			})
			continue
		}
		merged, err := backfillAcrossDirs(rec, win, transcriptDirs)
		if err != nil {
			return nil, fmt.Errorf("scoring: backfill iteration %d: %w", records[i].Iteration, err)
		}
		out = append(out, merged)
	}
	return out, nil
}

// backfillAcrossDirs runs the window scan over every transcript directory and
// merges the results: tokens and tool counts add up, and the first present
// token-efficiency signal (native, then any transcript hit) wins.
func backfillAcrossDirs(rec IterationRecord, win IterationWindow, dirs []string) (BackfillSignals, error) {
	merged := transcriptTotals{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			continue // a missing transcript root is not fatal
		}
		totals, err := scanTranscriptWindow(dir, win.Start, win.End)
		if err != nil {
			return BackfillSignals{}, err
		}
		merged.InputTokens += totals.InputTokens
		merged.OutputTokens += totals.OutputTokens
		merged.CacheReadTokens += totals.CacheReadTokens
		merged.CacheCreationTokens += totals.CacheCreationTokens
		merged.ToolCalls += totals.ToolCalls
		merged.ToolErrors += totals.ToolErrors
		merged.Turns += totals.Turns
	}

	out := BackfillSignals{Iteration: win.Iteration}
	switch {
	case rec.SessionTokens != nil:
		out.TokenEfficiency = PresentSignal(
			rec.SessionTokens.CacheHitRate,
			fmt.Sprintf("native session_tokens: cache_hit_rate %.3f", rec.SessionTokens.CacheHitRate),
		)
	case merged.Turns > 0:
		rate := merged.cacheHitRate()
		out.TokenEfficiency = PresentSignal(rate, fmt.Sprintf(
			"backfilled from %d transcript turn(s): cache_hit_rate %.3f", merged.Turns, rate))
	default:
		out.TokenEfficiency = AbsentSignal("no transcript covers the iteration window")
	}
	if merged.ToolCalls > 0 {
		out.ToolErrorRate = merged.toolErrorRate()
		out.ToolErrorRatePresent = true
	}
	return out, nil
}

// resolveWindows derives the (Start, End] transcript window for each iteration
// from git commit times. An iteration whose commit SHA does not resolve is
// omitted from the map; its predecessor's End still anchors the next window
// only when that predecessor itself resolved.
func resolveWindows(records []IterationRecord, repoDir string) map[int]IterationWindow {
	windows := make(map[int]IterationWindow, len(records))

	sorted := make([]IterationRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Iteration < sorted[j].Iteration })

	var prevEnd time.Time // zero until the first commit resolves
	for _, rec := range sorted {
		end, ok := commitTime(repoDir, rec.Commit)
		if !ok {
			continue
		}
		windows[rec.Iteration] = IterationWindow{
			Iteration: rec.Iteration,
			Start:     prevEnd,
			End:       end,
		}
		prevEnd = end
	}
	return windows
}

// commitTime resolves a commit SHA to its committer timestamp via git. It
// returns ok=false for an empty SHA or any SHA git cannot resolve — abbreviated
// or rebased-away SHAs are expected and must not fail the batch.
func commitTime(repoDir, sha string) (time.Time, bool) {
	if strings.TrimSpace(sha) == "" {
		return time.Time{}, false
	}
	cmd := execabs.Command("git", "-C", repoDir, "show", "-s", "--format=%cI", sha)
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, false
	}
	return parseTranscriptTime(strings.TrimSpace(string(out)))
}
