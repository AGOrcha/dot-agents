// Package auth implements the R5 RBAC identity layer: a pluggable Authenticator
// interface, an admin-managed users file, bearer-token issuance and verification,
// and the role/permission model the review service enforces.
//
// The package is pure (no HTTP). Downstream HTTP handlers consume the
// Authenticator interface and the Permission checks defined here.
package auth

import "fmt"

// Role is one of the three single-tenant roles defined by R5 design D5.3.
type Role string

const (
	// RoleReviewer can read and submit/edit their own labels.
	RoleReviewer Role = "reviewer"
	// RoleAdmin can do everything a reviewer can plus manage users, edit any
	// label, and administer the audit log.
	RoleAdmin Role = "admin"
	// RoleReadonly can read labels but cannot mutate anything.
	RoleReadonly Role = "readonly"
)

// Permission is a capability the review service gates on. Permissions are
// derived from roles via RolePermissions; handlers check a permission rather
// than a role so the role→capability mapping stays in one place.
type Permission string

const (
	// PermReadLabels allows fetching labels.
	PermReadLabels Permission = "labels:read"
	// PermWriteLabels allows submitting and editing labels.
	PermWriteLabels Permission = "labels:write"
	// PermManageUsers allows creating, listing, and removing users.
	PermManageUsers Permission = "users:manage"
	// PermAdminLabels allows editing any reviewer's label (admin correction).
	PermAdminLabels Permission = "labels:admin"
	// PermReadAudit allows reading and verifying the audit log.
	PermReadAudit Permission = "audit:read"
)

// rolePermissions is the canonical role→permission table.
var rolePermissions = map[Role][]Permission{
	RoleReadonly: {PermReadLabels},
	RoleReviewer: {PermReadLabels, PermWriteLabels},
	RoleAdmin: {
		PermReadLabels,
		PermWriteLabels,
		PermManageUsers,
		PermAdminLabels,
		PermReadAudit,
	},
}

// Valid reports whether r is a recognized role.
func (r Role) Valid() bool {
	_, ok := rolePermissions[r]
	return ok
}

// ParseRole validates and normalizes a role string.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("auth: unknown role %q (want reviewer|admin|readonly)", s)
	}
	return r, nil
}

// Permissions returns the permission set granted to the role. The returned
// slice is a copy; callers may not mutate the canonical table.
func (r Role) Permissions() []Permission {
	perms := rolePermissions[r]
	out := make([]Permission, len(perms))
	copy(out, perms)
	return out
}

// Can reports whether the role grants the given permission.
func (r Role) Can(p Permission) bool {
	for _, have := range rolePermissions[r] {
		if have == p {
			return true
		}
	}
	return false
}
