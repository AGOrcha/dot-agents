// Package state persists the per-task watermark sidecars that give the
// `da service` background tasks restart safety (R3 design decision D3).
//
// Each task owns exactly one human-readable YAML sidecar at
// .agents/active/service-state/<task-name>.watermark.yaml. On startup a task
// reads its watermark; absent means "start from scratch". Writes are atomic
// (temp+rename via fsops.WriteFileAtomic, the same primitive the scoring
// sidecar writers use) so a crash never leaves a partial watermark behind.
//
// The package is deliberately shape-agnostic: each task defines its own
// watermark struct (e.g. the iter-log ingester's {last_iter_processed,
// last_mtime, rubric_version}) and hands it to Load/Save as an opaque value.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// watermarkSuffix is the filename suffix shared by every task watermark.
const watermarkSuffix = ".watermark.yaml"

// Path returns the canonical watermark sidecar path for taskName in the
// repository rooted at repoDir: .agents/active/service-state/<task>.watermark.yaml.
func Path(repoDir, taskName string) string {
	return filepath.Join(repoDir, ".agents", "active", "service-state", taskName+watermarkSuffix)
}

// Load reads the watermark at path into v. A missing file is not an error:
// it returns found=false with a nil error, leaving v untouched, which is the
// D3 "absent means start from scratch" contract. A blocked parent (ENOTDIR:
// a path component is a regular file) is also treated as absent — Windows
// already reports that case as not-exist, and the subsequent Save surfaces
// the broken hierarchy loudly. Read or parse failures are returned as errors
// so a corrupt watermark is surfaced instead of silently reprocessing from
// zero.
func Load(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("state: read watermark %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("state: parse watermark %s: %w", path, err)
	}
	return true, nil
}

// Save atomically persists v as YAML at path, creating the parent directory
// when missing. The temp+rename write guarantees a concurrent reader (or a
// crash) never observes a partial watermark.
func Save(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("state: marshal watermark %s: %w", path, err)
	}
	if err := fsops.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state: create state dir for %s: %w", path, err)
	}
	if err := fsops.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("state: write watermark %s: %w", path, err)
	}
	return nil
}
