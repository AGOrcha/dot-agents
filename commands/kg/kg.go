package kg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// ── KG config ────────────────────────────────────────────────────────────────

// KGConfig is the schema for KG_HOME/self/config.yaml
type KGConfig struct {
	SchemaVersion   int      `json:"schema_version" yaml:"schema_version"`
	Name            string   `json:"name" yaml:"name"`
	Description     string   `json:"description" yaml:"description"`
	AdaptersEnabled []string `json:"adapters_enabled" yaml:"adapters_enabled"`
	CreatedAt       string   `json:"created_at" yaml:"created_at"`
	UpdatedAt       string   `json:"updated_at" yaml:"updated_at"`
}

func kgHome() string {
	if v := os.Getenv("KG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "knowledge-graph")
}

func kgConfigPath() string {
	return filepath.Join(kgHome(), "self", "config.yaml")
}

// ConfigPath returns the path to KG_HOME/self/config.yaml (used by other commands packages).
func ConfigPath() string {
	return kgConfigPath()
}

func loadKGConfig() (*KGConfig, error) {
	data, err := os.ReadFile(kgConfigPath())
	if err != nil {
		return nil, err
	}
	var cfg KGConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse kg config: %w", err)
	}
	return &cfg, nil
}

// SaveKGConfig writes cfg to KG_HOME/self/config.yaml.
func SaveKGConfig(cfg *KGConfig) error {
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	dir := filepath.Dir(kgConfigPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(kgConfigPath(), data, 0644)
}

// ── Graph note schema ─────────────────────────────────────────────────────────

// GraphNote represents the YAML frontmatter of a knowledge graph page.
type GraphNote struct {
	SchemaVersion int      `json:"schema_version" yaml:"schema_version"`
	ID            string   `json:"id" yaml:"id"`
	Type          string   `json:"type" yaml:"type"` // source|entity|concept|synthesis|decision|repo|session
	Title         string   `json:"title" yaml:"title"`
	Summary       string   `json:"summary" yaml:"summary"`
	Status        string   `json:"status" yaml:"status"` // draft|active|stale|superseded|archived
	SourceRefs    []string `json:"source_refs,omitempty" yaml:"source_refs,omitempty"`
	Links         []string `json:"links,omitempty" yaml:"links,omitempty"`
	CreatedAt     string   `json:"created_at" yaml:"created_at"`
	UpdatedAt     string   `json:"updated_at" yaml:"updated_at"`
	Confidence    string   `json:"confidence,omitempty" yaml:"confidence,omitempty"` // low|medium|high
	Version       int      `json:"version,omitempty" yaml:"version,omitempty"`       // reserved for LWW sync
}

var validNoteTypes = map[string]bool{
	"source": true, "entity": true, "concept": true,
	"synthesis": true, "decision": true, "repo": true, "session": true,
}

var validNoteStatuses = map[string]bool{
	"draft": true, "active": true, "stale": true, "superseded": true, "archived": true,
}

var validConfidenceLevels = map[string]bool{
	"low": true, "medium": true, "high": true,
}

const (
	kgIndexFileName = "index.md"
	kgLogFileName   = "log.md"
)

func isValidNoteType(t string) bool   { return validNoteTypes[t] }
func isValidNoteStatus(s string) bool { return validNoteStatuses[s] }
func isValidConfidence(c string) bool { return c == "" || validConfidenceLevels[c] }

// parseGraphNote splits YAML frontmatter from markdown body.
// Returns (note, body, error).
func parseGraphNote(content []byte) (*GraphNote, string, error) {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return nil, s, fmt.Errorf("no frontmatter found")
	}
	// Find closing ---
	rest := s[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, "", fmt.Errorf("unclosed frontmatter")
	}
	fmStr := rest[:idx]
	// rest[idx+4:] starts with \n (end of "---\n"), then optional blank separator
	body := strings.TrimPrefix(strings.TrimPrefix(rest[idx+4:], "\n"), "\n")
	var note GraphNote
	if err := yaml.Unmarshal([]byte(fmStr), &note); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return &note, body, nil
}

// renderGraphNote serializes note + body back to bytes with YAML frontmatter.
func renderGraphNote(note *GraphNote, body string) ([]byte, error) {
	fm, err := yaml.Marshal(note)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if body != "" {
		buf.WriteString("\n")
		buf.WriteString(body)
	}
	return buf.Bytes(), nil
}

// ── Index and log ─────────────────────────────────────────────────────────────

// IndexEntry is one record in notes/index.md
type IndexEntry struct {
	ID             string
	Type           string
	Title          string
	OneLineSummary string
	Path           string
}

func appendLogEntry(kgHomeDir, entry string) error {
	logPath := filepath.Join(kgHomeDir, "notes", kgLogFileName)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n## [%s] %s\n", time.Now().UTC().Format("2006-01-02"), entry)
	return err
}

func readLogEntries(kgHomeDir string, limit int) ([]string, error) {
	logPath := filepath.Join(kgHomeDir, "notes", kgLogFileName)
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var current strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			if current.Len() > 0 {
				entries = append(entries, strings.TrimSpace(current.String()))
				current.Reset()
			}
		}
		if current.Len() > 0 || strings.HasPrefix(line, "## ") {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	if current.Len() > 0 {
		entries = append(entries, strings.TrimSpace(current.String()))
	}
	if limit > 0 && len(entries) > limit {
		return entries[len(entries)-limit:], nil
	}
	return entries, nil
}

// updateIndex adds or replaces a note entry in notes/index.md.
func updateIndex(kgHomeDir string, note *GraphNote) error {
	indexPath := filepath.Join(kgHomeDir, "notes", kgIndexFileName)
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		data = []byte("# Knowledge Graph Index\n")
	} else if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	entryLine := buildIndexEntryLine(note)

	idPrefix := fmt.Sprintf("- [%s]", note.ID)
	if replaced := replaceIndexEntry(lines, idPrefix, entryLine); !replaced {
		lines = insertIndexEntry(lines, note.Type, entryLine)
	}
	return os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")), 0644)
}

// buildIndexEntryLine renders the markdown bullet entry for a note in
// notes/index.md, truncating overly long summaries.
func buildIndexEntryLine(note *GraphNote) string {
	notePath := filepath.Join("notes", noteSubdir(note.Type), note.ID+".md")
	summary := note.Summary
	if len(summary) > 80 {
		summary = summary[:77] + "..."
	}
	return fmt.Sprintf("- [%s](%s): %s — %s", note.ID, notePath, note.Title, summary)
}

// replaceIndexEntry mutates lines to replace any existing entry with the
// same id prefix; returns true when a replacement happened.
func replaceIndexEntry(lines []string, idPrefix, entryLine string) bool {
	for i, l := range lines {
		if strings.HasPrefix(l, idPrefix) {
			lines[i] = entryLine
			return true
		}
	}
	return false
}

// insertIndexEntry returns lines with entryLine inserted under the matching
// `## <type>s` section, creating the section at the end of the document
// when it does not exist.
func insertIndexEntry(lines []string, noteType, entryLine string) []string {
	sectionHeader := fmt.Sprintf("## %ss", noteType)
	sectionIdx := indexOfTrimmed(lines, sectionHeader)
	if sectionIdx < 0 {
		return append(lines, "", sectionHeader, entryLine)
	}
	insertAt := sectionIdx + 1
	for insertAt < len(lines) && lines[insertAt] != "" && !strings.HasPrefix(lines[insertAt], "## ") {
		insertAt++
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, entryLine)
	out = append(out, lines[insertAt:]...)
	return out
}

// indexOfTrimmed returns the index of the first line whose trimmed text
// equals target, or -1 when no match exists.
func indexOfTrimmed(lines []string, target string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) == target {
			return i
		}
	}
	return -1
}

// readIndex parses entries from notes/index.md.
func readIndex(kgHomeDir string) ([]IndexEntry, error) {
	indexPath := filepath.Join(kgHomeDir, "notes", kgIndexFileName)
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	var currentType string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			section := strings.TrimPrefix(line, "## ")
			// strip trailing 's' for type (e.g. "entities" -> "entit" — better to store as-is)
			currentType = strings.TrimSuffix(strings.ToLower(section), "s")
		}
		if strings.HasPrefix(line, "- [") {
			// Parse: - [id](path): title — summary
			e := parseIndexLine(line, currentType)
			if e != nil {
				entries = append(entries, *e)
			}
		}
	}
	return entries, nil
}

func parseIndexLine(line, noteType string) *IndexEntry {
	// Format: - [id](path): title — summary
	s := strings.TrimPrefix(line, "- [")
	idEnd := strings.Index(s, "]")
	if idEnd < 0 {
		return nil
	}
	id := s[:idEnd]
	rest := s[idEnd:]
	pathStart := strings.Index(rest, "(")
	pathEnd := strings.Index(rest, ")")
	if pathStart < 0 || pathEnd < 0 {
		return nil
	}
	path := rest[pathStart+1 : pathEnd]
	titleSummary := strings.TrimPrefix(rest[pathEnd+1:], ": ")
	parts := strings.SplitN(titleSummary, " — ", 2)
	title := strings.TrimSpace(parts[0])
	summary := ""
	if len(parts) == 2 {
		summary = strings.TrimSpace(parts[1])
	}
	return &IndexEntry{
		ID:             id,
		Type:           noteType,
		Title:          title,
		OneLineSummary: summary,
		Path:           path,
	}
}

// noteSubdir returns the notes subdirectory for a note type.
func noteSubdir(noteType string) string {
	m := map[string]string{
		"source":    "sources",
		"entity":    "entities",
		"concept":   "concepts",
		"synthesis": "synthesis",
		"decision":  "decisions",
		"repo":      "repos",
		"session":   "sessions",
	}
	if d, ok := m[noteType]; ok {
		return d
	}
	return noteType + "s"
}

// ── Graph health ──────────────────────────────────────────────────────────────

// GraphHealth is the schema for ops/health/graph-health.json
type GraphHealth struct {
	SchemaVersion      int      `json:"schema_version"`
	Timestamp          string   `json:"timestamp"`
	NoteCount          int      `json:"note_count"`
	SourceCount        int      `json:"source_count"`
	OrphanCount        int      `json:"orphan_count"`
	BrokenLinkCount    int      `json:"broken_link_count"`
	StaleCount         int      `json:"stale_count"`
	ContradictionCount int      `json:"contradiction_count"`
	QueueDepth         int      `json:"queue_depth"`
	Status             string   `json:"status"` // healthy|warn|error
	Warnings           []string `json:"warnings"`
}

func computeGraphHealth(kgHomeDir string) (GraphHealth, error) {
	h := GraphHealth{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	for _, sub := range []string{"sources", "entities", "concepts", "synthesis", "decisions", "repos", "sessions"} {
		if err := tallyGraphNoteDir(filepath.Join(kgHomeDir, "notes", sub), sub, &h); err != nil {
			return h, err
		}
	}
	h.QueueDepth = countQueueEntries(filepath.Join(kgHomeDir, "raw", "inbox"))
	deriveGraphHealthStatus(&h)
	return h, nil
}

// tallyGraphNoteDir counts notes (and stale notes) under a single notes/<sub>/
// directory, updating h in place. Missing directories are not an error.
func tallyGraphNoteDir(dir, sub string, h *GraphHealth) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		h.NoteCount++
		if sub == "sources" {
			h.SourceCount++
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		note, _, parseErr := parseGraphNote(data)
		if parseErr == nil && note.Status == "stale" {
			h.StaleCount++
		}
	}
	return nil
}

// countQueueEntries returns the number of non-directory entries in the raw
// inbox queue. Errors (including missing directory) are treated as zero.
func countQueueEntries(queueDir string) int {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

// deriveGraphHealthStatus sets h.Status to "healthy"/"warn" and appends the
// matching warnings based on the populated counts.
func deriveGraphHealthStatus(h *GraphHealth) {
	h.Status = "healthy"
	if h.OrphanCount > 0 {
		h.Warnings = append(h.Warnings, fmt.Sprintf("%d orphan notes detected", h.OrphanCount))
		h.Status = "warn"
	}
	if h.QueueDepth > 10 {
		h.Warnings = append(h.Warnings, fmt.Sprintf("inbox queue depth is %d", h.QueueDepth))
		if h.Status == "healthy" {
			h.Status = "warn"
		}
	}
}

func writeGraphHealth(kgHomeDir string, health GraphHealth) error {
	healthPath := filepath.Join(kgHomeDir, "ops", "health", "graph-health.json")
	if err := os.MkdirAll(filepath.Dir(healthPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(healthPath, data, 0644)
}

func readGraphHealth(kgHomeDir string) (*GraphHealth, error) {
	healthPath := filepath.Join(kgHomeDir, "ops", "health", "graph-health.json")
	data, err := os.ReadFile(healthPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h GraphHealth
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ── kg setup ──────────────────────────────────────────────────────────────────

func runKGSetup() error {
	home := kgHome()

	// Check if already initialized
	if _, err := os.Stat(kgConfigPath()); err == nil {
		cfg, _ := loadKGConfig()
		name := ""
		if cfg != nil {
			name = cfg.Name
		}
		ui.InfoBox("Knowledge graph already initialized",
			fmt.Sprintf("Graph home: %s", home),
			fmt.Sprintf("Name: %s", name),
		)
		ui.Info("Run 'dot-agents kg health' to check graph status.")
		return nil
	}

	// Create full directory tree
	dirs := []string{
		"self/schema",
		"self/prompts",
		"self/policies",
		"raw/inbox",
		"raw/imported",
		"raw/assets",
		"notes/sources",
		"notes/entities",
		"notes/concepts",
		"notes/synthesis",
		"notes/decisions",
		"notes/repos",
		"ops/queue",
		"ops/sessions",
		"ops/lint",
		"ops/adapters",
		"ops/health",
		"ops/integrity",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	// Write initial config
	cfg := &KGConfig{
		SchemaVersion:   1,
		Name:            filepath.Base(home),
		Description:     "Personal knowledge graph",
		AdaptersEnabled: []string{},
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveKGConfig(cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Write initial index
	indexPath := filepath.Join(home, "notes", kgIndexFileName)
	indexContent := "# Knowledge Graph Index\n\nThis file is maintained automatically by dot-agents kg.\n"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Write initial log
	logPath := filepath.Join(home, "notes", kgLogFileName)
	logContent := "# Knowledge Graph Operation Log\n\nAppend-only log of graph operations.\n"
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		return fmt.Errorf("write log: %w", err)
	}

	// Compute and write initial health
	health, err := computeGraphHealth(home)
	if err != nil {
		return fmt.Errorf("compute health: %w", err)
	}
	if err := writeGraphHealth(home, health); err != nil {
		return fmt.Errorf("write health: %w", err)
	}

	// Write bridge contract schema
	if err := writeBridgeContract(home); err != nil {
		return fmt.Errorf("write bridge contract: %w", err)
	}

	// Phase 6A: initialize empty integrity manifest
	emptyManifest := &IntegrityManifest{SchemaVersion: 1, Notes: map[string]IntegrityManifestEntry{}}
	if err := saveManifest(home, emptyManifest); err != nil {
		return fmt.Errorf("write integrity manifest: %w", err)
	}

	// Phase D: initialize warm-layer SQLite database
	warmStore, err := openKGStore(home)
	if err != nil {
		return fmt.Errorf("init warm store: %w", err)
	}
	warmStore.Close()

	// Append setup event to log
	if err := appendLogEntry(home, "setup | graph initialized"); err != nil {
		return fmt.Errorf("append log: %w", err)
	}

	ui.SuccessBox(
		fmt.Sprintf("Knowledge graph initialized at %s", home),
		"dot-agents kg health — check graph status",
		"dot-agents kg ingest <file> — ingest raw sources",
	)
	return nil
}

// ── kg health ─────────────────────────────────────────────────────────────────

func runKGHealth(deps Deps, cmd *cobra.Command) error {
	home := kgHome()

	// Verify initialized
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized at %s — run 'dot-agents kg setup' first", home)
	}

	health, err := computeGraphHealth(home)
	if err != nil {
		return fmt.Errorf("compute health: %w", err)
	}
	if err := writeGraphHealth(home, health); err != nil {
		return fmt.Errorf("write health: %w", err)
	}

	if commandJSON(cmd) {
		data, err := json.MarshalIndent(health, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	renderGraphHealthText(home, health)
	return nil
}

// graphHealthStatusBadge returns the colored status badge string for the
// current graph health, falling back to the raw status when unrecognized.
func graphHealthStatusBadge(status string) string {
	badges := map[string]string{
		"healthy": ui.ColorText(ui.Green, "healthy"),
		"warn":    ui.ColorText(ui.Yellow, "warn"),
		"error":   ui.ColorText(ui.Red, "error"),
	}
	if b, ok := badges[status]; ok {
		return b
	}
	return status
}

// renderGraphHealthText writes the human-readable graph health report
// (Notes/Queue/Warnings sections) to stdout.
func renderGraphHealthText(home string, health GraphHealth) {
	ui.Header(fmt.Sprintf("Knowledge Graph Health  [%s]", graphHealthStatusBadge(health.Status)))
	ui.Info(fmt.Sprintf("Graph home: %s", home))
	ui.Info(fmt.Sprintf("Timestamp:  %s", health.Timestamp))
	fmt.Println()

	ui.Section("Notes")
	ui.Bullet("found", fmt.Sprintf("Total notes: %d", health.NoteCount))
	ui.Bullet("found", fmt.Sprintf("Sources: %d", health.SourceCount))
	if health.StaleCount > 0 {
		ui.Bullet("warn", fmt.Sprintf("Stale: %d", health.StaleCount))
	}
	if health.OrphanCount > 0 {
		ui.Bullet("warn", fmt.Sprintf("Orphans: %d", health.OrphanCount))
	}
	fmt.Println()

	ui.Section("Queue")
	if health.QueueDepth == 0 {
		ui.Bullet("ok", "Inbox empty")
	} else {
		ui.Bullet("warn", fmt.Sprintf("Pending in inbox: %d", health.QueueDepth))
	}

	if len(health.Warnings) > 0 {
		fmt.Println()
		ui.Section("Warnings")
		for _, w := range health.Warnings {
			ui.Bullet("warn", w)
		}
	}
	fmt.Println()
}

func runKGServe(_ *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	srv := graphstore.NewMCPServer(workDir)
	return srv.Serve(os.Stdin, os.Stdout)
}

// walkNoteFiles calls fn for every .md file under kgHomeDir/notes/*/.
func walkNoteFiles(kgHomeDir string, fn func(path string, info fs.DirEntry) error) error {
	notesDir := filepath.Join(kgHomeDir, "notes")
	entries, err := os.ReadDir(notesDir)
	if err != nil {
		return err
	}
	for _, sub := range entries {
		if !sub.IsDir() {
			continue
		}
		if err := walkNoteFilesIn(filepath.Join(notesDir, sub.Name()), fn); err != nil {
			return err
		}
	}
	return nil
}

// walkNoteFilesIn invokes fn for each top-level .md note file under subDir,
// returning early on the first error returned by fn. Read errors on subDir
// itself are treated as "no notes" rather than fatal.
func walkNoteFilesIn(subDir string, fn func(path string, info fs.DirEntry) error) error {
	files, err := os.ReadDir(subDir)
	if err != nil {
		return nil
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		if err := fn(filepath.Join(subDir, f.Name()), f); err != nil {
			return err
		}
	}
	return nil
}

// ── Phase 2: Raw source recording ────────────────────────────────────────────

// RawSource is the frontmatter for files in raw/inbox/.
type RawSource struct {
	SchemaVersion int    `json:"schema_version" yaml:"schema_version"`
	ID            string `json:"id" yaml:"id"`
	Title         string `json:"title" yaml:"title"`
	SourceType    string `json:"source_type" yaml:"source_type"` // markdown|pdf|text|url|transcript|meeting_notes|repo_doc
	OriginalPath  string `json:"original_path,omitempty" yaml:"original_path,omitempty"`
	CapturedAt    string `json:"captured_at" yaml:"captured_at"`
	Status        string `json:"status" yaml:"status"` // pending|imported|skipped
	Summary       string `json:"summary,omitempty" yaml:"summary,omitempty"`
}

var validSourceTypes = map[string]bool{
	"markdown": true, "pdf": true, "text": true, "url": true,
	"transcript": true, "meeting_notes": true, "repo_doc": true,
}

func isValidSourceType(t string) bool { return validSourceTypes[t] }

// recordRawSource writes a raw source + its content to raw/inbox/<id>.md.
func recordRawSource(kgHomeDir string, source RawSource, content []byte) error {
	inboxDir := filepath.Join(kgHomeDir, "raw", "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		return err
	}
	fm, err := yaml.Marshal(source)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.Write(content)
	return os.WriteFile(filepath.Join(inboxDir, source.ID+".md"), buf.Bytes(), 0644)
}

// moveToImported moves a raw source from inbox to imported.
func moveToImported(kgHomeDir, sourceID string) error {
	src := filepath.Join(kgHomeDir, "raw", "inbox", sourceID+".md")
	dst := filepath.Join(kgHomeDir, "raw", "imported", sourceID+".md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// listPendingRawSources returns all sources in raw/inbox/.
func listPendingRawSources(kgHomeDir string) ([]RawSource, error) {
	inboxDir := filepath.Join(kgHomeDir, "raw", "inbox")
	entries, err := os.ReadDir(inboxDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sources []RawSource
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(inboxDir, e.Name()))
		if err != nil {
			continue
		}
		// Parse YAML frontmatter into RawSource
		s := string(data)
		if !strings.HasPrefix(s, "---") {
			continue
		}
		rest := s[3:]
		idx := strings.Index(rest, "\n---")
		if idx < 0 {
			continue
		}
		var src RawSource
		if err := yaml.Unmarshal([]byte(rest[:idx]), &src); err == nil {
			sources = append(sources, src)
		}
	}
	return sources, nil
}

// ── Phase 2: Extraction helpers ───────────────────────────────────────────────

// extractClaims returns key claims from markdown: headers, bold text, assertions in list items.
func extractClaims(content string) []string {
	var claims []string
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		claim := extractClaim(strings.TrimSpace(line))
		if claim == "" || seen[claim] {
			continue
		}
		seen[claim] = true
		claims = append(claims, claim)
	}
	return claims
}

// extractClaim returns the claim text for a single trimmed line, or "" when
// the line does not match any of the heading / bold / assertive-bullet
// patterns recognized by extractClaims.
func extractClaim(line string) string {
	if strings.HasPrefix(line, "#") {
		return strings.TrimSpace(strings.TrimLeft(line, "#"))
	}
	if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") && len(line) > 4 {
		return line[2 : len(line)-2]
	}
	if (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")) && len(line) > 8 {
		item := line[2:]
		if isAssertive(item) {
			return item
		}
	}
	return ""
}

func isAssertive(s string) bool {
	lower := strings.ToLower(s)
	for _, kw := range []string{"is ", "are ", "was ", "were ", "will ", "should ", "must ", "can ", "does "} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// extractEntities returns named entities: capitalized multi-word phrases and code identifiers.
func extractEntities(content string) []string {
	var entities []string
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		entities = appendBacktickEntities(entities, line, seen)
		entities = appendCapitalizedPhrases(entities, line, seen)
	}
	return entities
}

// appendBacktickEntities appends every code-fenced identifier on line to
// entities, skipping duplicates already in seen.
func appendBacktickEntities(entities []string, line string, seen map[string]bool) []string {
	parts := strings.Split(line, "`")
	for i := 1; i < len(parts); i += 2 {
		e := strings.TrimSpace(parts[i])
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		entities = append(entities, e)
	}
	return entities
}

// appendCapitalizedPhrases appends every adjacent pair of capitalized words
// (each at least 2 characters) found in line to entities, skipping
// duplicates already in seen.
func appendCapitalizedPhrases(entities []string, line string, seen map[string]bool) []string {
	words := strings.Fields(line)
	for i := 0; i+1 < len(words); i++ {
		w1 := cleanWord(words[i])
		w2 := cleanWord(words[i+1])
		if !isCapitalized(w1) || !isCapitalized(w2) || len(w1) <= 1 || len(w2) <= 1 {
			continue
		}
		phrase := w1 + " " + w2
		if seen[phrase] {
			continue
		}
		seen[phrase] = true
		entities = append(entities, phrase)
	}
	return entities
}

func cleanWord(w string) string {
	return strings.Trim(w, ".,;:!?()[]{}\"'")
}

func isCapitalized(w string) bool {
	if len(w) == 0 {
		return false
	}
	return w[0] >= 'A' && w[0] <= 'Z'
}

// extractDecisions returns decision-like statements.
func extractDecisions(content string) []string {
	var decisions []string
	seen := map[string]bool{}
	keywords := []string{"decided", "chose", "will use", "should use", "selected", "adopted", "rejected"}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) && !seen[line] {
				seen[line] = true
				decisions = append(decisions, line)
				break
			}
		}
	}
	return decisions
}

// ── Phase 2: Note creation and update ─────────────────────────────────────────

// noteExists checks whether a note with the given ID exists anywhere under notes/.
// Returns (exists, fullPath).
func noteExists(kgHomeDir, noteID string) (bool, string) {
	for subdir := range validNoteTypes {
		p := filepath.Join(kgHomeDir, "notes", noteSubdir(subdir), noteID+".md")
		if _, err := os.Stat(p); err == nil {
			return true, p
		}
	}
	return false, ""
}

// createGraphNote writes a new note file, updates index, log, and integrity manifest.
// Returns an error if a note with the same ID already exists.
func createGraphNote(kgHomeDir string, note *GraphNote, body string) error {
	if exists, _ := noteExists(kgHomeDir, note.ID); exists {
		return fmt.Errorf("note %s already exists; use updateGraphNote instead", note.ID)
	}
	note.Version = 0 // Phase 6B: initialize version counter
	dir := filepath.Join(kgHomeDir, "notes", noteSubdir(note.Type))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := renderGraphNote(note, body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, note.ID+".md"), data, 0644); err != nil {
		return err
	}
	if err := updateIndex(kgHomeDir, note); err != nil {
		return err
	}
	// Phase 6A: update integrity manifest after write
	_ = updateManifest(kgHomeDir, note.ID, body)
	return appendLogEntry(kgHomeDir, fmt.Sprintf("create | %s (%s)", note.ID, note.Type))
}

// updateGraphNote updates an existing note's frontmatter, replaces body, updates index/log, and integrity manifest.
func updateGraphNote(kgHomeDir string, note *GraphNote, body string) error {
	exists, path := noteExists(kgHomeDir, note.ID)
	if !exists {
		return fmt.Errorf("note %s not found", note.ID)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	oldNote, _, err := parseGraphNote(existing)
	if err != nil {
		return err
	}
	// Preserve created_at; increment version (Phase 6B); update updated_at
	note.CreatedAt = oldNote.CreatedAt
	note.Version = oldNote.Version + 1
	note.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := renderGraphNote(note, body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	if err := updateIndex(kgHomeDir, note); err != nil {
		return err
	}
	// Phase 6A: update integrity manifest after write
	_ = updateManifest(kgHomeDir, note.ID, body)
	return appendLogEntry(kgHomeDir, fmt.Sprintf("update | %s (%s)", note.ID, note.Type))
}

// ── Phase 2: Ingest pipeline ──────────────────────────────────────────────────

// IngestResult summarizes what happened during an ingest run.
type IngestResult struct {
	SourceID     string   `json:"source_id"`
	NotesCreated []string `json:"notes_created"`
	NotesUpdated []string `json:"notes_updated"`
	Warnings     []string `json:"warnings"`
	Errors       []string `json:"errors"`
}

// ingestSource processes one raw source from inbox: creates notes, updates index/log, moves to imported.
func ingestSource(kgHomeDir, sourceID string) (*IngestResult, error) {
	result := &IngestResult{SourceID: sourceID}

	data, err := os.ReadFile(filepath.Join(kgHomeDir, "raw", "inbox", sourceID+".md"))
	if err != nil {
		return nil, fmt.Errorf("read inbox source: %w", err)
	}
	src, rawBody := parseRawSourceFrontmatter(string(data), sourceID)

	now := time.Now().UTC().Format(time.RFC3339)
	srcNote := buildSourceNote(src, rawBody, now)
	if err := createGraphNote(kgHomeDir, srcNote, rawBody); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("source note: %v", err))
	} else {
		result.NotesCreated = append(result.NotesCreated, srcNote.ID)
	}

	ingestEntityNotes(kgHomeDir, src, srcNote, rawBody, now, result)
	ingestDecisionNotes(kgHomeDir, src, srcNote, rawBody, now, result)

	if err := moveToImported(kgHomeDir, sourceID); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("move to imported: %v", err))
	}
	if health, err := computeGraphHealth(kgHomeDir); err == nil {
		_ = writeGraphHealth(kgHomeDir, health)
	}
	return result, nil
}

// parseRawSourceFrontmatter extracts the YAML frontmatter and remaining body
// from a raw inbox source, applying default values when fields are absent.
func parseRawSourceFrontmatter(s, sourceID string) (RawSource, string) {
	var src RawSource
	var rawBody string
	if strings.HasPrefix(s, "---") {
		rest := s[3:]
		if idx := strings.Index(rest, "\n---"); idx >= 0 {
			_ = yaml.Unmarshal([]byte(rest[:idx]), &src)
			rawBody = strings.TrimPrefix(strings.TrimPrefix(rest[idx+4:], "\n"), "\n")
		}
	}
	if src.ID == "" {
		src.ID = sourceID
	}
	if src.Title == "" {
		src.Title = sourceID
	}
	if src.SourceType == "" {
		src.SourceType = "markdown"
	}
	return src, rawBody
}

// buildSourceNote constructs the source-summary GraphNote for an ingested
// raw source.
func buildSourceNote(src RawSource, rawBody, now string) *GraphNote {
	return &GraphNote{
		SchemaVersion: 1,
		ID:            "src-" + src.ID,
		Type:          "source",
		Title:         src.Title,
		Summary:       summarize(rawBody, 120),
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// ingestEntityNotes extracts up to five entity references from rawBody and
// creates a draft entity note for each new symbol.
func ingestEntityNotes(kgHomeDir string, src RawSource, srcNote *GraphNote, rawBody, now string, result *IngestResult) {
	for i, entity := range extractEntities(rawBody) {
		if i >= 5 { // cap to 5 entities per source to avoid noise
			break
		}
		entID := slugify("ent-" + entity)
		if exists, _ := noteExists(kgHomeDir, entID); exists {
			result.NotesUpdated = append(result.NotesUpdated, entID)
			continue
		}
		entNote := &GraphNote{
			SchemaVersion: 1,
			ID:            entID,
			Type:          "entity",
			Title:         entity,
			Summary:       fmt.Sprintf("Entity extracted from %s.", src.Title),
			Status:        "draft",
			SourceRefs:    []string{srcNote.ID},
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := createGraphNote(kgHomeDir, entNote, ""); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("entity %s: %v", entity, err))
			continue
		}
		result.NotesCreated = append(result.NotesCreated, entID)
	}
}

// ingestDecisionNotes extracts up to three decision-shaped sentences from
// rawBody and creates a draft decision note for each.
func ingestDecisionNotes(kgHomeDir string, src RawSource, srcNote *GraphNote, rawBody, now string, result *IngestResult) {
	for i, dec := range extractDecisions(rawBody) {
		if i >= 3 {
			break
		}
		decID := fmt.Sprintf("dec-%s-%d", src.ID, i+1)
		decNote := &GraphNote{
			SchemaVersion: 1,
			ID:            decID,
			Type:          "decision",
			Title:         truncate(dec, 60),
			Summary:       dec,
			Status:        "draft",
			SourceRefs:    []string{srcNote.ID},
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := createGraphNote(kgHomeDir, decNote, ""); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("decision: %v", err))
			continue
		}
		result.NotesCreated = append(result.NotesCreated, decID)
	}
}

// slugify converts a string to a lowercase, hyphen-separated identifier.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' {
			b.WriteRune('-')
		}
	}
	// Collapse consecutive hyphens
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

// summarize returns the first N chars of a string.
func summarize(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ── kg ingest subcommand ──────────────────────────────────────────────────────

func runKGIngest(deps Deps, cmd *cobra.Command, args []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	opts := readIngestFlags(deps, cmd)

	sourceIDs, done, err := resolveIngestSources(home, args, opts)
	if err != nil || done {
		return err
	}

	if opts.dryRun {
		ui.InfoBox("Dry run — would ingest", fmt.Sprintf("%d sources from inbox", len(sourceIDs)))
		return nil
	}

	for _, sid := range sourceIDs {
		runSingleIngest(deps, home, sid)
	}
	return nil
}

// kgIngestOptions captures the parsed flags for the kg ingest subcommand.
type kgIngestOptions struct {
	ingestAll   bool
	dryRun      bool
	sourceTitle string
	sourceType  string
}

// readIngestFlags pulls the ingest-related flags off cmd, applying defaults
// and merging the global dry-run flag with the local one.
func readIngestFlags(deps Deps, cmd *cobra.Command) kgIngestOptions {
	ingestAll, _ := cmd.Flags().GetBool("all")
	localDryRun, _ := cmd.Flags().GetBool("dry-run")
	sourceTitle, _ := cmd.Flags().GetString("title")
	sourceType, _ := cmd.Flags().GetString("type")
	if sourceType == "" {
		sourceType = "markdown"
	}
	return kgIngestOptions{
		ingestAll:   ingestAll,
		dryRun:      deps.Flags.DryRun || localDryRun,
		sourceTitle: sourceTitle,
		sourceType:  sourceType,
	}
}

// resolveIngestSources expands the user request into a slice of source IDs.
// It returns done=true when the command has already produced its output
// (inbox empty, or a single-file dry-run preview) and no further work
// should run.
func resolveIngestSources(home string, args []string, opts kgIngestOptions) ([]string, bool, error) {
	if opts.ingestAll {
		ids, done, err := resolveIngestAll(home)
		return ids, done, err
	}
	return resolveIngestSingle(home, args, opts)
}

// resolveIngestAll resolves the --all flow: gather every pending inbox source,
// returning done=true when the inbox is empty so the caller short-circuits.
func resolveIngestAll(home string) ([]string, bool, error) {
	pending, err := listPendingRawSources(home)
	if err != nil {
		return nil, false, fmt.Errorf("list inbox: %w", err)
	}
	ids := make([]string, 0, len(pending))
	for _, s := range pending {
		ids = append(ids, s.ID)
	}
	if len(ids) == 0 {
		ui.Info("Inbox is empty — nothing to ingest.")
		return nil, true, nil
	}
	return ids, false, nil
}

// resolveIngestSingle handles the single-file ingest flow: validate args,
// read the source, build the RawSource record, and either preview (dry-run)
// or record it for the downstream loop.
func resolveIngestSingle(home string, args []string, opts kgIngestOptions) ([]string, bool, error) {
	if len(args) == 0 {
		return nil, false, fmt.Errorf("provide a file path to ingest or use --all")
	}
	srcPath := args[0]
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, false, fmt.Errorf("read source file: %w", err)
	}
	srcID := slugify(filepath.Base(strings.TrimSuffix(srcPath, filepath.Ext(srcPath))))
	if srcID == "" {
		srcID = fmt.Sprintf("src-%d", time.Now().Unix())
	}
	title := opts.sourceTitle
	if title == "" {
		title = filepath.Base(srcPath)
	}
	raw := RawSource{
		SchemaVersion: 1,
		ID:            srcID,
		Title:         title,
		SourceType:    opts.sourceType,
		OriginalPath:  srcPath,
		CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		Status:        "pending",
	}
	if opts.dryRun {
		previewSingleIngest(srcID, title, opts.sourceType, srcData)
		return nil, true, nil
	}
	if err := recordRawSource(home, raw, srcData); err != nil {
		return nil, false, fmt.Errorf("record source: %w", err)
	}
	return []string{srcID}, false, nil
}

// previewSingleIngest prints the dry-run summary for a single-file ingest:
// the source identifiers plus extracted entity/decision counts.
func previewSingleIngest(srcID, title, sourceType string, srcData []byte) {
	ui.InfoBox("Dry run — would ingest",
		fmt.Sprintf("Source ID: %s", srcID),
		fmt.Sprintf("Title: %s", title),
		fmt.Sprintf("Type: %s", sourceType),
	)
	entities := extractEntities(string(srcData))
	decisions := extractDecisions(string(srcData))
	ui.Info(fmt.Sprintf("  Entities found: %d", len(entities)))
	ui.Info(fmt.Sprintf("  Decisions found: %d", len(decisions)))
}

// runSingleIngest ingests one source and renders the human-readable summary
// (or JSON, when requested) without short-circuiting the caller's loop on
// per-source errors.
func runSingleIngest(deps Deps, home, sid string) {
	result, err := ingestSource(home, sid)
	if err != nil {
		ui.Error(fmt.Sprintf("ingest %s: %v", sid, err))
		return
	}
	if deps.Flags.JSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}
	ui.Success(fmt.Sprintf("Ingested %s", sid))
	if len(result.NotesCreated) > 0 {
		ui.Info(fmt.Sprintf("  Notes created: %s", strings.Join(result.NotesCreated, ", ")))
	}
	if len(result.NotesUpdated) > 0 {
		ui.Info(fmt.Sprintf("  Notes updated: %s", strings.Join(result.NotesUpdated, ", ")))
	}
	for _, w := range result.Warnings {
		ui.Warn(w)
	}
}

// ── kg queue subcommand ───────────────────────────────────────────────────────

func runKGQueue(deps Deps) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	pending, err := listPendingRawSources(home)
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	if deps.Flags.JSON {
		data, _ := json.MarshalIndent(pending, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	ui.Header(fmt.Sprintf("Inbox Queue  [%d items]", len(pending)))
	if len(pending) == 0 {
		ui.Info("Inbox is empty.")
		return nil
	}
	for _, s := range pending {
		ui.Bullet("found", fmt.Sprintf("[%s] %s (%s)", s.ID, s.Title, s.SourceType))
	}
	return nil
}
