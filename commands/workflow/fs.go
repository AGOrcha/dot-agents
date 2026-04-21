package workflow

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// isDMAFile returns true for files that are managed by the delegation lifecycle
// (delegation contracts, merge-backs, closeouts, verification results). These are
// always skipped during plan archive merges to avoid overwriting history DMA artifacts.
func isDMAFile(relPath string) bool {
	base := filepath.Base(relPath)
	// DMA files by base name
	switch base {
	case "delegation.yaml", "merge-back.md", "closeout.yaml":
		return true
	}
	// DMA directories in path
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		switch seg {
		case "delegate-merge-back-archive", "delegation", "merge-back", "fold-back", "verification":
			return true
		}
	}
	return false
}

// isCanonicalPlanFile returns true for the three files that always overwrite in a merge.
func isCanonicalPlanFile(relPath, planID string) bool {
	switch relPath {
	case "PLAN.yaml", "TASKS.yaml", planID+".plan.md":
		return true
	}
	return false
}

// sha256File returns the SHA-256 hash of the named file.
func sha256File(path string) ([32]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// mergeWorkflowPlanDir merges a plan source directory into a history destination directory.
//
// Rules (applied per file):
//   - DMA artifacts (delegation.yaml, merge-back.md, closeout.yaml, delegate-merge-back-archive/)
//     → always skip.
//   - PLAN.yaml, TASKS.yaml, <planID>.plan.md → always overwrite.
//   - All other files → sha256 compare:
//     identical hash → skip (no-op).
//     source is newer or hashes differ → overwrite.
//     destination is newer → skip + log warning.
//
// If dstDir does not exist, os.Rename is used as a fast path (no walk needed).
// On RemoveAll failure after a successful merge, retries once automatically.
// In dry-run mode no filesystem changes are made; per-file decisions are printed.
func mergeWorkflowPlanDir(planID, srcDir, dstDir string, dryRun bool) error {
	// Fast path: destination does not exist — rename is atomic and cheap.
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if dryRun {
			fmt.Printf("  [dry-run] rename %s → %s\n", srcDir, dstDir)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
			return fmt.Errorf("create history parent: %w", err)
		}
		return os.Rename(srcDir, dstDir)
	}

	// Walk the source and apply merge rules.
	if err := filepath.WalkDir(srcDir, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if dryRun {
				return nil
			}
			return os.MkdirAll(filepath.Join(dstDir, rel), 0755)
		}

		dstPath := filepath.Join(dstDir, rel)

		// Rule 1: DMA artifacts — always skip.
		if isDMAFile(rel) {
			if dryRun {
				fmt.Printf("  [dry-run] skip (dma)      %s\n", rel)
			}
			return nil
		}

		// Rule 2: Canonical plan files — always overwrite.
		if isCanonicalPlanFile(rel, planID) {
			if dryRun {
				fmt.Printf("  [dry-run] overwrite (canonical) %s\n", rel)
				return nil
			}
			return copyWorkflowArtifact(srcPath, dstPath)
		}

		// Rule 3: All other files — sha256 + mtime compare.
		srcHash, err := sha256File(srcPath)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}

		dstStat, dstStatErr := os.Stat(dstPath)
		if dstStatErr != nil && !os.IsNotExist(dstStatErr) {
			return fmt.Errorf("stat dst %s: %w", rel, dstStatErr)
		}

		if dstStatErr == nil {
			dstHash, err := sha256File(dstPath)
			if err != nil {
				return fmt.Errorf("hash dst %s: %w", rel, err)
			}
			if srcHash == dstHash {
				// Identical — skip.
				if dryRun {
					fmt.Printf("  [dry-run] skip (identical) %s\n", rel)
				}
				return nil
			}
			// Check mtime.
			srcStat, err := os.Stat(srcPath)
			if err != nil {
				return fmt.Errorf("stat src %s: %w", rel, err)
			}
			if dstStat.ModTime().After(srcStat.ModTime()) {
				// History is newer — warn and skip.
				fmt.Printf("  warn: history file is newer than source, skipping %s\n", rel)
				if dryRun {
					fmt.Printf("  [dry-run] skip (history newer) %s\n", rel)
				}
				return nil
			}
		}

		// Overwrite (source is newer or different, or dst absent).
		if dryRun {
			fmt.Printf("  [dry-run] overwrite %s\n", rel)
			return nil
		}
		return copyWorkflowArtifact(srcPath, dstPath)
	}); err != nil {
		return err
	}

	return nil
}

// removeAllWithRetry calls os.RemoveAll and retries once on failure.
func removeAllWithRetry(path string) error {
	if err := os.RemoveAll(path); err != nil {
		time.Sleep(50 * time.Millisecond)
		return os.RemoveAll(path)
	}
	return nil
}
