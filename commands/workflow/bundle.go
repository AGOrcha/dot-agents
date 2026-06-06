package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// localPromptSource is the well-known source id for repo-local prompt files.
// A legacy bare-string prompt_files entry migrates to this source.
const localPromptSource = "local"

// promptFile is one entry in a verifierProfile's prompt_files list. It is the
// source-aware successor to the legacy flat string form: a prompt file may now
// declare which config source it is fetched from and at what version, mirroring
// the "source-id:path@version" reference model used by extends/packages
// (config-distribution-model §5).
//
// Migration (Q1 ruling Option B — typed objects everywhere): a legacy bare
// string entry decodes to a repo-local promptFile (Source=="local", Path==the
// string). The typed object form is:
//
//	{"source": "acme", "path": "verifiers/unit.project.md", "version": "1.2.0"}
type promptFile struct {
	// Source is the source-id the prompt file is resolved from. Empty or
	// "local" means the repo-local working tree (the legacy behaviour).
	Source string `json:"source,omitempty"`
	// Path is the prompt file path, repo-relative for local sources or
	// layer-relative for remote sources.
	Path string `json:"path"`
	// Version is the optional version spec applied to remote sources.
	Version string `json:"version,omitempty"`
}

// isLocal reports whether the prompt file resolves from the repo-local working
// tree (the default when Source is empty or the well-known "local" id).
func (p promptFile) isLocal() bool {
	return p.Source == "" || p.Source == localPromptSource
}

// ref renders the canonical "source-id:path@version" reference string. Local
// prompt files render as the bare path so they round-trip to the legacy form
// existing bundle consumers (prompt.prompt_files) expect.
func (p promptFile) ref() string {
	if p.isLocal() {
		return p.Path
	}
	out := p.Source + ":" + p.Path
	if p.Version != "" {
		out += "@" + p.Version
	}
	return out
}

// promptFileWire is the typed object on-disk shape, shared by Marshal/Unmarshal.
type promptFileWire struct {
	Source  string `json:"source,omitempty"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

// MarshalJSON emits the compact string form for repo-local entries with no
// version (preserving the legacy on-disk shape byte-for-byte) and the typed
// object form otherwise. Round-trip is stable under repeated marshal/unmarshal.
func (p promptFile) MarshalJSON() ([]byte, error) {
	if p.isLocal() && p.Version == "" {
		return json.Marshal(p.Path)
	}
	return json.Marshal(promptFileWire{Source: p.Source, Path: p.Path, Version: p.Version})
}

// UnmarshalJSON accepts either a legacy bare string (migrated to a local
// promptFile) or the typed object form {source, path, version}.
func (p *promptFile) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.Source = localPromptSource
		p.Path = s
		p.Version = ""
		return nil
	}
	var w promptFileWire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("prompt_files entry must be a string or {source?,path,version?}: %w", err)
	}
	if strings.TrimSpace(w.Path) == "" {
		return fmt.Errorf("prompt_files entry object form requires non-empty path")
	}
	p.Source = w.Source
	p.Path = w.Path
	p.Version = w.Version
	return nil
}

// verifierProfile is a named verifier profile referenced by app_type_verifier_map
// and workflow fanout. PromptFiles is source-aware per the Q1 ruling (typed
// objects everywhere); legacy flat string lists migrate transparently via
// promptFile.UnmarshalJSON.
type verifierProfile struct {
	// Label is the human-readable profile name surfaced in fanout output.
	Label string `json:"label,omitempty"`
	// PromptFiles lists the prompt files appended to the verifier's brief.
	PromptFiles []promptFile `json:"prompt_files,omitempty"`
}

// parseVerifierProfiles decodes a raw verifier_profiles JSON object into the
// typed map. It is the single migration seam from the on-disk shape (which may
// carry legacy flat prompt_files) to the typed verifierProfile shape the fanout
// path operates on. A JSON null or empty input decodes to a nil map.
func parseVerifierProfiles(raw json.RawMessage) (map[string]verifierProfile, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]verifierProfile
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse verifier_profiles: %w", err)
	}
	return out, nil
}

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
// profile contributes no usable prompt files.
func resolveVerifierPromptFiles(profile verifierProfile) []string {
	if len(profile.PromptFiles) == 0 {
		return nil
	}
	out := make([]string, 0, len(profile.PromptFiles))
	for _, pf := range profile.PromptFiles {
		if strings.TrimSpace(pf.Path) == "" {
			continue
		}
		out = append(out, pf.ref())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveSequencePromptFiles decodes the raw verifier_profiles manifest entry
// and gathers, in sequence order, the source-aware prompt file references for
// every verifier id in the bundle's verifier_sequence. Unknown ids contribute
// nothing (reference validation lives in fanout). Duplicate references across
// profiles are de-duplicated while preserving first-seen order so the bundle's
// prompt.prompt_files list stays stable. The migration from legacy flat string
// prompt_files happens transparently inside ParseVerifierProfiles.
func resolveSequencePromptFiles(rawProfiles json.RawMessage, sequence []string) ([]string, error) {
	profiles, err := parseVerifierProfiles(rawProfiles)
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
