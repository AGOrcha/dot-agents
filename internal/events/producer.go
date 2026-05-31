package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"time"
)

// FetchSpec describes how the producer obtains the raw JSON document for a
// cycle. Exactly one of Argv (exec) or URL (http) must be set.
type FetchSpec struct {
	Argv    []string          // exec: command and arguments
	URL     string            // http: request URL
	Method  string            // http: defaults to GET
	Headers map[string]string // http: request headers
}

// mode reports whether the spec is exec- or http-driven; it errors when the
// spec is ambiguous or empty.
func (s FetchSpec) mode() (string, error) {
	hasArgv := len(s.Argv) > 0
	hasURL := s.URL != ""
	switch {
	case hasArgv && hasURL:
		return "", fmt.Errorf("events: fetch spec sets both Argv and URL")
	case hasArgv:
		return fetchModeExec, nil
	case hasURL:
		return fetchModeHTTP, nil
	default:
		return "", fmt.Errorf("events: fetch spec sets neither Argv nor URL")
	}
}

const (
	fetchModeExec = "exec"
	fetchModeHTTP = "http"
)

// ProducerConfig drives the generic producer engine. There are deliberately no
// per-platform fields: a GitHub PR producer and a metrics producer differ only
// by config.
type ProducerConfig struct {
	Type   string            // event type emitted for each change
	Source string            // envelope source
	Fetch  FetchSpec         // how to obtain the document
	Each   string            // jsonpath to the list of items
	Map    map[string]string // canonical field -> jsonpath (relative to item)
	KeyBy  string            // canonical field used as the snapshot/idempotency key
}

// Fetcher is the seam isolating exec/http I/O so tests stay hermetic.
type Fetcher interface {
	Fetch(ctx context.Context, spec FetchSpec) ([]byte, error)
}

// Producer is a stateful event source: each Cycle fetches, maps, diffs against
// the previous snapshot, and returns Envelopes for changed items.
type Producer struct {
	cfg      ProducerConfig
	fetcher  Fetcher
	snapshot map[string]string // key -> content fingerprint
}

// NewProducer builds a Producer. A nil fetcher defaults to the real exec/http
// fetcher; tests inject a fake.
func NewProducer(cfg ProducerConfig, fetcher Fetcher) (*Producer, error) {
	if err := requireField("type", cfg.Type); err != nil {
		return nil, err
	}
	if err := requireField("source", cfg.Source); err != nil {
		return nil, err
	}
	if err := requireField("each", cfg.Each); err != nil {
		return nil, err
	}
	if len(cfg.Map) == 0 {
		return nil, fmt.Errorf("events: producer %q has empty Map", cfg.Type)
	}
	if fetcher == nil {
		fetcher = DefaultFetcher{}
	}
	return &Producer{cfg: cfg, fetcher: fetcher, snapshot: map[string]string{}}, nil
}

// Cycle runs one fetch/diff cycle and returns Envelopes for new or changed
// items. The first cycle treats every item as new.
func (p *Producer) Cycle(ctx context.Context) ([]Envelope, error) {
	items, err := p.fetchItems(ctx)
	if err != nil {
		return nil, err
	}
	mapped, err := p.mapItems(items)
	if err != nil {
		return nil, err
	}
	return p.diff(mapped)
}

// fetchItems fetches the document and extracts the Each list.
func (p *Producer) fetchItems(ctx context.Context) ([]any, error) {
	raw, err := p.fetcher.Fetch(ctx, p.cfg.Fetch)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("events: producer %q: decode document: %w", p.cfg.Type, err)
	}
	listVal, err := JSONPath(doc, p.cfg.Each)
	if err != nil {
		return nil, fmt.Errorf("events: producer %q: each %q: %w", p.cfg.Type, p.cfg.Each, err)
	}
	list, ok := listVal.([]any)
	if !ok {
		return nil, fmt.Errorf("events: producer %q: each %q is not a list", p.cfg.Type, p.cfg.Each)
	}
	return list, nil
}

// mapItems applies the canonical field Map to every item.
func (p *Producer) mapItems(items []any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		rec, err := p.mapOne(item)
		if err != nil {
			return nil, fmt.Errorf("events: producer %q: item %d: %w", p.cfg.Type, i, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// mapOne resolves every Map entry against a single item.
func (p *Producer) mapOne(item any) (map[string]any, error) {
	rec := make(map[string]any, len(p.cfg.Map))
	for _, field := range sortedKeys(p.cfg.Map) {
		v, err := JSONPath(item, p.cfg.Map[field])
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field, err)
		}
		rec[field] = v
	}
	return rec, nil
}

// diff compares mapped records to the previous snapshot, emits Envelopes for
// changes, and replaces the snapshot.
func (p *Producer) diff(records []map[string]any) ([]Envelope, error) {
	next := make(map[string]string, len(records))
	var out []Envelope
	for i, rec := range records {
		key, fp, payload, err := p.fingerprint(rec, i)
		if err != nil {
			return nil, err
		}
		next[key] = fp
		if prev, ok := p.snapshot[key]; ok && prev == fp {
			continue
		}
		env, err := NewEnvelope(p.cfg.Type, p.cfg.Source, key, time.Now().UTC(), payload)
		if err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	p.snapshot = next
	return out, nil
}

// fingerprint derives the snapshot key and content fingerprint for a record and
// marshals its canonical payload.
func (p *Producer) fingerprint(rec map[string]any, idx int) (key, fp string, payload json.RawMessage, err error) {
	payload, err = json.Marshal(rec)
	if err != nil {
		return "", "", nil, fmt.Errorf("events: producer %q: marshal payload: %w", p.cfg.Type, err)
	}
	fp = string(payload)
	key = p.keyFor(rec, idx)
	return key, fp, payload, nil
}

// keyFor selects the idempotency/snapshot key: the KeyBy field when present and
// non-empty, otherwise the positional index.
func (p *Producer) keyFor(rec map[string]any, idx int) string {
	if p.cfg.KeyBy != "" {
		if v, ok := rec[p.cfg.KeyBy]; ok {
			if s := fmt.Sprintf("%v", v); s != "" {
				return s
			}
		}
	}
	return fmt.Sprintf("%s#%d", p.cfg.Type, idx)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DefaultFetcher implements Fetcher with real exec and http I/O. It is the
// production seam; tests substitute a fake.
type DefaultFetcher struct {
	// Client is used for http fetches; nil falls back to http.DefaultClient.
	Client *http.Client
}

// Fetch dispatches to exec or http based on the spec.
func (f DefaultFetcher) Fetch(ctx context.Context, spec FetchSpec) ([]byte, error) {
	mode, err := spec.mode()
	if err != nil {
		return nil, err
	}
	if mode == fetchModeExec {
		return f.fetchExec(ctx, spec)
	}
	return f.fetchHTTP(ctx, spec)
}

func (f DefaultFetcher) fetchExec(ctx context.Context, spec FetchSpec) ([]byte, error) {
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("events: exec fetch %v: %w", spec.Argv, err)
	}
	return out, nil
}

func (f DefaultFetcher) fetchHTTP(ctx context.Context, spec FetchSpec) ([]byte, error) {
	method := spec.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, spec.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("events: http build request: %w", err)
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("events: http fetch %s: %w", spec.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("events: http fetch %s: status %d", spec.URL, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
