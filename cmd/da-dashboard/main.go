// Command da-dashboard is the standalone R2 observability dashboard server.
// It wires the dashboard store, SSE broker, REST/SSE handlers, and iter-log
// filesystem watcher into a single loopback HTTP server (internal/dashboard/
// server) and serves both the JSON API under /api and the built single-page
// front-end for everything else.
//
// Usage:
//
//	da-dashboard --iter-log-dir .agents/active/iteration-log [--iter-log-dir ...]
//	             [--addr 127.0.0.1:7300]
//	             [--dev-asset-proxy http://127.0.0.1:5173]
//	             [--static-dir web/dashboard/dist]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AGOrcha/dot-agents/internal/dashboard/server"
)

// stringSlice collects a repeatable string flag (--iter-log-dir a --iter-log-dir b).
type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprint([]string(*s)) }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "da-dashboard:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("da-dashboard", flag.ContinueOnError)

	var iterLogDirs stringSlice
	fs.Var(&iterLogDirs, "iter-log-dir", "iter-log root to serve and watch (repeatable)")
	addr := fs.String("addr", server.DefaultAddr, "TCP address to bind (loopback by default)")
	devAssetProxy := fs.String("dev-asset-proxy", "", "reverse-proxy non-/api requests to this Vite dev server URL")
	staticDir := fs.String("static-dir", "", "serve the front-end from this on-disk dir instead of the embedded bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv, err := server.New(server.Config{
		Addr:          *addr,
		IterLogDirs:   iterLogDirs,
		DevAssetProxy: *devAssetProxy,
		StaticDir:     *staticDir,
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx)
}
