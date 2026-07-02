//go:build windows

package http

import (
	"context"
	"errors"
	"testing"
)

// testPipeName is the placeholder named-pipe path the gated stubs receive.
const testPipeName = `\\.\pipe\da-service`

// TestCtlWindowsGated pins the documented Windows posture: until the
// named-pipe transport lands (see the TODO in control_windows.go), every
// control-plane entry point fails fast with the same clear error — no
// half-broken pipe behaviour, while the HTTP/SSE edge stays fully usable.
func TestCtlWindowsGated(t *testing.T) {
	if _, err := listenControl(testPipeName); !errors.Is(err, errControlUnsupportedWindows) {
		t.Fatalf("listenControl = %v, want errControlUnsupportedWindows", err)
	}
	if _, err := dialControl(context.Background(), testPipeName); !errors.Is(err, errControlUnsupportedWindows) {
		t.Fatalf("dialControl = %v, want errControlUnsupportedWindows", err)
	}
	if _, err := defaultPeerUID(nil); !errors.Is(err, errControlUnsupportedWindows) {
		t.Fatalf("defaultPeerUID = %v, want errControlUnsupportedWindows", err)
	}

	c := NewControl(testPipeName, fakeState{}, func() {})
	if err := c.Serve(context.Background()); !errors.Is(err, errControlUnsupportedWindows) {
		t.Fatalf("Serve = %v, want errControlUnsupportedWindows", err)
	}
	if _, err := NewControlClient(testPipeName).Status(context.Background()); !errors.Is(err, ErrControlUnavailable) {
		t.Fatalf("Status = %v, want ErrControlUnavailable", err)
	}
}
