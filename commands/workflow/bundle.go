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

// bundlePromptFile is one source-aware prompt_files entry carried into a worker
// bundle. It preserves the typed-object provenance (config-v2 Q1, Option B): a
// legacy bare-string entry resolves to {Path:<string>} with an empty Source and
// Version, while a typed entry keeps the source it is pinned to and any version.
type bundlePromptFile struct {
	Source  string `json:"source,omitempty" yaml:"source,omitempty"`
	Path    string `json:"path" yaml:"path"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// bundlePromptFilesFromRefs converts a verifier/reviewer profile's source-aware
// prompt_files (config.PromptFileRef) into the bundle's prompt-file carrier,
// dropping entries with an empty path so a malformed profile never injects a
// blank prompt into the worker bundle. Order is preserved (profiles compose
// base-first).
func bundlePromptFilesFromRefs(refs []config.PromptFileRef) []bundlePromptFile {
	out := make([]bundlePromptFile, 0, len(refs))
	for _, r := range refs {
		path := strings.TrimSpace(r.Path)
		if path == "" {
			continue
		}
		out = append(out, bundlePromptFile{
			Source:  strings.TrimSpace(r.Source),
			Path:    path,
			Version: strings.TrimSpace(r.Version),
		})
	}
	return out
}

// flattenBundlePromptPaths reduces source-aware prompt files to the flat path
// list the bundle's prompt.prompt_files field accepts. This is the bridge from
// the typed source-aware model to the existing []string bundle surface: the
// source/version provenance is retained on bundlePromptFile for callers that
// fetch from a config source, while consumers that only resolve a local prompt
// path (the legacy resolver) see the plain path list they already expect.
func flattenBundlePromptPaths(files []bundlePromptFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if p := strings.TrimSpace(f.Path); p != "" {
			out = append(out, p)
		}
	}
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
