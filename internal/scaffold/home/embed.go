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
//go:embed starter starter/** starter/.gitignore
var embedded embed.FS

// CopyMissingStarterAssets copies embedded starter home assets into dstRoot.
// Existing files are preserved and never overwritten.
func CopyMissingStarterAssets(dstRoot string) error {
	return fs.WalkDir(embedded, "starter", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("starter", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstRoot, rel)
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
	})
}
