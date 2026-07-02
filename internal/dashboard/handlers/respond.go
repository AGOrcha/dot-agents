// respond.go carries the API.md §1 response conventions: the §1.2 success
// envelope, the §1.3 error envelope, and the §1.5 ETag / If-None-Match cache
// handshake.
package handlers

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
)

// Stable machine error codes (API.md §1.3).
const (
	codeBadRequest = "bad_request"
	codeNotFound   = "not_found"
	codeInternal   = "internal"
)

// contentTypeJSON is sent on every JSON response, success and error (§1.3).
const contentTypeJSON = "application/json; charset=utf-8"

// envelope is the §1.2 success wrapper. Data is pre-marshaled so the same
// bytes feed both the etag hash and the response body.
type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta meta            `json:"meta"`
}

// meta is the envelope's meta block; Count appears on list endpoints only.
type meta struct {
	ETag  string `json:"etag"`
	Count *int   `json:"count,omitempty"`
}

// errorBody is the §1.3 error wrapper.
type errorBody struct {
	Error errorInfo `json:"error"`
}

// errorInfo carries the stable machine code and the human-readable message.
type errorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// payload describes one successful response before envelope wrapping.
type payload struct {
	// resource keys the etag so distinct resources never share a cache
	// identity even when their payload bytes coincide.
	resource string
	// data is the DTO (object) or DTO list per the relevant schema.
	data any
	// count, when non-nil, is emitted as meta.count (list endpoints only).
	count *int
	// etag, when non-empty, overrides the content-derived etag (the rubric
	// endpoint pins etag = rubric version, §3.5).
	etag string
}

// respond marshals p.data, resolves the etag, honours If-None-Match with a
// 304 (§1.5), and otherwise writes the 200 envelope.
func (m *Mount) respond(w http.ResponseWriter, r *http.Request, p payload) {
	raw, err := json.Marshal(p.data)
	if err != nil {
		m.logger.Error("dashboard/handlers: response marshal failed",
			"path", r.URL.Path, "error", err)
		m.writeError(w, http.StatusInternalServerError, codeInternal,
			"unexpected server error")
		return
	}
	etag := p.etag
	if etag == "" {
		etag = contentETag(p.resource, raw)
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	if ifNoneMatchSatisfied(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	m.writeJSON(w, http.StatusOK, envelope{Data: raw, Meta: meta{ETag: etag, Count: p.count}})
}

// writeError writes the §1.3 error envelope.
func (m *Mount) writeError(w http.ResponseWriter, status int, code, message string) {
	m.writeJSON(w, status, errorBody{Error: errorInfo{Code: code, Message: message}})
}

// writeJSON writes one JSON body with the mandated content type. An encode
// failure past WriteHeader cannot be reported to the client anymore, so it is
// only logged (in practice: the client hung up mid-write).
func (m *Mount) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		m.logger.Warn("dashboard/handlers: response write failed", "error", err)
	}
}

// contentETag derives the §1.5 opaque cache key: the resource key plus an
// FNV-1a hash of the marshaled data payload. The pinned Store interface
// exposes no raw file mtimes, but every mtime-visible change surfaces in the
// payload itself (last_update, counts, scores), so the hash changes exactly
// when the representation changes — the same invalidation property as a
// max-mtime key, with byte-level precision for filtered/paginated views.
func contentETag(resource string, data []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return fmt.Sprintf("%s:%016x", resource, h.Sum64())
}

// ifNoneMatchSatisfied reports whether the request's If-None-Match header
// matches etag: comma-separated candidates, weak comparison (a W/ prefix and
// surrounding quotes are ignored), and "*" matching any current
// representation (RFC 9110 §13.1.2).
func ifNoneMatchSatisfied(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, cand := range strings.Split(header, ",") {
		cand = strings.TrimPrefix(strings.TrimSpace(cand), "W/")
		cand = strings.Trim(cand, `"`)
		if cand == "*" || cand == etag {
			return true
		}
	}
	return false
}
