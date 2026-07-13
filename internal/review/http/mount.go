// Package http is the R5 review collection endpoint: the HTTP arm of the §2A
// surface→transport map. It builds a plain net/http handler (Mount) that the
// R3 service runtime stitches onto its HTTP/SSE edge via
// internal/service/http.(*Server).RegisterMount(mount.Prefix(), mount) — this
// package never touches the R3 host, middleware chain, or lifecycle (spec
// D5.5: route ownership lives here, hosting lives in R3). No EventBus access
// is needed in v1 (r3 §D4.3).
//
// Route trees, rooted at the mount prefix (default /api/reviews):
//
//	GET    {prefix}/runs/{iteration}/labels            list labels   (labels:read)
//	POST   {prefix}/runs/{iteration}/labels            submit label  (labels:write, audited)
//	PATCH  {prefix}/runs/{iteration}/labels/{label_id} edit label    (labels:write, audited)
//	GET    {prefix}/audit                              audit view    (audit:read, admin)
//	GET    {prefix}/users                              list users    (users:manage, admin)
//	POST   {prefix}/users                              create user   (users:manage, audited)
//	PATCH  {prefix}/users/{email}                      change role   (users:manage, audited)
//	DELETE {prefix}/users/{email}                      delete user   (users:manage, audited)
//
// net/http mounts do not strip prefixes, so the full route paths (prefix
// included) are registered on the internal mux at construction time; R3 simply
// forwards requests whose path lives under the prefix.
//
// Auth is bearer-token via the pluggable internal/review/auth Authenticator
// (missing/invalid token → 401, insufficient role → 403 — spec R8). Every
// mutating call is recorded in the internal/review/audit chained log by the
// audit middleware, which is FAIL-CLOSED: each mutating request's
// [read → mutate → audit] section runs under a per-target lock (in-process
// mutex + agentslock file lock, so concurrent requests and CLI processes
// cannot interleave read-modify-write cycles and drop each other's writes),
// with the target file's pre-image captured up front — if the audit append
// fails, the mutation is rolled back to the pre-image and the client gets a
// 500 carrying the X-Request-Id. A persisted-but-unaudited mutation can only
// survive in the doubly-degraded case where the rollback write also fails
// (reported loudly with the request id; audit.Append's at-least-once note
// applies to that residual only — reconcile by request id, never blind-retry).
//
// The package name collides with the standard library, so net/http is imported
// under the alias nethttp (matching internal/service/http).
package http

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

// DefaultPrefix is the mount point R3 registers this handler under (see
// internal/service/http server docs: "R5 its review queue at /api/reviews").
const DefaultPrefix = "/api/reviews"

// Errors returned by New.
var (
	// ErrInvalidPrefix is returned when the mount prefix is not a rooted,
	// non-root path.
	ErrInvalidPrefix = errors.New("review/http: mount prefix must be a rooted path below /")
	// ErrNilDependency is returned when a required dependency is missing.
	ErrNilDependency = errors.New("review/http: missing dependency")
)

// LabelStore is the label persistence surface the handlers consume. The
// production implementation is SidecarLabelStore (iter-N.labels.yaml sidecars
// via internal/review/labels); tests inject failures through this seam.
type LabelStore interface {
	List(iteration int) ([]labels.Label, error)
	Get(iteration int, labelID string) (labels.Label, error)
	Add(iteration int, in labels.AddInput) (labels.Label, error)
	Edit(iteration int, labelID string, in labels.EditInput) (labels.Label, error)
	// SidecarPath is the file a mutation on the iteration will modify — the
	// mutation guard locks it and captures its pre-image before the handler
	// runs. An empty string disables the guard (stores with no file target).
	SidecarPath(iteration int) string
}

// SidecarLabelStore is the production LabelStore, delegating to the
// internal/review/labels sidecar CRUD rooted at the iteration-log directory.
type SidecarLabelStore struct {
	// Dir is the iteration-log directory holding iter-N.labels.yaml sidecars.
	Dir string
}

func (s SidecarLabelStore) List(iteration int) ([]labels.Label, error) {
	return labels.List(s.Dir, iteration)
}

func (s SidecarLabelStore) Get(iteration int, labelID string) (labels.Label, error) {
	return labels.Get(s.Dir, iteration, labelID)
}

func (s SidecarLabelStore) Add(iteration int, in labels.AddInput) (labels.Label, error) {
	return labels.Add(s.Dir, iteration, in)
}

func (s SidecarLabelStore) Edit(iteration int, labelID string, in labels.EditInput) (labels.Label, error) {
	return labels.EditLabel(s.Dir, iteration, labelID, in)
}

func (s SidecarLabelStore) SidecarPath(iteration int) string {
	return labels.IterationLabelsPath(s.Dir, iteration)
}

// UserStore is the users-file surface the admin handlers consume. The
// production implementation is FileUserStore over the local users file
// (~/.config/da/review/users.yaml — auth.DefaultUsersPath).
type UserStore interface {
	Load() (*auth.UsersFile, error)
	Save(uf *auth.UsersFile) error
	// FilePath is the users file a mutation will modify — the mutation guard
	// locks it and captures its pre-image before the handler runs. An empty
	// string disables the guard (stores with no file target).
	FilePath() string
}

// FileUserStore is the production UserStore over the users file at Path.
type FileUserStore struct {
	Path string
}

func (s FileUserStore) Load() (*auth.UsersFile, error) { return auth.LoadUsersFile(s.Path) }

func (s FileUserStore) Save(uf *auth.UsersFile) error { return uf.Save(s.Path) }

func (s FileUserStore) FilePath() string { return s.Path }

// AuditLog is the audit surface the mount consumes: chained append for the
// audit middleware, record read-back for the admin audit view. *audit.Log
// satisfies it.
type AuditLog interface {
	Append(e audit.Event) (audit.Record, error)
	Records() ([]audit.Record, error)
}

// Deps carries the collaborators a Mount is built from. All fields are
// required.
type Deps struct {
	// Auth resolves bearer tokens to identities (401/403 enforcement).
	Auth auth.Authenticator
	// Labels persists label submissions and edits.
	Labels LabelStore
	// Users backs the admin user-management routes.
	Users UserStore
	// Audit records every mutating call (spec R6).
	Audit AuditLog
}

// validate reports the first missing dependency.
func (d Deps) validate() error {
	if d.Auth == nil {
		return fmt.Errorf("%w: Auth", ErrNilDependency)
	}
	if d.Labels == nil {
		return fmt.Errorf("%w: Labels", ErrNilDependency)
	}
	if d.Users == nil {
		return fmt.Errorf("%w: Users", ErrNilDependency)
	}
	if d.Audit == nil {
		return fmt.Errorf("%w: Audit", ErrNilDependency)
	}
	return nil
}

// Mount is the review collection endpoint as a plain http.Handler. Construct
// it with New; R3 registers it via RegisterMount(m.Prefix(), m). It is safe
// for concurrent use — all mutable state lives in the backing stores, which
// serialize their own writes.
type Mount struct {
	prefix string
	mux    *nethttp.ServeMux
	auth   auth.Authenticator
	labels LabelStore
	users  UserStore
	audit  AuditLog

	// locks serializes mutating requests per target file (in-process half of
	// the mutation guard; the agentslock file lock is the cross-process half).
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

// New builds a Mount whose routes are rooted at prefix (typically
// DefaultPrefix). The prefix must be rooted and below "/"; a trailing slash is
// normalized away, mirroring the R3 side's RegisterMount normalization so the
// registered mux patterns and the mount registration always agree.
func New(prefix string, deps Deps) (*Mount, error) {
	p, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}
	m := &Mount{
		prefix: p,
		mux:    nethttp.NewServeMux(),
		auth:   deps.Auth,
		labels: deps.Labels,
		users:  deps.Users,
		audit:  deps.Audit,
		locks:  map[string]*sync.Mutex{},
	}
	m.routes()
	return m, nil
}

// Prefix returns the normalized mount prefix, i.e. the exact value R3 passes
// to RegisterMount.
func (m *Mount) Prefix() string { return m.prefix }

// ServeHTTP dispatches to the mount's internal mux. Patterns carry the full
// prefixed paths, so no prefix stripping happens (or is needed) on the R3 side.
func (m *Mount) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	m.mux.ServeHTTP(w, r)
}

// routes registers the route trees on the internal mux, each wrapped in the
// auth middleware (and, for mutating routes, the audit middleware).
func (m *Mount) routes() {
	labelsPat := m.prefix + "/runs/{iteration}/labels"
	m.handle("GET "+labelsPat, auth.PermReadLabels, nil, m.handleListLabels)
	m.handle("POST "+labelsPat, auth.PermWriteLabels, m.labelSidecarTarget, m.handleSubmitLabel)
	m.handle("PATCH "+labelsPat+"/{label_id}", auth.PermWriteLabels, m.labelSidecarTarget, m.handleEditLabel)

	m.handle("GET "+m.prefix+"/audit", auth.PermReadAudit, nil, m.handleAuditView)

	usersPat := m.prefix + "/users"
	m.handle("GET "+usersPat, auth.PermManageUsers, nil, m.handleListUsers)
	m.handle("POST "+usersPat, auth.PermManageUsers, m.usersTarget, m.handleCreateUser)
	m.handle("PATCH "+usersPat+"/{email}", auth.PermManageUsers, m.usersTarget, m.handleChangeRole)
	m.handle("DELETE "+usersPat+"/{email}", auth.PermManageUsers, m.usersTarget, m.handleDeleteUser)
}

// handle wires one route: permission gate outermost, then (for mutating
// routes, identified by a non-nil target resolver) the locking + fail-closed
// audit guard, then the handler.
func (m *Mount) handle(pattern string, perm auth.Permission, target targetFunc, h nethttp.HandlerFunc) {
	var next nethttp.Handler = h
	if target != nil {
		next = m.withAudit(target, next)
	}
	m.mux.Handle(pattern, m.requireAuth(perm, next))
}

// labelSidecarTarget resolves the sidecar file a label mutation will modify.
// It returns "" for a malformed iteration — the handler rejects such requests
// with a 400 before any mutation, so no lock or pre-image is needed.
func (m *Mount) labelSidecarTarget(r *nethttp.Request) string {
	iter, err := strconv.Atoi(r.PathValue("iteration"))
	if err != nil || iter < 0 {
		return ""
	}
	return m.labels.SidecarPath(iter)
}

// usersTarget resolves the users file a user mutation will modify.
func (m *Mount) usersTarget(*nethttp.Request) string { return m.users.FilePath() }

// normalizePrefix validates a rooted, non-root prefix and strips any trailing
// slash.
func normalizePrefix(prefix string) (string, error) {
	if !strings.HasPrefix(prefix, "/") {
		return "", fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}
	trimmed := strings.TrimRight(prefix, "/")
	if trimmed == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}
	return trimmed, nil
}
