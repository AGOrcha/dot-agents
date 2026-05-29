package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGenerator is a test double proving Generator interface conformance and
// exercising the registry without any I/O.
type fakeGenerator struct {
	lang    Language
	spec    *TaskSpec
	genErr  error
	calls   int
	lastOpt GenerateOptions
}

func (f *fakeGenerator) Language() Language { return f.lang }

func (f *fakeGenerator) Generate(_ context.Context, opts GenerateOptions) (*TaskSpec, error) {
	f.calls++
	f.lastOpt = opts
	if f.genErr != nil {
		return nil, f.genErr
	}
	return f.spec, nil
}

// compile-time conformance assertion.
var _ Generator = (*fakeGenerator)(nil)

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	g := &fakeGenerator{lang: LanguageGo}
	if err := r.Register(g); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	got, ok := r.Lookup(LanguageGo)
	if !ok {
		t.Fatalf("Lookup(go) ok = false, want true")
	}
	if got != g {
		t.Errorf("Lookup(go) returned a different generator")
	}
}

func TestRegistryLookupMissing(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Lookup(LanguagePython); ok {
		t.Errorf("Lookup on empty registry ok = true, want false")
	}
	// register one language, look up another.
	if err := r.Register(&fakeGenerator{lang: LanguageGo}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, ok := r.Lookup(LanguageTypeScript); ok {
		t.Errorf("Lookup(typescript) ok = true, want false")
	}
}

func TestRegistryRegisterErrors(t *testing.T) {
	tests := []struct {
		name    string
		gen     Generator
		wantErr string
	}{
		{"nil generator", nil, "nil generator"},
		{"invalid language", &fakeGenerator{lang: "cobol"}, "invalid language"},
		{"empty language", &fakeGenerator{lang: ""}, "invalid language"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			err := r.Register(tc.gen)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Register() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestRegistryDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeGenerator{lang: LanguageGo}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	err := r.Register(&fakeGenerator{lang: LanguageGo})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate Register() = %v, want already-registered error", err)
	}
}

func TestRegistryLanguages(t *testing.T) {
	r := NewRegistry()
	if got := r.Languages(); len(got) != 0 {
		t.Fatalf("Languages() on empty = %v, want []", got)
	}
	// register out of sorted order.
	for _, l := range []Language{LanguageTypeScript, LanguageGo, LanguagePython} {
		if err := r.Register(&fakeGenerator{lang: l}); err != nil {
			t.Fatalf("Register(%q) error = %v", l, err)
		}
	}
	got := r.Languages()
	want := []Language{LanguageGo, LanguagePython, LanguageTypeScript}
	if len(got) != len(want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Languages()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestGeneratorConformanceViaRegistry(t *testing.T) {
	spec := validSpec()
	g := &fakeGenerator{lang: LanguageGo, spec: &spec}
	r := NewRegistry()
	if err := r.Register(g); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resolved, ok := r.Lookup(LanguageGo)
	if !ok {
		t.Fatalf("Lookup(go) failed")
	}
	opts := GenerateOptions{Difficulty: DifficultyHard, TemplateID: "impl-pure-fn"}
	out, err := resolved.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if out.TaskID != spec.TaskID {
		t.Errorf("Generate() returned wrong spec: %+v", out)
	}
	if g.calls != 1 {
		t.Errorf("generator called %d times, want 1", g.calls)
	}
	if g.lastOpt != opts {
		t.Errorf("options not threaded: got %+v, want %+v", g.lastOpt, opts)
	}
	if resolved.Language() != LanguageGo {
		t.Errorf("Language() = %q, want go", resolved.Language())
	}
}

func TestGeneratorConformanceError(t *testing.T) {
	sentinel := errors.New("kg unavailable")
	g := &fakeGenerator{lang: LanguagePython, genErr: sentinel}
	_, err := g.Generate(context.Background(), GenerateOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Generate() error = %v, want %v", err, sentinel)
	}
}
