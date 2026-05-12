package platform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// scanJSONLForLastModel opens a JSONL file and calls extract on each line,
// keeping the last non-empty string returned. Platform-specific model
// resolvers use this as their shared scanning primitive.
func scanJSONLForLastModel(path string, extract func([]byte) string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	model := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if v := extract(sc.Bytes()); v != "" {
			model = v
		}
	}
	return model
}

// claudeSessionJSONLPath returns the path to the Claude Code session JSONL
// for the given project and session ID.
// Claude Code hashes the project path by replacing "/" with "-".
func claudeSessionJSONLPath(home, projectPath, sessionID string) string {
	hash := strings.ReplaceAll(projectPath, "/", "-")
	return filepath.Join(home, ".claude", "projects", hash, sessionID+".jsonl")
}

// findCodexSessionFile walks ~/.codex/sessions/YYYY/MM/DD/ looking for a
// JSONL file whose name ends with "-<sessionID>.jsonl". Returns "" if not found.
func findCodexSessionFile(home, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	suffix := "-" + sessionID + ".jsonl"
	found := ""
	_ = filepath.WalkDir(filepath.Join(home, ".codex", "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), suffix) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// resolveClaudeCodeModelFromJSONL scans the Claude Code session JSONL for the
// most recent model identifier used in an assistant turn.
func resolveClaudeCodeModelFromJSONL(home, projectPath, sessionID string) string {
	return scanJSONLForLastModel(claudeSessionJSONLPath(home, projectPath, sessionID), func(line []byte) string {
		if !bytes.Contains(line, []byte(`"assistant"`)) {
			return ""
		}
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return ""
		}
		if entry.Type == "assistant" {
			return entry.Message.Model
		}
		return ""
	})
}

// resolveCodexModelFromJSONL scans the Codex session JSONL for the model used
// in a response_item of type "response".
func resolveCodexModelFromJSONL(home, sessionID string) string {
	path := findCodexSessionFile(home, sessionID)
	if path == "" {
		return ""
	}
	return scanJSONLForLastModel(path, func(line []byte) string {
		if !bytes.Contains(line, []byte(`"response_item"`)) {
			return ""
		}
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				Type  string `json:"type"`
				Model string `json:"model"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return ""
		}
		if entry.Type == "response_item" && entry.Payload.Type == "response" {
			return entry.Payload.Model
		}
		return ""
	})
}

// parseJSONLTimestamp parses an ISO 8601 UTC timestamp from a JSONL entry,
// trying RFC3339Nano first then RFC3339.
func parseJSONLTimestamp(ts string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// claudeScanSessionTokens aggregates token usage from Claude Code assistant
// turns in the session JSONL after afterTimestamp (RFC3339; empty = all).
func claudeScanSessionTokens(home, projectPath, sessionID, afterTimestamp string) SessionTokenMetrics {
	path := claudeSessionJSONLPath(home, projectPath, sessionID)
	f, err := os.Open(path)
	if err != nil {
		return SessionTokenMetrics{}
	}
	defer f.Close()

	var after time.Time
	if afterTimestamp != "" {
		after, _ = time.Parse(time.RFC3339, afterTimestamp)
	}

	var m SessionTokenMetrics
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var entry struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Usage struct {
					InputTokens              int `json:"input_tokens"`
					OutputTokens             int `json:"output_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil || entry.Type != "assistant" {
			continue
		}
		if !after.IsZero() {
			if ts, ok := parseJSONLTimestamp(entry.Timestamp); ok && !ts.After(after) {
				continue
			}
		}
		m.InputTokens += entry.Message.Usage.InputTokens
		m.OutputTokens += entry.Message.Usage.OutputTokens
		m.CacheReadTokens += entry.Message.Usage.CacheReadInputTokens
		m.CacheCreationTokens += entry.Message.Usage.CacheCreationInputTokens
		m.MessageCount++
	}
	if total := m.CacheReadTokens + m.CacheCreationTokens; total > 0 {
		m.CacheHitRate = float64(m.CacheReadTokens) / float64(total)
	}
	return m
}

// codexScanSessionTokens aggregates token usage from Codex token_count events
// in the session JSONL after afterTimestamp (RFC3339; empty = all).
// Uses last_token_usage (per-turn delta), not total_token_usage (cumulative).
// Guards against nil info fields — the first event in a session sometimes
// omits them.
func codexScanSessionTokens(home, sessionID, afterTimestamp string) SessionTokenMetrics {
	path := findCodexSessionFile(home, sessionID)
	if path == "" {
		return SessionTokenMetrics{}
	}
	f, err := os.Open(path)
	if err != nil {
		return SessionTokenMetrics{}
	}
	defer f.Close()

	var after time.Time
	if afterTimestamp != "" {
		after, _ = time.Parse(time.RFC3339, afterTimestamp)
	}

	var m SessionTokenMetrics
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"token_count"`)) {
			continue
		}
		var entry struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type string `json:"type"`
				Info *struct {
					LastTokenUsage *struct {
						InputTokens           int `json:"input_tokens"`
						OutputTokens          int `json:"output_tokens"`
						CachedInputTokens     int `json:"cached_input_tokens"`
						ReasoningOutputTokens int `json:"reasoning_output_tokens"`
					} `json:"last_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "event_msg" || entry.Payload.Type != "token_count" {
			continue
		}
		if entry.Payload.Info == nil || entry.Payload.Info.LastTokenUsage == nil {
			continue
		}
		if !after.IsZero() {
			if ts, ok := parseJSONLTimestamp(entry.Timestamp); ok && !ts.After(after) {
				continue
			}
		}
		u := entry.Payload.Info.LastTokenUsage
		m.InputTokens += u.InputTokens
		m.OutputTokens += u.OutputTokens
		m.CacheReadTokens += u.CachedInputTokens
		m.ReasoningTokens += u.ReasoningOutputTokens
		m.MessageCount++
	}
	return m
}
