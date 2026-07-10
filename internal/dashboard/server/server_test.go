// server_test.go exercises the standalone dashboard server's observable
// contracts: the live-socket lifecycle + bare liveness route, the embedded
// SPA static handler with its single-page-app fallback, and the on-disk
// --static-dir override (existing assets served verbatim, unknown routes
// falling back to that directory's index.html). Everything here drives the
// public surface — Start/Stop/Addr/Handler — never internal state.
package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// serve drives one GET through the composed handler with no socket and returns
// the status code and the full response body.
func serve(t *testing.T, h http.Handler, target string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
	return rr.Code, rr.Body.String()
}

// TestLifecycleAndHealthOverSocket binds an ephemeral port, resolves it, and
// hits the bare /api/health route over a real TCP connection, then shuts down
// cleanly. It defends: Start resolving the :0 wildcard to a concrete non-zero
// port surfaced by Addr(); the liveness route's exact status/content-type/body
// contract over the wire; and Stop draining without error.
func TestLifecycleAndHealthOverSocket(t *testing.T) {
	srv, err := New(Config{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop exactly once — whichever of the explicit assertion below or this
	// cleanup net (reached only when an assertion above t.Fatal-s) runs first.
	var once sync.Once
	stop := func() error {
		var stopErr error
		once.Do(func() { stopErr = srv.Stop(context.Background()) })
		return stopErr
	}
	t.Cleanup(func() { _ = stop() })

	addr := srv.Addr()
	if addr == "127.0.0.1:0" {
		t.Fatalf("Addr() still the wildcard %q after Start; want a resolved port", addr)
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Addr() = %q not host:port: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Addr() port %q not numeric: %v", portStr, err)
	}
	if port == 0 {
		t.Fatalf("Addr() = %q resolved to port 0; want an ephemeral non-zero port", addr)
	}

	resp, err := http.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health over socket: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read /api/health body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/health status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("/api/health Content-Type = %q, want to contain application/json", ct)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Errorf("/api/health body = %q, want %q", got, `{"status":"ok"}`)
	}

	if err := stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestEmbeddedSPAFallback drives the composed handler (no socket) with the
// default embedded dist/. It defends: the root serving the committed SPA
// shell; an unknown client-side route falling back to that shell with 200
// (not 404); and /api/health keeping precedence over the "/" SPA catch-all.
func TestEmbeddedSPAFallback(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Close the broker heartbeat goroutine even though we never Start.
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	h := srv.Handler()

	const shell = `<div id="root">`

	t.Run("root serves the embedded shell", func(t *testing.T) {
		code, body := serve(t, h, "/")
		if code != http.StatusOK {
			t.Fatalf("GET / status = %d, want 200", code)
		}
		if !strings.Contains(body, shell) {
			t.Errorf("GET / body missing %q; got %q", shell, body)
		}
	})

	t.Run("unknown client route falls back to the shell", func(t *testing.T) {
		code, body := serve(t, h, "/some/client/route")
		if code != http.StatusOK {
			t.Fatalf("GET /some/client/route status = %d, want 200 (SPA fallback)", code)
		}
		if !strings.Contains(body, shell) {
			t.Errorf("GET /some/client/route body missing %q; got %q", shell, body)
		}
	})

	t.Run("api health beats the SPA catch-all", func(t *testing.T) {
		code, body := serve(t, h, "/api/health")
		if code != http.StatusOK {
			t.Fatalf("GET /api/health status = %d, want 200", code)
		}
		if body != `{"status":"ok"}` {
			t.Errorf("GET /api/health body = %q, want %q (SPA handler shadowed the health route?)", body, `{"status":"ok"}`)
		}
	})
}

// TestStaticDirOverride points the server at an on-disk directory and proves
// the two distinct static behaviors: an existing asset is served byte-for-byte
// (never replaced by the fallback), while an unknown path falls back to that
// directory's index.html.
func TestStaticDirOverride(t *testing.T) {
	dir := t.TempDir()
	const (
		indexHTML = `<html><body>STATIC-ROOT</body></html>`
		appJS     = `console.log('hi')`
	)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(appJS), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	srv, err := New(Config{StaticDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	h := srv.Handler()

	t.Run("existing asset served verbatim", func(t *testing.T) {
		code, body := serve(t, h, "/app.js")
		if code != http.StatusOK {
			t.Fatalf("GET /app.js status = %d, want 200", code)
		}
		if body != appJS {
			t.Errorf("GET /app.js body = %q, want %q (fallback served the shell instead?)", body, appJS)
		}
	})

	t.Run("unknown route falls back to that dir's index.html", func(t *testing.T) {
		code, body := serve(t, h, "/deep/unknown/route")
		if code != http.StatusOK {
			t.Fatalf("GET /deep/unknown/route status = %d, want 200 (SPA fallback)", code)
		}
		if !strings.Contains(body, "STATIC-ROOT") {
			t.Errorf("GET /deep/unknown/route body missing STATIC-ROOT; got %q", body)
		}
	})
}
