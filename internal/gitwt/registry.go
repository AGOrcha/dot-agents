package gitwt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-git/go-billy/v6"
	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// registryFile is the per-worktree metadata sidecar written alongside wt1's
// base-ref file under .git/worktrees/<name>/. Living in the admin dir (not the
// linked worktree's own checkout) is load-bearing: an orchestrator scanning
// from the MAIN repo can read it regardless of which worktree a process is
// currently in — the `worktree-isolation-defeats-status-tracking` lesson.
const registryFile = "registry.yaml"

// rosterFile is the name index kept in the repository's git dir. It records
// which worktree names the registry knows about, INDEPENDENTLY of the
// per-worktree admin dirs, so Reconcile can still detect an entry whose admin
// dir was removed out-of-band (the sidecar vanishes with the dir, but the
// roster name persists). Kept flat in the git dir so it, too, is readable from
// the main repo.
const rosterFile = "dot-agents-worktree-registry.yaml"

// registrySchemaVersion tags the roster payload so a later format change can be
// detected rather than silently misread.
const registrySchemaVersion = 1

// Registry errors. Callers should match with errors.Is. ErrWorktreeNotFound
// (declared in gitwt.go) is reused for operations targeting a worktree whose
// admin metadata is absent.
var (
	// ErrMetadataExists is returned by Registry.Create when a metadata record
	// already exists for the worktree. Use Update to modify an existing record.
	ErrMetadataExists = errors.New("gitwt: worktree metadata already exists")

	// ErrMetadataNotRecorded is returned by Registry.Get/Update when the
	// worktree exists but carries no metadata record.
	ErrMetadataNotRecorded = errors.New("gitwt: worktree metadata not recorded")
)

// Metadata is the semantic, per-worktree record the registry persists next to
// wt1's base-ref file. It is keyed by worktree name so it reconciles 1:1 with
// Manager.List(). Base-ref storage is NOT duplicated here — that stays wt1's
// RecordBaseRef/BaseRef; this carries only what go-git does not track.
type Metadata struct {
	// Name is the worktree name; it is the registry key and is always set to
	// the lookup name on write, so a mutator can never repoint it.
	Name string `yaml:"name"`
	// Purpose is a free-form human note (e.g. the task or delegation slice the
	// worktree was created for).
	Purpose string `yaml:"purpose"`
	// ParentPR is the pull-request number the worktree's work feeds into, or 0
	// when none is associated yet.
	ParentPR int `yaml:"parent_pr"`
	// CreatedAt is stamped once at Create and never rewritten afterwards.
	CreatedAt time.Time `yaml:"created_at"`
	// LastUsed drives the auto-prune-if-unchanged idle check; Touch re-stamps
	// it whenever the worktree is worked in.
	LastUsed time.Time `yaml:"last_used"`
}

// roster is the on-disk shape of rosterFile: the set of worktree names the
// registry has recorded metadata for.
type roster struct {
	SchemaVersion int      `yaml:"schema_version"`
	Names         []string `yaml:"names"`
}

// Registry layers rich per-worktree metadata and an auto-prune-if-unchanged
// staleness policy on top of wt1's Manager. It persists the metadata under the
// same admin dir as the recorded base ref and keeps a separate name roster so
// reconciliation against Manager.List() can surface orphans.
type Registry struct {
	mgr *manager
	ttl time.Duration
	now func() time.Time
}

// NewRegistry binds a Registry to a Manager. ttl is the last-used idle window
// PruneScan enforces (an unchanged, idle-past-ttl worktree is prune-eligible).
// The Manager must be the go-git implementation returned by NewManager.
func NewRegistry(m Manager, ttl time.Duration) (*Registry, error) {
	mgr, ok := m.(*manager)
	if !ok {
		return nil, fmt.Errorf("gitwt: registry requires the go-git Manager, got %T", m)
	}
	return &Registry{mgr: mgr, ttl: ttl, now: time.Now}, nil
}

// Create records new metadata for the named worktree. It returns the stored
// record with CreatedAt/LastUsed stamped (from the registry clock when the
// caller leaves them zero). It returns ErrMetadataExists if a record already
// exists, or ErrWorktreeNotFound if the worktree has no admin metadata.
func (r *Registry) Create(name string, meta Metadata) (Metadata, error) {
	if _, err := r.readSidecar(name); err == nil {
		return Metadata{}, fmt.Errorf(errFmtWrap, ErrMetadataExists, name)
	} else if !errors.Is(err, ErrMetadataNotRecorded) {
		return Metadata{}, err
	}
	meta.Name = name
	now := r.now().UTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.LastUsed.IsZero() {
		meta.LastUsed = meta.CreatedAt
	}
	if err := r.writeSidecar(name, meta); err != nil {
		return Metadata{}, err
	}
	if err := r.addToRoster(name); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

// Get reads the metadata record for the named worktree. It returns
// ErrMetadataNotRecorded if the worktree exists but has no record, or
// ErrWorktreeNotFound if the worktree itself is unknown.
func (r *Registry) Get(name string) (Metadata, error) {
	return r.readSidecar(name)
}

// Update reads the record, applies mutate, and persists the result. The name
// key is always restored after mutate so it cannot be repointed. It returns
// ErrMetadataNotRecorded/ErrWorktreeNotFound like Get.
func (r *Registry) Update(name string, mutate func(*Metadata)) (Metadata, error) {
	meta, err := r.readSidecar(name)
	if err != nil {
		return Metadata{}, err
	}
	if mutate != nil {
		mutate(&meta)
	}
	meta.Name = name
	if err := r.writeSidecar(name, meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

// Touch re-stamps LastUsed to the current time — the last-used marker the
// idle check keys off. It is the common Update the orchestrator makes each
// time a worktree is worked in.
func (r *Registry) Touch(name string) (Metadata, error) {
	now := r.now().UTC()
	return r.Update(name, func(m *Metadata) { m.LastUsed = now })
}

// Deregister removes the metadata sidecar (if present) and drops the name from
// the roster. It is idempotent and tolerates an already-removed worktree admin
// dir, so it doubles as the cleanup for a name Reconcile flagged as orphaned.
func (r *Registry) Deregister(name string) error {
	path, err := r.sidecarPath(name)
	switch {
	case err == nil:
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("gitwt: remove metadata for %q: %w", name, rmErr)
		}
	case !errors.Is(err, ErrWorktreeNotFound):
		return err
	}
	return r.removeFromRoster(name)
}

// ReconcileResult reports how the registry's recorded names line up with the
// worktrees Manager.List() currently returns.
type ReconcileResult struct {
	// Tracked names are recorded AND still returned by List().
	Tracked []string
	// Orphaned names are recorded but no longer returned by List() (their admin
	// dir was removed out-of-band). Reconcile drops them from the roster so
	// they are not silently kept.
	Orphaned []string
	// Untracked names are returned by List() but have no metadata record.
	Untracked []string
}

// Reconcile diffs the recorded roster against Manager.List(). Orphaned entries
// (recorded but no longer listed) are detected and pruned from the roster;
// untracked worktrees (listed but unrecorded) are surfaced but left alone,
// since the registry has no metadata to attach to them.
func (r *Registry) Reconcile() (ReconcileResult, error) {
	known, err := r.readRoster()
	if err != nil {
		return ReconcileResult{}, err
	}
	names, err := r.mgr.List()
	if err != nil {
		return ReconcileResult{}, err
	}
	live := make(map[string]bool, len(names))
	for _, n := range names {
		live[n] = true
	}
	var res ReconcileResult
	for name := range known {
		if live[name] {
			res.Tracked = append(res.Tracked, name)
			continue
		}
		res.Orphaned = append(res.Orphaned, name)
		delete(known, name)
	}
	for _, name := range names {
		if !known[name] {
			res.Untracked = append(res.Untracked, name)
		}
	}
	if len(res.Orphaned) > 0 {
		if err := r.writeRoster(known); err != nil {
			return ReconcileResult{}, err
		}
	}
	sort.Strings(res.Tracked)
	sort.Strings(res.Orphaned)
	sort.Strings(res.Untracked)
	return res, nil
}

// PruneScan is the auto-prune-if-unchanged classification of every recorded
// worktree that Manager.List() still returns.
type PruneScan struct {
	// Eligible worktrees have no commits past their recorded base AND are idle
	// past the TTL — safe to auto-prune.
	Eligible []string
	// Abandoned worktrees have commits past their recorded base and are idle:
	// that is unmerged work, never auto-pruned — surfaced for a human to judge.
	Abandoned []string
	// Kept worktrees are held for any other reason: recently used (fresh), or
	// with an indeterminate base/tip or missing metadata.
	Kept []string
}

// PruneScan classifies each recorded, still-listed worktree by the
// auto-prune-if-unchanged policy: (no commits past base) AND (idle past TTL)
// => Eligible; (commits past base) AND idle => Abandoned; everything else =>
// Kept. Unlike Manager.Prune (which only reclaims worktrees whose dir is gone),
// this compares the current tip against wt1's recorded base ref.
func (r *Registry) PruneScan() (PruneScan, error) {
	known, err := r.readRoster()
	if err != nil {
		return PruneScan{}, err
	}
	names, err := r.mgr.List()
	if err != nil {
		return PruneScan{}, err
	}
	var scan PruneScan
	for _, name := range names {
		if known[name] {
			r.classify(name, &scan)
		}
	}
	sort.Strings(scan.Eligible)
	sort.Strings(scan.Abandoned)
	sort.Strings(scan.Kept)
	return scan, nil
}

// classify places one worktree into the PruneScan buckets. Anything the policy
// cannot decide safely (missing metadata, unrecorded base, unopenable tip)
// falls through to Kept — the registry never auto-prunes on incomplete data.
func (r *Registry) classify(name string, scan *PruneScan) {
	meta, err := r.readSidecar(name)
	if err != nil {
		scan.Kept = append(scan.Kept, name)
		return
	}
	unchanged, ok := r.unchangedSinceBase(name)
	if !ok {
		scan.Kept = append(scan.Kept, name)
		return
	}
	idle := r.now().UTC().Sub(meta.LastUsed) > r.ttl
	switch {
	case unchanged && idle:
		scan.Eligible = append(scan.Eligible, name)
	case !unchanged && idle:
		scan.Abandoned = append(scan.Abandoned, name)
	default:
		scan.Kept = append(scan.Kept, name)
	}
}

// unchangedSinceBase reports whether the worktree's current tip equals its
// wt1-recorded base ref (i.e. no commits past base). The second return is
// false when the answer is indeterminate — no recorded base, or the tip cannot
// be read — so the caller keeps rather than prunes.
func (r *Registry) unchangedSinceBase(name string) (bool, bool) {
	base, err := r.mgr.BaseRef(name)
	if err != nil {
		return false, false
	}
	path, err := r.mgr.worktreeDir(name)
	if err != nil {
		return false, false
	}
	wt, err := r.mgr.Open(path)
	if err != nil {
		return false, false
	}
	head, err := wt.Head()
	if err != nil {
		return false, false
	}
	return head.Hash() == base, true
}

// sidecarPath resolves the metadata file path under the worktree's admin dir,
// surfacing ErrWorktreeNotFound when the worktree is unknown.
func (r *Registry) sidecarPath(name string) (string, error) {
	dir, err := r.mgr.adminDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryFile), nil
}

// readSidecar loads and parses the metadata record for one worktree.
func (r *Registry) readSidecar(name string) (Metadata, error) {
	path, err := r.sidecarPath(name)
	if err != nil {
		return Metadata{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, fmt.Errorf(errFmtWrap, ErrMetadataNotRecorded, name)
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("gitwt: read metadata for %q: %w", name, err)
	}
	var meta Metadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("gitwt: parse metadata for %q: %w", name, err)
	}
	return meta, nil
}

// writeSidecar persists the metadata record atomically under the admin dir.
func (r *Registry) writeSidecar(name string, meta Metadata) error {
	path, err := r.sidecarPath(name)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("gitwt: marshal metadata for %q: %w", name, err)
	}
	if err := fsops.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("gitwt: write metadata for %q: %w", name, err)
	}
	return nil
}

// rosterPath is the name index location in the repository git dir.
func (r *Registry) rosterPath() string {
	return filepath.Join(r.mgr.gitDir(), rosterFile)
}

// readRoster loads the recorded name set. A missing roster is an empty set,
// not an error, so a fresh repository reconciles cleanly.
func (r *Registry) readRoster() (map[string]bool, error) {
	data, err := os.ReadFile(r.rosterPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitwt: read worktree roster: %w", err)
	}
	var ro roster
	if err := yaml.Unmarshal(data, &ro); err != nil {
		return nil, fmt.Errorf("gitwt: parse worktree roster: %w", err)
	}
	set := make(map[string]bool, len(ro.Names))
	for _, n := range ro.Names {
		set[n] = true
	}
	return set, nil
}

// writeRoster persists the name set as a sorted list for deterministic output.
func (r *Registry) writeRoster(set map[string]bool) error {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	data, err := yaml.Marshal(&roster{SchemaVersion: registrySchemaVersion, Names: names})
	if err != nil {
		return fmt.Errorf("gitwt: marshal worktree roster: %w", err)
	}
	if err := fsops.WriteFileAtomic(r.rosterPath(), data); err != nil {
		return fmt.Errorf("gitwt: write worktree roster: %w", err)
	}
	return nil
}

// addToRoster records name in the roster (a no-op if already present).
func (r *Registry) addToRoster(name string) error {
	set, err := r.readRoster()
	if err != nil {
		return err
	}
	if set[name] {
		return nil
	}
	set[name] = true
	return r.writeRoster(set)
}

// removeFromRoster drops name from the roster (a no-op if absent).
func (r *Registry) removeFromRoster(name string) error {
	set, err := r.readRoster()
	if err != nil {
		return err
	}
	if !set[name] {
		return nil
	}
	delete(set, name)
	return r.writeRoster(set)
}

// gitDir returns the repository's git directory as an OS path, mirroring the
// filesystem access adminDir uses so the roster sits beside the worktrees dir.
func (m *manager) gitDir() string {
	return m.repo.Storer.(interface{ Filesystem() billy.Filesystem }).Filesystem().Root()
}
