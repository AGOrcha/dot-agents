package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RuleFileSpec describes one canonical rule file under ~/.agents/rules/<scope>/.
type RuleFileSpec struct {
	Scope      string
	BaseName   string // full file name, e.g. rules.mdc
	SourcePath string
}

func isCanonicalRuleFileName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mdc", ".md", ".txt":
		return true
	default:
		return false
	}
}

// ListCanonicalRuleFiles returns non-directory rule files under ~/.agents/rules/<scope>/,
// sorted by basename. Subdirectories are ignored. If the scope directory is missing,
// the error satisfies os.IsNotExist.
func ListCanonicalRuleFiles(agentsHome, scope string) ([]RuleFileSpec, error) {
	root := filepath.Join(agentsHome, "rules", scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []RuleFileSpec
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isCanonicalRuleFileName(name) {
			continue
		}
		out = append(out, RuleFileSpec{
			Scope:      scope,
			BaseName:   name,
			SourcePath: filepath.Join(root, name),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].BaseName) < strings.ToLower(out[j].BaseName)
	})
	return out, nil
}

// ResolveCanonicalRuleFile finds a rule file by scope and name. Name may be a full
// basename (e.g. rules.mdc) or a stem; stems try .mdc, .md, .txt in order.
func ResolveCanonicalRuleFile(agentsHome, scope, name string) (*RuleFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("rule name is empty")
	}
	root := filepath.Join(agentsHome, "rules", scope)
	candidates := []string{name}
	if !strings.Contains(name, ".") {
		for _, ext := range []string{".mdc", ".md", ".txt"} {
			candidates = append(candidates, name+ext)
		}
	}
	for _, cand := range candidates {
		p := filepath.Join(root, cand)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isCanonicalRuleFileName(cand) {
			return &RuleFileSpec{
				Scope:      scope,
				BaseName:   cand,
				SourcePath: p,
			}, nil
		}
	}
	return nil, fmt.Errorf("rule not found: %s / %s", scope, name)
}

// EnsureUnderRulesScopeTree checks that target is under agentsHome/rules/scope (after clean).
func EnsureUnderRulesScopeTree(agentsHome, scope, target string) error {
	root := filepath.Join(agentsHome, "rules", scope)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to touch path outside %s", cleanRoot)
	}
	return nil
}
