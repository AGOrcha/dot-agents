package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

// fakeFetcher is the hermetic seam used by producer tests.
type fakeFetcher struct {
	out  []byte
	err  error
	seen []FetchSpec
}

func (f *fakeFetcher) Fetch(_ context.Context, spec FetchSpec) ([]byte, error) {
	f.seen = append(f.seen, spec)
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

// sequenceFetcher returns a different document on each call to exercise diffing.
type sequenceFetcher struct {
	docs [][]byte
	i    int
}

func (f *sequenceFetcher) Fetch(context.Context, FetchSpec) ([]byte, error) {
	d := f.docs[f.i]
	if f.i < len(f.docs)-1 {
		f.i++
	}
	return d, nil
}

func prConfig() ProducerConfig {
	return ProducerConfig{
		Type:   "event.pr.merged",
		Source: "github",
		Each:   ".pulls",
		Map: map[string]string{
			"id":     ".number",
			"failed": ".checks[?(@.conclusion=='FAILURE')].name",
		},
		KeyBy: "id",
	}
}

func TestNewProducerValidation(t *testing.T) {
	base := prConfig()
	tests := []struct {
		name   string
		mutate func(*ProducerConfig)
	}{
		{"empty type", func(c *ProducerConfig) { c.Type = "" }},
		{"empty source", func(c *ProducerConfig) { c.Source = "" }},
		{"empty each", func(c *ProducerConfig) { c.Each = "" }},
		{"empty map", func(c *ProducerConfig) { c.Map = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if _, err := NewProducer(cfg, &fakeFetcher{}); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestNewProducerDefaultsFetcher(t *testing.T) {
	p, err := NewProducer(prConfig(), nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if _, ok := p.fetcher.(DefaultFetcher); !ok {
		t.Fatalf("expected DefaultFetcher default, got %T", p.fetcher)
	}
}

func TestProducerCycleEmitsAndDiffs(t *testing.T) {
	docV1 := []byte(`{"pulls":[
		{"number":1,"checks":[{"name":"t","conclusion":"FAILURE"}]},
		{"number":2,"checks":[{"name":"t","conclusion":"FAILURE"}]}
	]}`)
	// Second cycle: pr 1 unchanged, pr 2 changed conclusion field.
	docV2 := []byte(`{"pulls":[
		{"number":1,"checks":[{"name":"t","conclusion":"FAILURE"}]},
		{"number":2,"checks":[{"name":"other","conclusion":"FAILURE"}]}
	]}`)
	p, err := NewProducer(prConfig(), &sequenceFetcher{docs: [][]byte{docV1, docV2}})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	first, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("cycle 1 expected 2 envelopes, got %d", len(first))
	}
	assertPayloadField(t, first[0], "id", float64(1))

	second, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("cycle 2 expected 1 changed envelope, got %d", len(second))
	}
	if second[0].IdempotencyKey != "2" {
		t.Fatalf("expected changed pr 2, got key %q", second[0].IdempotencyKey)
	}
}

func TestProducerKeyByFallbackToIndex(t *testing.T) {
	cfg := prConfig()
	cfg.KeyBy = "" // force positional key
	doc := []byte(`{"pulls":[{"number":7,"checks":[{"name":"t","conclusion":"FAILURE"}]}]}`)
	p, err := NewProducer(cfg, &fakeFetcher{out: doc})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(envs) != 1 || envs[0].IdempotencyKey != "event.pr.merged#0" {
		t.Fatalf("expected positional key, got %+v", envs)
	}
}

func TestProducerCycleErrors(t *testing.T) {
	tests := []struct {
		name    string
		fetcher Fetcher
		each    string
	}{
		{"fetch error", &fakeFetcher{err: errors.New("boom")}, ".pulls"},
		{"bad json", &fakeFetcher{out: []byte(`not json`)}, ".pulls"},
		{"each missing", &fakeFetcher{out: []byte(`{"x":1}`)}, ".pulls"},
		{"each not list", &fakeFetcher{out: []byte(`{"pulls":{"a":1}}`)}, ".pulls"},
		{"map field missing", &fakeFetcher{out: []byte(`{"pulls":[{"x":1}]}`)}, ".pulls"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := prConfig()
			cfg.Each = tc.each
			p, err := NewProducer(cfg, tc.fetcher)
			if err != nil {
				t.Fatalf("NewProducer: %v", err)
			}
			if _, err := p.Cycle(context.Background()); err == nil {
				t.Fatalf("expected cycle error")
			}
		})
	}
}

func TestFetchSpecMode(t *testing.T) {
	tests := []struct {
		name    string
		spec    FetchSpec
		want    string
		wantErr bool
	}{
		{"exec", FetchSpec{Argv: []string{"echo"}}, fetchModeExec, false},
		{"http", FetchSpec{URL: "http://x"}, fetchModeHTTP, false},
		{"both", FetchSpec{Argv: []string{"echo"}, URL: "http://x"}, "", true},
		{"neither", FetchSpec{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.spec.mode()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("mode() = %q,%v want %q", got, err, tc.want)
			}
		})
	}
}

// echoArgv builds a portable argv that prints s verbatim to stdout by re-execing
// the test binary itself into TestHelperProcess (the os/exec stdlib pattern). This
// avoids shell echo/printf quoting differences across OSes — notably cmd.exe
// mangling JSON quotes on Windows. The caller must set GO_WANT_HELPER_PROCESS=1.
func echoArgv(s string) []string {
	return []string{os.Args[0], "-test.run=TestHelperProcess", "--", s}
}

// failArgv is a non-zero-exit command on every supported OS.
func failArgv() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "exit", "1"}
	}
	return []string{"false"}
}

func TestDefaultFetcherExec(t *testing.T) {
	// The exec'd subprocess (this same test binary) inherits the env; the flag
	// makes TestHelperProcess emit the payload verbatim and exit.
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	f := DefaultFetcher{}
	out, err := f.Fetch(context.Background(), FetchSpec{Argv: echoArgv(`{"ok":1}`)})
	if err != nil {
		t.Fatalf("exec fetch: %v", err)
	}
	// Trim any trailing newline/CR a runtime may append for a portable assert.
	got := strings.TrimRight(string(out), "\r\n")
	if got != `{"ok":1}` {
		t.Fatalf("exec output = %q", got)
	}
}

func TestDefaultFetcherExecError(t *testing.T) {
	f := DefaultFetcher{}
	if _, err := f.Fetch(context.Background(), FetchSpec{Argv: failArgv()}); err == nil {
		t.Fatalf("expected exec error")
	}
}

// TestHelperProcess is not a real test — it is the subprocess echoArgv re-execs.
// It runs only when GO_WANT_HELPER_PROCESS=1 (set by the calling test), writes the
// argument after "--" verbatim to stdout, and exits — giving deterministic exec
// output on every OS without relying on a shell builtin.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) > 0 {
		fmt.Fprint(os.Stdout, args[0])
	}
	os.Exit(0)
}

func TestDefaultFetcherHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	f := DefaultFetcher{Client: srv.Client()}
	out, err := f.Fetch(context.Background(), FetchSpec{
		URL:     srv.URL,
		Headers: map[string]string{"X-Test": "1"},
	})
	if err != nil {
		t.Fatalf("http fetch: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("http output = %q", out)
	}
}

func TestDefaultFetcherHTTPNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := DefaultFetcher{}
	if _, err := f.Fetch(context.Background(), FetchSpec{URL: srv.URL}); err == nil {
		t.Fatalf("expected non-2xx error")
	}
}

func TestDefaultFetcherHTTPBadRequest(t *testing.T) {
	f := DefaultFetcher{}
	// control char in URL makes NewRequestWithContext fail.
	if _, err := f.Fetch(context.Background(), FetchSpec{URL: "http://\x7f"}); err == nil {
		t.Fatalf("expected request build error")
	}
}

func TestDefaultFetcherHTTPDialError(t *testing.T) {
	f := DefaultFetcher{}
	// Reserved TEST-NET address that should not be routable.
	if _, err := f.Fetch(context.Background(), FetchSpec{URL: "http://127.0.0.1:0"}); err == nil {
		t.Fatalf("expected dial error")
	}
}

func assertPayloadField(t *testing.T, env Envelope, field string, want any) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(env.Payload, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if m[field] != want {
		t.Fatalf("payload[%q] = %v, want %v", field, m[field], want)
	}
}
