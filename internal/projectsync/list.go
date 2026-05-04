package projectsync

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// BucketSpec describes a per-resource-type bucket under ~/.agents/<bucket>/ so
// list/promote helpers can be shared between agents and skills (and any future
// resource types that follow the same shape).
type BucketSpec struct {
	// Bucket is the directory name under ~/.agents/, e.g. "agents" or "skills".
	Bucket string
	// ManifestName is the filename that must exist inside each resource directory
	// to count as installed, e.g. "AGENT.md" or "SKILL.md".
	ManifestName string
	// Singular is the lowercase noun used in count output, e.g. "agent" or "skill".
	Singular string
	// Plural is the capitalized heading noun, e.g. "Agents" or "Skills".
	Plural string
}

// ListBucket prints resources under ~/.agents/<bucket>/<scope>/ following the
// shared layout: each resource is a directory containing the spec's manifest.
// Description is parsed from the manifest's YAML frontmatter when present.
func ListBucket(scope string, spec BucketSpec) error {
	agentsHome := config.AgentsHome()
	dir := filepath.Join(agentsHome, spec.Bucket, scope)

	entries, err := os.ReadDir(dir)
	if err != nil {
		ui.Info(fmt.Sprintf("No %ss found in ~/.agents/%s/%s/", spec.Singular, spec.Bucket, scope))
		return nil
	}

	ui.Header(fmt.Sprintf("%s (%s)", spec.Plural, scope))
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, e.Name(), spec.ManifestName)
		if _, err := os.Stat(manifestPath); err == nil {
			desc := ReadFrontmatterDescription(manifestPath)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no "+spec.ManifestName+")")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d %s(s) in %s scope%s\n\n", ui.Dim, count, spec.Singular, scope, ui.Reset)
	return nil
}

// ReadFrontmatterDescription parses the YAML frontmatter of a markdown file
// and returns the value of the "description:" field. Returns "" if the file
// cannot be opened, has no frontmatter, or has no description field.
func ReadFrontmatterDescription(mdPath string) string {
	f, err := os.Open(mdPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum == 1 {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
			} else {
				return ""
			}
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				break
			}
			if val, ok := strings.CutPrefix(line, "description:"); ok {
				val = strings.TrimSpace(val)
				val = strings.Trim(val, `"'`)
				return val
			}
		}
	}
	return ""
}
