package crgrelease

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureDir is the generated release contract the embedded copy mirrors.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "crg-release", "v"+Version)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("release fixtures missing at %s: %v", dir, err)
	}
	return dir
}

// The embedded contract is what a shipped binary answers tools/list from,
// while the fixture directory is what the behaviour tests compare against. If
// the two drift, the server advertises one contract and is verified against
// another — so byte equality is the guard.
func TestEmbeddedContractMatchesGeneratedFixture(t *testing.T) {
	for _, name := range []string{"tools-list.json", "release.json", "prompts.json"} {
		embedded, err := contractFS.ReadFile("contract/" + Version + "/" + name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		generated, err := os.ReadFile(filepath.Join(fixtureDir(t), name))
		if err != nil {
			t.Fatalf("read generated %s: %v", name, err)
		}
		if !bytes.Equal(embedded, generated) {
			t.Fatalf("embedded contract/%s/%s differs from the generated fixture; "+
				"re-run tools/crgrelease/generate_release_contract.py and copy both "+
				"files into internal/crgrelease/contract/%s/", Version, name, Version)
		}
	}
}

// The constants in release.go are what the rest of the product keys off; the
// fixture is what upstream actually reported. They must agree.
func TestReleaseConstantsMatchFixture(t *testing.T) {
	meta, err := Metadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Version != Version {
		t.Errorf("fixture version %q != constant %q", meta.Version, Version)
	}
	if meta.TagCommit != TagCommit {
		t.Errorf("fixture tag %q != constant %q", meta.TagCommit, TagCommit)
	}
	if meta.SchemaVersion != SchemaVersion {
		t.Errorf("fixture schema %d != constant %d", meta.SchemaVersion, SchemaVersion)
	}
	if meta.ProtocolVersion != ProtocolVersion {
		t.Errorf("fixture protocol %q != constant %q", meta.ProtocolVersion, ProtocolVersion)
	}
	if meta.ToolCount != len(Surface()) {
		t.Errorf("fixture tool count %d != decoded surface %d", meta.ToolCount, len(Surface()))
	}
}

// tools/list must reproduce the release's payload exactly: same tools, same
// order, same descriptions, and schemas that are byte-identical (property
// descriptions, defaults, the anyOf spelling of optionals and
// additionalProperties:false are all part of what clients validate against).
func TestToolsListResultMatchesRelease(t *testing.T) {
	raw, err := ToolsListResult()
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var payload struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	fixture, err := os.ReadFile(filepath.Join(fixtureDir(t), "tools-list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var expected []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(fixture, &expected); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	if len(payload.Tools) != len(expected) {
		t.Fatalf("advertised %d tools, release publishes %d", len(payload.Tools), len(expected))
	}
	for i, want := range expected {
		got := payload.Tools[i]
		if got.Name != want.Name {
			t.Fatalf("tool %d: advertised %q, release publishes %q", i, got.Name, want.Name)
		}
		if got.Description != want.Description {
			t.Errorf("%s: description differs from the release", want.Name)
		}
		if !jsonEqual(t, got.InputSchema, want.InputSchema) {
			t.Errorf("%s: input schema differs from the release\n got: %s\nwant: %s",
				want.Name, got.InputSchema, want.InputSchema)
		}
	}
}

// Every published tool must have an explicit native-or-bridge decision, and a
// bridge-only tool must name the upstream capability that keeps it there.
func TestCapabilitiesCoverEveryPublishedTool(t *testing.T) {
	capabilities, err := Capabilities()
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if len(capabilities) != len(Surface()) {
		t.Fatalf("%d capabilities for %d tools", len(capabilities), len(Surface()))
	}
	for _, capability := range capabilities {
		if capability.NativeBackend {
			if capability.Backend() != BackendNative {
				t.Errorf("%s: native tool routed to %s", capability.Tool, capability.Backend())
			}
			continue
		}
		if capability.BridgeOnlyReason == "" {
			t.Errorf("%s: bridge-only with no reason", capability.Tool)
		}
		if capability.Backend() != BackendBridge {
			t.Errorf("%s: bridge-only tool routed to %s", capability.Tool, capability.Backend())
		}
	}
}

func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		t.Fatalf("decode left: %v", err)
	}
	if err := json.Unmarshal(b, &right); err != nil {
		t.Fatalf("decode right: %v", err)
	}
	leftBytes, err := json.Marshal(left)
	if err != nil {
		t.Fatalf("re-encode left: %v", err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		t.Fatalf("re-encode right: %v", err)
	}
	return bytes.Equal(leftBytes, rightBytes)
}
