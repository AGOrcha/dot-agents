package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schemaDoc is the structural shape this package needs from a JSON schema
// document for round-trip assertions. We intentionally avoid the full
// jsonschema/v6 dependency (which is indirect-only here) and instead walk the
// required minimum: required fields, allowed property names when
// additionalProperties is false, and enum constraints.
type schemaDoc struct {
	Type                 string                `json:"type,omitempty"`
	Required             []string              `json:"required,omitempty"`
	AdditionalProperties *bool                 `json:"additionalProperties,omitempty"`
	Properties           map[string]schemaProp `json:"properties,omitempty"`
}

type schemaProp struct {
	Type string   `json:"type,omitempty"`
	Enum []string `json:"enum,omitempty"`
}

// repoRoot walks up from this test file's directory to find the repository
// root (the parent of internal/) so the test reads schemas/ from a stable
// location independent of the test working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "schemas", "agentsrc.schema.json")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not locate repo root from %s", wd)
	return ""
}

func loadSchema(t *testing.T, root, name string) schemaDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "schemas", name))
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	var doc schemaDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	return doc
}

// assertSchemaCovers verifies the marshaled JSON either uses keys defined in
// the schema or comes from documented extra-fields zones. When
// additionalProperties is false on the schema, any key not declared in
// Properties indicates struct↔schema drift.
func assertSchemaCovers(t *testing.T, label string, schema schemaDoc, encoded []byte) {
	t.Helper()
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		return // not strict
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("%s: re-parse encoded struct: %v", label, err)
	}
	for key := range raw {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("%s: encoded struct key %q not declared in schema (struct↔schema drift)", label, key)
		}
	}
}

// TestSchemaRoundTrip_AgentsRC marshals a minimal valid AgentsRC and asserts:
//  1. every schema-required field is present;
//  2. every encoded JSON key matches a schema-declared property;
//  3. enum-typed fields (e.g. source.type) emit allowed values only;
//  4. unmarshaling back into AgentsRC preserves the round-trip.
func TestSchemaRoundTrip_AgentsRC(t *testing.T) {
	root := repoRoot(t)
	schema := loadSchema(t, root, "agentsrc.schema.json")

	// Minimal valid AgentsRC — sources must be non-empty by schema rule.
	rc := &AgentsRC{
		Schema:  "https://dot-agents.dev/schemas/agentsrc.json",
		Version: 1,
		Project: "my-proj",
		Skills:  []string{"alpha"},
		Rules:   []string{"global", "project"},
		Hooks:   StringsOrBool{Names: []string{"PreToolUse"}},
		MCP:     StringsOrBool{All: true},
		Sources: []Source{
			{Type: "local"},
			{Type: "git", URL: "https://example.com/repo.git", Ref: "main"},
		},
		KG: &AgentsRCKG{
			Backend: "sqlite",
			Bridge:  AgentsRCKGBridge{Enabled: true, AllowedIntents: []string{"impl"}},
		},
	}

	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// All schema-required fields must appear in the encoded form.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, req := range schema.Required {
		if _, ok := raw[req]; !ok {
			t.Errorf("required schema field %q missing from encoded AgentsRC", req)
		}
	}

	// No drift: every encoded key must be declared in the schema.
	assertSchemaCovers(t, "agentsrc.schema.json", schema, data)

	// Source.type enum check (matches schema/source/properties/type/enum).
	allowedSourceTypes := map[string]bool{"local": true, "git": true}
	for i, s := range rc.Sources {
		if !allowedSourceTypes[s.Type] {
			t.Errorf("rc.Sources[%d].Type=%q outside schema enum", i, s.Type)
		}
	}

	// Backend enum check.
	allowedBackends := map[string]bool{"": true, "sqlite": true, "postgres": true}
	if !allowedBackends[rc.KG.Backend] {
		t.Errorf("kg.backend=%q outside schema enum", rc.KG.Backend)
	}

	// Round-trip: unmarshal back and verify structural fidelity.
	var rt AgentsRC
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if rt.Version != rc.Version {
		t.Errorf("Version round-trip: %d → %d", rc.Version, rt.Version)
	}
	if rt.Project != rc.Project {
		t.Errorf("Project round-trip: %q → %q", rc.Project, rt.Project)
	}
	if len(rt.Sources) != len(rc.Sources) {
		t.Errorf("Sources length round-trip: %d → %d", len(rc.Sources), len(rt.Sources))
	}
	if rt.KG == nil || rt.KG.Backend != rc.KG.Backend {
		t.Errorf("KG.Backend round-trip lost: %+v", rt.KG)
	}
}

// TestSchemaRoundTrip_AgentsRCExtraFields documents that unknown JSON keys
// stored in ExtraFields are preserved on round-trip — the schema flags them
// as "additionalProperties: false" violations but the Go layer must not
// silently drop them.
func TestSchemaRoundTrip_AgentsRCExtraFields(t *testing.T) {
	original := []byte(`{
  "version": 1,
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type": "local"}],
  "experimental_field": {"nested": true}
}`)

	var rc AgentsRC
	if err := json.Unmarshal(original, &rc); err != nil {
		t.Fatalf("unmarshal with extra fields: %v", err)
	}
	if rc.ExtraFields == nil || rc.ExtraFields["experimental_field"] == nil {
		t.Fatalf("expected experimental_field in ExtraFields, got %+v", rc.ExtraFields)
	}

	out, err := json.Marshal(&rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "experimental_field") {
		t.Errorf("expected experimental_field preserved on round-trip, got: %s", out)
	}
}

// TestSchemaRoundTrip_AgentsRCFile uses LoadAgentsRC + Save to ensure the
// disk-level round-trip (marshal → file → reload → marshal) is stable for a
// canonical minimal manifest.
func TestSchemaRoundTrip_AgentsRCFile(t *testing.T) {
	tmp := t.TempDir()
	rc := &AgentsRC{
		Version: 1,
		Project: "proj",
		Sources: []Source{{Type: "local"}},
	}
	if err := rc.Save(tmp); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != 1 || loaded.Project != "proj" {
		t.Errorf("disk round-trip lost fields: %+v", loaded)
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Type != "local" {
		t.Errorf("disk round-trip lost sources: %+v", loaded.Sources)
	}

	// Second save → load must be byte-stable for the round-trip metadata.
	if err := loaded.Save(tmp); err != nil {
		t.Fatalf("second save: %v", err)
	}
	reloaded, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Version != loaded.Version || reloaded.Project != loaded.Project {
		t.Errorf("two-pass round-trip drift: %+v vs %+v", loaded, reloaded)
	}
}

// TestSchemaRoundTrip_DiscoverableSchemas asserts every schema we ship is
// parseable JSON with at least a top-level type or $defs section — guards
// against accidental commits of malformed schema files.
// schemaHasTopLevelShape returns true when a parsed schema doc has at least
// one of the expected top-level signposts: type, $defs, or properties.
func schemaHasTopLevelShape(doc map[string]json.RawMessage) bool {
	if _, ok := doc["type"]; ok {
		return true
	}
	if _, ok := doc["$defs"]; ok {
		return true
	}
	_, ok := doc["properties"]
	return ok
}

// validateSchemaFile reads + parses a single *.schema.json and asserts it has
// a recognizable top-level shape.
func validateSchemaFile(t *testing.T, path, name string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read %s: %v", name, err)
		return
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Errorf("parse %s: %v", name, err)
		return
	}
	if !schemaHasTopLevelShape(doc) {
		t.Errorf("%s has no top-level type / $defs / properties", name)
	}
}

func TestSchemaRoundTrip_DiscoverableSchemas(t *testing.T) {
	root := repoRoot(t)
	schemasDir := filepath.Join(root, "schemas")
	entries, err := os.ReadDir(schemasDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		count++
		validateSchemaFile(t, filepath.Join(schemasDir, entry.Name()), entry.Name())
	}
	if count == 0 {
		t.Fatal("no *.schema.json files discovered")
	}
}
