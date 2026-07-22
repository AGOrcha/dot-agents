package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Proposal struct {
	SchemaVersion int      `yaml:"schema_version"`
	ID            string   `yaml:"id"`
	Status        string   `yaml:"status"`
	Type          string   `yaml:"type"`
	Action        string   `yaml:"action"`
	Target        string   `yaml:"target"`
	WriteScope    []string `yaml:"write_scope,omitempty"`
	Rationale     string   `yaml:"rationale"`
	Content       string   `yaml:"content"`
	CreatedAt     string   `yaml:"created_at"`
	CreatedBy     string   `yaml:"created_by"`
	ReviewedAt    string   `yaml:"reviewed_at"`
	ReviewReason  string   `yaml:"review_reason"`
}

var (
	ErrProposalNotFound      = errors.New("proposal not found")
	ErrInvalidProposalTarget = errors.New("invalid proposal target")
)

func ProposalsDir() string {
	return filepath.Join(AgentsHome(), "proposals")
}

func ArchivedProposalsDir() string {
	return filepath.Join(ProposalsDir(), "archived")
}

func ProposalPath(id string) string {
	return filepath.Join(ProposalsDir(), id+".yaml")
}

func ArchivedProposalPath(id string) string {
	return filepath.Join(ArchivedProposalsDir(), id+".yaml")
}

func LoadProposal(id string) (*Proposal, error) {
	path := ProposalPath(id)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrProposalNotFound
		}
		return nil, err
	}
	var proposal Proposal
	if err := yaml.Unmarshal(content, &proposal); err != nil {
		return nil, fmt.Errorf("parse proposal %s: %w", id, err)
	}
	return &proposal, nil
}

func ListPendingProposals() ([]Proposal, error) {
	dir := ProposalsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// On Windows, ReadDir of a path that exists but is a regular
			// file returns a NotExist-class error that os.IsNotExist would
			// wrongly swallow (POSIX returns ENOTDIR, which does not).
			// Only a genuinely-absent dir means "no pending proposals".
			// %v (not %w) keeps this off the fs.ErrNotExist chain.
			if _, statErr := os.Lstat(dir); statErr == nil {
				return nil, fmt.Errorf("read proposals dir %s: %v", dir, err)
			}
			return nil, nil
		}
		return nil, err
	}

	proposals := make([]Proposal, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		proposal, err := loadProposalByPath(filepath.Join(ProposalsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		if proposal.Status == "pending" {
			proposals = append(proposals, *proposal)
		}
	}
	sort.Slice(proposals, func(i, j int) bool {
		return proposals[i].ID < proposals[j].ID
	})
	return proposals, nil
}

func SaveProposal(proposal *Proposal, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content, err := yaml.Marshal(proposal)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

func ValidateProposal(proposal *Proposal) error {
	if proposal.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if strings.TrimSpace(proposal.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if !containsString([]string{"draft", "pending", "approved", "rejected"}, proposal.Status) {
		return fmt.Errorf("invalid status %q", proposal.Status)
	}
	if !containsString([]string{"rule", "skill", "hook", "setting"}, proposal.Type) {
		return fmt.Errorf("invalid type %q", proposal.Type)
	}
	if !containsString([]string{"add", "modify", "remove"}, proposal.Action) {
		return fmt.Errorf("invalid action %q", proposal.Action)
	}
	if err := ValidateProposalTarget(proposal.Target); err != nil {
		return err
	}
	if err := validateProposalWriteScope(proposal.WriteScope); err != nil {
		return err
	}
	if !proposalTargetInScope(proposal.Target, proposal.WriteScope) {
		return fmt.Errorf("target %q does not route into any write_scope entry %v", proposal.Target, proposal.WriteScope)
	}
	if strings.TrimSpace(proposal.Rationale) == "" {
		return fmt.Errorf("rationale is required")
	}
	if err := validateProposalContent(proposal); err != nil {
		return err
	}
	if strings.TrimSpace(proposal.CreatedAt) == "" {
		return fmt.Errorf("created_at is required")
	}
	if strings.TrimSpace(proposal.CreatedBy) == "" {
		return fmt.Errorf("created_by is required")
	}
	return nil
}

// validateProposalContent enforces the action-specific content rule: add/modify
// require content; remove requires content to be empty.
func validateProposalContent(proposal *Proposal) error {
	switch proposal.Action {
	case "add", "modify":
		if proposal.Content == "" {
			return fmt.Errorf("content is required for %s", proposal.Action)
		}
	case "remove":
		if strings.TrimSpace(proposal.Content) != "" {
			return fmt.Errorf("content must be empty for remove")
		}
	}
	return nil
}

func ValidateProposalTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("%w: target is required", ErrInvalidProposalTarget)
	}
	// Proposal targets are logical, forward-slash, repo-relative paths.
	// filepath.IsAbs is OS-dependent (a leading "/" is not "absolute" on
	// Windows, and "\foo" / "C:foo" only matter there), so check rootedness
	// with slash semantics on every OS instead of relying on it.
	slashed := strings.ReplaceAll(target, `\`, "/")
	if filepath.IsAbs(target) || filepath.VolumeName(target) != "" || strings.HasPrefix(slashed, "/") {
		return fmt.Errorf("%w: absolute paths are not allowed", ErrInvalidProposalTarget)
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == "" {
		return fmt.Errorf("%w: target is required", ErrInvalidProposalTarget)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w: parent-directory traversal is not allowed", ErrInvalidProposalTarget)
	}
	return nil
}

// isWellFormedProposalPath reports whether p is a well-formed, AgentsHome-relative
// proposal path: non-empty after trim, no leading slash / volume, no `..`
// traversal. Namespace-agnostic well-formedness — the SAME namespace as
// Proposal.Target (which ProposalTargetPath joins onto AgentsHome()).
func isWellFormedProposalPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" || strings.HasPrefix(slashed, "/") {
		return false
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return false
		}
	}
	clean := path.Clean(slashed)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// validateProposalWriteScope enforces that every declared scope entry is a
// well-formed AgentsHome-relative path. An empty scope is valid (unconstrained,
// preserving pre-scope behavior).
func validateProposalWriteScope(scope []string) error {
	for i, s := range scope {
		if !isWellFormedProposalPath(s) {
			return fmt.Errorf("write_scope[%d]=%q must be an AgentsHome-relative path (no leading slash, no `..` traversal)", i, s)
		}
	}
	return nil
}

// proposalTargetInScope reports whether target routes into one of the declared
// scope entries via clean-path prefix (target == entry, or target under
// entry+"/"), compared in the shared AgentsHome-relative namespace. An empty
// scope is unconstrained. A target that does not resolve to a well-formed
// AgentsHome-relative path (e.g. a non-file/non-path scheme) is treated as
// scope-inapplicable — never rejected on scope grounds.
func proposalTargetInScope(target string, scope []string) bool {
	if len(scope) == 0 {
		return true
	}
	if !isWellFormedProposalPath(target) {
		return true
	}
	t := path.Clean(strings.ReplaceAll(strings.TrimSpace(target), `\`, "/"))
	for _, s := range scope {
		c := path.Clean(strings.ReplaceAll(strings.TrimSpace(s), `\`, "/"))
		if t == c || strings.HasPrefix(t, c+"/") {
			return true
		}
	}
	return false
}

func ProposalTargetPath(target string) (string, error) {
	if err := ValidateProposalTarget(target); err != nil {
		return "", err
	}
	return filepath.Join(AgentsHome(), filepath.Clean(target)), nil
}

func ApplyProposal(proposal *Proposal) error {
	targetPath, err := ProposalTargetPath(proposal.Target)
	if err != nil {
		return err
	}
	switch proposal.Action {
	case "add", "modify":
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, []byte(proposal.Content), 0644)
	case "remove":
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported action %q", proposal.Action)
	}
}

func ArchiveProposal(proposal *Proposal) error {
	src := ProposalPath(proposal.ID)
	dst := ArchivedProposalPath(proposal.ID)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := SaveProposal(proposal, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func MarkProposalReviewed(proposal *Proposal, status, reason string) {
	proposal.Status = status
	proposal.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
	proposal.ReviewReason = reason
}

func loadProposalByPath(path string) (*Proposal, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var proposal Proposal
	if err := yaml.Unmarshal(content, &proposal); err != nil {
		return nil, fmt.Errorf("parse proposal %s: %w", filepath.Base(path), err)
	}
	return &proposal, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
