package complianceregister

import "testing"

// TestMustFromYAMLPanics drives the panic seam New uses, with an invalid schema,
// so the panic branch is exercised. New itself can only be called with the valid
// embed, so this is the only way to reach the panic.
func TestMustFromYAMLPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("mustFromYAML did not panic on an invalid schema")
		}
	}()
	mustFromYAML([]byte("name: x\n")) // missing version -> LoadSchema error -> panic
}
