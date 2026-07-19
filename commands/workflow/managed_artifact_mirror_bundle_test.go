package workflow

import (
	"errors"
	"os"
	"testing"
)

// TestSaveDelegationBundleWithBase_WriteError covers the write-error branch the
// state-ref mirror wiring introduced in saveDelegationBundleWithBase: the
// working-copy write failure surfaces before the mirror runs.
func TestSaveDelegationBundleWithBase_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })
	err := saveDelegationBundleWithBase(t.TempDir(), &delegationBundleYAML{DelegationID: "d1"}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// TestSaveDelegationBundle_EmptyIDErrors covers the empty-delegation_id guard.
func TestSaveDelegationBundle_EmptyIDErrors(t *testing.T) {
	if err := saveDelegationBundle(t.TempDir(), &delegationBundleYAML{}); err == nil {
		t.Fatal("empty delegation_id must error")
	}
}
