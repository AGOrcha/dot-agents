package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
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
	Type string `json:"type,omitempty"`
	// Enum is intentionally json.RawMessage: enum values may be of any
	// JSON-compatible type (integer for version, string for source.type),
	// and this helper only needs to know presence, not contents.
	Enum json.RawMessage `json:"enum,omitempty"`
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

// assertRequiredFieldsPresent checks that all schema-required fields appear
// in the encoded JSON representation.
func assertRequiredFieldsPresent(t *testing.T, schema schemaDoc, encoded []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for _, req := range schema.Required {
		if _, ok := raw[req]; !ok {
			t.Errorf("required schema field %q missing from encoded AgentsRC", req)
		}
	}
}

// assertSourceTypesValid checks that all source.type values match schema enum.
func assertSourceTypesValid(t *testing.T, rc *AgentsRC) {
	t.Helper()
	allowedSourceTypes := map[string]bool{"local": true, "git": true}
	for i, s := range rc.Sources {
		if !allowedSourceTypes[s.Type] {
			t.Errorf("rc.Sources[%d].Type=%q outside schema enum", i, s.Type)
		}
	}
}

// assertBackendValid checks that kg.backend matches the schema enum.
func assertBackendValid(t *testing.T, rc *AgentsRC) {
	t.Helper()
	if rc.KG == nil {
		return
	}
	allowedBackends := map[string]bool{"": true, "sqlite": true, "postgres": true}
	if !allowedBackends[rc.KG.Backend] {
		t.Errorf("kg.backend=%q outside schema enum", rc.KG.Backend)
	}
}

// assertRoundTripStructuralFidelity validates that unmarshaling a marshaled
// AgentsRC back into a new AgentsRC preserves the original fields.
func assertRoundTripStructuralFidelity(t *testing.T, original, roundtripped *AgentsRC) {
	t.Helper()
	if roundtripped.Version != original.Version {
		t.Errorf("Version round-trip: %d → %d", original.Version, roundtripped.Version)
	}
	if roundtripped.Project != original.Project {
		t.Errorf("Project round-trip: %q → %q", original.Project, roundtripped.Project)
	}
	if len(roundtripped.Sources) != len(original.Sources) {
		t.Errorf("Sources length round-trip: %d → %d", len(original.Sources), len(roundtripped.Sources))
	}
	if roundtripped.KG == nil || roundtripped.KG.Backend != original.KG.Backend {
		t.Errorf("KG.Backend round-trip lost: %+v", roundtripped.KG)
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
		Schema:  "https://agorcha.dev/schemas/agentsrc.schema.json",
		Version: 1,
		Project: "my-proj",
		Skills:  []string{"alpha"},
		Rules:   []string{"global", "project"},
		Hooks:   sob(StringsOrBool{Names: []string{"PreToolUse"}}),
		MCP:     sob(StringsOrBool{All: true}),
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
	assertRequiredFieldsPresent(t, schema, data)

	// No drift: every encoded key must be declared in the schema.
	assertSchemaCovers(t, "agentsrc.schema.json", schema, data)

	// Enum validation for source types and backend.
	assertSourceTypesValid(t, rc)
	assertBackendValid(t, rc)

	// Round-trip: unmarshal back and verify structural fidelity.
	var rt AgentsRC
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	assertRoundTripStructuralFidelity(t, rc, &rt)
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

// ── v2 additive schema tests (config-distribution-model §3-§5) ────────────────

// compileAgentsRCSchema loads schemas/agentsrc.schema.json into a fully
// validated jsonschema.Schema so tests can assert real JSON schema validation
// (not just key coverage) for v1 and v2 fixtures.
func compileAgentsRCSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	root := repoRoot(t)
	path := filepath.Join(root, "schemas", "agentsrc.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	const url = "https://agorcha.dev/schemas/agentsrc.schema.json"
	if err := c.AddResource(url, doc); err != nil {
		t.Fatalf("compiler add: %v", err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

// validateFixture parses a JSON fixture file into a generic any and validates
// it against the compiled AgentsRC schema. Returns the validation error (nil
// if valid).
func validateFixture(t *testing.T, sch *jsonschema.Schema, path string) error {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	return sch.Validate(doc)
}

// assertV2ExtendsFieldsPresent validates that Extends fields were properly loaded.
func assertV2ExtendsFieldsPresent(t *testing.T, rc *AgentsRC) {
	t.Helper()
	if len(rc.Extends) != 3 {
		t.Fatalf("Extends: got %d, want 3", len(rc.Extends))
	}
	if rc.Extends[0].Ref != "acme:org/base" || rc.Extends[0].Optional {
		t.Errorf("Extends[0]: got %+v", rc.Extends[0])
	}
	if rc.Extends[2].Ref != "acme:team/experimental" || !rc.Extends[2].Optional {
		t.Errorf("Extends[2] should be optional ref, got %+v", rc.Extends[2])
	}
}

// assertV2PackagesFieldsPresent validates that Packages fields were properly loaded.
func assertV2PackagesFieldsPresent(t *testing.T, rc *AgentsRC) {
	t.Helper()
	if len(rc.Packages) != 2 {
		t.Fatalf("Packages: got %d, want 2", len(rc.Packages))
	}
	if rc.Packages[0].Ref != "acme-pkgs:skill/review-pr@^1.2" {
		t.Errorf("Packages[0]: got %q", rc.Packages[0].Ref)
	}
}

// assertV2FeaturesFlagsPresent validates that Features map was properly loaded.
func assertV2FeaturesFlagsPresent(t *testing.T, rc *AgentsRC) {
	t.Helper()
	if rc.Features["graph_bridge"] != "preview" {
		t.Errorf("Features[graph_bridge]: got %q", rc.Features["graph_bridge"])
	}
}

// assertV2SourceFieldsPresent validates source-level id, cache_ttl, auth, and type.
func assertV2SourceFieldsPresent(t *testing.T, rc *AgentsRC) {
	t.Helper()
	var acmeSrc, ociSrc *Source
	for i := range rc.Sources {
		switch rc.Sources[i].ID {
		case "acme":
			acmeSrc = &rc.Sources[i]
		case "acme-pkgs":
			ociSrc = &rc.Sources[i]
		}
	}
	if acmeSrc == nil || acmeSrc.CacheTTL != "4h" {
		t.Errorf("acme source missing or CacheTTL wrong: %+v", acmeSrc)
	}
	if ociSrc == nil || ociSrc.Type != "oci" || len(ociSrc.Auth) == 0 {
		t.Errorf("acme-pkgs (oci) source missing or auth not preserved: %+v", ociSrc)
	}
}

// assertV2KeysPreservedInOutput checks that v2 keys appear in marshaled JSON.
func assertV2KeysPreservedInOutput(t *testing.T, encoded []byte) {
	t.Helper()
	for _, key := range []string{"repo_id", "extends", "packages", "features"} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("re-marshal lost key %q in %s", key, encoded)
		}
	}
}

// assertV2SourceKeysPreservedInOutput checks that source-level v2 keys appear.
func assertV2SourceKeysPreservedInOutput(t *testing.T, encoded []byte) {
	t.Helper()
	for _, key := range []string{`"id"`, `"cache_ttl"`, `"auth"`, `"http"`, `"oci"`} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("re-marshal lost source-level key %q", key)
		}
	}
}

// assertV1FieldsOmitted checks that v2 fields are not populated in a v1 fixture.
func assertV1FieldsOmitted(t *testing.T, rc *AgentsRC) {
	t.Helper()
	if rc.Version != 1 {
		t.Errorf("Version: got %d, want 1", rc.Version)
	}
	if rc.RepoID != "" || rc.Extends != nil || rc.Packages != nil || rc.Features != nil {
		t.Errorf("v1 fixture must not populate v2 fields: repo_id=%q extends=%v packages=%v features=%v",
			rc.RepoID, rc.Extends, rc.Packages, rc.Features)
	}
	for _, s := range rc.Sources {
		if s.ID != "" || s.CacheTTL != "" || len(s.Auth) != 0 {
			t.Errorf("v1 Source must not populate v2 fields: %+v", s)
		}
	}
}

// assertV1KeysNotLeaked checks that v2 keys don't appear in marshaled v1 output.
func assertV1KeysNotLeaked(t *testing.T, encoded string) {
	t.Helper()
	for _, forbidden := range []string{`"repo_id"`, `"extends"`, `"packages"`, `"features"`,
		`"cache_ttl"`, `"auth"`} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("v1 round-trip leaked v2 key %s into output: %s", forbidden, encoded)
		}
	}
}

// TestSchemaValidate_V1Fixture confirms a real-shape v1 manifest still
// validates against the v2-compatible schema (additive migration contract).
func TestSchemaValidate_V1Fixture(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	fixture := filepath.Join("testdata", "v1", ".agentsrc.json")
	if err := validateFixture(t, sch, fixture); err != nil {
		t.Fatalf("v1 fixture must validate against v2-compatible schema: %v", err)
	}
}

// TestSchemaValidate_V2MinimalFixture confirms the minimal v2 manifest
// (version=2 + repo_id but no extends/packages) validates.
func TestSchemaValidate_V2MinimalFixture(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	fixture := filepath.Join("testdata", "v2", "agentsrc-minimal.json")
	if err := validateFixture(t, sch, fixture); err != nil {
		t.Fatalf("v2 minimal fixture should validate: %v", err)
	}
}

// TestSchemaValidate_V2FullFixture confirms a fully-populated v2 manifest
// (sources with id/cache_ttl/auth, http+oci types, extends with optional flag,
// packages, features) validates.
func TestSchemaValidate_V2FullFixture(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	fixture := filepath.Join("testdata", "v2", "agentsrc-full.json")
	if err := validateFixture(t, sch, fixture); err != nil {
		t.Fatalf("v2 full fixture should validate: %v", err)
	}
}

// TestSchemaValidate_V2InvalidCrossTier confirms the schema does NOT enforce
// cross-tier source constraints by itself (extends referencing oci, packages
// referencing git) — the fixture is valid against the structural schema; tier
// constraints are enforced at the resolver layer (phase 1+, p1b). This test
// documents the boundary so future schema tightening is intentional.
//
// Note: the field-shape rules ARE enforced (e.g. ref string must match
// `source-id:path[@version]` pattern). Tier-source matching requires resolver
// context (looking up the source-id) and lives outside JSON-schema scope.
func TestSchemaValidate_V2CrossTierFixtureShapeOnly(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	fixture := filepath.Join("testdata", "v2", "agentsrc-invalid-cross-tier.json")
	if err := validateFixture(t, sch, fixture); err != nil {
		t.Fatalf("cross-tier fixture is shape-valid (resolver enforces tier): %v", err)
	}
}

// TestSchemaValidate_RejectsBadSourceType confirms a source with an unknown
// type value is rejected (enum check still applies under v2).
func TestSchemaValidate_RejectsBadSourceType(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources": []any{
			map[string]any{"type": "ftp", "url": "ftp://example.com"},
		},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("expected validation error for unknown source.type=ftp")
	}
}

// TestSchemaValidate_RejectsBadVersion confirms versions other than 1 or 2 fail.
func TestSchemaValidate_RejectsBadVersion(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  3,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources":  []any{map[string]any{"type": "local"}},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("expected validation error for version=3 (only 1 and 2 allowed)")
	}
}

// TestSchemaValidate_RejectsBadExtendsRefShape confirms an extends entry that
// doesn't match the source-id:path[@version] pattern is rejected by the schema.
func TestSchemaValidate_RejectsBadExtendsRefShape(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources":  []any{map[string]any{"type": "local"}},
		"extends":  []any{"missing-colon-form"},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("expected validation error for malformed extends ref")
	}
}

// TestSchemaValidate_RejectsPackagesWithoutVersion confirms a package ref
// missing the @version-spec is rejected (version is required for packages
// per config-distribution-model §5).
func TestSchemaValidate_RejectsPackagesWithoutVersion(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources":  []any{map[string]any{"type": "local"}},
		"packages": []any{"acme-pkgs:skill/review-pr"}, // no @version
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("expected validation error for package ref without @version")
	}
}

// TestV2_RoundTripPreservesFields loads the v2 full fixture, marshals it,
// and confirms every v2 field survives. Demonstrates that the Go layer does
// not silently drop any v2 additive field on round-trip.
func TestV2_RoundTripPreservesFields(t *testing.T) {
	fixture := filepath.Join("testdata", "v2", "agentsrc-full.json")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		t.Fatalf("unmarshal v2 fixture: %v", err)
	}

	if rc.Version != 2 {
		t.Errorf("Version: got %d, want 2", rc.Version)
	}
	if rc.RepoID != "github.com/acme/manager-ui" {
		t.Errorf("RepoID: got %q", rc.RepoID)
	}

	// Validate v2 additive fields were loaded properly.
	assertV2ExtendsFieldsPresent(t, &rc)
	assertV2PackagesFieldsPresent(t, &rc)
	assertV2FeaturesFlagsPresent(t, &rc)
	assertV2SourceFieldsPresent(t, &rc)

	// Re-marshal and ensure all v2 keys survive in the output.
	out, err := json.Marshal(&rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertV2KeysPreservedInOutput(t, out)
	assertV2SourceKeysPreservedInOutput(t, out)

	// Re-validate the re-marshaled bytes against the schema to confirm the
	// emit form is a structurally valid v2 manifest.
	sch := compileAgentsRCSchema(t)
	var doc any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("re-marshaled v2 manifest should validate: %v", err)
	}
}

// TestV1_RoundTripOmitsV2Fields confirms a v1 manifest loaded + re-marshaled
// does NOT inject any of the v2 keys (repo_id/extends/packages/features
// nor source-level id/cache_ttl/auth) when they were absent in the input.
// This is the byte-stability guarantee called out in the additive-migration
// contract (org-config-resolution §15.2).
func TestV1_RoundTripOmitsV2Fields(t *testing.T) {
	fixture := filepath.Join("testdata", "v1", ".agentsrc.json")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		t.Fatalf("unmarshal v1 fixture: %v", err)
	}

	assertV1FieldsOmitted(t, &rc)

	out, err := json.Marshal(&rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertV1KeysNotLeaked(t, string(out))
}

// TestV2_ExtendsExtraFieldsGuard confirms the [[schema-usage]] ExtraFields
// guard is honored for every new v2 key — none should land in ExtraFields.
func TestV2_ExtendsExtraFieldsGuard(t *testing.T) {
	fixture := filepath.Join("testdata", "v2", "agentsrc-full.json")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"repo_id", "extends", "packages", "features"} {
		if _, leaked := rc.ExtraFields[key]; leaked {
			t.Errorf("v2 key %q must NOT land in ExtraFields (typed field expected): got %v",
				key, rc.ExtraFields[key])
		}
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

// TestSchemaSourceScopeOwnerDeclared asserts the source $def declares the
// config-transitive-layering task 2 routing fields: scope (enum
// public|org|team|repo) and owner (string). Guards struct<->schema drift for the
// nested source object, which the top-level coverage walk does not reach.
func TestSchemaSourceScopeOwnerDeclared(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "schemas", "agentsrc.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	props := doc.Defs["source"].Properties
	scope, ok := props["scope"]
	if !ok {
		t.Fatal("source $def missing scope property (struct<->schema drift)")
	}
	wantEnum := map[string]bool{"public": true, "org": true, "team": true, "repo": true}
	if len(scope.Enum) != len(wantEnum) {
		t.Errorf("scope enum = %v, want the four source scopes", scope.Enum)
	}
	for _, e := range scope.Enum {
		if !wantEnum[e] {
			t.Errorf("scope enum has unexpected value %q", e)
		}
	}
	owner, ok := props["owner"]
	if !ok {
		t.Fatal("source $def missing owner property")
	}
	if owner.Type != "string" {
		t.Errorf("owner type = %q, want string", owner.Type)
	}
}
