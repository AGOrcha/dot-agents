package config

// Coverage for the `gitignore_projections` knob (config-distribution-model §15
// D14): the full AgentsRC field lifecycle (decode → typed field, not
// ExtraFields; re-encode → omitted when absent, emitted when set) plus the
// tri-state default-on accessor.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentsRCGitignoreProjections_DecodeRoundTrip(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name string
		raw  string
		want *bool
		// wantEmitted is the substring the re-encoded manifest must (or must
		// not) carry, proving omitempty behaves for the absent case.
		wantEmitted bool
	}{
		{
			name: "absent key decodes to nil and is not re-emitted",
			raw:  `{"version":2}`,
			want: nil,
		},
		{
			name:        "explicit false is preserved",
			raw:         `{"version":2,"gitignore_projections":false}`,
			want:        boolPtr(false),
			wantEmitted: true,
		},
		{
			name:        "explicit true is preserved",
			raw:         `{"version":2,"gitignore_projections":true}`,
			want:        boolPtr(true),
			wantEmitted: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rc AgentsRC
			if err := json.Unmarshal([]byte(tc.raw), &rc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			switch {
			case tc.want == nil && rc.GitignoreProjections != nil:
				t.Fatalf("expected nil, got %v", *rc.GitignoreProjections)
			case tc.want != nil && rc.GitignoreProjections == nil:
				t.Fatalf("expected %v, got nil", *tc.want)
			case tc.want != nil && *rc.GitignoreProjections != *tc.want:
				t.Fatalf("got %v, want %v", *rc.GitignoreProjections, *tc.want)
			}

			// The key is "known": it must never land in ExtraFields, which
			// would re-emit it twice and defeat the typed field.
			if _, leaked := rc.ExtraFields["gitignore_projections"]; leaked {
				t.Error("gitignore_projections leaked into ExtraFields")
			}

			out, err := json.Marshal(rc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := strings.Contains(string(out), "gitignore_projections")
			if got != tc.wantEmitted {
				t.Errorf("re-emitted=%v, want %v: %s", got, tc.wantEmitted, out)
			}

			// Round-trip stability: decoding the re-encoded form yields the
			// same tri-state value.
			var back AgentsRC
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if back.GitignoreProjectionsEnabled() != rc.GitignoreProjectionsEnabled() {
				t.Errorf("round-trip changed effective value: %v -> %v",
					rc.GitignoreProjectionsEnabled(), back.GitignoreProjectionsEnabled())
			}
		})
	}
}

func TestAgentsRCGitignoreProjectionsEnabled(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name string
		rc   *AgentsRC
		want bool
	}{
		{name: "nil manifest defaults on", rc: nil, want: true},
		{name: "absent key defaults on", rc: &AgentsRC{Version: 2}, want: true},
		{name: "explicit true", rc: &AgentsRC{GitignoreProjections: boolPtr(true)}, want: true},
		{name: "explicit false opts out", rc: &AgentsRC{GitignoreProjections: boolPtr(false)}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rc.GitignoreProjectionsEnabled(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
