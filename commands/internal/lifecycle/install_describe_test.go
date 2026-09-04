package lifecycle

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// TestDescribeStringsOrBool covers the --dry-run preview rendering for the
// optional hooks/mcp declaration.
//
// The nil case is the one that matters: a nil pointer means the key will be
// OMITTED from the manifest (deferring to the config layer stack), which is
// materially different from an explicit `false` (a repo-local override that
// wins the layer merge). The preview must never collapse those two into the
// same string, or a user reviewing `da install --generate --dry-run` cannot
// tell which one they are about to get.
func TestDescribeStringsOrBool(t *testing.T) {
	const absent = "(absent — inherited from config layers)"

	tests := []struct {
		name string
		in   *config.StringsOrBool
		want string
	}{
		{name: "nil is absent, not false", in: nil, want: absent},
		{name: "explicit false", in: &config.StringsOrBool{}, want: "false"},
		{name: "explicit true", in: &config.StringsOrBool{All: true}, want: "true"},
		{
			name: "named list",
			in:   &config.StringsOrBool{Names: []string{"PreToolUse", "PostToolUse"}},
			want: "[PreToolUse PostToolUse]",
		},
		{
			name: "named list wins over All",
			in:   &config.StringsOrBool{All: true, Names: []string{"PreToolUse"}},
			want: "[PreToolUse]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeStringsOrBool(tc.in); got != tc.want {
				t.Errorf("describeStringsOrBool(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDescribeOptionalBool covers the bool analog: absent must be rendered
// distinctly from an explicit false.
func TestDescribeOptionalBool(t *testing.T) {
	const absent = "(absent — inherited from config layers)"
	tr, fa := true, false

	tests := []struct {
		name string
		in   *bool
		want string
	}{
		{name: "nil is absent, not false", in: nil, want: absent},
		{name: "explicit false", in: &fa, want: "false"},
		{name: "explicit true", in: &tr, want: "true"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeOptionalBool(tc.in); got != tc.want {
				t.Errorf("describeOptionalBool(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
