package eval

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// GenerateOptions carries the per-call inputs a generator needs to frame a
// task. It is intentionally small and I/O-free at this layer; downstream
// generators thread their own KG reader and other collaborators through their
// constructor, not through this struct.
type GenerateOptions struct {
	// Difficulty optionally constrains the band of the generated task. The
	// zero value (empty string) lets the generator choose.
	Difficulty Difficulty
	// TemplateID optionally selects a specific template; empty lets the
	// generator pick.
	TemplateID string
}

// Generator produces a versioned TaskSpec for a single language. Each
// per-language adapter (internal/eval/gen/<lang>) implements this interface
// and registers itself in a Registry. The interface is the seam between the
// language-agnostic harness and language-specific task synthesis.
type Generator interface {
	// Language reports the language this generator produces tasks for. A
	// generator handles exactly one language.
	Language() Language
	// Generate synthesizes one TaskSpec. Implementations must return a spec
	// that passes TaskSpec.Validate, or an error.
	Generate(ctx context.Context, opts GenerateOptions) (*TaskSpec, error)
}

// Registry maps a Language to the Generator that produces tasks for it. It is
// safe for concurrent use. A Registry is the lookup surface the harness uses
// to resolve `da eval gen --language <lang>` to a concrete generator.
type Registry struct {
	mu  sync.RWMutex
	gen map[Language]Generator
}

// NewRegistry returns an empty Registry ready for use.
func NewRegistry() *Registry {
	return &Registry{gen: make(map[Language]Generator)}
}

// Register adds g to the registry keyed by its Language. It errors on a nil
// generator, an invalid language, or a duplicate registration so collisions
// surface at wiring time rather than silently shadowing.
func (r *Registry) Register(g Generator) error {
	if g == nil {
		return fmt.Errorf("eval: cannot register nil generator")
	}
	lang := g.Language()
	if !lang.Valid() {
		return fmt.Errorf("eval: cannot register generator for invalid language %q", lang)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.gen[lang]; ok {
		return fmt.Errorf("eval: generator for language %q already registered", lang)
	}
	r.gen[lang] = g
	return nil
}

// Lookup returns the generator registered for lang. The boolean is false when
// no generator is registered for the language.
func (r *Registry) Lookup(lang Language) (Generator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.gen[lang]
	return g, ok
}

// Languages returns the registered languages in sorted order.
func (r *Registry) Languages() []Language {
	r.mu.RLock()
	defer r.mu.RUnlock()
	langs := make([]Language, 0, len(r.gen))
	for l := range r.gen {
		langs = append(langs, l)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}
