package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	nethttp "net/http"
	"strconv"
)

// contentTypeJSON is the response media type for every JSON body.
const contentTypeJSON = "application/json"

// maxBodyBytes bounds a request body; the largest legitimate payload (a label
// with a 4000-char free text) is far below 1 MiB.
const maxBodyBytes = 1 << 20

// marshalJSON is a seam over json.Marshal so the otherwise-unreachable marshal
// error branches (writeJSON, hashJSON — the DTOs are plain structs) are
// coverable in tests, following the internal/review/audit pattern.
var marshalJSON = json.Marshal

// errorBody is the uniform JSON error shape.
type errorBody struct {
	Error string `json:"error"`
	// RequestID is present on mutating-route failures so the caller can
	// reconcile against the audit log before retrying.
	RequestID string `json:"request_id,omitempty"`
}

// writeJSON renders v as a JSON response with the given status.
func writeJSON(w nethttp.ResponseWriter, status int, v any) {
	data, err := marshalJSON(v)
	if err != nil {
		nethttp.Error(w, "encode response", nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// writeError renders the uniform error body.
func writeError(w nethttp.ResponseWriter, status int, msg, requestID string) {
	writeJSON(w, status, errorBody{Error: msg, RequestID: requestID})
}

// decodeJSON parses a JSON request body into v, rejecting unknown fields so a
// misspelled payload key fails loudly instead of silently defaulting. On
// failure it writes a 400 and reports false.
func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, v any) bool {
	dec := json.NewDecoder(nethttp.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, nethttp.StatusBadRequest, "invalid request body: "+err.Error(), "")
		return false
	}
	return true
}

// iterationParam parses the {iteration} path segment. On failure it writes a
// 400 and reports false.
func iterationParam(w nethttp.ResponseWriter, r *nethttp.Request) (int, bool) {
	iter, err := strconv.Atoi(r.PathValue("iteration"))
	if err != nil || iter < 0 {
		writeError(w, nethttp.StatusBadRequest, "iteration must be a non-negative integer", "")
		return 0, false
	}
	return iter, true
}

// hashJSON returns the hex SHA-256 of v's JSON encoding — the before/after
// content hashes recorded on audit events. The DTOs passed in are plain
// structs, so the marshal error branch is defensive only (empty hash rather
// than a failed mutation response).
func hashJSON(v any) string {
	data, err := marshalJSON(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
