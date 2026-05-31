package events

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func newEnv(t *testing.T, typ string) Envelope {
	t.Helper()
	e, err := NewEnvelope(typ, "src", "key-1", time.Now(), nil)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	return e
}

func TestDispatchInvalidEnvelope(t *testing.T) {
	d := NewDispatcher(NewRegistry(), nil)
	if err := d.Dispatch(Envelope{Type: "event.x"}); err == nil {
		t.Fatalf("expected validation error for envelope missing source/key")
	}
}

func TestDispatchToRegisteredHandler(t *testing.T) {
	r := NewRegistry()
	d := NewDispatcher(r, nil)
	var got Envelope
	mustOn(t, d, "event.pr.merged", HandlerFunc(func(e Envelope) error {
		got = e
		return nil
	}))
	env := newEnv(t, "event.pr.merged")
	if err := d.Dispatch(env); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got.Type != "event.pr.merged" {
		t.Fatalf("handler not invoked, got %+v", got)
	}
}

func TestDispatchHandlerError(t *testing.T) {
	d := NewDispatcher(NewRegistry(), nil)
	sentinel := errors.New("boom")
	mustOn(t, d, "event.pr.merged", HandlerFunc(func(Envelope) error { return sentinel }))
	if err := d.Dispatch(newEnv(t, "event.pr.merged")); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestDispatchUnregisteredReject(t *testing.T) {
	r := NewRegistry()
	mustNS(t, r, "event.pr", DispositionReject)
	d := NewDispatcher(r, nil)
	err := d.Dispatch(newEnv(t, "event.pr.merged"))
	if err == nil || !strings.Contains(err.Error(), "reject namespace") {
		t.Fatalf("expected loud reject error, got %v", err)
	}
}

func TestDispatchSoftWithGenericHandler(t *testing.T) {
	r := NewRegistry()
	mustNS(t, r, "event.metric", DispositionSoft)
	var buf bytes.Buffer
	d := NewDispatcher(r, &buf)
	var routed string
	d.SetSoftHandler(HandlerFunc(func(e Envelope) error {
		routed = e.Type
		return nil
	}))
	if err := d.Dispatch(newEnv(t, "event.metric.cpu")); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if routed != "event.metric.cpu" {
		t.Fatalf("soft handler not invoked, routed=%q", routed)
	}
	if !strings.Contains(buf.String(), "WARNING soft-routing") {
		t.Fatalf("expected warning log, got %q", buf.String())
	}
}

func TestDispatchSoftWithoutGenericHandler(t *testing.T) {
	r := NewRegistry()
	mustNS(t, r, "event.metric", DispositionSoft)
	d := NewDispatcher(r, nil) // nil logw -> io.Discard
	if err := d.Dispatch(newEnv(t, "event.metric.cpu")); err != nil {
		t.Fatalf("soft route with no handler should be a no-op, got %v", err)
	}
}

func TestDispatcherOnErrors(t *testing.T) {
	d := NewDispatcher(NewRegistry(), nil)
	if err := d.On("", HandlerFunc(func(Envelope) error { return nil })); err == nil {
		t.Fatalf("expected error for empty type")
	}
	if err := d.On("event.x", nil); err == nil {
		t.Fatalf("expected error for nil handler")
	}
}

func mustOn(t *testing.T, d *Dispatcher, typ string, h Handler) {
	t.Helper()
	if err := d.On(typ, h); err != nil {
		t.Fatalf("On(%q): %v", typ, err)
	}
}
