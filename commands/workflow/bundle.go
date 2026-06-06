package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
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

// verifierProfilePromptRefs returns the ordered, source-aware reference strings
// for a verifier profile's prompt files. Each ref is the canonical
// "source-id:path[@version]" form (config-distribution-model §5); a repo-local
// entry collapses to its bare path. Blank-path entries are skipped so a partially
// migrated profile still yields a clean list for the verifier worker.
func verifierProfilePromptRefs(p config.VerifierProfile) []string {
	out := make([]string, 0, len(p.PromptFiles))
	for _, pf := range p.PromptFiles {
		if strings.TrimSpace(pf.Path) == "" {
			continue
		}
		out = append(out, pf.Ref())
	}
	return out
}

// resolveVerifierStagePromptRefs looks up the verifier profile named by a stage's
// verifier_type and returns its source-aware prompt refs. It errors when the
// profile is not declared under verifier_profiles, mirroring the fanout-time
// validation so a bundle stage cannot reference an undefined verifier.
func resolveVerifierStagePromptRefs(rc *config.AgentsRC, verifierType string) ([]string, error) {
	verifierType = strings.TrimSpace(verifierType)
	if verifierType == "" {
		return nil, fmt.Errorf("empty verifier_type")
	}
	if rc == nil {
		return nil, fmt.Errorf("verifier profile %q is not defined under verifier_profiles", verifierType)
	}
	profile, ok := rc.VerifierProfiles[verifierType]
	if !ok {
		return nil, fmt.Errorf("verifier profile %q is not defined under verifier_profiles", verifierType)
	}
	return verifierProfilePromptRefs(profile), nil
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
