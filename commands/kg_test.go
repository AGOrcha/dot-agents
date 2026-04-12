package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTempKG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KG_HOME", dir)
	return dir
}

// ── KG config ─────────────────────────────────────────────────────────────────

func TestKGHome_EnvOverride(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/my-graph")
	if got := kgHome(); got != "/tmp/my-graph" {
		t.Errorf("expected /tmp/my-graph, got %s", got)
	}
}

func TestKGConfigRoundTrip(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "self"), 0755)

	cfg := &KGConfig{
		SchemaVersion:   1,
		Name:            "test-graph",
		Description:     "Test graph",
		AdaptersEnabled: []string{"mcp"},
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveKGConfig(cfg); err != nil {
		t.Fatalf("saveKGConfig: %v", err)
	}

	loaded, err := loadKGConfig()
	if err != nil {
		t.Fatalf("loadKGConfig: %v", err)
	}
	if loaded.Name != "test-graph" {
		t.Errorf("name: got %s, want test-graph", loaded.Name)
	}
	if len(loaded.AdaptersEnabled) != 1 || loaded.AdaptersEnabled[0] != "mcp" {
		t.Errorf("adapters_enabled mismatch: %v", loaded.AdaptersEnabled)
	}
}

func TestEnsureProjectKGMCPConfigs(t *testing.T) {
	projectDir := t.TempDir()
	agentsHome := t.TempDir()

	rcPath := filepath.Join(projectDir, ".agentsrc.json")
	if err := os.WriteFile(rcPath, []byte(`{"version":1,"project":"demo","sources":[{"type":"local"}],"kg":{"enabled":true}}`), 0644); err != nil {
		t.Fatalf("write agentsrc: %v", err)
	}

	if err := ensureProjectKGMCPConfigs("demo", projectDir, agentsHome); err != nil {
		t.Fatalf("ensureProjectKGMCPConfigs: %v", err)
	}

	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		path := filepath.Join(agentsHome, "mcp", "demo", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var payload struct {
			Servers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Type    string   `json:"type"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		server, ok := payload.Servers["dot-agents-kg"]
		if !ok {
			t.Fatalf("%s missing dot-agents-kg entry: %+v", name, payload.Servers)
		}
		if server.Command == "" || server.Type != "stdio" {
			t.Fatalf("%s invalid server config: %+v", name, server)
		}
		if len(server.Args) != 2 || server.Args[0] != "kg" || server.Args[1] != "serve" {
			t.Fatalf("%s unexpected args: %+v", name, server.Args)
		}
	}
}

func TestEnsureGlobalKGMCPConfigs(t *testing.T) {
	kgHomeDir := t.TempDir()
	t.Setenv("KG_HOME", kgHomeDir)
	agentsHome := t.TempDir()

	if err := saveKGConfig(&KGConfig{SchemaVersion: 1, Name: "kg", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("saveKGConfig: %v", err)
	}
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("ensureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "mcp.json")); err != nil {
		t.Fatalf("expected global kg mcp config: %v", err)
	}
}

// ── GraphNote parse/render ────────────────────────────────────────────────────

func TestParseGraphNote_RoundTrip(t *testing.T) {
	note := &GraphNote{
		SchemaVersion: 1,
		ID:            "note-001",
		Type:          "decision",
		Title:         "Use YAML frontmatter",
		Summary:       "Decided to use YAML frontmatter for all graph notes.",
		Status:        "active",
		Confidence:    "high",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	body := "## Rationale\n\nYAML is human-readable and widely supported.\n"

	rendered, err := renderGraphNote(note, body)
	if err != nil {
		t.Fatalf("renderGraphNote: %v", err)
	}

	parsed, parsedBody, err := parseGraphNote(rendered)
	if err != nil {
		t.Fatalf("parseGraphNote: %v", err)
	}
	if parsed.ID != note.ID {
		t.Errorf("ID: got %s, want %s", parsed.ID, note.ID)
	}
	if parsed.Type != note.Type {
		t.Errorf("Type: got %s, want %s", parsed.Type, note.Type)
	}
	if parsed.Status != note.Status {
		t.Errorf("Status: got %s, want %s", parsed.Status, note.Status)
	}
	if parsedBody != body {
		t.Errorf("body mismatch:\ngot:  %q\nwant: %q", parsedBody, body)
	}
}

func TestParseGraphNote_NoFrontmatter(t *testing.T) {
	_, _, err := parseGraphNote([]byte("Just some markdown without frontmatter."))
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseGraphNote_UnclosedFrontmatter(t *testing.T) {
	_, _, err := parseGraphNote([]byte("---\nid: x\ntitle: test\n"))
	if err == nil {
		t.Error("expected error for unclosed frontmatter")
	}
}

func TestValidators(t *testing.T) {
	for _, typ := range []string{"source", "entity", "concept", "synthesis", "decision", "repo", "session"} {
		if !isValidNoteType(typ) {
			t.Errorf("expected %s to be valid note type", typ)
		}
	}
	if isValidNoteType("unknown") {
		t.Error("'unknown' should not be a valid note type")
	}

	for _, s := range []string{"draft", "active", "stale", "superseded", "archived"} {
		if !isValidNoteStatus(s) {
			t.Errorf("expected %s to be valid note status", s)
		}
	}

	for _, c := range []string{"low", "medium", "high", ""} {
		if !isValidConfidence(c) {
			t.Errorf("expected %q to be valid confidence", c)
		}
	}
	if isValidConfidence("extreme") {
		t.Error("'extreme' should not be valid confidence")
	}
}

// ── Index and log ──────────────────────────────────────────────────────────────

func TestAppendLogEntry(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "notes"), 0755)

	logPath := filepath.Join(home, "notes", "log.md")
	_ = os.WriteFile(logPath, []byte("# Log\n"), 0644)

	if err := appendLogEntry(home, "setup | initialized"); err != nil {
		t.Fatalf("appendLogEntry: %v", err)
	}
	if err := appendLogEntry(home, "ingest | source-001"); err != nil {
		t.Fatalf("appendLogEntry: %v", err)
	}

	entries, err := readLogEntries(home, 0)
	if err != nil {
		t.Fatalf("readLogEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestReadLogEntries_Limit(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "notes"), 0755)
	logPath := filepath.Join(home, "notes", "log.md")
	_ = os.WriteFile(logPath, []byte("# Log\n"), 0644)

	for i := 0; i < 5; i++ {
		_ = appendLogEntry(home, "op | entry")
	}

	entries, err := readLogEntries(home, 3)
	if err != nil {
		t.Fatalf("readLogEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with limit=3, got %d", len(entries))
	}
}

func TestReadLogEntries_MissingFile(t *testing.T) {
	home := newTempKG(t)
	entries, err := readLogEntries(home, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}

func TestUpdateIndex_AddAndReplace(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "notes"), 0755)

	indexPath := filepath.Join(home, "notes", "index.md")
	_ = os.WriteFile(indexPath, []byte("# Knowledge Graph Index\n"), 0644)

	note := &GraphNote{
		ID:      "dec-001",
		Type:    "decision",
		Title:   "Use Go",
		Summary: "We chose Go for the implementation.",
	}
	if err := updateIndex(home, note); err != nil {
		t.Fatalf("updateIndex: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	if !strings.Contains(string(data), "dec-001") {
		t.Error("index should contain dec-001")
	}
	if !strings.Contains(string(data), "Use Go") {
		t.Error("index should contain note title")
	}

	// Update same entry with new title
	note.Title = "Use Go 1.23+"
	if err := updateIndex(home, note); err != nil {
		t.Fatalf("updateIndex (update): %v", err)
	}
	data, _ = os.ReadFile(indexPath)
	content := string(data)
	if !strings.Contains(content, "Use Go 1.23+") {
		t.Error("index should contain updated title")
	}
	// Should not have a duplicate entry (ID appears twice in one valid entry: [dec-001] + dec-001.md)
	if strings.Count(content, "- [dec-001]") != 1 {
		t.Errorf("expected 1 entry for dec-001, got %d", strings.Count(content, "- [dec-001]"))
	}
}

func TestReadIndex(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "notes"), 0755)
	indexPath := filepath.Join(home, "notes", "index.md")
	_ = os.WriteFile(indexPath, []byte("# Knowledge Graph Index\n"), 0644)

	notes := []*GraphNote{
		{ID: "ent-001", Type: "entity", Title: "Claude", Summary: "AI assistant"},
		{ID: "dec-001", Type: "decision", Title: "Use YAML", Summary: "Chosen format"},
	}
	for _, n := range notes {
		_ = updateIndex(home, n)
	}

	entries, err := readIndex(home)
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

// ── GraphHealth ────────────────────────────────────────────────────────────────

func TestGraphHealthWriteRead(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "ops", "health"), 0755)

	h := GraphHealth{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		NoteCount:     5,
		Status:        "healthy",
	}
	if err := writeGraphHealth(home, h); err != nil {
		t.Fatalf("writeGraphHealth: %v", err)
	}

	got, err := readGraphHealth(home)
	if err != nil {
		t.Fatalf("readGraphHealth: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil health")
	}
	if got.NoteCount != 5 {
		t.Errorf("NoteCount: got %d, want 5", got.NoteCount)
	}
	if got.Status != "healthy" {
		t.Errorf("Status: got %s, want healthy", got.Status)
	}
}

func TestReadGraphHealth_Missing(t *testing.T) {
	home := newTempKG(t)
	got, err := readGraphHealth(home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing health file")
	}
}

func TestComputeGraphHealth_Empty(t *testing.T) {
	home := newTempKG(t)
	// Create minimal structure
	for _, d := range []string{"notes/sources", "notes/entities", "raw/inbox", "ops/health"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0755)
	}

	h, err := computeGraphHealth(home)
	if err != nil {
		t.Fatalf("computeGraphHealth: %v", err)
	}
	if h.NoteCount != 0 {
		t.Errorf("expected 0 notes, got %d", h.NoteCount)
	}
	if h.QueueDepth != 0 {
		t.Errorf("expected 0 queue depth, got %d", h.QueueDepth)
	}
	if h.Status != "healthy" {
		t.Errorf("expected healthy status, got %s", h.Status)
	}
}

func TestComputeGraphHealth_WithNotes(t *testing.T) {
	home := newTempKG(t)
	for _, d := range []string{"notes/decisions", "notes/entities", "raw/inbox", "ops/health"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0755)
	}

	// Write a decision note
	note := &GraphNote{
		SchemaVersion: 1, ID: "dec-001", Type: "decision",
		Title: "T", Summary: "S", Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	rendered, _ := renderGraphNote(note, "body")
	_ = os.WriteFile(filepath.Join(home, "notes", "decisions", "dec-001.md"), rendered, 0644)

	// Write a stale entity note
	staleNote := &GraphNote{
		SchemaVersion: 1, ID: "ent-001", Type: "entity",
		Title: "E", Summary: "S", Status: "stale",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	rendered2, _ := renderGraphNote(staleNote, "body")
	_ = os.WriteFile(filepath.Join(home, "notes", "entities", "ent-001.md"), rendered2, 0644)

	h, err := computeGraphHealth(home)
	if err != nil {
		t.Fatalf("computeGraphHealth: %v", err)
	}
	if h.NoteCount != 2 {
		t.Errorf("expected 2 notes, got %d", h.NoteCount)
	}
	if h.StaleCount != 1 {
		t.Errorf("expected 1 stale note, got %d", h.StaleCount)
	}
}

// ── kg setup command ──────────────────────────────────────────────────────────

func TestKGSetup_CreatesAllDirs(t *testing.T) {
	home := newTempKG(t)

	if err := runKGSetup(); err != nil {
		t.Fatalf("runKGSetup: %v", err)
	}

	// Verify key directories exist
	expectedDirs := []string{
		"self", "raw/inbox", "raw/imported",
		"notes/sources", "notes/entities", "notes/concepts",
		"notes/synthesis", "notes/decisions", "notes/repos",
		"ops/queue", "ops/health",
	}
	for _, d := range expectedDirs {
		if _, err := os.Stat(filepath.Join(home, d)); err != nil {
			t.Errorf("expected dir %s to exist: %v", d, err)
		}
	}
}

func TestKGSetup_CreatesConfigAndIndex(t *testing.T) {
	home := newTempKG(t)

	if err := runKGSetup(); err != nil {
		t.Fatalf("runKGSetup: %v", err)
	}

	// Config should exist
	cfg, err := loadKGConfig()
	if err != nil {
		t.Fatalf("loadKGConfig after setup: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d, want 1", cfg.SchemaVersion)
	}

	// Index should exist
	if _, err := os.Stat(filepath.Join(home, "notes", "index.md")); err != nil {
		t.Errorf("notes/index.md missing: %v", err)
	}

	// Log should have setup entry
	entries, _ := readLogEntries(home, 0)
	if len(entries) == 0 {
		t.Error("expected at least one log entry after setup")
	}
}

func TestKGSetup_Idempotent(t *testing.T) {
	newTempKG(t)

	if err := runKGSetup(); err != nil {
		t.Fatalf("first runKGSetup: %v", err)
	}
	// Second run should not error
	if err := runKGSetup(); err != nil {
		t.Fatalf("second runKGSetup (idempotent): %v", err)
	}
}

// ── kg health command ─────────────────────────────────────────────────────────

func TestKGHealth_NotInitialized(t *testing.T) {
	newTempKG(t)
	err := runKGHealth()
	if err == nil {
		t.Error("expected error when KG not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' in error, got: %v", err)
	}
}

func TestKGHealth_AfterSetup(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runKGHealth(); err != nil {
		t.Fatalf("runKGHealth: %v", err)
	}
}

// ── Phase 2: Raw source ────────────────────────────────────────────────────────

func TestRecordRawSource_And_ListPending(t *testing.T) {
	home := newTempKG(t)
	_ = os.MkdirAll(filepath.Join(home, "raw", "inbox"), 0755)

	src := RawSource{
		SchemaVersion: 1,
		ID:            "test-src-001",
		Title:         "Test Document",
		SourceType:    "markdown",
		CapturedAt:    "2026-01-01T00:00:00Z",
		Status:        "pending",
	}
	content := []byte("# Test\n\nSome content.")
	if err := recordRawSource(home, src, content); err != nil {
		t.Fatalf("recordRawSource: %v", err)
	}

	pending, err := listPendingRawSources(home)
	if err != nil {
		t.Fatalf("listPendingRawSources: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending source, got %d", len(pending))
	}
	if pending[0].ID != "test-src-001" {
		t.Errorf("ID: got %s, want test-src-001", pending[0].ID)
	}
}

func TestMoveToImported(t *testing.T) {
	home := newTempKG(t)
	for _, d := range []string{"raw/inbox", "raw/imported"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0755)
	}

	src := RawSource{SchemaVersion: 1, ID: "mv-001", Title: "T", SourceType: "markdown", Status: "pending"}
	_ = recordRawSource(home, src, []byte("body"))

	if err := moveToImported(home, "mv-001"); err != nil {
		t.Fatalf("moveToImported: %v", err)
	}
	// Inbox empty
	pending, _ := listPendingRawSources(home)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after move, got %d", len(pending))
	}
	// Imported file exists
	if _, err := os.Stat(filepath.Join(home, "raw", "imported", "mv-001.md")); err != nil {
		t.Errorf("imported file missing: %v", err)
	}
}

func TestIsValidSourceType(t *testing.T) {
	for _, typ := range []string{"markdown", "pdf", "text", "url", "transcript", "meeting_notes", "repo_doc"} {
		if !isValidSourceType(typ) {
			t.Errorf("expected %s to be valid source type", typ)
		}
	}
	if isValidSourceType("unknown") {
		t.Error("'unknown' should not be valid source type")
	}
}

// ── Phase 2: Extraction helpers ───────────────────────────────────────────────

func TestExtractClaims(t *testing.T) {
	content := `# Main Title
Some text.
- **Bold claim**
- This is a simple list item
- Item that is assertive
`
	claims := extractClaims(content)
	if len(claims) == 0 {
		t.Error("expected at least one claim")
	}
	found := false
	for _, c := range claims {
		if c == "Main Title" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Main Title' in claims, got: %v", claims)
	}
}

func TestExtractEntities(t *testing.T) {
	content := "We use `GraphNote` and `cobra.Command` for the implementation. Claude Code is the tool."
	entities := extractEntities(content)
	if len(entities) == 0 {
		t.Error("expected at least one entity")
	}
	found := false
	for _, e := range entities {
		if e == "GraphNote" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'GraphNote' in entities, got: %v", entities)
	}
}

func TestExtractDecisions(t *testing.T) {
	content := `We decided to use Go for the project.
This is a normal sentence.
The team chose YAML for configuration.
`
	decisions := extractDecisions(content)
	if len(decisions) < 2 {
		t.Errorf("expected at least 2 decisions, got %d: %v", len(decisions), decisions)
	}
}

// ── Phase 2: Note create / update ────────────────────────────────────────────

func TestCreateGraphNote_And_Update(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	note := &GraphNote{
		SchemaVersion: 1,
		ID:            "dec-phase2",
		Type:          "decision",
		Title:         "Use YAML",
		Summary:       "We chose YAML.",
		Status:        "draft",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(home, note, "body"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}

	// Duplicate creation should fail
	if err := createGraphNote(home, note, "body"); err == nil {
		t.Error("expected error for duplicate note creation")
	}

	// Update the note
	note.Title = "Use YAML v2"
	note.Summary = "Updated summary."
	if err := updateGraphNote(home, note, "new body"); err != nil {
		t.Fatalf("updateGraphNote: %v", err)
	}

	// Verify file was updated
	path := filepath.Join(home, "notes", "decisions", "dec-phase2.md")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Use YAML v2") {
		t.Error("updated title not found in note file")
	}
}

func TestNoteExists(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	note := &GraphNote{
		SchemaVersion: 1, ID: "exist-test", Type: "concept",
		Title: "T", Summary: "S", Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	_ = createGraphNote(home, note, "")

	if exists, _ := noteExists(home, "exist-test"); !exists {
		t.Error("expected note to exist")
	}
	if exists, _ := noteExists(home, "does-not-exist"); exists {
		t.Error("expected note to not exist")
	}
}

// ── Phase 2: Full ingest pipeline ────────────────────────────────────────────

func TestIngestSource_FullPipeline(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	content := `# Design Decision

We decided to use Go modules for dependency management.
The team chose cobra for CLI parsing.

## Claude Code Integration

This repo uses Claude Code and GitHub Actions for automation.
`
	src := RawSource{
		SchemaVersion: 1, ID: "design-001", Title: "Design Decision",
		SourceType: "markdown", Status: "pending",
		CapturedAt: "2026-01-01T00:00:00Z",
	}
	if err := recordRawSource(home, src, []byte(content)); err != nil {
		t.Fatalf("recordRawSource: %v", err)
	}

	result, err := ingestSource(home, "design-001")
	if err != nil {
		t.Fatalf("ingestSource: %v", err)
	}

	if result.SourceID != "design-001" {
		t.Errorf("SourceID: got %s, want design-001", result.SourceID)
	}
	if len(result.NotesCreated) == 0 {
		t.Error("expected at least one note created")
	}

	// Source summary note should exist
	if exists, _ := noteExists(home, "src-design-001"); !exists {
		t.Error("source summary note src-design-001 should exist")
	}

	// Source should be moved to imported
	if _, err := os.Stat(filepath.Join(home, "raw", "inbox", "design-001.md")); !os.IsNotExist(err) {
		t.Error("source should no longer be in inbox")
	}
	if _, err := os.Stat(filepath.Join(home, "raw", "imported", "design-001.md")); err != nil {
		t.Errorf("source should be in imported: %v", err)
	}

	// Health should be updated
	health, err := readGraphHealth(home)
	if err != nil || health == nil {
		t.Fatalf("readGraphHealth: %v", err)
	}
	if health.NoteCount == 0 {
		t.Error("expected note count > 0 after ingest")
	}
}

// ── Phase 2: Queue command ────────────────────────────────────────────────────

func TestKGQueue_Empty(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runKGQueue(); err != nil {
		t.Fatalf("runKGQueue: %v", err)
	}
}

func TestKGQueue_WithItems(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for i, id := range []string{"q-001", "q-002"} {
		src := RawSource{
			SchemaVersion: 1, ID: id, Title: fmt.Sprintf("Source %d", i+1),
			SourceType: "markdown", Status: "pending",
		}
		_ = recordRawSource(home, src, []byte("content"))
	}

	pending, err := listPendingRawSources(home)
	if err != nil {
		t.Fatalf("listPendingRawSources: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending items, got %d", len(pending))
	}
}

// ── Phase 3: Query types and validation ───────────────────────────────────────

func TestIsValidQueryIntent(t *testing.T) {
	valid := []string{
		"source_lookup", "entity_context", "concept_context", "decision_lookup",
		"repo_context", "synthesis_lookup", "related_notes", "contradictions", "graph_health",
	}
	for _, intent := range valid {
		if !isValidQueryIntent(intent) {
			t.Errorf("expected %s to be valid intent", intent)
		}
	}
	if isValidQueryIntent("unknown_intent") {
		t.Error("'unknown_intent' should not be valid")
	}
}

// ── Phase 3: Search engine ────────────────────────────────────────────────────

func setupKGWithNotes(t *testing.T) string {
	t.Helper()
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	notes := []*GraphNote{
		{SchemaVersion: 1, ID: "ent-cobra", Type: "entity", Title: "cobra", Summary: "CLI framework for Go.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "ent-yaml", Type: "entity", Title: "YAML", Summary: "Configuration format.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-use-cobra", Type: "decision", Title: "Use cobra for CLI", Summary: "We decided to use cobra.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-use-yaml", Type: "decision", Title: "Use YAML config", Summary: "Team chose YAML for all configuration.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "rep-dot-agents", Type: "repo", Title: "dot-agents", Summary: "CLI for managing agent configs.", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		if err := createGraphNote(home, n, "note body for "+n.ID); err != nil {
			t.Fatalf("createGraphNote %s: %v", n.ID, err)
		}
	}
	return home
}

func TestSearchNotes_ByType(t *testing.T) {
	home := setupKGWithNotes(t)

	results, err := searchNotes(home, "decision", "cobra", 10)
	if err != nil {
		t.Fatalf("searchNotes: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for 'cobra' in decisions")
	}
	if results[0].ID != "dec-use-cobra" {
		t.Errorf("expected dec-use-cobra as top result, got %s", results[0].ID)
	}
}

func TestSearchNotes_AllTypes(t *testing.T) {
	home := setupKGWithNotes(t)

	results, err := searchNotes(home, "", "YAML", 10)
	if err != nil {
		t.Fatalf("searchNotes all: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 YAML results across types, got %d", len(results))
	}
}

func TestSearchNotes_Limit(t *testing.T) {
	home := setupKGWithNotes(t)

	// Empty query should match all (score 0 via body)
	results, err := searchNotes(home, "entity", "note body", 1)
	if err != nil {
		t.Fatalf("searchNotes: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("expected limit=1 to cap results, got %d", len(results))
	}
}

func TestSearchNotes_Empty(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	results, err := searchNotes(home, "entity", "anything", 10)
	if err != nil {
		t.Fatalf("searchNotes empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results in empty graph, got %d", len(results))
	}
}

func TestSearchByLinks(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"

	// Create a note with a link
	linked := &GraphNote{
		SchemaVersion: 1, ID: "ent-linked", Type: "entity",
		Title: "Linked Entity", Summary: "A linked note.", Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	_ = createGraphNote(home, linked, "")

	root := &GraphNote{
		SchemaVersion: 1, ID: "dec-root", Type: "decision",
		Title: "Root Decision", Summary: "Root.", Status: "active",
		Links:     []string{"ent-linked"},
		CreatedAt: now, UpdatedAt: now,
	}
	_ = createGraphNote(home, root, "")

	results, err := searchByLinks(home, "dec-root")
	if err != nil {
		t.Fatalf("searchByLinks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 linked result, got %d", len(results))
	}
	if results[0].ID != "ent-linked" {
		t.Errorf("expected ent-linked, got %s", results[0].ID)
	}
}

func TestSearchByLinks_NotFound(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := searchByLinks(home, "does-not-exist")
	if err == nil {
		t.Error("expected error for missing note")
	}
}

// ── Phase 3: Intent dispatch ──────────────────────────────────────────────────

func TestExecuteQuery_DecisionLookup(t *testing.T) {
	home := setupKGWithNotes(t)

	resp, err := executeQuery(home, GraphQuery{Intent: "decision_lookup", Query: "cobra", Limit: 10})
	if err != nil {
		t.Fatalf("executeQuery: %v", err)
	}
	if resp.Intent != "decision_lookup" {
		t.Errorf("intent mismatch: got %s", resp.Intent)
	}
	if resp.Provider != "local-index" {
		t.Errorf("provider: got %s", resp.Provider)
	}
	if len(resp.Results) == 0 {
		t.Error("expected results for 'cobra' decision lookup")
	}
}

func TestExecuteQuery_GraphHealth(t *testing.T) {
	home := setupKGWithNotes(t)

	resp, err := executeQuery(home, GraphQuery{Intent: "graph_health", Query: ""})
	if err != nil {
		t.Fatalf("executeQuery graph_health: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Error("expected health result")
	}
	if resp.Results[0].ID != "graph-health" {
		t.Errorf("expected graph-health result, got %s", resp.Results[0].ID)
	}
}

func TestExecuteQuery_Contradictions_NoConflict(t *testing.T) {
	// setupKGWithNotes has decisions "Use cobra for CLI" and "Use YAML config" —
	// different enough topics that contradiction detection finds nothing.
	home := setupKGWithNotes(t)

	resp, err := executeQuery(home, GraphQuery{Intent: "contradictions", Query: ""})
	if err != nil {
		t.Fatalf("executeQuery contradictions: %v", err)
	}
	// Results may be empty (no contradictions in fixture); just verify no error and valid shape
	if resp.SchemaVersion != 1 {
		t.Errorf("expected schema_version 1, got %d", resp.SchemaVersion)
	}
	if resp.Results == nil {
		t.Error("Results should be non-nil slice")
	}
}

func TestExecuteQuery_UnknownIntent(t *testing.T) {
	home := setupKGWithNotes(t)

	_, err := executeQuery(home, GraphQuery{Intent: "does_not_exist", Query: "x"})
	if err == nil {
		t.Error("expected error for unknown intent")
	}
}

func TestExecuteQuery_LogsEntry(t *testing.T) {
	home := setupKGWithNotes(t)

	_, _ = executeQuery(home, GraphQuery{Intent: "decision_lookup", Query: "yaml", Limit: 5})

	entries, err := readLogEntries(home, 0)
	if err != nil {
		t.Fatalf("readLogEntries: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e, "query") && strings.Contains(e, "decision_lookup") {
			found = true
		}
	}
	if !found {
		t.Error("expected query log entry after executeQuery")
	}
}

func TestExecuteQuery_AllTypedIntents(t *testing.T) {
	home := setupKGWithNotes(t)

	intents := []string{"source_lookup", "entity_context", "concept_context", "decision_lookup", "repo_context", "synthesis_lookup"}
	for _, intent := range intents {
		resp, err := executeQuery(home, GraphQuery{Intent: intent, Query: "anything", Limit: 5})
		if err != nil {
			t.Errorf("executeQuery %s: %v", intent, err)
			continue
		}
		if resp.SchemaVersion != 1 {
			t.Errorf("%s: schema_version should be 1", intent)
		}
		// Results may be empty (no notes of that type seeded), but should not error
		if resp.Results == nil {
			t.Errorf("%s: Results should be non-nil slice", intent)
		}
	}
}

// ── Phase 3: Batch query ──────────────────────────────────────────────────────

func TestExecuteBatchQuery(t *testing.T) {
	home := setupKGWithNotes(t)

	queries := []GraphQuery{
		{Intent: "decision_lookup", Query: "cobra", Limit: 5},
		{Intent: "entity_context", Query: "yaml", Limit: 5},
		{Intent: "graph_health", Query: ""},
	}
	responses, err := executeBatchQuery(home, queries)
	if err != nil {
		t.Fatalf("executeBatchQuery: %v", err)
	}
	if len(responses) != 3 {
		t.Errorf("expected 3 responses, got %d", len(responses))
	}
	for i, r := range responses {
		if r.Intent != queries[i].Intent {
			t.Errorf("response[%d] intent mismatch: got %s, want %s", i, r.Intent, queries[i].Intent)
		}
	}
}

// ── Phase 4: Link graph ───────────────────────────────────────────────────────

func TestBuildLinkGraph_Empty(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	adj, notes, err := buildLinkGraph(home)
	if err != nil {
		t.Fatalf("buildLinkGraph: %v", err)
	}
	if len(adj) != 0 || len(notes) != 0 {
		t.Errorf("expected empty graph, got adj=%d notes=%d", len(adj), len(notes))
	}
}

func TestBuildLinkGraph_WithLinks(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	target := &GraphNote{SchemaVersion: 1, ID: "ent-target", Type: "entity", Title: "Target", Summary: "T", Status: "active", CreatedAt: now, UpdatedAt: now}
	root := &GraphNote{SchemaVersion: 1, ID: "dec-root", Type: "decision", Title: "Root", Summary: "R", Status: "active", Links: []string{"ent-target"}, CreatedAt: now, UpdatedAt: now}
	_ = createGraphNote(home, target, "")
	_ = createGraphNote(home, root, "")

	adj, notes, err := buildLinkGraph(home)
	if err != nil {
		t.Fatalf("buildLinkGraph: %v", err)
	}
	if len(notes) != 2 {
		t.Errorf("expected 2 notes, got %d", len(notes))
	}
	if len(adj["dec-root"]) != 1 || adj["dec-root"][0] != "ent-target" {
		t.Errorf("expected dec-root -> ent-target link, got %v", adj["dec-root"])
	}
}

// ── Phase 4: Individual lint checks ──────────────────────────────────────────

func setupLintFixture(t *testing.T) (string, map[string][]string, map[string]*GraphNote) {
	t.Helper()
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	notes := []*GraphNote{
		{SchemaVersion: 1, ID: "ent-a", Type: "entity", Title: "Entity A", Summary: "Summary A.", Status: "active", SourceRefs: []string{"src-1"}, CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-good", Type: "decision", Title: "Use Go", Summary: "We chose Go.", Status: "active", SourceRefs: []string{"src-1"}, Links: []string{"ent-a"}, CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-orphan", Type: "decision", Title: "Orphan Decision", Summary: "No refs.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-broken", Type: "decision", Title: "Broken Link", Summary: "Has broken link.", Status: "active", Links: []string{"does-not-exist"}, CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		if err := createGraphNote(home, n, "body"); err != nil {
			t.Fatalf("createGraphNote %s: %v", n.ID, err)
		}
	}
	adj, noteMap, err := buildLinkGraph(home)
	if err != nil {
		t.Fatalf("buildLinkGraph: %v", err)
	}
	return home, adj, noteMap
}

func TestLintBrokenLinks(t *testing.T) {
	_, adj, notes := setupLintFixture(t)
	results := lintBrokenLinks(adj, notes)
	found := false
	for _, r := range results {
		if r.NoteID == "dec-broken" && r.Check == "broken_links" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected broken_links finding for dec-broken, got: %v", results)
	}
}

func TestLintOrphanPages(t *testing.T) {
	_, adj, notes := setupLintFixture(t)
	results := lintOrphanPages(adj, notes)
	found := false
	for _, r := range results {
		if r.NoteID == "dec-orphan" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected orphan finding for dec-orphan, got: %v", results)
	}
}

func TestLintMissingSourceRefs(t *testing.T) {
	_, _, notes := setupLintFixture(t)
	results := lintMissingSourceRefs(notes)
	found := false
	for _, r := range results {
		if r.NoteID == "dec-orphan" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing_source_refs for dec-orphan, got: %v", results)
	}
}

func TestLintStalePages(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	oldTime := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	staleNote := &GraphNote{
		SchemaVersion: 1, ID: "ent-stale", Type: "entity",
		Title: "Old Entity", Summary: "Very old.", Status: "active",
		CreatedAt: oldTime, UpdatedAt: oldTime,
	}
	_ = createGraphNote(home, staleNote, "")

	_, notes, _ := buildLinkGraph(home)
	results := lintStalePages(notes, 90*24*time.Hour)
	if len(results) == 0 {
		t.Error("expected stale_pages finding")
	}
	if results[0].NoteID != "ent-stale" {
		t.Errorf("expected ent-stale, got %s", results[0].NoteID)
	}
}

func TestLintIndexDrift(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	note := &GraphNote{SchemaVersion: 1, ID: "ent-drift", Type: "entity", Title: "Drift", Summary: "S", Status: "active", CreatedAt: now, UpdatedAt: now}
	_ = createGraphNote(home, note, "")

	// Manually remove from index to create drift
	indexPath := filepath.Join(home, "notes", "index.md")
	data, _ := os.ReadFile(indexPath)
	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, l := range lines {
		if !strings.Contains(l, "ent-drift") {
			kept = append(kept, l)
		}
	}
	_ = os.WriteFile(indexPath, []byte(strings.Join(kept, "\n")), 0644)

	_, noteMap, _ := buildLinkGraph(home)
	results := lintIndexDrift(home, noteMap)
	if len(results) == 0 {
		t.Error("expected index_drift finding")
	}
}

func TestLintContradictions(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	notes := []*GraphNote{
		{SchemaVersion: 1, ID: "dec-use-yaml", Type: "decision", Title: "Use YAML config format", Summary: "Use YAML.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-use-json", Type: "decision", Title: "Use JSON config format", Summary: "Use JSON.", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		_ = createGraphNote(home, n, "")
	}
	_, noteMap, _ := buildLinkGraph(home)
	results := lintContradictions(noteMap)
	if len(results) == 0 {
		t.Error("expected contradiction finding between YAML and JSON config decisions")
	}
}

func TestLintContradictions_NonConflicting(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	notes := []*GraphNote{
		{SchemaVersion: 1, ID: "dec-a", Type: "decision", Title: "Use cobra for CLI parsing", Summary: "S.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-b", Type: "decision", Title: "Deploy to production weekly", Summary: "S.", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		_ = createGraphNote(home, n, "")
	}
	_, noteMap, _ := buildLinkGraph(home)
	results := lintContradictions(noteMap)
	if len(results) != 0 {
		t.Errorf("expected no contradictions for unrelated decisions, got: %v", results)
	}
}

// ── Phase 4: Full lint run ────────────────────────────────────────────────────

func TestRunGraphLint_FullRun(t *testing.T) {
	home, _, _ := setupLintFixture(t)

	report, err := runGraphLint(home)
	if err != nil {
		t.Fatalf("runGraphLint: %v", err)
	}
	if report.ChecksRun != 8 { // 7 original + integrity_violation (Phase 6A)
		t.Errorf("expected 8 checks, got %d", report.ChecksRun)
	}
	// Should have at least one error (broken link)
	if report.ErrorCount == 0 {
		t.Error("expected at least one error from broken_links")
	}
	// Report file should exist
	if _, err := os.Stat(filepath.Join(home, "ops", "lint", "lint-report.json")); err != nil {
		t.Errorf("lint-report.json missing: %v", err)
	}
}

// ── Phase 4: Contradictions query (Phase 3 upgrade) ──────────────────────────

func TestExecuteQuery_Contradictions_Live(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	notes := []*GraphNote{
		{SchemaVersion: 1, ID: "dec-yaml", Type: "decision", Title: "Use YAML config format", Summary: "YAML.", Status: "active", CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, ID: "dec-toml", Type: "decision", Title: "Use TOML config format", Summary: "TOML.", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		_ = createGraphNote(home, n, "")
	}

	resp, err := executeQuery(home, GraphQuery{Intent: "contradictions", Query: ""})
	if err != nil {
		t.Fatalf("executeQuery contradictions: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Error("expected contradiction results from live detection")
	}
}

// ── Phase 4: Maintenance operations ──────────────────────────────────────────

func TestRunKGReweave_RemovesBrokenLinks(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	note := &GraphNote{
		SchemaVersion: 1, ID: "dec-reweave", Type: "decision",
		Title: "Reweave Test", Summary: "S", Status: "active",
		Links:     []string{"does-not-exist"},
		CreatedAt: now, UpdatedAt: now,
	}
	_ = createGraphNote(home, note, "body")

	if err := runKGReweave(home); err != nil {
		t.Fatalf("runKGReweave: %v", err)
	}

	// Verify broken link was removed
	path := filepath.Join(home, "notes", "decisions", "dec-reweave.md")
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "does-not-exist") {
		t.Error("broken link should have been removed by reweave")
	}
}

func TestRunKGMarkStale(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	oldTime := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	note := &GraphNote{
		SchemaVersion: 1, ID: "ent-mark-stale", Type: "entity",
		Title: "Old", Summary: "S", Status: "active",
		CreatedAt: oldTime, UpdatedAt: oldTime,
	}
	_ = createGraphNote(home, note, "body")

	if err := runKGMarkStale(home, 90*24*time.Hour); err != nil {
		t.Fatalf("runKGMarkStale: %v", err)
	}

	path := filepath.Join(home, "notes", "entities", "ent-mark-stale.md")
	data, _ := os.ReadFile(path)
	parsed, _, _ := parseGraphNote(data)
	if parsed.Status != "stale" {
		t.Errorf("expected status=stale, got %s", parsed.Status)
	}
}

func TestRunKGCompact(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-01-01T00:00:00Z"
	note := &GraphNote{
		SchemaVersion: 1, ID: "dec-archived", Type: "decision",
		Title: "Old Decision", Summary: "S", Status: "archived",
		CreatedAt: now, UpdatedAt: now,
	}
	_ = createGraphNote(home, note, "body")

	if err := runKGCompact(home); err != nil {
		t.Fatalf("runKGCompact: %v", err)
	}

	// Note should be moved to _archived/
	archivePath := filepath.Join(home, "notes", "_archived", "dec-archived.md")
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("expected note in _archived: %v", err)
	}
	// Original should be gone
	origPath := filepath.Join(home, "notes", "decisions", "dec-archived.md")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Error("original note should have been moved")
	}
}

// ── Phase 5: Bridge intent mapping ───────────────────────────────────────────

func TestResolveBridgeQuery(t *testing.T) {
	queries, err := resolveBridgeQuery("plan_context", "deployment")
	if err != nil {
		t.Fatalf("resolveBridgeQuery: %v", err)
	}
	if len(queries) < 2 {
		t.Errorf("plan_context should fan out to 2+ KG queries, got %d", len(queries))
	}
	for _, q := range queries {
		if q.Query != "deployment" {
			t.Errorf("query string not propagated: got %s", q.Query)
		}
	}
}

func TestResolveBridgeQuery_Unknown(t *testing.T) {
	_, err := resolveBridgeQuery("unknown_bridge_intent", "x")
	if err == nil {
		t.Error("expected error for unknown bridge intent")
	}
}

func TestMergeBridgeResults_Deduplication(t *testing.T) {
	r := GraphQueryResult{ID: "dec-001", Type: "decision", Title: "T", Summary: "S"}
	resp1 := GraphQueryResponse{Intent: "decision_lookup", Results: []GraphQueryResult{r}}
	resp2 := GraphQueryResponse{Intent: "synthesis_lookup", Results: []GraphQueryResult{r}} // same note

	merged := mergeBridgeResults([]GraphQueryResponse{resp1, resp2}, "plan_context")
	if len(merged.Results) != 1 {
		t.Errorf("expected 1 deduplicated result, got %d", len(merged.Results))
	}
	if merged.Intent != "plan_context" {
		t.Errorf("expected plan_context intent, got %s", merged.Intent)
	}
}

// ── Phase 5: LocalFileAdapter ─────────────────────────────────────────────────

func TestLocalFileAdapter_Available(t *testing.T) {
	home := newTempKG(t)
	adapter := NewLocalFileAdapter(home)
	if adapter.Available() {
		t.Error("adapter should be unavailable before setup")
	}
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !adapter.Available() {
		t.Error("adapter should be available after setup")
	}
}

func TestLocalFileAdapter_Query(t *testing.T) {
	home := setupKGWithNotes(t)
	adapter := NewLocalFileAdapter(home)

	resp, err := adapter.Query(GraphQuery{Intent: "decision_lookup", Query: "cobra", Limit: 5})
	if err != nil {
		t.Fatalf("adapter.Query: %v", err)
	}
	if resp.Provider != "local-index" {
		t.Errorf("provider: got %s", resp.Provider)
	}
	if len(resp.Results) == 0 {
		t.Error("expected results for 'cobra'")
	}
}

func TestLocalFileAdapter_Health(t *testing.T) {
	home := setupKGWithNotes(t)
	adapter := NewLocalFileAdapter(home)

	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("adapter.Health: %v", err)
	}
	if !h.Available {
		t.Error("expected adapter available")
	}
	if h.NoteCount == 0 {
		t.Error("expected note count > 0")
	}
}

// ── Phase 5: executeBridgeQuery ───────────────────────────────────────────────

func TestExecuteBridgeQuery(t *testing.T) {
	home := setupKGWithNotes(t)

	resp, err := executeBridgeQuery(home, "decision_lookup", "cobra")
	if err != nil {
		t.Fatalf("executeBridgeQuery: %v", err)
	}
	if resp.Intent != "decision_lookup" {
		t.Errorf("intent: got %s", resp.Intent)
	}
	if len(resp.Results) == 0 {
		t.Error("expected results for cobra decision lookup")
	}
}

func TestExecuteBridgeQuery_PlanContext_Fanout(t *testing.T) {
	home := setupKGWithNotes(t)

	resp, err := executeBridgeQuery(home, "plan_context", "cobra")
	if err != nil {
		t.Fatalf("executeBridgeQuery plan_context: %v", err)
	}
	if resp.Intent != "plan_context" {
		t.Errorf("intent: got %s", resp.Intent)
	}
	// plan_context fans out to decision_lookup + synthesis_lookup — should get decision results
	if len(resp.Results) == 0 {
		t.Error("expected results from plan_context fanout")
	}
}

// ── Phase 5: Bridge contract ──────────────────────────────────────────────────

func TestWriteBridgeContract(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Setup already calls writeBridgeContract; verify file exists and is valid YAML
	contractPath := filepath.Join(home, "self", "schema", "bridge-contract.yaml")
	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("bridge-contract.yaml missing: %v", err)
	}
	if !strings.Contains(string(data), "plan_context") {
		t.Error("contract should contain plan_context intent")
	}
	if !strings.Contains(string(data), "local-file") {
		t.Error("contract should list local-file adapter")
	}
}

// ── Phase 6A: Integrity manifest ─────────────────────────────────────────────

func TestManifest_InitAndLoad(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	m, err := loadManifest(home)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d", m.SchemaVersion)
	}
	if len(m.Notes) != 0 {
		t.Errorf("expected empty manifest after setup, got %d entries", len(m.Notes))
	}
}

func TestManifest_UpdatedOnCreate(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	note := &GraphNote{SchemaVersion: 1, ID: "ent-test-001", Type: "entity", Title: "Test", Status: "active", CreatedAt: "2026-04-10T00:00:00Z"}
	body := "Test body content."
	if err := createGraphNote(home, note, body); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}
	m, err := loadManifest(home)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	entry, ok := m.Notes["ent-test-001"]
	if !ok {
		t.Fatal("manifest should have entry for ent-test-001")
	}
	if entry.Hash != noteBodyHash(body) {
		t.Errorf("hash mismatch: got %s", entry.Hash)
	}
}

func TestManifest_VersionIncrementOnUpdate(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	note := &GraphNote{SchemaVersion: 1, ID: "ent-v-001", Type: "entity", Title: "V Test", Status: "active", CreatedAt: "2026-04-10T00:00:00Z"}
	if err := createGraphNote(home, note, "v0"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}
	if note.Version != 0 {
		t.Errorf("version after create: want 0, got %d", note.Version)
	}
	note.Title = "V Test Updated"
	if err := updateGraphNote(home, note, "v1"); err != nil {
		t.Fatalf("updateGraphNote: %v", err)
	}
	// Re-read from disk and check version
	path := filepath.Join(home, "notes", "entities", "ent-v-001.md")
	data, _ := os.ReadFile(path)
	reloaded, _, _ := parseGraphNote(data)
	if reloaded.Version != 1 {
		t.Errorf("version after first update: want 1, got %d", reloaded.Version)
	}
}

func TestLintIntegrityViolations_CleanGraph(t *testing.T) {
	home := setupKGWithNotes(t)
	_, notes, err := buildLinkGraph(home)
	if err != nil {
		t.Fatalf("buildLinkGraph: %v", err)
	}
	results := lintIntegrityViolations(home, notes)
	if len(results) != 0 {
		t.Errorf("expected no integrity violations on clean graph, got %d", len(results))
	}
}

func TestLintIntegrityViolations_DetectsOutOfBandEdit(t *testing.T) {
	home := setupKGWithNotes(t)
	// Directly modify a note file outside of kg commands
	notePath := filepath.Join(home, "notes", "entities", "ent-cobra.md")
	existing, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	// Append directly to file (bypassing updateGraphNote)
	modified := string(existing) + "\nOut-of-band edit.\n"
	if err := os.WriteFile(notePath, []byte(modified), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	_, notes, err := buildLinkGraph(home)
	if err != nil {
		t.Fatalf("buildLinkGraph: %v", err)
	}
	results := lintIntegrityViolations(home, notes)
	found := false
	for _, r := range results {
		if r.NoteID == "ent-cobra" && r.Check == "integrity_violation" {
			found = true
		}
	}
	if !found {
		t.Error("expected integrity_violation for ent-cobra after out-of-band edit")
	}
}

// ── Phase D: warm layer + note→symbol links ────────────────────────────────

func TestRunKGSetup_InitializesWarmDB(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("runKGSetup: %v", err)
	}
	dbPath := graphstoreDBPath(home)
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected warm DB at %s, got: %v", dbPath, err)
	}
}

func TestRunKGWarm_IndexesNotes(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	cmd := newKGWarmCmdForTest()
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	// setupKGWithNotes creates 5 notes (2 entities, 2 decisions, 1 repo)
	if stats.NotesCount != 5 {
		t.Errorf("expected 5 notes in warm layer, got %d", stats.NotesCount)
	}
}

func TestRunKGWarm_TypeFilter(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	cmd := newKGWarmCmdForTest()
	if err := cmd.Flags().Set("type", "entity"); err != nil {
		t.Fatalf("set type flag: %v", err)
	}
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("runKGWarm with type=entity: %v", err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	stats, _ := store.GetStats()
	// Only 2 entity notes
	if stats.NotesCount != 2 {
		t.Errorf("expected 2 entity notes after type filter, got %d", stats.NotesCount)
	}
}

func TestRunKGWarm_Idempotent(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	cmd := newKGWarmCmdForTest()
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("first runKGWarm: %v", err)
	}
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("second runKGWarm: %v", err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	stats, _ := store.GetStats()
	if stats.NotesCount != 5 {
		t.Errorf("idempotent warm should produce 5 notes, got %d", stats.NotesCount)
	}
}

func TestRunKGWarm_ArchivedNotesIndexed(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	// Compact to move superseded/archived notes to _archived dir
	// First mark a note as archived
	archiveDir := filepath.Join(home, "notes", "_archived")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Manually write an archived note
	archivedNote := &GraphNote{
		SchemaVersion: 1, ID: "archived-001", Type: "decision",
		Title: "Old Decision", Status: "archived",
		CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-06-01T00:00:00Z",
	}
	if err := createGraphNote(home, archivedNote, "This decision was superseded."); err != nil {
		t.Fatalf("createGraphNote archived: %v", err)
	}
	// Move it to _archived
	src := filepath.Join(home, "notes", "decisions", "archived-001.md")
	dst := filepath.Join(archiveDir, "archived-001.md")
	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("move to archived: %v", err)
	}

	cmd := newKGWarmCmdForTest()
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	n, err := store.GetKGNote("archived-001")
	if err != nil {
		t.Fatalf("GetKGNote: %v", err)
	}
	if n == nil {
		t.Fatal("archived note not indexed in warm layer")
	}
	if n.ArchivedAt == "" {
		t.Error("archived note should have archived_at set")
	}
}

func TestNoteSymbolLink_AddListRemove(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	// Warm first so notes exist
	cmd := newKGWarmCmdForTest()
	_ = runKGWarm(cmd, nil)

	// Add a link
	addCmd := newKGLinkAddCmdForTest("mentions")
	if err := runKGLinkAdd(addCmd, []string{"dec-use-cobra", "commands::NewKGCmd"}); err != nil {
		t.Fatalf("runKGLinkAdd: %v", err)
	}

	// List links
	if err := runKGLinkList(nil, []string{"dec-use-cobra"}); err != nil {
		t.Fatalf("runKGLinkList: %v", err)
	}

	// Verify via store
	store, _ := openKGStore(home)
	defer store.Close()
	links, _ := store.GetLinksForNote("dec-use-cobra")
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].QualifiedName != "commands::NewKGCmd" {
		t.Errorf("unexpected qualified name: %s", links[0].QualifiedName)
	}
	if links[0].LinkKind != "mentions" {
		t.Errorf("unexpected link kind: %s", links[0].LinkKind)
	}

	// Remove the link
	linkID := fmt.Sprintf("%d", links[0].ID)
	removeCmd := newKGLinkRemoveCmdForTest()
	if err := runKGLinkRemove(removeCmd, []string{linkID}); err != nil {
		t.Fatalf("runKGLinkRemove: %v", err)
	}
	links2, _ := store.GetLinksForNote("dec-use-cobra")
	if len(links2) != 0 {
		t.Errorf("expected 0 links after remove, got %d", len(links2))
	}
}

func TestNoteSymbolLink_InvalidKind(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	addCmd := newKGLinkAddCmdForTest("bad-kind")
	err := runKGLinkAdd(addCmd, []string{"dec-use-cobra", "cmd::F"})
	if err == nil {
		t.Error("expected error for invalid link kind")
	}
}

func TestNoteSymbolLink_InvalidRemoveID(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	removeCmd := newKGLinkRemoveCmdForTest()
	err := runKGLinkRemove(removeCmd, []string{"not-a-number"})
	if err == nil {
		t.Error("expected error for non-integer link ID")
	}
}

func TestRunKGWarmStats(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	cmd := newKGWarmCmdForTest()
	_ = runKGWarm(cmd, nil)

	if err := runKGWarmStats(nil, nil); err != nil {
		t.Fatalf("runKGWarmStats: %v", err)
	}
}

// ── test helpers ──────────────────────────────────────────────────────────────

func newKGWarmCmdForTest() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "", "")
	return cmd
}

func newKGLinkAddCmdForTest(kind string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("kind", kind, "")
	return cmd
}

func newKGLinkRemoveCmdForTest() *cobra.Command {
	return &cobra.Command{}
}
