package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"os"
	"strings"
	"sync"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
)

// HeaderRequestID is the request/response header carrying the per-request id
// used as the audit idempotency key. A client-supplied value is honored (so a
// reconciling retry can reuse it); otherwise the audit middleware generates
// one. It is echoed on every mutating response, success or failure.
const HeaderRequestID = "X-Request-Id"

// maxRequestIDLen bounds a client-supplied request id; anything longer is
// replaced with a generated one rather than stored into the audit log.
const maxRequestIDLen = 128

// bearerScheme is the accepted Authorization scheme prefix.
const bearerScheme = "Bearer "

// Seams over the crypto/lock/filesystem primitives so the request-id,
// lock-failure, pre-image-read, and rollback-failure branches are coverable
// deterministically in tests (mirroring internal/review/audit).
var (
	// randRead backs request-id generation.
	randRead = rand.Read
	// acquireFileLock is the cross-process half of the mutation guard: the
	// same advisory lock primitive the audit log (and, prospectively, the
	// admin CLI) uses, so HTTP requests and CLI processes mutating the same
	// sidecar or users file serialize their read-modify-write sections.
	acquireFileLock = agentslock.AcquireFileLock
	// readTargetFile captures a mutation target's pre-image.
	readTargetFile = os.ReadFile
	// restoreTargetFile / removeTargetFile perform the fail-closed rollback.
	restoreTargetFile = fsops.WriteFileAtomic
	removeTargetFile  = fsops.Remove
	// criticalLog receives the screaming report for the one residual the
	// fail-closed guard cannot fix: the audit append AND the rollback both
	// failed, so an UNAUDITED mutation persisted.
	criticalLog io.Writer = os.Stderr
)

// identityCtxKey keys the authenticated identity in the request context.
type identityCtxKey struct{}

// auditCtxKey keys the staged-audit holder in the request context.
type auditCtxKey struct{}

// stagedAudit is the per-request holder a mutating handler fills with the
// audit event describing the mutation it performed. The audit middleware
// appends it after the handler succeeds.
type stagedAudit struct {
	ev *audit.Event
}

// stageAudit records the audit event for the current mutating request. It is
// a no-op when the request is not wrapped by the audit middleware (which does
// not happen for wired routes; the middleware treats a successful mutating
// response with no staged event as a 500-worthy contract violation).
func stageAudit(ctx context.Context, ev audit.Event) {
	if holder, ok := ctx.Value(auditCtxKey{}).(*stagedAudit); ok {
		holder.ev = &ev
	}
}

// identityFrom returns the authenticated identity placed by requireAuth.
func identityFrom(ctx context.Context) (auth.Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(auth.Identity)
	return id, ok
}

// mustIdentity fetches the request identity, writing a 500 when it is absent
// (a route wired without requireAuth — a programming error, not a client one).
func mustIdentity(w nethttp.ResponseWriter, r *nethttp.Request) (auth.Identity, bool) {
	id, ok := identityFrom(r.Context())
	if !ok {
		writeError(w, nethttp.StatusInternalServerError, "no authenticated identity on request", "")
	}
	return id, ok
}

// requireAuth enforces spec R8: it resolves the bearer token through the
// pluggable Authenticator and gates on the route's permission. Missing or
// invalid token → 401; authenticated but insufficient role → 403; an
// operational authenticator failure (e.g. unreadable users file) → 500, never
// conflated with a 401.
func (m *Mount) requireAuth(perm auth.Permission, next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, nethttp.StatusUnauthorized, "missing bearer token", "")
			return
		}
		id, err := m.auth.Authenticate(token)
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(w, nethttp.StatusUnauthorized, "invalid token", "")
			return
		}
		if err != nil {
			writeError(w, nethttp.StatusInternalServerError, "authentication backend failure", "")
			return
		}
		if !id.Can(perm) {
			writeError(w, nethttp.StatusForbidden, fmt.Sprintf("role %s lacks permission %s", id.Role, perm), "")
			return
		}
		ctx := context.WithValue(r.Context(), identityCtxKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header.
func bearerToken(r *nethttp.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, bearerScheme) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearerScheme))
	return token, token != ""
}

// targetFunc resolves the file a mutating route will modify, so the mutation
// guard can lock it and capture its pre-image before the handler runs. It
// returns "" when the target cannot be derived from the request (malformed
// iteration) — the handler rejects such requests before mutating anything —
// or when the backing store has no file target (test fakes).
type targetFunc func(r *nethttp.Request) string

// withAudit is the mutation guard + audit middleware. For each mutating
// request it:
//
//  1. locks the target file (per-path in-process mutex + agentslock file
//     lock), serializing the whole [read → mutate → audit] critical section
//     against concurrent requests AND other processes — without this, two
//     simultaneous label POSTs both read the same sidecar and the later
//     atomic replace silently drops the earlier label;
//  2. captures the target's pre-image;
//  3. runs the handler with its response buffered;
//  4. appends the staged audit event, and only then releases the response.
//
// The audit is FAIL-CLOSED (spec R6: every mutating action writes one audit
// record): if the append fails, the target file is restored to its pre-image
// (still under the lock) and the client gets a 500 carrying the X-Request-Id
// — the mutation does not survive unaudited. Two residuals remain, both
// bounded by audit.Append's at-least-once contract: (a) the failed append may
// still have landed its record, leaving an audit line for a rolled-back
// mutation (benign — the record's request id matches the 500 the client saw);
// (b) if the rollback write itself also fails (doubly-degraded environment),
// the mutation survives unaudited and is reported on criticalLog with the
// request id. Callers must reconcile by request id, never blind-retry.
func (m *Mount) withAudit(target targetFunc, next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		reqID, err := requestIDFor(r)
		if err != nil {
			writeError(w, nethttp.StatusInternalServerError, "generate request id", "")
			return
		}
		w.Header().Set(HeaderRequestID, reqID)

		path := target(r)
		pre, unlock, ok := m.prepareTarget(w, path, reqID)
		if !ok {
			return
		}
		defer unlock()

		staged := &stagedAudit{}
		buf := newBufferedResponse()
		ctx := context.WithValue(r.Context(), auditCtxKey{}, staged)
		next.ServeHTTP(buf, r.WithContext(ctx))
		if buf.status() >= nethttp.StatusBadRequest {
			buf.flushTo(w)
			return
		}
		if staged.ev == nil {
			// Contract violation (handler mutated without staging): treat like
			// a failed audit — roll the mutation back rather than letting it
			// persist unaudited.
			m.failClosed(w, path, pre, reqID,
				"mutating handler completed without staging an audit event")
			return
		}
		ev := *staged.ev
		ev.RequestID = reqID
		if _, err := m.audit.Append(ev); err != nil {
			m.failClosed(w, path, pre, reqID, "audit append failed")
			return
		}
		buf.flushTo(w)
	})
}

// prepareTarget locks the mutation target and snapshots its pre-image (steps
// 1-2 of the guard). On failure it writes the 500 itself, releases anything it
// acquired, and reports ok=false. A request with no resolvable target ("")
// skips the guard and gets a no-op unlock.
func (m *Mount) prepareTarget(w nethttp.ResponseWriter, path, reqID string) (preImage, func(), bool) {
	if path == "" {
		// No mutation target on this route: nothing was locked, so the
		// returned unlock is intentionally a no-op.
		return preImage{}, func() { /* no lock taken for target-less request */ }, true
	}
	unlock, err := m.lockTarget(path)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "lock mutation target: "+err.Error(), reqID)
		return preImage{}, nil, false
	}
	pre, err := readPreImage(path)
	if err != nil {
		unlock()
		writeError(w, nethttp.StatusInternalServerError, "capture mutation pre-image: "+err.Error(), reqID)
		return preImage{}, nil, false
	}
	return pre, unlock, true
}

// failClosed enforces the no-unaudited-mutation invariant after an audit
// failure: it restores the mutation target to its pre-image (the caller still
// holds the target lock) and writes the 500. When the rollback itself fails —
// or no target file is known — the degraded outcome is stated explicitly.
func (m *Mount) failClosed(w nethttp.ResponseWriter, path string, pre preImage, reqID, cause string) {
	if path != "" && rollback(path, pre, reqID) {
		writeError(w, nethttp.StatusInternalServerError,
			cause+"; the mutation was rolled back — safe to retry with the same X-Request-Id", reqID)
		return
	}
	writeError(w, nethttp.StatusInternalServerError,
		cause+"; rollback failed — the mutation may have persisted UNAUDITED, reconcile using this request id", reqID)
}

// preImage is a mutation target's byte-exact state before the handler ran.
type preImage struct {
	data    []byte
	existed bool
}

// readPreImage snapshots the target file; a missing file is a valid pre-image
// (rollback then removes whatever the handler created).
func readPreImage(path string) (preImage, error) {
	data, err := readTargetFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return preImage{}, nil
	}
	if err != nil {
		return preImage{}, err
	}
	return preImage{data: data, existed: true}, nil
}

// rollback restores path to its pre-image and reports success. On failure the
// mutation survives UNAUDITED — the one residual the fail-closed guard cannot
// fix — so it is reported loudly on criticalLog with the request id.
func rollback(path string, pre preImage, reqID string) bool {
	var err error
	if pre.existed {
		err = restoreTargetFile(path, pre.data)
	} else if err = removeTargetFile(path); errors.Is(err, os.ErrNotExist) {
		// The handler never materialized the file; absence IS the pre-image.
		err = nil
	}
	if err == nil {
		return true
	}
	fmt.Fprintf(criticalLog,
		"review/http: CRITICAL: audit append failed AND pre-image rollback failed — "+
			"an UNAUDITED mutation persisted at %s (request_id=%s): %v\n",
		path, reqID, err)
	return false
}

// pathMutex returns the in-process mutex guarding one target file.
func (m *Mount) pathMutex(path string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	mu, ok := m.locks[path]
	if !ok {
		mu = &sync.Mutex{}
		m.locks[path] = mu
	}
	return mu
}

// lockTarget serializes a mutation on path against other goroutines in this
// process (per-path mutex) and against other processes mutating the same file,
// e.g. the admin CLI (agentslock file lock). The returned func releases both.
func (m *Mount) lockTarget(path string) (func(), error) {
	mu := m.pathMutex(path)
	mu.Lock()
	release, err := acquireFileLock(path)
	if err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("review/http: lock %s: %w", path, err)
	}
	return func() {
		_ = release()
		mu.Unlock()
	}, nil
}

// requestIDFor returns the client-supplied X-Request-Id when present and
// sane, otherwise a freshly generated one.
func requestIDFor(r *nethttp.Request) (string, error) {
	id := strings.TrimSpace(r.Header.Get(HeaderRequestID))
	if id != "" && len(id) <= maxRequestIDLen {
		return id, nil
	}
	return newRequestID()
}

// newRequestID returns a 32-char hex id from crypto/rand.
func newRequestID() (string, error) {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		return "", fmt.Errorf("review/http: generate request id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// bufferedResponse captures a handler's response so the audit middleware can
// decide — after the handler ran — whether to release it or replace it with an
// audit-failure error.
type bufferedResponse struct {
	header nethttp.Header
	code   int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: nethttp.Header{}}
}

func (b *bufferedResponse) Header() nethttp.Header { return b.header }

// WriteHeader records the first explicit status code; later calls are ignored,
// matching net/http semantics.
func (b *bufferedResponse) WriteHeader(code int) {
	if b.code == 0 {
		b.code = code
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.code == 0 {
		b.code = nethttp.StatusOK
	}
	return b.body.Write(p)
}

// status returns the effective status code (200 when the handler never set
// one, matching net/http's implicit-200 behavior).
func (b *bufferedResponse) status() int {
	if b.code == 0 {
		return nethttp.StatusOK
	}
	return b.code
}

// flushTo replays the buffered headers, status, and body onto the real writer.
func (b *bufferedResponse) flushTo(w nethttp.ResponseWriter) {
	dst := w.Header()
	for k, vs := range b.header {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
	w.WriteHeader(b.status())
	_, _ = w.Write(b.body.Bytes())
}
