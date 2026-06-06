package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

// bundlePromptFileRef is a source-aware verifier prompt file surfaced on a
// verifier stage. It mirrors config.VerifierPromptFile so the bundle stage list
// tells the worker which prompt file to load and from which source (p1c:
// source-aware verifier_profile.prompt_files).
type bundlePromptFileRef struct {
	Source  string `json:"source,omitempty"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

// bundleStageEntry is one stage in the expanded impl → verifier(s) → review chain for a bundle.
type bundleStageEntry struct {
	Stage        string `json:"stage"`
	VerifierType string `json:"verifier_type,omitempty"`
	// PromptFiles carries the resolved, source-aware prompt files for a verifier
	// stage when the bundle's project declares the matching verifier_profile.
	// Nil for impl/review stages and for verifier types with no profile entry.
	PromptFiles []bundlePromptFileRef `json:"prompt_files,omitempty"`
}

// expandBundleStages returns the ordered stage list for a bundle:
// impl, then one verifier entry per VerifierSequence element, then review.
//
// profiles, when non-nil, supplies the source-aware verifier_profiles registry
// resolved from the bundle's project .agentsrc.json; matching verifier stages
// are annotated with their typed prompt files. A nil registry leaves prompt
// files unresolved (text/JSON output still lists the stage ordering).
func expandBundleStages(b *delegationBundleYAML, profiles map[string]config.VerifierProfile) []bundleStageEntry {
	out := make([]bundleStageEntry, 0, len(b.Verification.VerifierSequence)+2)
	out = append(out, bundleStageEntry{Stage: "impl"})
	for _, vt := range b.Verification.VerifierSequence {
		vt = strings.TrimSpace(vt)
		if vt == "" {
			continue
		}
		entry := bundleStageEntry{Stage: "verifier", VerifierType: vt}
		if prof, ok := profiles[vt]; ok {
			entry.PromptFiles = promptFileRefs(prof.PromptFiles)
		}
		out = append(out, entry)
	}
	out = append(out, bundleStageEntry{Stage: "review"})
	return out
}

func promptFileRefs(files []config.VerifierPromptFile) []bundlePromptFileRef {
	if len(files) == 0 {
		return nil
	}
	refs := make([]bundlePromptFileRef, 0, len(files))
	for _, f := range files {
		refs = append(refs, bundlePromptFileRef{Source: f.Source, Path: f.Path, Version: f.Version})
	}
	return refs
}

// projectRootForBundle walks up from a bundle file path to the directory that
// contains .agentsrc.json. Bundles live under
// <root>/.agents/active/delegation-bundles/<id>.yaml, so the manifest is found
// a few levels up. Returns "" when no manifest is found (e.g. a fixture written
// to a bare temp dir) — callers then skip prompt-file resolution.
func projectRootForBundle(bundlePath string) string {
	dir, err := filepath.Abs(bundlePath)
	if err != nil {
		dir = bundlePath
	}
	dir = filepath.Dir(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, config.AgentsRCFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// loadVerifierProfiles loads the source-aware verifier_profiles registry from
// the project that owns the bundle. A missing project root or manifest is not an
// error — it yields a nil registry and prompt-file resolution is skipped.
func loadVerifierProfiles(bundlePath string) (map[string]config.VerifierProfile, error) {
	root := projectRootForBundle(bundlePath)
	if root == "" {
		return nil, nil
	}
	rc, err := config.LoadAgentsRC(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load %s for bundle prompt files: %w", config.AgentsRCFile, err)
	}
	return rc.VerifierProfiles, nil
}

// runWorkflowBundleStages reads a bundle YAML and prints or encodes the ordered stage list.
// Text output (one per line): "impl", "verifier:<type>", "review". When a
// verifier stage has resolved source-aware prompt files they are appended as
// "verifier:<type>\t<source/>path@version, …".
// JSON output: array of bundleStageEntry (with prompt_files when resolved).
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
	profiles, err := loadVerifierProfiles(bundlePath)
	if err != nil {
		return err
	}
	stages := expandBundleStages(&b, profiles)
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stages)
	}
	for _, s := range stages {
		if s.VerifierType == "" {
			fmt.Fprintln(os.Stdout, s.Stage)
			continue
		}
		if len(s.PromptFiles) == 0 {
			fmt.Fprintf(os.Stdout, "verifier:%s\n", s.VerifierType)
			continue
		}
		fmt.Fprintf(os.Stdout, "verifier:%s\t%s\n", s.VerifierType, formatPromptFileRefs(s.PromptFiles))
	}
	return nil
}

func formatPromptFileRefs(refs []bundlePromptFileRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Path
		if r.Source != "" {
			s = r.Source + "/" + r.Path
		}
		if r.Version != "" {
			s += "@" + r.Version
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
