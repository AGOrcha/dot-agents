package hooks

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// embedded contains the starter canonical workflow hook bundles scaffolded by init.
//
//go:embed global/**
var embedded embed.FS

// CopyMissingGlobalBundles copies embedded global hook bundles into dstRoot.
// Existing bundle directories are preserved and never overwritten.
func CopyMissingGlobalBundles(dstRoot string) error {
	entries, err := fs.ReadDir(embedded, "global")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		dstBundle := filepath.Join(dstRoot, name)
		if _, err := os.Stat(dstBundle); err == nil {
			continue
		}
		if err := copyEmbeddedTree(filepath.Join("global", name), dstBundle); err != nil {
			return err
		}
	}
	return nil
}

func copyEmbeddedTree(srcRoot, dstRoot string) error {
	return fs.WalkDir(embedded, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dstPath := dstRoot
		if rel != "." {
			dstPath = filepath.Join(dstRoot, rel)
		}
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		content, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(d.Name(), ".sh") {
			mode = 0755
		}
		return os.WriteFile(dstPath, content, mode)
	})
}
