package labels

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// failReader always errors, used to drive newID's entropy-failure path.
type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestNewIDEntropyFailure(t *testing.T) {
	orig := randReader
	randReader = failReader{}
	defer func() { randReader = orig }()
	if _, err := newID(); err == nil {
		t.Fatal("want entropy error, got nil")
	}
}

func validStructured() Structured {
	return Structured{
		Correctness:    2,
		ScopeJudgement: ScopeOnTarget,
		Hallucination:  HallucinationNone,
	}
}

func validLabel() Label {
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	return Label{
		ID:            "abc123",
		Iteration:     3,
		Actor:         "reviewer@example.com",
		Role:          RoleReviewer,
		SchemaVersion: LabelSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
		History: []Edit{{
			Actor:      "reviewer@example.com",
			Role:       RoleReviewer,
			Timestamp:  now,
			Structured: validStructured(),
			FreeText:   "looks good",
		}},
	}
}

func TestLabelValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Label)
		wantErr error
	}{
		{"valid", func(*Label) {}, nil},
		{"empty id", func(l *Label) { l.ID = "  " }, ErrEmptyID},
		{"empty actor", func(l *Label) { l.Actor = "" }, ErrEmptyActor},
		{"invalid role", func(l *Label) { l.Role = Role("ceo") }, ErrInvalidRole},
		{"negative iteration", func(l *Label) { l.Iteration = -1 }, ErrNegativeIteration},
		{"empty schema version", func(l *Label) { l.SchemaVersion = "" }, ErrSchemaVersionEmpty},
		{"empty history", func(l *Label) { l.History = nil }, ErrEmptyHistory},
		{"correctness too high", func(l *Label) { l.History[0].Structured.Correctness = 4 }, ErrCorrectnessRange},
		{"correctness too low", func(l *Label) { l.History[0].Structured.Correctness = -1 }, ErrCorrectnessRange},
		{"invalid scope", func(l *Label) { l.History[0].Structured.ScopeJudgement = "sideways" }, ErrInvalidScope},
		{"invalid hallucination", func(l *Label) { l.History[0].Structured.Hallucination = "yes" }, ErrInvalidHalluc},
		{"free text too long", func(l *Label) { l.History[0].FreeText = strings.Repeat("x", FreeTextMaxLen+1) }, ErrFreeTextTooLong},
		{"edit empty actor", func(l *Label) { l.History[0].Actor = " " }, ErrEmptyActor},
		{"edit invalid role", func(l *Label) { l.History[0].Role = "bogus" }, ErrInvalidRole},
		{"free text at max ok", func(l *Label) { l.History[0].FreeText = strings.Repeat("x", FreeTextMaxLen) }, nil},
		{"correctness at max ok", func(l *Label) { l.History[0].Structured.Correctness = CorrectnessMax }, nil},
		{"correctness at min ok", func(l *Label) { l.History[0].Structured.Correctness = CorrectnessMin }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := validLabel()
			tc.mutate(&l)
			err := l.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSidecarValidate(t *testing.T) {
	base := func() Sidecar {
		l := validLabel()
		return Sidecar{Iteration: 3, SchemaVersion: LabelSchemaVersion, Labels: []Label{l}}
	}
	tests := []struct {
		name    string
		mutate  func(*Sidecar)
		wantErr error
	}{
		{"valid", func(*Sidecar) {}, nil},
		{"empty labels valid", func(s *Sidecar) { s.Labels = []Label{} }, nil},
		{"negative iteration", func(s *Sidecar) { s.Iteration = -2 }, ErrNegativeIteration},
		{"empty schema version", func(s *Sidecar) { s.SchemaVersion = "" }, ErrSchemaVersionEmpty},
		{"label invalid bubbles up", func(s *Sidecar) { s.Labels[0].ID = "" }, ErrEmptyID},
		{"iteration mismatch", func(s *Sidecar) { s.Labels[0].Iteration = 99 }, ErrIterationMismatch},
		{"duplicate id", func(s *Sidecar) {
			dup := validLabel()
			s.Labels = append(s.Labels, dup)
		}, ErrDuplicateID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mutate(&s)
			err := s.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLabelAccessors(t *testing.T) {
	l := validLabel()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	l.History = append(l.History, Edit{
		Actor:      "admin@example.com",
		Role:       RoleAdmin,
		Timestamp:  now,
		Structured: Structured{Correctness: 3, ScopeJudgement: ScopeBreach, Hallucination: HallucinationMinor},
		FreeText:   "revised",
	})
	if got := l.Latest().Actor; got != "admin@example.com" {
		t.Fatalf("Latest actor = %q", got)
	}
	if got := l.EffectiveStructured().Correctness; got != 3 {
		t.Fatalf("EffectiveStructured.Correctness = %d", got)
	}
	if got := l.EffectiveFreeText(); got != "revised" {
		t.Fatalf("EffectiveFreeText = %q", got)
	}
}

func TestNewIDUniqueAndShape(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id, err := newID()
		if err != nil {
			t.Fatalf("newID: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("id len = %d, want 32 (%q)", len(id), id)
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("non-hex char %q in id %q", c, id)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}
