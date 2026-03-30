package platform

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

type resourceDir struct {
	Name string
	Dir  string
	File string
}

func scopedNames(project string) []string {
	return []string{project, "global"}
}

func resolveScopedFile(agentsHome, bucket, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, name := range names {
			src := filepath.Join(agentsHome, bucket, scope, name)
			if _, err := os.Stat(src); err == nil {
				return src
			}
		}
	}
	return ""
}

func resolveScopedFileFromBuckets(agentsHome string, buckets []string, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, bucket := range buckets {
			for _, name := range names {
				src := filepath.Join(agentsHome, bucket, scope, name)
				if _, err := os.Stat(src); err == nil {
					return src
				}
			}
		}
	}
	return ""
}

func listScopedResourceDirs(agentsHome, bucket, scope, marker string) ([]resourceDir, error) {
	root := filepath.Join(agentsHome, bucket, scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	out := []resourceDir{}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !links.IsDirEntry(dir) {
			continue
		}
		markerPath := filepath.Join(dir, marker)
		if _, err := os.Stat(markerPath); err != nil {
			continue
		}
		out = append(out, resourceDir{
			Name: e.Name(),
			Dir:  dir,
			File: markerPath,
		})
	}
	return out, nil
}

func syncScopedDirSymlinks(agentsHome, bucket, scope, marker, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, bucket, scope, marker)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := links.Symlink(entry.Dir, filepath.Join(dstRoot, entry.Name)); err != nil {
			return err
		}
	}
	return nil
}

func syncScopedFileSymlinks(agentsHome, bucket, scope, marker, dstRoot, suffix string) error {
	entries, err := listScopedResourceDirs(agentsHome, bucket, scope, marker)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := links.Symlink(entry.File, filepath.Join(dstRoot, entry.Name+suffix)); err != nil {
			return err
		}
	}
	return nil
}

func readFrontmatter(mdPath string) map[string]string {
	f, err := os.Open(mdPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	inFrontmatter := false
	out := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum == 1 {
			if strings.TrimSpace(line) != "---" {
				return out
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			return out
		}
		if strings.TrimSpace(line) == "---" {
			return out
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}
