package graphstore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// TestOpenPostgres_PingError exercises the ping-failure path: a syntactically
// valid DSN that points at a port no postgres is listening on. The pool is
// created lazily but Ping should fail within ctx.
func TestOpenPostgres_PingError(t *testing.T) {
	// Use a short-timeout context so we don't wait the default 30s.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 127.0.0.1:1 is the discard port; nothing should be listening.
	_, err := graphstore.OpenPostgres(ctx, "postgres://user:pass@127.0.0.1:1/dbname?connect_timeout=1&sslmode=disable")
	if err == nil {
		t.Fatal("expected ping error against closed port")
	}
	if !strings.Contains(err.Error(), "ping") && !strings.Contains(err.Error(), "pool") {
		t.Logf("note: error string was %q (acceptable, but unexpected wording)", err)
	}
}
