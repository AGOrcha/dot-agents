package events

import (
	"context"
	"testing"
)

func TestDispositionString(t *testing.T) {
	tests := []struct {
		disp Disposition
		want string
	}{
		{DispositionReject, "reject"},
		{DispositionSoft, "soft"},
		{Disposition(99), "Disposition(99)"},
	}
	for _, tc := range tests {
		if got := tc.disp.String(); got != tc.want {
			t.Fatalf("Disposition(%d).String() = %q, want %q", int(tc.disp), got, tc.want)
		}
	}
}

func TestRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("event.pr.merged", DispositionReject); err != nil {
		t.Fatalf("Register: %v", err)
	}
	k, ok := r.Lookup("event.pr.merged")
	if !ok {
		t.Fatalf("expected kind to be found")
	}
	if k.Name != "event.pr.merged" || k.Disposition != DispositionReject {
		t.Fatalf("unexpected kind %+v", k)
	}
	if _, ok := r.Lookup("event.unknown"); ok {
		t.Fatalf("unexpected lookup hit for unknown kind")
	}
}

func TestRegisterEmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("", DispositionSoft); err == nil {
		t.Fatalf("expected error for empty name")
	}
	if err := r.SetNamespaceDisposition("", DispositionSoft); err == nil {
		t.Fatalf("expected error for empty namespace")
	}
}

func TestRegisterProducer(t *testing.T) {
	r := NewRegistry()
	factory := func() (*Producer, error) {
		return NewProducer(ProducerConfig{
			Type:   "event.metric.cpu",
			Source: "metrics",
			Each:   ".items",
			Map:    map[string]string{"id": ".id"},
		}, &fakeFetcher{out: []byte(`{"items":[]}`)})
	}
	if err := r.RegisterProducer("event.metric.cpu", factory); err != nil {
		t.Fatalf("RegisterProducer: %v", err)
	}
	k, ok := r.Lookup("event.metric.cpu")
	if !ok || k.Producer == nil {
		t.Fatalf("expected producer to be registered, got %+v ok=%v", k, ok)
	}
	if k.Disposition != DispositionReject {
		t.Fatalf("producer kind should default to reject, got %v", k.Disposition)
	}
	p, err := k.Producer()
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, err := p.Cycle(context.Background()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
}

func TestRegisterProducerPreservesDisposition(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("event.metric.cpu", DispositionSoft); err != nil {
		t.Fatalf("Register: %v", err)
	}
	factory := func() (*Producer, error) { return nil, nil }
	if err := r.RegisterProducer("event.metric.cpu", factory); err != nil {
		t.Fatalf("RegisterProducer: %v", err)
	}
	k, _ := r.Lookup("event.metric.cpu")
	if k.Disposition != DispositionSoft {
		t.Fatalf("disposition overwritten: got %v", k.Disposition)
	}
}

func TestRegisterProducerErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterProducer("", nil); err == nil {
		t.Fatalf("expected error for empty name")
	}
	if err := r.RegisterProducer("event.x", nil); err == nil {
		t.Fatalf("expected error for nil factory")
	}
}

func TestDispositionForResolution(t *testing.T) {
	r := NewRegistry()
	mustNS(t, r, "event.metric", DispositionSoft)
	mustNS(t, r, "event", DispositionReject)
	if err := r.Register("event.metric.exact", DispositionReject); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tests := []struct {
		typ  string
		want Disposition
	}{
		{"event.metric.exact", DispositionReject}, // exact kind wins
		{"event.metric.cpu", DispositionSoft},     // longest namespace match
		{"event.pr.merged", DispositionReject},    // shorter namespace match
		{"sentinel.tick", DispositionReject},      // falls to registry default
	}
	for _, tc := range tests {
		t.Run(tc.typ, func(t *testing.T) {
			if got := r.DispositionFor(tc.typ); got != tc.want {
				t.Fatalf("DispositionFor(%q) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

func TestRegistryNames(t *testing.T) {
	r := NewRegistry()
	mustReg(t, r, "event.b", DispositionReject)
	mustReg(t, r, "event.a", DispositionReject)
	names := r.Names()
	if len(names) != 2 || names[0] != "event.a" || names[1] != "event.b" {
		t.Fatalf("Names() = %v, want sorted [event.a event.b]", names)
	}
}

func TestNamespaceMatches(t *testing.T) {
	tests := []struct {
		typ, ns string
		want    bool
	}{
		{"event.pr", "event.pr", true},
		{"event.pr.merged", "event.pr", true},
		{"event.prx.y", "event.pr", false},
		{"event", "event.pr", false},
	}
	for _, tc := range tests {
		if got := namespaceMatches(tc.typ, tc.ns); got != tc.want {
			t.Fatalf("namespaceMatches(%q,%q) = %v, want %v", tc.typ, tc.ns, got, tc.want)
		}
	}
}

func mustReg(t *testing.T, r *Registry, name string, d Disposition) {
	t.Helper()
	if err := r.Register(name, d); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
}

func mustNS(t *testing.T, r *Registry, ns string, d Disposition) {
	t.Helper()
	if err := r.SetNamespaceDisposition(ns, d); err != nil {
		t.Fatalf("SetNamespaceDisposition(%q): %v", ns, err)
	}
}
