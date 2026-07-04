package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// claudeBin is the Claude Code CLI binary name resolved via PATH; claudeHarness
// is the platform identity recorded in telemetry and start-failure diagnostics.
const (
	claudeBin     = "claude"
	claudeHarness = "claude-code"
)

// claudeRunner invokes the Claude Code CLI against a sandbox workdir.
// It requests JSON output (--output-format json) to extract token usage and
// session identity from the structured response; falling back to raw text
// when the flag is unsupported.
type claudeRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
	// scan is the token-scanner fallback wired to platform claude's
	// ScanSessionTokens (walks <home>/.claude/projects/<cwd-hash>/
	// <sessionID>.jsonl). It is only consulted when the inline JSON envelope
	// carried a session id but no usage block, so a run whose stdout was
	// captured without --output-format json still recovers telemetry. A struct
	// field (not a package var) keeps tests off a real ~/.claude.
	scan scanFn
}

func newClaudeRunner() *claudeRunner {
	return &claudeRunner{run: realExec, scan: asScanner(platform.NewClaude())}
}

var _ Runner = (*claudeRunner)(nil)

// Run implements Runner. It invokes `claude --print --output-format json
// <prompt>` in instance.Workdir with instance.Env appended to the process
// environment. Token usage and session identity are extracted from the JSON
// response when present; absent fields leave Telemetry partial but valid.
func (r *claudeRunner) Run(
	ctx context.Context,
	spec *eval.TaskSpec,
	instance *sandbox.Instance,
) (Result, error) {
	if spec == nil {
		return Result{}, fmt.Errorf("runner/claude: task spec is nil")
	}
	if instance == nil {
		return Result{}, fmt.Errorf("runner/claude: sandbox instance is nil")
	}

	env := buildEnv(instance.Env)
	// Runner invocation: `claude --print --output-format json <prompt>` with
	// empty stdin (realExec pins cmd.Stdin = bytes.NewReader(nil)).
	//
	// This deliberately diverges from the repo's headless REVIEWER convention
	// (`claude --print --output-format text`, prompt via STDIN, e.g.
	// prompts/reviewers/cross-harness-adversarial.md), for two reasons:
	//
	//  1. Token telemetry: --output-format json emits a structured envelope
	//     containing session_id, model, and the usage block (input_tokens,
	//     output_tokens, cache_read/creation_tokens, cache_hit_rate).
	//     parseClaudeTelemetry extracts these from stdout; the text format
	//     provides no machine-readable token counts.
	//  2. Prompt delivery: the RUNNER hands an opaque task prompt (not an
	//     agent-to-agent message); passing it as ARGV keeps the subprocess
	//     interface uniform with codex (`codex exec <prompt>`) and copilot
	//     (`copilot -p <prompt>`). STDIN is held empty so no adapter can block
	//     waiting on interactive TTY input.
	args := []string{"--print", "--output-format", "json", spec.Prompt}

	start := time.Now()
	after := start.UTC().Format(time.RFC3339)
	stdout, stderr, code, err := r.run(ctx, claudeBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		if se := classifyExecError(claudeHarness, claudeBin, err); se != nil {
			return Result{}, se
		}
		return Result{}, fmt.Errorf("runner/claude: exec: %w", err)
	}

	telemetry := parseClaudeTelemetry(stdout)
	r.backfillTokens(&telemetry, instance, after)
	return Result{
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  code,
		Duration:  dur,
		Telemetry: telemetry,
	}, nil
}

// backfillTokens recovers token telemetry from the on-disk Claude session
// store when the inline JSON envelope reported a session id but no usage
// block. The session JSONL lives under the scratch HOME keyed by the working
// directory (the run cwd) and the session id; the mtime/timestamp filter is
// scoped to this run via after. When the inline usage was already present or
// no session id was captured, this is a no-op.
func (r *claudeRunner) backfillTokens(t *AgentTelemetry, instance *sandbox.Instance, after string) {
	if t.Tokens != nil || t.SessionID == "" {
		return
	}
	home := scratchHomeFromEnv(instance.Env)
	t.Tokens = tokensFromMetrics(r.scan(home, instance.Workdir, t.SessionID, after))
}

// claudeJSONResponse is the subset of Claude Code's JSON output envelope that
// carries session and token telemetry. Unknown fields are ignored so a future
// schema addition does not break existing eval runs.
type claudeJSONResponse struct {
	SessionID string            `json:"session_id"`
	Model     string            `json:"model"`
	Usage     *claudeTokenUsage `json:"usage"`
}

// claudeTokenUsage mirrors the usage block Claude Code emits in JSON mode.
type claudeTokenUsage struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationTokens int     `json:"cache_creation_input_tokens"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
}

// parseClaudeTelemetry extracts session identity and token usage from the
// JSON envelope Claude Code writes to stdout in --output-format json mode.
// Partial or unparseable output is gracefully degraded — the caller still
// gets a valid AgentTelemetry, just with fewer fields set.
func parseClaudeTelemetry(stdout []byte) AgentTelemetry {
	t := AgentTelemetry{Harness: claudeHarness}

	// Claude may emit multiple JSON objects; the last well-formed one with
	// a usage block is the session summary.
	lines := splitLines(stdout)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var resp claudeJSONResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.SessionID != "" {
			t.SessionID = resp.SessionID
		}
		if resp.Model != "" {
			t.Model = resp.Model
		}
		if resp.Usage != nil {
			hitRate := computeHitRate(resp.Usage)
			t.Tokens = &scoring.TokenUsage{
				InputTokens:         resp.Usage.InputTokens,
				OutputTokens:        resp.Usage.OutputTokens,
				CacheReadTokens:     resp.Usage.CacheReadTokens,
				CacheCreationTokens: resp.Usage.CacheCreationTokens,
				CacheHitRate:        hitRate,
			}
		}
	}
	return t
}

// computeHitRate derives cache hit rate as cache_read / (cache_read +
// cache_creation) when the CLI did not report a rate directly. This is the
// shipped contract used everywhere else in the codebase — scoring's transcript
// backfill (internal/scoring/signal_backfill.go cacheHitRate) and the platform
// session scanners (internal/platform/session.go claudeScanSessionTokens).
// input_tokens is deliberately NOT in the denominator: including it
// under-reports cache efficiency and would skew the token_efficiency signal,
// which scoring treats native session_tokens.cache_hit_rate as authoritative
// for. Returns 0 when no cacheable context existed (divide-by-zero guard).
func computeHitRate(u *claudeTokenUsage) float64 {
	if u.CacheHitRate > 0 {
		return u.CacheHitRate
	}
	denom := u.CacheReadTokens + u.CacheCreationTokens
	if denom <= 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(denom)
}

// buildEnv constructs the subprocess environment by appending sandbox
// overrides after the current process environment. Sandbox entries win on
// duplicate keys because os.Environ() convention is first-key-wins on most
// implementations, but the runner appends the sandbox overrides last so they
// take effect on all standard library implementations.
func buildEnv(sandboxEnv []string) []string {
	base := os.Environ()
	result := make([]string, len(base)+len(sandboxEnv))
	copy(result, base)
	copy(result[len(base):], sandboxEnv)
	return result
}

// splitLines splits bytes on newline boundaries, returning non-empty strings.
func splitLines(data []byte) []string {
	raw := strings.Split(string(data), "\n")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
