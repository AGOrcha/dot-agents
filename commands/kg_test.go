package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		Links: []string{"ent-linked"},
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

func TestExecuteQuery_Contradictions_Stub(t *testing.T) {
	home := setupKGWithNotes(t)

	resp, err := executeQuery(home, GraphQuery{Intent: "contradictions", Query: ""})
	if err != nil {
		t.Fatalf("executeQuery contradictions: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Error("expected stub warning for contradictions")
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
