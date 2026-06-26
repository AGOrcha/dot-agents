// Package workstore is a proof-of-concept in-memory WorkStore that validates
// two open questions from the work-tracking-storage-abstraction spec:
//
//   - OQ1 atomic claim/lease: a scout LEASES an eligible task before
//     dispatching it, so a second concurrent wave sees it already claimed and
//     backs off. Two mechanisms are prototyped and compared on the same
//     scenarios: a TTL lease and a compare-and-set (CAS) claim.
//   - OQ2 per-field conflict resolution: status is backend-wins (the
//     lease/transition is authoritative) and content is local-wins (a worker's
//     edits to write_scope/notes win). A field-ownership map drives the merge.
//
// The store is the shared source of truth (KG-as-SOT). Concurrency safety is
// enforced by a single mutex guarding all task mutations; the proofs run the
// store under -race across many goroutine interleavings.
package workstore

import (
	"errors"
	"sync"
	"time"
)

// Status is a coordination-state value for a task. The values mirror the REAL
// CanonicalTask status enum from schemas/workflow-tasks.schema.json so the
// experiment is faithful to the production task shape, not a 2-state toy:
//
//	pending | in_progress | blocked | completed | cancelled
//
// The lease/claim semantics map the spec's coordination transitions onto these:
// a scout claiming an eligible task moves it pending -> in_progress (the
// "claimed" state); the leaseholder later completes it. There is no separate
// "claimed" enum value — claimed == in_progress with a live lease, exactly as
// the canonical schema (which has no claimed state) would represent it.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
)

// Lease records who holds a task and when their hold expires. A zero Lease
// (empty Owner) means the task is unleased.
type Lease struct {
	Owner   string
	Expires time.Time
}

// held reports whether the lease is currently active at now.
func (l Lease) held(now time.Time) bool {
	return l.Owner != "" && now.Before(l.Expires)
}

// Task is a single coordination-state entity. The structural fields mirror the
// REAL CanonicalTask (schemas/workflow-tasks.schema.json): ID, Status,
// WriteScope ([]string), Notes, DependsOn ([]string), Owner. Lease and Version
// are the coordination-state additions this spec/prototype validates (not in
// the committed schema today — that is precisely what the prototype is testing).
//
// Status is backend-owned; the content fields (WriteScope, Notes, DependsOn)
// are worker-owned. Version increments on every successful mutation and is the
// basis for the CAS claim.
type Task struct {
	ID         string
	Status     Status
	Version    int
	Lease      Lease
	Owner      string   // workflow owner field (distinct from lease owner)
	WriteScope []string // real schema: array of path globs
	Notes      string
	DependsOn  []string // real schema: cross/intra-plan deps
}

// clone returns a deep-enough value copy so callers never alias the store's
// internal slices. The slices are copied because a worker editing write_scope
// must not mutate the store's backing array.
func (t Task) clone() Task {
	cp := t
	cp.WriteScope = append([]string(nil), t.WriteScope...)
	cp.DependsOn = append([]string(nil), t.DependsOn...)
	return cp
}

// Sentinel errors let the proofs assert the exact back-off reason rather than
// just "an error happened".
var (
	// ErrAlreadyClaimed means another owner holds a live lease (TTL path) or
	// the task is not pending (CAS path) — the caller must back off.
	ErrAlreadyClaimed = errors.New("workstore: task already claimed")
	// ErrVersionConflict means the CAS expected-version did not match — the
	// task changed since the scout read it; back off and re-read.
	ErrVersionConflict = errors.New("workstore: version conflict")
	// ErrNotFound means the task id is unknown.
	ErrNotFound = errors.New("workstore: task not found")
	// ErrNotOwner means a release/complete was attempted by a non-holder.
	ErrNotOwner = errors.New("workstore: caller does not hold the lease")
)

// Store is the in-memory WorkStore. All mutations are serialized by mu so the
// claim path is atomic: read-decide-write happens under one lock, which is the
// in-process analogue of an atomic backend transaction.
type Store struct {
	mu    sync.Mutex
	tasks map[string]*Task
}

// New returns an empty Store.
func New() *Store {
	return &Store{tasks: make(map[string]*Task)}
}

// Add inserts a pending task with the given content. Used to seed scenarios.
// writeScope and dependsOn mirror the real array-typed schema fields.
func (s *Store) Add(id string, writeScope []string, notes string, dependsOn ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[id] = &Task{
		ID:         id,
		Status:     StatusPending,
		WriteScope: writeScope,
		Notes:      notes,
		DependsOn:  dependsOn,
	}
}

// Get returns a value copy of the task, or ErrNotFound.
func (s *Store) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return t.clone(), nil
}

// ClaimTTL is the TTL-lease claim mechanism. It succeeds only if the task is
// pending and currently unleased (or holds an EXPIRED lease — a dead worker's
// hold is reclaimable). On success it marks the task claimed, stamps a lease
// owned by owner that expires at now+ttl, and bumps the version. A live lease
// held by anyone (including a different owner) causes ErrAlreadyClaimed.
//
// This is the mechanism that survives a leaseholder dying mid-work: the lease
// simply expires and the next scout reclaims it. No external liveness signal is
// required.
func (s *Store) ClaimTTL(id, owner string, now time.Time, ttl time.Duration) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	if !t.ttlClaimable(now) {
		return Task{}, ErrAlreadyClaimed
	}
	t.Status = StatusInProgress
	t.Lease = Lease{Owner: owner, Expires: now.Add(ttl)}
	t.Version++
	return t.clone(), nil
}

// ttlClaimable reports whether the task may be claimed via the TTL mechanism at
// now: it is claimable when pending, or when a prior holder's lease has expired
// (a dead worker's hold is reclaimable). A completed/awaiting-review task or a
// live lease is not claimable.
func (t *Task) ttlClaimable(now time.Time) bool {
	switch t.Status {
	case StatusPending:
		return true
	case StatusInProgress:
		return !t.Lease.held(now) // expired lease -> reclaimable
	default:
		return false
	}
}

// ClaimCAS is the compare-and-set claim mechanism. The scout reads the task,
// then claims it passing expectedVersion. The claim succeeds only if the
// version is unchanged since the read AND the task is still pending; otherwise
// ErrVersionConflict / ErrAlreadyClaimed. Unlike TTL, CAS has no expiry: if the
// holder dies, the task stays claimed forever (the failure mode H-ttl shows).
func (s *Store) ClaimCAS(id, owner string, expectedVersion int) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	if t.Version != expectedVersion {
		return Task{}, ErrVersionConflict
	}
	if t.Status != StatusPending {
		return Task{}, ErrAlreadyClaimed
	}
	t.Status = StatusInProgress
	t.Lease = Lease{Owner: owner} // CAS lease has no expiry by design
	t.Version++
	return t.clone(), nil
}

// Release returns a claimed task to pending (e.g. the worker backed out). Only
// the current owner may release.
func (s *Store) Release(id, owner string) error {
	return s.transitionByOwner(id, owner, StatusPending, true)
}

// Complete marks a claimed task completed and drops the lease. Only the owner
// may complete.
func (s *Store) Complete(id, owner string) error {
	return s.transitionByOwner(id, owner, StatusCompleted, false)
}

// transitionByOwner is the shared owner-gated transition used by Release and
// Complete. clearLease drops the lease; the lease owner is the authority.
func (s *Store) transitionByOwner(id, owner string, to Status, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return ErrNotFound
	}
	if t.Lease.Owner != owner {
		return ErrNotOwner
	}
	t.Status = to
	t.Lease = Lease{}
	t.Version++
	return nil
}

// MarkInProgress is the non-atomic "set status to in_progress" write used by
// the naive negative-control scout. It is a LAST-WRITER-WINS update with no
// guard: it does not check whether someone else already claimed the task. This
// is deliberately the broken primitive — it cannot prevent double-dispatch
// because the eligibility decision was made before it ran.
func (s *Store) MarkInProgress(id, owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return
	}
	t.Status = StatusInProgress
	t.Owner = owner
	t.Version++
}

// AdvanceStatus forces a backend-authoritative status transition (e.g. the
// daemon recording awaiting_review). It models a backend-side write that
// participates in the per-field merge as the backend-wins source.
func (s *Store) AdvanceStatus(id string, to Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return ErrNotFound
	}
	t.Status = to
	t.Version++
	return nil
}
