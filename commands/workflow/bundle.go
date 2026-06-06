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

// resolveVerifierPromptFiles renders a verifier profile's source-aware
// prompt_files into the flat reference list the delegation bundle carries under
// prompt.prompt_files. Repo-local entries (the migrated legacy form) render as
// their bare path so existing bundle consumers see the same value they did
// before prompt_files became typed; remote entries render in the canonical
// "source-id:path@version" form (config-distribution-model §5). Entries with an
// empty path are skipped defensively. The returned slice is nil when the
// profile declares no prompt files.
func resolveVerifierPromptFiles(profile config.VerifierProfile) []string {
	if len(profile.PromptFiles) == 0 {
		return nil
	}
	out := make([]string, 0, len(profile.PromptFiles))
	for _, pf := range profile.PromptFiles {
		if strings.TrimSpace(pf.Path) == "" {
			continue
		}
		out = append(out, pf.Ref())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveSequencePromptFiles decodes the raw verifier_profiles manifest entry
// and gathers, in sequence order, the source-aware prompt file references for
// every verifier id in the bundle's verifier_sequence. Unknown ids contribute
// nothing (validation of references lives in fanout). Duplicate references
// across profiles are de-duplicated while preserving first-seen order so the
// bundle's prompt.prompt_files list stays stable. The migration from legacy
// flat string prompt_files happens transparently inside ParseVerifierProfiles.
func resolveSequencePromptFiles(rawProfiles json.RawMessage, sequence []string) ([]string, error) {
	profiles, err := config.ParseVerifierProfiles(rawProfiles)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 || len(sequence) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, id := range sequence {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		profile, ok := profiles[id]
		if !ok {
			continue
		}
		for _, ref := range resolveVerifierPromptFiles(profile) {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out, nil
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
