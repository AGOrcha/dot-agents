package http

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
)

// userJSON is one user in admin responses. The stored token hash is never
// serialized (spec D5.3: plaintext appears only once at issuance; the hash is
// server-side state).
type userJSON struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at,omitempty"`
}

// usersListJSON is the GET {prefix}/users response.
type usersListJSON struct {
	Users []userJSON `json:"users"`
}

// createUserPayload is the POST {prefix}/users body.
type createUserPayload struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// createUserJSON is the POST {prefix}/users response: the only place the
// plaintext token ever appears (OQ1 print-once).
type createUserJSON struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	Token string `json:"token"`
}

// changeRolePayload is the PATCH {prefix}/users/{email} body.
type changeRolePayload struct {
	Role string `json:"role"`
}

// auditListJSON is the GET {prefix}/audit response. Total is the full record
// count before any ?limit tail-trimming, so a paginating client knows how much
// history exists.
type auditListJSON struct {
	Total   int            `json:"total"`
	Records []audit.Record `json:"records"`
}

// userTarget builds the audit-record target for a user mutation.
func userTarget(email string) string { return "user/" + email }

// addUser is a seam over (*auth.UsersFile).AddUser so the entropy-failure
// branch (token generation) is coverable in tests, following the seam pattern
// in internal/review/audit.
var addUser = (*auth.UsersFile).AddUser

// toUserJSON converts a stored user to its response DTO (hash omitted).
func toUserJSON(u auth.User) userJSON {
	return userJSON{Email: u.Email, Role: string(u.Role), CreatedAt: u.CreatedAt}
}

// handleAuditView serves GET {prefix}/audit (admin). An optional ?limit=N
// returns only the newest N records.
func (m *Mount) handleAuditView(w nethttp.ResponseWriter, r *nethttp.Request) {
	limit, ok := limitParam(w, r)
	if !ok {
		return
	}
	recs, err := m.audit.Records()
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return
	}
	total := len(recs)
	if limit > 0 && limit < total {
		recs = recs[total-limit:]
	}
	writeJSON(w, nethttp.StatusOK, auditListJSON{Total: total, Records: recs})
}

// limitParam parses the optional ?limit query value; absence means no limit.
func limitParam(w nethttp.ResponseWriter, r *nethttp.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		writeError(w, nethttp.StatusBadRequest, "limit must be a non-negative integer", "")
		return 0, false
	}
	return n, true
}

// handleListUsers serves GET {prefix}/users (admin).
func (m *Mount) handleListUsers(w nethttp.ResponseWriter, _ *nethttp.Request) {
	uf, err := m.users.Load()
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return
	}
	out := make([]userJSON, 0, len(uf.Users))
	for _, u := range uf.Users {
		out = append(out, toUserJSON(u))
	}
	writeJSON(w, nethttp.StatusOK, usersListJSON{Users: out})
}

// handleCreateUser serves POST {prefix}/users (admin). The response carries
// the issued plaintext token exactly once; only its argon2id hash persists.
func (m *Mount) handleCreateUser(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := mustIdentity(w, r)
	if !ok {
		return
	}
	var p createUserPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	role, uf, ok := m.loadForUserWrite(w, p.Email, p.Role)
	if !ok {
		return
	}
	token, err := addUser(uf, p.Email, role)
	if errors.Is(err, auth.ErrUserExists) {
		writeError(w, nethttp.StatusConflict, err.Error(), "")
		return
	}
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return
	}
	if !m.saveUsers(w, uf) {
		return
	}
	stageAudit(r.Context(), audit.Event{
		Actor:     id.Email,
		Role:      string(id.Role),
		Action:    audit.ActionUserCreate,
		Target:    userTarget(p.Email),
		AfterHash: hashJSON(userJSON{Email: p.Email, Role: string(role)}),
	})
	writeJSON(w, nethttp.StatusCreated, createUserJSON{Email: p.Email, Role: string(role), Token: token})
}

// handleChangeRole serves PATCH {prefix}/users/{email} (admin).
func (m *Mount) handleChangeRole(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := mustIdentity(w, r)
	if !ok {
		return
	}
	var p changeRolePayload
	if !decodeJSON(w, r, &p) {
		return
	}
	email := r.PathValue("email")
	role, uf, ok := m.loadForUserWrite(w, email, p.Role)
	if !ok {
		return
	}
	idx := findUser(uf, email)
	if idx < 0 {
		writeError(w, nethttp.StatusNotFound, "user not found: "+email, "")
		return
	}
	oldRole := uf.Users[idx].Role
	uf.Users[idx].Role = role
	if !m.saveUsers(w, uf) {
		return
	}
	stageAudit(r.Context(), audit.Event{
		Actor:      id.Email,
		Role:       string(id.Role),
		Action:     audit.ActionRoleChange,
		Target:     userTarget(email),
		BeforeHash: hashJSON(userJSON{Email: email, Role: string(oldRole)}),
		AfterHash:  hashJSON(userJSON{Email: email, Role: string(role)}),
	})
	writeJSON(w, nethttp.StatusOK, userJSON{Email: email, Role: string(role)})
}

// handleDeleteUser serves DELETE {prefix}/users/{email} (admin).
func (m *Mount) handleDeleteUser(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := mustIdentity(w, r)
	if !ok {
		return
	}
	email := r.PathValue("email")
	uf, err := m.users.Load()
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return
	}
	idx := findUser(uf, email)
	if idx < 0 {
		writeError(w, nethttp.StatusNotFound, "user not found: "+email, "")
		return
	}
	before := uf.Users[idx]
	uf.Users = append(uf.Users[:idx], uf.Users[idx+1:]...)
	if !m.saveUsers(w, uf) {
		return
	}
	stageAudit(r.Context(), audit.Event{
		Actor:      id.Email,
		Role:       string(id.Role),
		Action:     audit.ActionUserDelete,
		Target:     userTarget(email),
		BeforeHash: hashJSON(toUserJSON(before)),
	})
	w.WriteHeader(nethttp.StatusNoContent)
}

// loadForUserWrite validates the email/role inputs shared by the user-mutating
// handlers and loads the users file. On any failure it writes the response and
// reports ok=false.
func (m *Mount) loadForUserWrite(w nethttp.ResponseWriter, email, rawRole string) (auth.Role, *auth.UsersFile, bool) {
	if strings.TrimSpace(email) == "" {
		writeError(w, nethttp.StatusBadRequest, "email is required", "")
		return "", nil, false
	}
	role, err := auth.ParseRole(rawRole)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error(), "")
		return "", nil, false
	}
	uf, err := m.users.Load()
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return "", nil, false
	}
	return role, uf, true
}

// saveUsers persists the users file, writing a 500 on failure.
func (m *Mount) saveUsers(w nethttp.ResponseWriter, uf *auth.UsersFile) bool {
	if err := m.users.Save(uf); err != nil {
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
		return false
	}
	return true
}

// findUser returns the index of the user with the given email
// (case-insensitive, whitespace-trimmed), or -1.
func findUser(uf *auth.UsersFile, email string) int {
	want := strings.ToLower(strings.TrimSpace(email))
	for i, u := range uf.Users {
		if strings.ToLower(strings.TrimSpace(u.Email)) == want {
			return i
		}
	}
	return -1
}
