// Package graphstore — coverage for the rare write-side error branches of
// MCPServer.Serve. We simulate a writer that fails on the first call so
// json.Encoder.Encode returns an error, exercising the encoder-error
// guard inside the Serve loop.
package graphstore

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// failingWriter implements io.Writer and always returns an error.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errors.New("write closed") }

// TestMCPServer_Serve_EncodeError ensures the `if err := enc.Encode(resp); err != nil { return err }`
// branch fires when the underlying writer is broken.
func TestMCPServer_Serve_EncodeError(t *testing.T) {
	srv := &MCPServer{}
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	// reader returns the request once, then EOF.
	err := srv.Serve(strings.NewReader(req), failingWriter{})
	if err == nil {
		t.Fatal("expected Serve to return Encoder error")
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("expected non-EOF error, got EOF")
	}
}
