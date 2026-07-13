package home

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// embedded contains starter shared-home assets scaffolded by `da init`.
//
//go:embed starter starter/** starter/.gitignore starter/.agentsrc.json
var embedded embed.FS

// copyStarterEntry copies a single embedded starter entry to dstRoot.
// Directories are created; files are only written if they do not already exist.
func copyStarterEntry(dstRoot, path string, d fs.DirEntry) error {
	// path is a forward-slash embed.FS path; filepath.Rel would mishandle
	// the separators on Windows. Trim the forward-slash root, then convert
	// to an OS path for the real destination.
	rel := strings.TrimPrefix(strings.TrimPrefix(path, "starter"), "/")
	if rel == "" {
		return nil
	}
	dstPath := filepath.Join(dstRoot, filepath.FromSlash(rel))
	if d.IsDir() {
		return os.MkdirAll(dstPath, 0755)
	}
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
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
}

// CopyMissingStarterAssets copies embedded starter home assets into dstRoot.
// Existing files are preserved and never overwritten.
func CopyMissingStarterAssets(dstRoot string) error {
	return fs.WalkDir(embedded, "starter", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return copyStarterEntry(dstRoot, path, d)
	})
}
