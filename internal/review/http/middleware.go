package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"

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

// randRead is a seam over crypto/rand.Read so the request-id generation error
// branch is coverable in tests (mirroring internal/review/auth).
var randRead = rand.Read

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

// withAudit is the audit middleware: it buffers the mutating handler's
// response, and only releases a success response after the staged audit event
// has been appended to the chained log (spec R6: every mutating action writes
// one audit record — the client never sees success for an unaudited mutation).
//
// Append is AT-LEAST-ONCE (see audit.Log.Append): on an append error the
// mutation has already persisted and the audit record itself may also be
// durable. The 500 returned here therefore carries the request id and warns
// against blind retries — a reconciling caller reuses the same X-Request-Id so
// the already-landed record is detectable.
func (m *Mount) withAudit(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		reqID, err := requestIDFor(r)
		if err != nil {
			writeError(w, nethttp.StatusInternalServerError, "generate request id", "")
			return
		}
		w.Header().Set(HeaderRequestID, reqID)
		staged := &stagedAudit{}
		buf := newBufferedResponse()
		ctx := context.WithValue(r.Context(), auditCtxKey{}, staged)
		next.ServeHTTP(buf, r.WithContext(ctx))
		if buf.status() >= nethttp.StatusBadRequest {
			buf.flushTo(w)
			return
		}
		if staged.ev == nil {
			writeError(w, nethttp.StatusInternalServerError,
				"mutating handler completed without staging an audit event", reqID)
			return
		}
		ev := *staged.ev
		ev.RequestID = reqID
		if _, err := m.audit.Append(ev); err != nil {
			writeError(w, nethttp.StatusInternalServerError,
				"audit append failed; the mutation may have persisted — do not blind-retry, "+
					"reconcile using this request id", reqID)
			return
		}
		buf.flushTo(w)
	})
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
