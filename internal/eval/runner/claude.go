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
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// claudeBin is the Claude Code CLI binary name resolved via PATH.
const claudeBin = "claude"

// claudeRunner invokes the Claude Code CLI against a sandbox workdir.
// It requests JSON output (--output-format json) to extract token usage and
// session identity from the structured response; falling back to raw text
// when the flag is unsupported.
type claudeRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
}

func newClaudeRunner() *claudeRunner {
	return &claudeRunner{run: realExec}
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
	args := []string{"--print", "--output-format", "json", spec.Prompt}

	start := time.Now()
	stdout, stderr, code, err := r.run(ctx, claudeBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("runner/claude: exec: %w", err)
	}

	telemetry := parseClaudeTelemetry(stdout)
	return Result{
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  code,
		Duration:  dur,
		Telemetry: telemetry,
	}, nil
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
	t := AgentTelemetry{Harness: "claude-code"}

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

// computeHitRate derives cache hit rate from token counts when the CLI did not
// report it directly. The formula matches the rubric's token-efficiency
// definition: cached reads as a fraction of total input cost.
func computeHitRate(u *claudeTokenUsage) float64 {
	if u.CacheHitRate > 0 {
		return u.CacheHitRate
	}
	total := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
	if total == 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(total)
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
