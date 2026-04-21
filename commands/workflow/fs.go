package workflow

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// copyWorkflowArtifact copies a single file from src to dst, creating parent dirs as needed.
func copyWorkflowArtifact(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// copyWorkflowDir recursively copies srcDir into dstDir.
func copyWorkflowDir(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyWorkflowArtifact(path, target)
	})
}

// mergeWorkflowPlanDir merges a plan source directory into a history destination directory.
// Stub — implemented in p2-archive-handler.
func mergeWorkflowPlanDir(planID, srcDir, dstDir string, dryRun bool) error {
	return nil
}
