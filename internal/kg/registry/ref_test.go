package registry

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Version
		wantErr bool
	}{
		{name: "full", in: "1.2.3", want: Version{1, 2, 3}},
		{name: "major only", in: "1", want: Version{1, 0, 0}},
		{name: "major minor", in: "1.2", want: Version{1, 2, 0}},
		{name: "whitespace trimmed", in: "  2.0.1 ", want: Version{2, 0, 1}},
		{name: "empty", in: "", wantErr: true},
		{name: "too many components", in: "1.2.3.4", wantErr: true},
		{name: "non-integer", in: "1.x.0", wantErr: true},
		{name: "negative", in: "1.-2.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseVersion(%q) want error, got nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseVersion(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	if got := (Version{1, 4, 2}).String(); got != "1.4.2" {
		t.Fatalf("String() = %q, want 1.4.2", got)
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantCaret bool
		wantBase  Version
		wantErr   bool
	}{
		{name: "caret", in: "^1.0", wantCaret: true, wantBase: Version{1, 0, 0}},
		{name: "exact", in: "1.2.3", wantCaret: false, wantBase: Version{1, 2, 3}},
		{name: "caret zero", in: "^0.3", wantCaret: true, wantBase: Version{0, 3, 0}},
		{name: "empty", in: "", wantErr: true},
		{name: "bad version", in: "^x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConstraint(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseConstraint(%q) want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConstraint(%q) unexpected error: %v", tt.in, err)
			}
			if got.Caret != tt.wantCaret || got.Base != tt.wantBase {
				t.Fatalf("ParseConstraint(%q) = %+v, want caret=%v base=%+v", tt.in, got, tt.wantCaret, tt.wantBase)
			}
		})
	}
}

func TestConstraintSatisfies(t *testing.T) {
	tests := []struct {
		name string
		c    string
		v    Version
		want bool
	}{
		{name: "exact match", c: "1.2.3", v: Version{1, 2, 3}, want: true},
		{name: "exact mismatch", c: "1.2.3", v: Version{1, 2, 4}, want: false},
		{name: "caret same major higher minor", c: "^1.0", v: Version{1, 5, 0}, want: true},
		{name: "caret same exact", c: "^1.0", v: Version{1, 0, 0}, want: true},
		{name: "caret lower than base", c: "^1.2", v: Version{1, 1, 0}, want: false},
		{name: "caret different major", c: "^1.0", v: Version{2, 0, 0}, want: false},
		{name: "caret zero same minor", c: "^0.3", v: Version{0, 3, 5}, want: true},
		{name: "caret zero different minor", c: "^0.3", v: Version{0, 4, 0}, want: false},
		{name: "caret patch bump", c: "^1.0.1", v: Version{1, 0, 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.c)
			if err != nil {
				t.Fatalf("ParseConstraint(%q): %v", tt.c, err)
			}
			if got := c.Satisfies(tt.v); got != tt.want {
				t.Fatalf("%q.Satisfies(%v) = %v, want %v", tt.c, tt.v, got, tt.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		name           string
		in             string
		wantName       string
		wantBuiltin    bool
		wantConstraint bool
		wantErr        bool
	}{
		{name: "builtin with caret", in: "dotagents-builtin:graph/none@^1.0", wantName: "none", wantBuiltin: true, wantConstraint: true},
		{name: "builtin no constraint", in: "dotagents-builtin:graph/none", wantName: "none", wantBuiltin: true},
		{name: "bare with exact", in: "none@1.0.0", wantName: "none", wantConstraint: true},
		{name: "bare name", in: "none", wantName: "none"},
		{name: "whitespace", in: "  none  ", wantName: "none"},
		{name: "empty", in: "", wantErr: true},
		{name: "missing name", in: "@1.0.0", wantErr: true},
		{name: "bad constraint", in: "none@^x", wantErr: true},
		{name: "invalid name chars", in: "no/ne", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRef(%q) want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q) unexpected error: %v", tt.in, err)
			}
			assertRef(t, tt.in, got, tt.wantName, tt.wantBuiltin, tt.wantConstraint)
		})
	}
}

// assertRef checks a parsed Ref's fields against the expected values.
func assertRef(t *testing.T, in string, got Ref, wantName string, wantBuiltin, wantConstraint bool) {
	t.Helper()
	if got.Name != wantName {
		t.Fatalf("ParseRef(%q).Name = %q, want %q", in, got.Name, wantName)
	}
	if got.Builtin != wantBuiltin {
		t.Fatalf("ParseRef(%q).Builtin = %v, want %v", in, got.Builtin, wantBuiltin)
	}
	if (got.Constraint != nil) != wantConstraint {
		t.Fatalf("ParseRef(%q) constraint present = %v, want %v", in, got.Constraint != nil, wantConstraint)
	}
}
