package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// bundleStageEntry is one stage in the expanded impl → verifier(s) → review chain for a bundle.
type bundleStageEntry struct {
	Stage        string `json:"stage"`
	VerifierType string `json:"verifier_type,omitempty"`
}

// expandBundleStages returns the ordered stage list for a bundle:
// impl, then one verifier entry per VerifierSequence element, then review.
func expandBundleStages(b *delegationBundleYAML) []bundleStageEntry {
	out := make([]bundleStageEntry, 0, len(b.Verification.VerifierSequence)+2)
	out = append(out, bundleStageEntry{Stage: "impl"})
	for _, vt := range b.Verification.VerifierSequence {
		vt = strings.TrimSpace(vt)
		if vt != "" {
			out = append(out, bundleStageEntry{Stage: "verifier", VerifierType: vt})
		}
	}
	out = append(out, bundleStageEntry{Stage: "review"})
	return out
}

// runWorkflowBundleStages reads a bundle YAML and prints or encodes the ordered stage list.
// Text output (one per line): "impl", "verifier:<type>", "review".
// JSON output: array of bundleStageEntry.
func runWorkflowBundleStages(bundlePath string) error {
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return fmt.Errorf("read bundle %s: %w", bundlePath, err)
	}
	var b delegationBundleYAML
	if err := yaml.Unmarshal(data, &b); err != nil {
		return fmt.Errorf("parse bundle %s: %w", bundlePath, err)
	}
	if strings.TrimSpace(b.TaskID) == "" {
		return fmt.Errorf("bundle %s: missing task_id", bundlePath)
	}
	stages := expandBundleStages(&b)
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stages)
	}
	for _, s := range stages {
		if s.VerifierType != "" {
			fmt.Fprintf(os.Stdout, "verifier:%s\n", s.VerifierType)
		} else {
			fmt.Fprintln(os.Stdout, s.Stage)
		}
	}
	return nil
}
