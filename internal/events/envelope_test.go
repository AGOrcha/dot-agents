package events

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNewEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		source  string
		key     string
		wantErr bool
	}{
		{"ok", "event.pr.merged", "github", "pr-1", false},
		{"empty type", "", "github", "pr-1", true},
		{"blank type", "   ", "github", "pr-1", true},
		{"empty source", "event.pr.merged", "", "pr-1", true},
		{"empty key", "event.pr.merged", "github", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, err := NewEnvelope(tc.typ, tc.source, tc.key, time.Time{}, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, errEmpty) {
					t.Fatalf("expected errEmpty, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if env.OccurredAt.IsZero() {
				t.Fatalf("OccurredAt should default to now")
			}
		})
	}
}

func TestNewEnvelopeKeepsExplicitTime(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	env, err := NewEnvelope("event.metric.x", "src", "k", ts, json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !env.OccurredAt.Equal(ts) {
		t.Fatalf("OccurredAt = %v, want %v", env.OccurredAt, ts)
	}
	if string(env.Payload) != `{"a":1}` {
		t.Fatalf("payload not preserved: %s", env.Payload)
	}
}

func TestEnvelopeNamespace(t *testing.T) {
	tests := []struct {
		typ  string
		want string
	}{
		{"event.pr.merged", "event.pr"},
		{"event.metric.cpu", "event.metric"},
		{"sentinel.tick", "sentinel"},
		{"flat", "flat"},
		{".leading", ".leading"},
	}
	for _, tc := range tests {
		t.Run(tc.typ, func(t *testing.T) {
			e := Envelope{Type: tc.typ}
			if got := e.Namespace(); got != tc.want {
				t.Fatalf("Namespace(%q) = %q, want %q", tc.typ, got, tc.want)
			}
		})
	}
}
