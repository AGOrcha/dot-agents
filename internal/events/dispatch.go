package events

import (
	"fmt"
	"io"
	"sync"
)

// Handler processes a single Envelope. Implementations must be safe to call
// from the Dispatcher and should treat the Envelope as read-only.
type Handler interface {
	Handle(Envelope) error
}

// HandlerFunc adapts an ordinary function to the Handler interface.
type HandlerFunc func(Envelope) error

// Handle calls the underlying function.
func (f HandlerFunc) Handle(e Envelope) error { return f(e) }

// Dispatcher routes Envelopes to Handlers using the Registry to resolve
// disposition. Routing is table-driven: there is no switch on the event type.
type Dispatcher struct {
	mu       sync.RWMutex
	registry *Registry
	handlers map[string]Handler
	soft     Handler
	logw     io.Writer
}

// NewDispatcher builds a Dispatcher over the given registry. logw receives the
// warning lines emitted for soft, unregistered types; pass io.Discard to mute.
func NewDispatcher(registry *Registry, logw io.Writer) *Dispatcher {
	if logw == nil {
		logw = io.Discard
	}
	return &Dispatcher{
		registry: registry,
		handlers: map[string]Handler{},
		logw:     logw,
	}
}

// On registers a per-type handler. The type need not be a registered kind, but
// emit-time validation still applies the namespace disposition.
func (d *Dispatcher) On(typ string, h Handler) error {
	if err := requireField("type", typ); err != nil {
		return err
	}
	if h == nil {
		return fmt.Errorf("events: nil handler for %q", typ)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[typ] = h
	return nil
}

// SetSoftHandler installs the generic handler used for soft-disposition types
// that have no explicit per-type handler.
func (d *Dispatcher) SetSoftHandler(h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.soft = h
}

// Dispatch validates and routes an Envelope. The resolution order is:
//   - invalid envelope          -> error
//   - explicit per-type handler -> invoke it
//   - reject disposition        -> loud fail-fast error
//   - soft disposition          -> warn + generic handler (no-op if unset)
func (d *Dispatcher) Dispatch(e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if h, ok := d.handlerFor(e.Type); ok {
		return h.Handle(e)
	}
	return d.routeUnhandled(e)
}

// routeUnhandled applies the namespace disposition for an envelope that has no
// explicit per-type handler.
func (d *Dispatcher) routeUnhandled(e Envelope) error {
	disp := d.registry.DispositionFor(e.Type)
	if disp == DispositionReject {
		return fmt.Errorf("events: unregistered type %q in reject namespace %q", e.Type, e.Namespace())
	}
	d.warnSoft(e)
	if soft := d.softHandler(); soft != nil {
		return soft.Handle(e)
	}
	return nil
}

func (d *Dispatcher) handlerFor(typ string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h, ok := d.handlers[typ]
	return h, ok
}

func (d *Dispatcher) softHandler() Handler {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.soft
}

func (d *Dispatcher) warnSoft(e Envelope) {
	fmt.Fprintf(d.logw, "events: WARNING soft-routing unregistered type %q (namespace %q)\n", e.Type, e.Namespace())
}
