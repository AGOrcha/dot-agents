package labels

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
	"go.yaml.in/yaml/v3"
)

// writeFileAtomic is the seam for atomically persisting the marshaled sidecar
// bytes. It defaults to fsops.WriteFileAtomic and is overridable in tests to
// drive the write-error branches (the atomic temp+rename primitive and its
// granular error paths are owned and tested by the fsops package).
var writeFileAtomic = fsops.WriteFileAtomic

// IterationLabelsPath returns the sidecar path for an iteration's labels: the
// iter-N.labels.yaml file adjacent to iter-N.yaml (and iter-N.score.yaml) in
// the iteration-log directory. It mirrors scoring.IterationScorePath.
func IterationLabelsPath(iterLogDir string, iteration int) string {
	return filepath.Join(iterLogDir, fmt.Sprintf("iter-%d.labels.yaml", iteration))
}

// ReadSidecar loads the labels sidecar for an iteration. A missing file is not
// an error: it returns an empty (but valid) sidecar so callers can treat
// "no labels yet" and "labels exist" uniformly. A present-but-corrupt or
// invalid file returns an error.
func ReadSidecar(iterLogDir string, iteration int) (Sidecar, error) {
	path := IterationLabelsPath(iterLogDir, iteration)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Sidecar{
			Iteration:     iteration,
			SchemaVersion: LabelSchemaVersion,
			Labels:        []Label{},
		}, nil
	}
	if err != nil {
		return Sidecar{}, fmt.Errorf("labels: read iter-%d sidecar: %w", iteration, err)
	}
	var sc Sidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return Sidecar{}, fmt.Errorf("labels: parse iter-%d sidecar: %w", iteration, err)
	}
	if sc.Labels == nil {
		sc.Labels = []Label{}
	}
	if err := sc.Validate(); err != nil {
		return Sidecar{}, fmt.Errorf("labels: invalid iter-%d sidecar: %w", iteration, err)
	}
	return sc, nil
}

// WriteSidecar persists a labels sidecar to its iteration path and returns the
// written path. The sidecar is validated before writing and the file is
// written atomically (temp file in the same directory renamed into place) so
// concurrent readers never see a partial write — mirroring scoring's
// writeYAMLAtomic. The iteration-log directory is assumed to exist (it holds
// the iter-N.yaml the labels comment on), as in the scoring sidecar writer.
func WriteSidecar(iterLogDir string, sc Sidecar) (string, error) {
	if err := sc.Validate(); err != nil {
		return "", fmt.Errorf("labels: refuse to write invalid sidecar: %w", err)
	}
	path := IterationLabelsPath(iterLogDir, sc.Iteration)
	if err := writeYAMLAtomic(path, sc); err != nil {
		return "", fmt.Errorf("labels: write iter-%d sidecar: %w", sc.Iteration, err)
	}
	return path, nil
}

// AddInput is the data a caller supplies to create a new label.
type AddInput struct {
	Actor         string
	Role          Role
	Structured    Structured
	FreeText      string
	AdminOverride bool
	// Now, if non-zero, fixes the timestamp (tests use this). Zero means
	// time.Now().UTC().
	Now time.Time
}

// Add creates a new label for an iteration and persists it. It reads the
// existing sidecar, appends a freshly-identified label whose history holds the
// original submission, validates, and writes atomically. The created label is
// returned.
func Add(iterLogDir string, iteration int, in AddInput) (Label, error) {
	sc, err := ReadSidecar(iterLogDir, iteration)
	if err != nil {
		return Label{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id, err := newID()
	if err != nil {
		return Label{}, err
	}
	edit := Edit{
		Actor:      in.Actor,
		Role:       in.Role,
		Timestamp:  now,
		Structured: in.Structured,
		FreeText:   in.FreeText,
	}
	label := Label{
		ID:            id,
		Iteration:     iteration,
		Actor:         in.Actor,
		Role:          in.Role,
		AdminOverride: in.AdminOverride,
		SchemaVersion: LabelSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
		History:       []Edit{edit},
	}
	if err := label.Validate(); err != nil {
		return Label{}, err
	}
	sc.Labels = append(sc.Labels, label)
	if _, err := WriteSidecar(iterLogDir, sc); err != nil {
		return Label{}, err
	}
	return label, nil
}

// EditInput is the data a caller supplies to edit an existing label.
type EditInput struct {
	// Actor / Role identify who is performing the edit (recorded in history).
	Actor      string
	Role       Role
	Structured Structured
	FreeText   string
	// Now, if non-zero, fixes the timestamp. Zero means time.Now().UTC().
	Now time.Time
}

// EditLabel appends an edit to an existing label (append-on-edit, never
// destructive — spec D5.1/R2) and persists the sidecar. Authorization rule
// (spec R2): the original author may edit their own label; an admin may edit
// any label. A reviewer editing someone else's label is rejected with
// ErrUnauthorizedEdit. The updated label is returned.
func EditLabel(iterLogDir string, iteration int, labelID string, in EditInput) (Label, error) {
	sc, err := ReadSidecar(iterLogDir, iteration)
	if err != nil {
		return Label{}, err
	}
	idx := -1
	for i := range sc.Labels {
		if sc.Labels[i].ID == labelID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Label{}, fmt.Errorf("%w: %s", ErrLabelNotFound, labelID)
	}
	target := sc.Labels[idx]
	if in.Role != RoleAdmin && in.Actor != target.Actor {
		return Label{}, fmt.Errorf("%w: %s by %s", ErrUnauthorizedEdit, labelID, in.Actor)
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	edit := Edit{
		Actor:      in.Actor,
		Role:       in.Role,
		Timestamp:  now,
		Structured: in.Structured,
		FreeText:   in.FreeText,
	}
	target.History = append(target.History, edit)
	target.UpdatedAt = now
	if err := target.Validate(); err != nil {
		return Label{}, err
	}
	sc.Labels[idx] = target
	if _, err := WriteSidecar(iterLogDir, sc); err != nil {
		return Label{}, err
	}
	return target, nil
}

// Get returns a single label by id from an iteration's sidecar.
func Get(iterLogDir string, iteration int, labelID string) (Label, error) {
	sc, err := ReadSidecar(iterLogDir, iteration)
	if err != nil {
		return Label{}, err
	}
	for _, l := range sc.Labels {
		if l.ID == labelID {
			return l, nil
		}
	}
	return Label{}, fmt.Errorf("%w: %s", ErrLabelNotFound, labelID)
}

// List returns all labels for an iteration (empty slice when none exist).
func List(iterLogDir string, iteration int) ([]Label, error) {
	sc, err := ReadSidecar(iterLogDir, iteration)
	if err != nil {
		return nil, err
	}
	return sc.Labels, nil
}

// writeYAMLAtomic marshals v to YAML and writes it to path atomically via the
// shared fsops atomic-write primitive (temp file in the same directory renamed
// into place), so a concurrent reader never observes a partial sidecar.
func writeYAMLAtomic(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return writeFileAtomic(path, data, 0o600)
}
