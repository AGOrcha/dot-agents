package auth

import "testing"

func TestParseRole(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Role
		wantErr bool
	}{
		{"reviewer", "reviewer", RoleReviewer, false},
		{"admin", "admin", RoleAdmin, false},
		{"readonly", "readonly", RoleReadonly, false},
		{"unknown", "superuser", "", true},
		{"empty", "", "", true},
		{"case sensitive", "Admin", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRole(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRole(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseRole(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRoleValid(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleReviewer, true},
		{RoleAdmin, true},
		{RoleReadonly, true},
		{Role("nope"), false},
		{Role(""), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid()=%v want %v", tt.role, got, tt.want)
		}
	}
}

func TestRoleCan(t *testing.T) {
	tests := []struct {
		name string
		role Role
		perm Permission
		want bool
	}{
		{"readonly reads", RoleReadonly, PermReadLabels, true},
		{"readonly cannot write", RoleReadonly, PermWriteLabels, false},
		{"readonly cannot manage users", RoleReadonly, PermManageUsers, false},
		{"reviewer reads", RoleReviewer, PermReadLabels, true},
		{"reviewer writes", RoleReviewer, PermWriteLabels, true},
		{"reviewer cannot manage users", RoleReviewer, PermManageUsers, false},
		{"reviewer cannot admin labels", RoleReviewer, PermAdminLabels, false},
		{"reviewer cannot read audit", RoleReviewer, PermReadAudit, false},
		{"admin reads", RoleAdmin, PermReadLabels, true},
		{"admin writes", RoleAdmin, PermWriteLabels, true},
		{"admin manages users", RoleAdmin, PermManageUsers, true},
		{"admin admins labels", RoleAdmin, PermAdminLabels, true},
		{"admin reads audit", RoleAdmin, PermReadAudit, true},
		{"unknown role grants nothing", Role("nope"), PermReadLabels, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.Can(tt.perm); got != tt.want {
				t.Errorf("Role(%q).Can(%q)=%v want %v", tt.role, tt.perm, got, tt.want)
			}
		})
	}
}

func TestRolePermissions(t *testing.T) {
	tests := []struct {
		role     Role
		wantLen  int
		contains Permission
	}{
		{RoleReadonly, 1, PermReadLabels},
		{RoleReviewer, 2, PermWriteLabels},
		{RoleAdmin, 5, PermManageUsers},
		{Role("nope"), 0, ""},
	}
	for _, tt := range tests {
		perms := tt.role.Permissions()
		if len(perms) != tt.wantLen {
			t.Errorf("Role(%q).Permissions() len=%d want %d", tt.role, len(perms), tt.wantLen)
		}
		if tt.contains != "" {
			found := false
			for _, p := range perms {
				if p == tt.contains {
					found = true
				}
			}
			if !found {
				t.Errorf("Role(%q).Permissions() missing %q", tt.role, tt.contains)
			}
		}
	}
}

// TestRolePermissionsIsCopy guards the canonical table from external mutation.
func TestRolePermissionsIsCopy(t *testing.T) {
	perms := RoleAdmin.Permissions()
	if len(perms) == 0 {
		t.Fatal("admin should have permissions")
	}
	perms[0] = Permission("tampered")
	if RoleAdmin.Permissions()[0] == Permission("tampered") {
		t.Fatal("Permissions() returned a reference to the canonical table")
	}
}
