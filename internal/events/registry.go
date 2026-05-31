package events

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Disposition controls what happens when an Envelope whose Type is not
// registered is encountered. It is resolved by the most specific matching
// namespace; an exact kind registration always wins.
type Disposition int

const (
	// DispositionReject is the fail-fast default: unregistered types in a
	// reject namespace are loud errors at emit time.
	DispositionReject Disposition = iota
	// DispositionSoft routes unregistered types to a generic handler and logs
	// a warning instead of failing.
	DispositionSoft
)

// String renders a Disposition for diagnostics and error messages.
func (d Disposition) String() string {
	switch d {
	case DispositionSoft:
		return "soft"
	case DispositionReject:
		return "reject"
	default:
		return fmt.Sprintf("Disposition(%d)", int(d))
	}
}

// Kind is a registered event kind. Producer is optional and only set when a
// producer factory was registered for the kind's name.
type Kind struct {
	Name        string
	Disposition Disposition
	Producer    ProducerFactory
}

// ProducerFactory builds a Producer for a registered kind. It is defined here
// (rather than producer.go) because the registry owns the binding from name to
// factory; producer.go supplies the concrete engine.
type ProducerFactory func() (*Producer, error)

// Registry is a runtime, thread-safe table of event kinds keyed by name plus a
// set of namespace-level dispositions. There is intentionally no Go enum of
// kind names — everything is registered at runtime.
type Registry struct {
	mu                 sync.RWMutex
	kinds              map[string]Kind
	namespaceDisp      map[string]Disposition
	defaultDisposition Disposition
}

// NewRegistry returns an empty Registry whose namespaces default to reject.
func NewRegistry() *Registry {
	return &Registry{
		kinds:              map[string]Kind{},
		namespaceDisp:      map[string]Disposition{},
		defaultDisposition: DispositionReject,
	}
}

// Register adds (or replaces) a kind by exact name with the given disposition.
func (r *Registry) Register(name string, disp Disposition) error {
	if err := requireField("name", name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.kinds[name]
	existing.Name = name
	existing.Disposition = disp
	r.kinds[name] = existing
	return nil
}

// RegisterProducer binds a producer factory to a kind name, creating the kind
// with the default disposition if it was not registered yet.
func (r *Registry) RegisterProducer(name string, factory ProducerFactory) error {
	if err := requireField("name", name); err != nil {
		return err
	}
	if factory == nil {
		return fmt.Errorf("events: nil producer factory for %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.kinds[name]
	if !ok {
		k = Kind{Name: name, Disposition: r.defaultDisposition}
	}
	k.Producer = factory
	r.kinds[name] = k
	return nil
}

// SetNamespaceDisposition declares the disposition for a whole namespace (e.g.
// "event.metric"). Unregistered types in that namespace inherit it.
func (r *Registry) SetNamespaceDisposition(namespace string, disp Disposition) error {
	if err := requireField("namespace", namespace); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namespaceDisp[namespace] = disp
	return nil
}

// Lookup returns the registered kind for an exact name.
func (r *Registry) Lookup(name string) (Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.kinds[name]
	return k, ok
}

// DispositionFor resolves the disposition for a type: an exact kind wins, then
// the longest matching namespace prefix, then the registry default.
func (r *Registry) DispositionFor(typ string) Disposition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if k, ok := r.kinds[typ]; ok {
		return k.Disposition
	}
	return r.namespaceDispositionLocked(typ)
}

// namespaceDispositionLocked finds the most specific namespace disposition for
// typ. Caller must hold at least the read lock.
func (r *Registry) namespaceDispositionLocked(typ string) Disposition {
	best := r.defaultDisposition
	bestLen := -1
	for ns, disp := range r.namespaceDisp {
		if !namespaceMatches(typ, ns) {
			continue
		}
		if len(ns) > bestLen {
			best, bestLen = disp, len(ns)
		}
	}
	return best
}

// Names returns the registered kind names sorted for deterministic output.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.kinds))
	for name := range r.kinds {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// namespaceMatches reports whether typ falls under namespace ns, matching on
// dotted-segment boundaries so "event.pr" matches "event.pr.merged" but not
// "event.prx.y".
func namespaceMatches(typ, ns string) bool {
	if typ == ns {
		return true
	}
	return strings.HasPrefix(typ, ns+".")
}
