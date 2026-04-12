package commands

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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

func saveKGConfig(cfg *KGConfig) error {
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

func appendLogEntry(kgHomeDir string, entry string) error {
	logPath := filepath.Join(kgHomeDir, "notes", "log.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n## [%s] %s\n", time.Now().UTC().Format("2006-01-02"), entry)
	return err
}

func readLogEntries(kgHomeDir string, limit int) ([]string, error) {
	logPath := filepath.Join(kgHomeDir, "notes", "log.md")
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
	indexPath := filepath.Join(kgHomeDir, "notes", "index.md")
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		data = []byte("# Knowledge Graph Index\n")
	} else if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	// Build the entry line
	notePath := filepath.Join("notes", noteSubdir(note.Type), note.ID+".md")
	summary := note.Summary
	if len(summary) > 80 {
		summary = summary[:77] + "..."
	}
	entryLine := fmt.Sprintf("- [%s](%s): %s — %s", note.ID, notePath, note.Title, summary)

	// Check if entry already exists and replace it; otherwise add under type section
	idPrefix := fmt.Sprintf("- [%s]", note.ID)
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, idPrefix) {
			lines[i] = entryLine
			found = true
			break
		}
	}

	if !found {
		// Find or create section header for the type
		sectionHeader := fmt.Sprintf("## %ss", note.Type)
		sectionIdx := -1
		for i, l := range lines {
			if strings.TrimSpace(l) == sectionHeader {
				sectionIdx = i
				break
			}
		}
		if sectionIdx < 0 {
			// Append new section
			lines = append(lines, "", sectionHeader, entryLine)
		} else {
			// Insert after header (and any existing entries before next blank/section)
			insertAt := sectionIdx + 1
			for insertAt < len(lines) && lines[insertAt] != "" && !strings.HasPrefix(lines[insertAt], "## ") {
				insertAt++
			}
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:insertAt]...)
			newLines = append(newLines, entryLine)
			newLines = append(newLines, lines[insertAt:]...)
			lines = newLines
		}
	}

	return os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")), 0644)
}

// readIndex parses entries from notes/index.md.
func readIndex(kgHomeDir string) ([]IndexEntry, error) {
	indexPath := filepath.Join(kgHomeDir, "notes", "index.md")
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

	// Count notes by walking notes/ subdirectories
	noteDirs := []string{"sources", "entities", "concepts", "synthesis", "decisions", "repos", "sessions"}
	for _, sub := range noteDirs {
		dir := filepath.Join(kgHomeDir, "notes", sub)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return h, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				h.NoteCount++
				if sub == "sources" {
					h.SourceCount++
				}
				// Check for stale status
				data, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err == nil {
					note, _, parseErr := parseGraphNote(data)
					if parseErr == nil && note.Status == "stale" {
						h.StaleCount++
					}
				}
			}
		}
	}

	// Count queue depth
	queueDir := filepath.Join(kgHomeDir, "raw", "inbox")
	queueEntries, err := os.ReadDir(queueDir)
	if err == nil {
		for _, e := range queueEntries {
			if !e.IsDir() {
				h.QueueDepth++
			}
		}
	}

	// Derive status
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

	return h, nil
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
	if err := saveKGConfig(cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Write initial index
	indexPath := filepath.Join(home, "notes", "index.md")
	indexContent := "# Knowledge Graph Index\n\nThis file is maintained automatically by dot-agents kg.\n"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Write initial log
	logPath := filepath.Join(home, "notes", "log.md")
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

func runKGHealth() error {
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

	if Flags.JSON {
		data, err := json.MarshalIndent(health, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	statusBadge := map[string]string{
		"healthy": ui.ColorText(ui.Green, "healthy"),
		"warn":    ui.ColorText(ui.Yellow, "warn"),
		"error":   ui.ColorText(ui.Red, "error"),
	}
	badge := statusBadge[health.Status]
	if badge == "" {
		badge = health.Status
	}

	ui.Header(fmt.Sprintf("Knowledge Graph Health  [%s]", badge))
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
	return nil
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
		subDir := filepath.Join(notesDir, sub.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				if err := fn(filepath.Join(subDir, f.Name()), f); err != nil {
					return err
				}
			}
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
func moveToImported(kgHomeDir string, sourceID string) error {
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
		line = strings.TrimSpace(line)
		var claim string
		switch {
		case strings.HasPrefix(line, "#"):
			claim = strings.TrimSpace(strings.TrimLeft(line, "#"))
		case strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") && len(line) > 4:
			claim = line[2 : len(line)-2]
		case (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")) && len(line) > 8:
			item := line[2:]
			if isAssertive(item) {
				claim = item
			}
		}
		if claim != "" && !seen[claim] {
			seen[claim] = true
			claims = append(claims, claim)
		}
	}
	return claims
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
		// Backtick code identifiers
		parts := strings.Split(line, "`")
		for i := 1; i < len(parts); i += 2 {
			if e := strings.TrimSpace(parts[i]); e != "" && !seen[e] {
				seen[e] = true
				entities = append(entities, e)
			}
		}
		// Capitalized multi-word phrases (2+ words, each capitalized)
		words := strings.Fields(line)
		for i := 0; i+1 < len(words); i++ {
			w1 := cleanWord(words[i])
			w2 := cleanWord(words[i+1])
			if isCapitalized(w1) && isCapitalized(w2) && len(w1) > 1 && len(w2) > 1 {
				phrase := w1 + " " + w2
				if !seen[phrase] {
					seen[phrase] = true
					entities = append(entities, phrase)
				}
			}
		}
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

	inboxPath := filepath.Join(kgHomeDir, "raw", "inbox", sourceID+".md")
	data, err := os.ReadFile(inboxPath)
	if err != nil {
		return nil, fmt.Errorf("read inbox source: %w", err)
	}

	// Parse source metadata from frontmatter
	s := string(data)
	var src RawSource
	var rawBody string
	if strings.HasPrefix(s, "---") {
		rest := s[3:]
		idx := strings.Index(rest, "\n---")
		if idx >= 0 {
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

	// Create source summary note
	now := time.Now().UTC().Format(time.RFC3339)
	srcNote := &GraphNote{
		SchemaVersion: 1,
		ID:            "src-" + src.ID,
		Type:          "source",
		Title:         src.Title,
		Summary:       summarize(rawBody, 120),
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := createGraphNote(kgHomeDir, srcNote, rawBody); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("source note: %v", err))
	} else {
		result.NotesCreated = append(result.NotesCreated, srcNote.ID)
	}

	// Extract and create entity notes
	entities := extractEntities(rawBody)
	for i, entity := range entities {
		if i >= 5 { // cap to 5 entities per source to avoid noise
			break
		}
		entID := slugify("ent-" + entity)
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
		if exists, _ := noteExists(kgHomeDir, entID); exists {
			result.NotesUpdated = append(result.NotesUpdated, entID)
		} else {
			if err := createGraphNote(kgHomeDir, entNote, ""); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("entity %s: %v", entity, err))
			} else {
				result.NotesCreated = append(result.NotesCreated, entID)
			}
		}
	}

	// Extract and create decision notes
	decisions := extractDecisions(rawBody)
	for i, dec := range decisions {
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
		} else {
			result.NotesCreated = append(result.NotesCreated, decID)
		}
	}

	// Move to imported
	if err := moveToImported(kgHomeDir, sourceID); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("move to imported: %v", err))
	}

	// Update health snapshot
	health, err := computeGraphHealth(kgHomeDir)
	if err == nil {
		_ = writeGraphHealth(kgHomeDir, health)
	}

	return result, nil
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

func runKGIngest(cmd *cobra.Command, args []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	ingestAll, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sourceTitle, _ := cmd.Flags().GetString("title")
	sourceType, _ := cmd.Flags().GetString("type")
	if sourceType == "" {
		sourceType = "markdown"
	}

	var sourceIDs []string

	if ingestAll {
		pending, err := listPendingRawSources(home)
		if err != nil {
			return fmt.Errorf("list inbox: %w", err)
		}
		for _, s := range pending {
			sourceIDs = append(sourceIDs, s.ID)
		}
		if len(sourceIDs) == 0 {
			ui.Info("Inbox is empty — nothing to ingest.")
			return nil
		}
	} else {
		if len(args) == 0 {
			return fmt.Errorf("provide a file path to ingest or use --all")
		}
		srcPath := args[0]
		srcData, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read source file: %w", err)
		}
		srcID := slugify(filepath.Base(strings.TrimSuffix(srcPath, filepath.Ext(srcPath))))
		if srcID == "" {
			srcID = fmt.Sprintf("src-%d", time.Now().Unix())
		}
		title := sourceTitle
		if title == "" {
			title = filepath.Base(srcPath)
		}
		raw := RawSource{
			SchemaVersion: 1,
			ID:            srcID,
			Title:         title,
			SourceType:    sourceType,
			OriginalPath:  srcPath,
			CapturedAt:    time.Now().UTC().Format(time.RFC3339),
			Status:        "pending",
		}
		if dryRun {
			ui.InfoBox("Dry run — would ingest", fmt.Sprintf("Source ID: %s", srcID), fmt.Sprintf("Title: %s", title), fmt.Sprintf("Type: %s", sourceType))
			entities := extractEntities(string(srcData))
			decisions := extractDecisions(string(srcData))
			ui.Info(fmt.Sprintf("  Entities found: %d", len(entities)))
			ui.Info(fmt.Sprintf("  Decisions found: %d", len(decisions)))
			return nil
		}
		if err := recordRawSource(home, raw, srcData); err != nil {
			return fmt.Errorf("record source: %w", err)
		}
		sourceIDs = []string{srcID}
	}

	if dryRun {
		ui.InfoBox("Dry run — would ingest", fmt.Sprintf("%d sources from inbox", len(sourceIDs)))
		return nil
	}

	for _, sid := range sourceIDs {
		result, err := ingestSource(home, sid)
		if err != nil {
			ui.Error(fmt.Sprintf("ingest %s: %v", sid, err))
			continue
		}
		if Flags.JSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			continue
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
	return nil
}

// ── kg queue subcommand ───────────────────────────────────────────────────────

func runKGQueue() error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	pending, err := listPendingRawSources(home)
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	if Flags.JSON {
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

// ── Phase 3: Query contract types ─────────────────────────────────────────────

// GraphQuery is the input to executeQuery.
type GraphQuery struct {
	Intent string `json:"intent"`
	Query  string `json:"query"`
	Scope  string `json:"scope,omitempty"`
	Limit  int    `json:"limit"`
}

// GraphQueryResult is one item in a query response.
type GraphQueryResult struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Path          string   `json:"path"`
	SourceRefs    []string `json:"source_refs,omitempty"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	FilePath      string   `json:"file_path,omitempty"`
	LineStart     int      `json:"line_start,omitempty"`
	LineEnd       int      `json:"line_end,omitempty"`
	Language      string   `json:"language,omitempty"`
	RiskScore     float64  `json:"risk_score,omitempty"`
	TestCoverage  string   `json:"test_coverage,omitempty"`
}

// GraphQueryResponse is the normalized response envelope.
type GraphQueryResponse struct {
	SchemaVersion int                `json:"schema_version"`
	Intent        string             `json:"intent"`
	Query         string             `json:"query"`
	Results       []GraphQueryResult `json:"results"`
	Warnings      []string           `json:"warnings"`
	Provider      string             `json:"provider"`
	Timestamp     string             `json:"timestamp"`
}

var validQueryIntents = map[string]bool{
	"source_lookup":    true,
	"entity_context":   true,
	"concept_context":  true,
	"decision_lookup":  true,
	"repo_context":     true,
	"synthesis_lookup": true,
	"related_notes":    true,
	"contradictions":   true,
	"graph_health":     true,
}

func isValidQueryIntent(intent string) bool { return validQueryIntents[intent] }

// ── Phase 3: Search engine ─────────────────────────────────────────────────────

// scoreMatch returns a relevance score for a note against a query string.
// Higher is better: 4=exact title, 3=title prefix, 2=title substring, 1=summary substring, 0=body substring, -1=no match.
func scoreMatch(note *GraphNote, body, query string) int {
	q := strings.ToLower(query)
	titleLower := strings.ToLower(note.Title)
	if titleLower == q {
		return 4
	}
	if strings.HasPrefix(titleLower, q) {
		return 3
	}
	if strings.Contains(titleLower, q) {
		return 2
	}
	if strings.Contains(strings.ToLower(note.Summary), q) {
		return 1
	}
	if strings.Contains(strings.ToLower(body), q) {
		return 0
	}
	return -1
}

// searchNotes searches notes in the given type subdirectory (or all subdirs if noteType=="").
func searchNotes(kgHomeDir, noteType, query string, limit int) ([]GraphQueryResult, error) {
	if limit <= 0 {
		limit = 10
	}
	type scoredResult struct {
		score  int
		result GraphQueryResult
	}
	var scored []scoredResult

	walkFn := func(path string, _ fs.DirEntry) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		note, body, err := parseGraphNote(data)
		if err != nil {
			return nil // skip unparseable files
		}
		s := scoreMatch(note, body, query)
		if s < 0 {
			return nil
		}
		relPath, _ := filepath.Rel(kgHomeDir, path)
		scored = append(scored, scoredResult{
			score: s,
			result: GraphQueryResult{
				ID:         note.ID,
				Type:       note.Type,
				Title:      note.Title,
				Summary:    note.Summary,
				Path:       relPath,
				SourceRefs: note.SourceRefs,
			},
		})
		return nil
	}

	if noteType != "" {
		subDir := filepath.Join(kgHomeDir, "notes", noteSubdir(noteType))
		entries, err := os.ReadDir(subDir)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				_ = walkFn(filepath.Join(subDir, e.Name()), e)
			}
		}
	} else {
		_ = walkNoteFiles(kgHomeDir, walkFn)
	}

	// Sort by score descending (simple selection — limit is small)
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	var results []GraphQueryResult
	for i, r := range scored {
		if i >= limit {
			break
		}
		results = append(results, r.result)
	}
	return results, nil
}

// searchByLinks returns notes linked from a given note's links field.
func searchByLinks(kgHomeDir, noteID string) ([]GraphQueryResult, error) {
	exists, path := noteExists(kgHomeDir, noteID)
	if !exists {
		return nil, fmt.Errorf("note %s not found", noteID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	note, _, err := parseGraphNote(data)
	if err != nil {
		return nil, err
	}
	var results []GraphQueryResult
	for _, linkedID := range note.Links {
		lExists, lPath := noteExists(kgHomeDir, linkedID)
		if !lExists {
			continue
		}
		lData, err := os.ReadFile(lPath)
		if err != nil {
			continue
		}
		lNote, _, err := parseGraphNote(lData)
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(kgHomeDir, lPath)
		results = append(results, GraphQueryResult{
			ID:         lNote.ID,
			Type:       lNote.Type,
			Title:      lNote.Title,
			Summary:    lNote.Summary,
			Path:       relPath,
			SourceRefs: lNote.SourceRefs,
		})
	}
	return results, nil
}

// ── Phase 3: Intent dispatch ──────────────────────────────────────────────────

// executeQuery dispatches a GraphQuery to the appropriate search function.
func executeQuery(kgHomeDir string, query GraphQuery) (GraphQueryResponse, error) {
	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        query.Intent,
		Query:         query.Query,
		Provider:      "local-index",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	if !isValidQueryIntent(query.Intent) {
		return resp, fmt.Errorf("unknown query intent %q — valid intents: %s",
			query.Intent, strings.Join(sortedKeys(validQueryIntents), ", "))
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	var err error
	switch query.Intent {
	case "source_lookup":
		resp.Results, err = searchNotes(kgHomeDir, "source", query.Query, limit)
	case "entity_context":
		resp.Results, err = searchNotes(kgHomeDir, "entity", query.Query, limit)
	case "concept_context":
		resp.Results, err = searchNotes(kgHomeDir, "concept", query.Query, limit)
	case "decision_lookup":
		resp.Results, err = searchNotes(kgHomeDir, "decision", query.Query, limit)
	case "repo_context":
		resp.Results, err = searchNotes(kgHomeDir, "repo", query.Query, limit)
	case "synthesis_lookup":
		resp.Results, err = searchNotes(kgHomeDir, "synthesis", query.Query, limit)
	case "related_notes":
		resp.Results, err = searchByLinks(kgHomeDir, query.Query)
	case "contradictions":
		resp.Results, err = findContradictions(kgHomeDir)
	case "graph_health":
		health, hErr := readGraphHealth(kgHomeDir)
		if hErr != nil {
			err = hErr
		} else if health != nil {
			resp.Results = []GraphQueryResult{{
				ID:      "graph-health",
				Type:    "health",
				Title:   "Graph Health",
				Summary: fmt.Sprintf("status=%s notes=%d queue=%d", health.Status, health.NoteCount, health.QueueDepth),
			}}
		}
	}

	if err != nil {
		return resp, err
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}

	// Log query event
	_ = appendLogEntry(kgHomeDir, fmt.Sprintf("query | %s: %s", query.Intent, query.Query))

	return resp, nil
}

// sortedKeys returns sorted keys from a bool map.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// executeBatchQuery runs multiple queries and returns all responses.
func executeBatchQuery(kgHomeDir string, queries []GraphQuery) ([]GraphQueryResponse, error) {
	responses := make([]GraphQueryResponse, 0, len(queries))
	for _, q := range queries {
		resp, err := executeQuery(kgHomeDir, q)
		if err != nil {
			resp.Warnings = append(resp.Warnings, err.Error())
		}
		responses = append(responses, resp)
	}
	return responses, nil
}

// ── kg query subcommand ───────────────────────────────────────────────────────

func runKGQuery(cmd *cobra.Command, args []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return fmt.Errorf("--intent is required (valid: %s)", strings.Join(sortedKeys(validQueryIntents), ", "))
	}
	limit, _ := cmd.Flags().GetInt("limit")
	scope, _ := cmd.Flags().GetString("scope")

	queryStr := ""
	if len(args) > 0 {
		queryStr = strings.Join(args, " ")
	}

	resp, err := executeQuery(home, GraphQuery{
		Intent: intent,
		Query:  queryStr,
		Scope:  scope,
		Limit:  limit,
	})
	if err != nil {
		return err
	}

	if Flags.JSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	ui.Header(fmt.Sprintf("Query: %s  [%s]", intent, queryStr))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, summarize(r.Summary, 60)))
			ui.Info(fmt.Sprintf("        id: %s  path: %s", r.ID, r.Path))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

// ── Phase 4: Link graph ────────────────────────────────────────────────────────

// buildLinkGraph walks all notes and returns:
//   - adjacency map: noteID -> []linked noteIDs
//   - note map: noteID -> *GraphNote
func buildLinkGraph(kgHomeDir string) (map[string][]string, map[string]*GraphNote, error) {
	adj := make(map[string][]string)
	notes := make(map[string]*GraphNote)

	err := walkNoteFiles(kgHomeDir, func(path string, _ fs.DirEntry) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		note, _, err := parseGraphNote(data)
		if err != nil {
			return nil
		}
		notes[note.ID] = note
		adj[note.ID] = note.Links
		return nil
	})
	return adj, notes, err
}

// ── Phase 4: Lint types and checks ────────────────────────────────────────────

// ── Phase 6A: Content hash manifest ──────────────────────────────────────────

// IntegrityManifestEntry holds the hash and timestamp for one note.
type IntegrityManifestEntry struct {
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updated_at"`
}

// IntegrityManifest maps note ID to its hash entry.
type IntegrityManifest struct {
	SchemaVersion int                               `json:"schema_version"`
	UpdatedAt     string                            `json:"updated_at"`
	Notes         map[string]IntegrityManifestEntry `json:"notes"`
}

func integrityManifestPath(kgHomeDir string) string {
	return filepath.Join(kgHomeDir, "ops", "integrity", "manifest.json")
}

func loadManifest(kgHomeDir string) (*IntegrityManifest, error) {
	data, err := os.ReadFile(integrityManifestPath(kgHomeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &IntegrityManifest{SchemaVersion: 1, Notes: map[string]IntegrityManifestEntry{}}, nil
		}
		return nil, err
	}
	var m IntegrityManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Notes == nil {
		m.Notes = map[string]IntegrityManifestEntry{}
	}
	return &m, nil
}

func saveManifest(kgHomeDir string, m *IntegrityManifest) error {
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	p := integrityManifestPath(kgHomeDir)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// noteBodyHash computes SHA-256 of just the note body (excludes frontmatter).
func noteBodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// updateManifest loads, sets the entry for noteID, and saves atomically.
func updateManifest(kgHomeDir, noteID, body string) error {
	m, err := loadManifest(kgHomeDir)
	if err != nil {
		return err
	}
	m.Notes[noteID] = IntegrityManifestEntry{
		Hash:      noteBodyHash(body),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return saveManifest(kgHomeDir, m)
}

// LintResult is one finding from a lint check.
type LintResult struct {
	Check    string `json:"check"`
	Severity string `json:"severity"` // error|warn|info
	Message  string `json:"message"`
	NoteID   string `json:"note_id,omitempty"`
	Path     string `json:"path,omitempty"`
}

func lintBrokenLinks(adj map[string][]string, notes map[string]*GraphNote) []LintResult {
	var results []LintResult
	for noteID, links := range adj {
		for _, linkedID := range links {
			if _, exists := notes[linkedID]; !exists {
				results = append(results, LintResult{
					Check:    "broken_links",
					Severity: "error",
					Message:  fmt.Sprintf("note %s links to %s which does not exist", noteID, linkedID),
					NoteID:   noteID,
				})
			}
		}
	}
	return results
}

func lintOrphanPages(adj map[string][]string, notes map[string]*GraphNote) []LintResult {
	// Build reverse link map: who points to this ID?
	inbound := make(map[string]int)
	for _, links := range adj {
		for _, linkedID := range links {
			inbound[linkedID]++
		}
	}
	var results []LintResult
	for id, note := range notes {
		if note.Type == "source" {
			continue // sources are entry points, never orphans
		}
		if inbound[id] == 0 && len(note.SourceRefs) == 0 {
			results = append(results, LintResult{
				Check:    "orphan_pages",
				Severity: "warn",
				Message:  fmt.Sprintf("note %s (%s) has no inbound links and no source_refs", id, note.Type),
				NoteID:   id,
			})
		}
	}
	return results
}

func lintMissingSourceRefs(notes map[string]*GraphNote) []LintResult {
	var results []LintResult
	for id, note := range notes {
		if note.Type == "source" || note.Type == "repo" || note.Type == "session" {
			continue
		}
		if len(note.SourceRefs) == 0 {
			results = append(results, LintResult{
				Check:    "missing_source_refs",
				Severity: "info",
				Message:  fmt.Sprintf("note %s (%s) has no source_refs", id, note.Type),
				NoteID:   id,
			})
		}
	}
	return results
}

func lintStalePages(notes map[string]*GraphNote, threshold time.Duration) []LintResult {
	cutoff := time.Now().UTC().Add(-threshold)
	var results []LintResult
	for id, note := range notes {
		if note.Status == "archived" || note.Status == "superseded" {
			continue
		}
		t, err := time.Parse(time.RFC3339, note.UpdatedAt)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			results = append(results, LintResult{
				Check:    "stale_pages",
				Severity: "warn",
				Message:  fmt.Sprintf("note %s not updated since %s", id, t.Format("2006-01-02")),
				NoteID:   id,
			})
		}
	}
	return results
}

func lintIndexDrift(kgHomeDir string, notes map[string]*GraphNote) []LintResult {
	indexed, err := readIndex(kgHomeDir)
	if err != nil {
		return nil
	}
	indexedIDs := make(map[string]bool, len(indexed))
	for _, e := range indexed {
		indexedIDs[e.ID] = true
	}
	var results []LintResult
	for id := range notes {
		if !indexedIDs[id] {
			results = append(results, LintResult{
				Check:    "index_drift",
				Severity: "warn",
				Message:  fmt.Sprintf("note %s exists on disk but is missing from index.md", id),
				NoteID:   id,
			})
		}
	}
	return results
}

func lintOversizePages(kgHomeDir string, notes map[string]*GraphNote, maxBytes int) []LintResult {
	var results []LintResult
	for id, note := range notes {
		subdir := noteSubdir(note.Type)
		path := filepath.Join(kgHomeDir, "notes", subdir, id+".md")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if int(info.Size()) > maxBytes {
			results = append(results, LintResult{
				Check:    "oversize_pages",
				Severity: "info",
				Message:  fmt.Sprintf("note %s is %d bytes (limit %d)", id, info.Size(), maxBytes),
				NoteID:   id,
				Path:     path,
			})
		}
	}
	return results
}

// lintContradictions groups decision notes by shared title keywords and flags pairs.
func lintContradictions(notes map[string]*GraphNote) []LintResult {
	type decisionNote struct {
		id    string
		title string
		words map[string]bool
	}
	var decisions []decisionNote
	for id, note := range notes {
		if note.Type != "decision" || note.Status != "active" {
			continue
		}
		words := make(map[string]bool)
		for _, w := range strings.Fields(strings.ToLower(note.Title)) {
			w = strings.Trim(w, ".,;:!?")
			if len(w) > 3 { // skip short stop-words
				words[w] = true
			}
		}
		decisions = append(decisions, decisionNote{id: id, title: note.Title, words: words})
	}

	var results []LintResult
	seen := make(map[string]bool)
	for i := 0; i < len(decisions); i++ {
		for j := i + 1; j < len(decisions); j++ {
			a, b := decisions[i], decisions[j]
			// Count shared keywords
			shared := 0
			for w := range a.words {
				if b.words[w] {
					shared++
				}
			}
			if shared >= 2 {
				key := a.id + "|" + b.id
				if !seen[key] {
					seen[key] = true
					results = append(results, LintResult{
						Check:    "contradictions",
						Severity: "warn",
						Message:  fmt.Sprintf("decisions %q and %q share keywords — potential conflict", a.title, b.title),
						NoteID:   a.id,
					})
				}
			}
		}
	}
	return results
}

// lintIntegrityViolations checks each note's body hash against ops/integrity/manifest.json.
// Notes edited outside of kg commands (direct filesystem writes) will have a mismatched hash.
// Notes not yet in the manifest are skipped (no hash on record → not a violation).
func lintIntegrityViolations(kgHomeDir string, notes map[string]*GraphNote) []LintResult {
	m, err := loadManifest(kgHomeDir)
	if err != nil {
		return nil // manifest unreadable → skip check
	}
	var results []LintResult
	for id, note := range notes {
		entry, ok := m.Notes[id]
		if !ok {
			continue // not yet in manifest, not a violation
		}
		subdir := noteSubdir(note.Type)
		data, err := os.ReadFile(filepath.Join(kgHomeDir, "notes", subdir, id+".md"))
		if err != nil {
			continue
		}
		_, body, err := parseGraphNote(data)
		if err != nil {
			continue
		}
		if noteBodyHash(body) != entry.Hash {
			results = append(results, LintResult{
				Check:    "integrity_violation",
				Severity: "warn",
				Message:  fmt.Sprintf("note %s was modified outside of kg commands (hash mismatch)", id),
				NoteID:   id,
				Path:     filepath.Join(kgHomeDir, "notes", subdir, id+".md"),
			})
		}
	}
	return results
}

// ── Phase 4: Aggregate lint runner ────────────────────────────────────────────

// LintReport is the full output of a lint run.
type LintReport struct {
	Timestamp  string       `json:"timestamp"`
	ChecksRun  int          `json:"checks_run"`
	Results    []LintResult `json:"results"`
	ErrorCount int          `json:"error_count"`
	WarnCount  int          `json:"warn_count"`
	InfoCount  int          `json:"info_count"`
}

const defaultStaleThreshold = 90 * 24 * time.Hour
const defaultMaxNoteBytes = 50 * 1024 // 50 KB

func runGraphLint(kgHomeDir string) (*LintReport, error) {
	report := &LintReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Results:   []LintResult{},
	}

	adj, notes, err := buildLinkGraph(kgHomeDir)
	if err != nil {
		return nil, fmt.Errorf("build link graph: %w", err)
	}

	checks := [][]LintResult{
		lintBrokenLinks(adj, notes),
		lintOrphanPages(adj, notes),
		lintMissingSourceRefs(notes),
		lintStalePages(notes, defaultStaleThreshold),
		lintIndexDrift(kgHomeDir, notes),
		lintOversizePages(kgHomeDir, notes, defaultMaxNoteBytes),
		lintContradictions(notes),
		lintIntegrityViolations(kgHomeDir, notes), // Phase 6A
	}
	report.ChecksRun = len(checks)

	for _, batch := range checks {
		report.Results = append(report.Results, batch...)
	}
	for _, r := range report.Results {
		switch r.Severity {
		case "error":
			report.ErrorCount++
		case "warn":
			report.WarnCount++
		default:
			report.InfoCount++
		}
	}

	// Write report
	reportPath := filepath.Join(kgHomeDir, "ops", "lint", "lint-report.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err == nil {
		if data, err := json.MarshalIndent(report, "", "  "); err == nil {
			_ = os.WriteFile(reportPath, data, 0644)
		}
	}

	// Append log entry
	_ = appendLogEntry(kgHomeDir, fmt.Sprintf("lint | %d errors, %d warnings", report.ErrorCount, report.WarnCount))

	// Update health with lint-derived metrics
	health, err := computeGraphHealth(kgHomeDir)
	if err == nil {
		// Count broken links and orphans from lint
		for _, r := range report.Results {
			switch r.Check {
			case "broken_links":
				health.BrokenLinkCount++
			case "orphan_pages":
				health.OrphanCount++
			case "contradictions":
				health.ContradictionCount++
			}
		}
		if health.BrokenLinkCount > 0 {
			health.Status = "error"
			health.Warnings = append(health.Warnings, fmt.Sprintf("%d broken links detected", health.BrokenLinkCount))
		} else if report.WarnCount > 0 && health.Status == "healthy" {
			health.Status = "warn"
		}
		_ = writeGraphHealth(kgHomeDir, health)
	}

	return report, nil
}

// findContradictions returns LintResults for contradiction detection (used by query intent).
func findContradictions(kgHomeDir string) ([]GraphQueryResult, error) {
	_, notes, err := buildLinkGraph(kgHomeDir)
	if err != nil {
		return nil, err
	}
	lintResults := lintContradictions(notes)
	var results []GraphQueryResult
	for _, lr := range lintResults {
		results = append(results, GraphQueryResult{
			ID:      lr.NoteID,
			Type:    "decision",
			Title:   lr.Message,
			Summary: lr.Message,
		})
	}
	return results, nil
}

// ── kg lint subcommand ────────────────────────────────────────────────────────

func runKGLint(cmd *cobra.Command, _ []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}

	checkFilter, _ := cmd.Flags().GetString("check")

	report, err := runGraphLint(home)
	if err != nil {
		return err
	}

	// Apply single-check filter
	if checkFilter != "" {
		var filtered []LintResult
		for _, r := range report.Results {
			if r.Check == checkFilter {
				filtered = append(filtered, r)
			}
		}
		report.Results = filtered
	}

	if Flags.JSON {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
		if report.ErrorCount > 0 {
			os.Exit(1)
		}
		return nil
	}

	badge := ui.ColorText(ui.Green, "ok")
	if report.ErrorCount > 0 {
		badge = ui.ColorText(ui.Red, "errors")
	} else if report.WarnCount > 0 {
		badge = ui.ColorText(ui.Yellow, "warnings")
	}
	ui.Header(fmt.Sprintf("Graph Lint  [%s]", badge))
	ui.Info(fmt.Sprintf("%d errors  %d warnings  %d info", report.ErrorCount, report.WarnCount, report.InfoCount))
	fmt.Println()

	if len(report.Results) == 0 {
		ui.Success("No issues found.")
		return nil
	}

	// Group by severity
	for _, sev := range []string{"error", "warn", "info"} {
		for _, r := range report.Results {
			if r.Severity != sev {
				continue
			}
			icon := map[string]string{"error": "error", "warn": "warn", "info": "found"}[sev]
			ui.Bullet(icon, fmt.Sprintf("[%s] %s", r.Check, r.Message))
		}
	}
	fmt.Println()

	if report.ErrorCount > 0 {
		return fmt.Errorf("lint found %d errors", report.ErrorCount)
	}
	return nil
}

// ── Phase 4: Maintenance operations ──────────────────────────────────────────

func runKGReweave(kgHomeDir string) error {
	adj, notes, err := buildLinkGraph(kgHomeDir)
	if err != nil {
		return err
	}

	removed, added := 0, 0

	for id, note := range notes {
		changed := false
		var validLinks []string

		// Remove broken links
		for _, linkedID := range adj[id] {
			if _, exists := notes[linkedID]; exists {
				validLinks = append(validLinks, linkedID)
			} else {
				removed++
				changed = true
			}
		}

		// Add links for IDs mentioned in source_refs that aren't already linked
		for _, refID := range note.SourceRefs {
			if _, exists := notes[refID]; !exists {
				continue
			}
			alreadyLinked := false
			for _, l := range validLinks {
				if l == refID {
					alreadyLinked = true
					break
				}
			}
			if !alreadyLinked {
				validLinks = append(validLinks, refID)
				added++
				changed = true
			}
		}

		if changed {
			note.Links = validLinks
			note.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := updateGraphNote(kgHomeDir, note, ""); err != nil {
				// Read existing body and re-write with repaired links
				path := filepath.Join(kgHomeDir, "notes", noteSubdir(note.Type), id+".md")
				existing, readErr := os.ReadFile(path)
				if readErr == nil {
					_, body, parseErr := parseGraphNote(existing)
					if parseErr == nil {
						_ = updateGraphNote(kgHomeDir, note, body)
					}
				}
			}
		}
	}

	ui.Success(fmt.Sprintf("Reweave complete: %d broken links removed, %d source_ref links added", removed, added))
	return nil
}

func runKGMarkStale(kgHomeDir string, threshold time.Duration) error {
	_, notes, err := buildLinkGraph(kgHomeDir)
	if err != nil {
		return err
	}

	cutoff := time.Now().UTC().Add(-threshold)
	count := 0
	for id, note := range notes {
		if note.Status == "archived" || note.Status == "superseded" || note.Status == "stale" {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339, note.UpdatedAt)
		if parseErr != nil {
			continue
		}
		if t.Before(cutoff) {
			note.Status = "stale"
			note.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			path := filepath.Join(kgHomeDir, "notes", noteSubdir(note.Type), id+".md")
			existing, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			_, body, parseErr := parseGraphNote(existing)
			if parseErr != nil {
				continue
			}
			if err := updateGraphNote(kgHomeDir, note, body); err == nil {
				count++
			}
		}
	}
	ui.Success(fmt.Sprintf("Marked %d notes as stale", count))
	return nil
}

func runKGCompact(kgHomeDir string) error {
	archiveDir := filepath.Join(kgHomeDir, "notes", "_archived")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	_, notes, err := buildLinkGraph(kgHomeDir)
	if err != nil {
		return err
	}

	count := 0
	for id, note := range notes {
		if note.Status != "archived" && note.Status != "superseded" {
			continue
		}
		src := filepath.Join(kgHomeDir, "notes", noteSubdir(note.Type), id+".md")
		dst := filepath.Join(archiveDir, id+".md")
		if err := os.Rename(src, dst); err != nil {
			continue
		}
		count++
		// Remove from index
		indexPath := filepath.Join(kgHomeDir, "notes", "index.md")
		data, readErr := os.ReadFile(indexPath)
		if readErr == nil {
			lines := strings.Split(string(data), "\n")
			idPrefix := fmt.Sprintf("- [%s]", id)
			var kept []string
			for _, l := range lines {
				if !strings.HasPrefix(l, idPrefix) {
					kept = append(kept, l)
				}
			}
			_ = os.WriteFile(indexPath, []byte(strings.Join(kept, "\n")), 0644)
		}
	}
	_ = appendLogEntry(kgHomeDir, fmt.Sprintf("compact | archived %d notes", count))
	ui.Success(fmt.Sprintf("Compacted %d notes to %s", count, archiveDir))
	return nil
}

// ── Phase 5: Bridge intent mapping ────────────────────────────────────────────

// BridgeIntentMapping maps one bridge intent to one or more KG query intents.
type BridgeIntentMapping struct {
	BridgeIntent string   `json:"bridge_intent" yaml:"bridge_intent"`
	KGIntents    []string `json:"kg_intents" yaml:"kg_intents"`
}

func defaultBridgeMappings() []BridgeIntentMapping {
	return []BridgeIntentMapping{
		{BridgeIntent: "plan_context", KGIntents: []string{"decision_lookup", "synthesis_lookup"}},
		{BridgeIntent: "decision_lookup", KGIntents: []string{"decision_lookup"}},
		{BridgeIntent: "entity_context", KGIntents: []string{"entity_context"}},
		{BridgeIntent: "workflow_memory", KGIntents: []string{"related_notes", "source_lookup"}},
		{BridgeIntent: "contradictions", KGIntents: []string{"contradictions"}},
		{BridgeIntent: "symbol_lookup", KGIntents: []string{"symbol_lookup"}},
		{BridgeIntent: "impact_radius", KGIntents: []string{"impact_radius"}},
		{BridgeIntent: "change_analysis", KGIntents: []string{"change_analysis"}},
		{BridgeIntent: "tests_for", KGIntents: []string{"tests_for"}},
		{BridgeIntent: "callers_of", KGIntents: []string{"callers_of"}},
		{BridgeIntent: "callees_of", KGIntents: []string{"callees_of"}},
		{BridgeIntent: "community_context", KGIntents: []string{"community_context"}},
		{BridgeIntent: "symbol_decisions", KGIntents: []string{"symbol_decisions"}},
		{BridgeIntent: "decision_symbols", KGIntents: []string{"decision_symbols"}},
	}
}

var validBridgeIntents = func() map[string]bool {
	m := make(map[string]bool)
	for _, bm := range defaultBridgeMappings() {
		m[bm.BridgeIntent] = true
	}
	return m
}()

func isValidBridgeIntent(intent string) bool { return validBridgeIntents[intent] }

var codeBridgeIntents = map[string]bool{
	"symbol_lookup":     true,
	"impact_radius":     true,
	"change_analysis":   true,
	"tests_for":         true,
	"callers_of":        true,
	"callees_of":        true,
	"community_context": true,
	"symbol_decisions":  true,
	"decision_symbols":  true,
}

func isCodeBridgeIntent(intent string) bool { return codeBridgeIntents[intent] }

// resolveBridgeQuery fans a bridge intent out to KG queries.
func resolveBridgeQuery(bridgeIntent, query string) ([]GraphQuery, error) {
	for _, bm := range defaultBridgeMappings() {
		if bm.BridgeIntent == bridgeIntent {
			queries := make([]GraphQuery, 0, len(bm.KGIntents))
			for _, kgIntent := range bm.KGIntents {
				queries = append(queries, GraphQuery{
					Intent: kgIntent,
					Query:  query,
					Limit:  10,
				})
			}
			return queries, nil
		}
	}
	return nil, fmt.Errorf("unknown bridge intent %q", bridgeIntent)
}

// mergeBridgeResults merges multiple KG responses into one, deduplicating by note ID.
func mergeBridgeResults(responses []GraphQueryResponse, bridgeIntent string) GraphQueryResponse {
	merged := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        bridgeIntent,
		Provider:      "local-index",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Results:       []GraphQueryResult{},
	}
	seen := make(map[string]bool)
	for _, resp := range responses {
		merged.Query = resp.Query
		for _, r := range resp.Results {
			if !seen[r.ID] {
				seen[r.ID] = true
				merged.Results = append(merged.Results, r)
			}
		}
		merged.Warnings = append(merged.Warnings, resp.Warnings...)
	}
	return merged
}

func graphNodeToQueryResult(node graphstore.GraphNode, resultType string) GraphQueryResult {
	summary := node.FilePath
	if summary == "" {
		summary = node.QualifiedName
	}
	if node.LineStart > 0 {
		summary = fmt.Sprintf("%s:%d", summary, node.LineStart)
		if node.LineEnd > node.LineStart {
			summary = fmt.Sprintf("%s-%d", summary, node.LineEnd)
		}
	}
	return GraphQueryResult{
		ID:            node.QualifiedName,
		Type:          resultType,
		Title:         node.QualifiedName,
		Summary:       summary,
		Path:          node.FilePath,
		QualifiedName: node.QualifiedName,
		Kind:          node.Kind,
		FilePath:      node.FilePath,
		LineStart:     node.LineStart,
		LineEnd:       node.LineEnd,
		Language:      node.Language,
	}
}

func graphNodeTypeLabel(node graphstore.GraphNode) string {
	if node.Kind != "" {
		return strings.ToLower(node.Kind)
	}
	if node.IsTest {
		return "test"
	}
	return "symbol"
}

func findCodeNodes(store *graphstore.SQLiteStore, query string, limit int) ([]graphstore.GraphNode, error) {
	if limit <= 0 {
		limit = 10
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	nodes := make([]graphstore.GraphNode, 0, limit)
	addNode := func(node *graphstore.GraphNode) {
		if node == nil || node.QualifiedName == "" || seen[node.QualifiedName] {
			return
		}
		seen[node.QualifiedName] = true
		nodes = append(nodes, *node)
	}

	if node, err := store.GetNode(q); err == nil && node != nil {
		addNode(node)
	}
	if fileNodes, err := store.GetNodesByFile(q); err == nil {
		for i := range fileNodes {
			addNode(&fileNodes[i])
		}
	}
	if len(nodes) < limit {
		matches, err := store.SearchNodes(q, limit)
		if err != nil {
			return nil, err
		}
		for i := range matches {
			addNode(&matches[i])
		}
	}
	return nodes, nil
}

func collectGraphResults(nodes []graphstore.GraphNode, resultType string, limit int) []GraphQueryResult {
	results := make([]GraphQueryResult, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		id := node.QualifiedName
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		nodeType := resultType
		if nodeType == "" {
			nodeType = graphNodeTypeLabel(node)
		}
		results = append(results, graphNodeToQueryResult(node, nodeType))
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func collectNeighborResults(store *graphstore.SQLiteStore, nodes []graphstore.GraphNode, edgeKind string, inbound bool, limit int) ([]GraphQueryResult, error) {
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, node := range nodes {
		var edges []graphstore.GraphEdge
		var err error
		if inbound {
			edges, err = store.GetEdgesByTarget(node.QualifiedName)
		} else {
			edges, err = store.GetEdgesBySource(node.QualifiedName)
		}
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edgeKind != "" && edge.Kind != edgeKind {
				continue
			}
			neighborQN := edge.SourceQualified
			if !inbound {
				neighborQN = edge.TargetQualified
			}
			if seen[neighborQN] {
				continue
			}
			neighbor, err := store.GetNode(neighborQN)
			if err != nil || neighbor == nil {
				continue
			}
			if edgeKind == graphstore.EdgeKindTestedBy && !neighbor.IsTest && neighbor.Kind != graphstore.NodeKindTest {
				continue
			}
			seen[neighborQN] = true
			results = append(results, graphNodeToQueryResult(*neighbor, graphNodeTypeLabel(*neighbor)))
			if limit > 0 && len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

func collectSymbolDecisionResults(store *graphstore.SQLiteStore, nodes []graphstore.GraphNode, limit int) ([]GraphQueryResult, error) {
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, node := range nodes {
		links, err := store.GetLinksForSymbol(node.QualifiedName)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if seen[link.NoteID] {
				continue
			}
			note, err := store.GetKGNote(link.NoteID)
			if err != nil || note == nil {
				continue
			}
			if note.NoteType != "decision" && note.NoteType != "synthesis" && note.NoteType != "concept" {
				continue
			}
			seen[link.NoteID] = true
			results = append(results, GraphQueryResult{
				ID:         note.ID,
				Type:       note.NoteType,
				Title:      note.Title,
				Summary:    note.Summary,
				Path:       note.FilePath,
				SourceRefs: []string{node.QualifiedName},
			})
			if limit > 0 && len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

func collectDecisionSymbolResults(store *graphstore.SQLiteStore, query string, limit int) ([]GraphQueryResult, error) {
	var candidates []graphstore.KGNote
	if note, err := store.GetKGNote(strings.TrimSpace(query)); err == nil && note != nil {
		if note.NoteType == "decision" || note.NoteType == "synthesis" || note.NoteType == "concept" {
			candidates = append(candidates, *note)
		}
	}
	if len(candidates) == 0 {
		notes, err := store.SearchKGNotes(query, limit)
		if err != nil {
			return nil, err
		}
		for _, note := range notes {
			if note.NoteType == "decision" || note.NoteType == "synthesis" || note.NoteType == "concept" {
				candidates = append(candidates, note)
			}
		}
	}
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, note := range candidates {
		links, err := store.GetLinksForNote(note.ID)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if seen[link.QualifiedName] {
				continue
			}
			node, err := store.GetNode(link.QualifiedName)
			if err != nil || node == nil {
				results = append(results, GraphQueryResult{
					ID:      link.QualifiedName,
					Type:    "symbol",
					Title:   link.QualifiedName,
					Summary: fmt.Sprintf("%s via %s", note.Title, link.LinkKind),
				})
			} else {
				results = append(results, graphNodeToQueryResult(*node, graphNodeTypeLabel(*node)))
				results[len(results)-1].Summary = fmt.Sprintf("%s via %s", note.Title, link.LinkKind)
			}
			seen[link.QualifiedName] = true
			if limit > 0 && len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

func collectChangeAnalysisResults(query string, limit int) (GraphQueryResponse, error) {
	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        "change_analysis",
		Query:         query,
		Provider:      "crg",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	root := crgRepoRoot()
	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		resp.Provider = "crg-unavailable"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, nil
	}
	report, err := bridge.DetectChanges(graphstore.DetectChangesOptions{})
	if err != nil {
		return resp, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	matches := func(texts ...string) bool {
		if q == "" {
			return true
		}
		for _, text := range texts {
			if strings.Contains(strings.ToLower(text), q) {
				return true
			}
		}
		return false
	}
	for _, fn := range report.ChangedFunctions {
		if !matches(fn.Name, fn.QualifiedName, fn.FilePath) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            fn.QualifiedName,
			Type:          "changed_function",
			Title:         fn.QualifiedName,
			Summary:       fn.FilePath,
			Path:          fn.FilePath,
			QualifiedName: fn.QualifiedName,
			FilePath:      fn.FilePath,
			RiskScore:     fn.RiskScore,
		})
		if limit > 0 && len(resp.Results) >= limit {
			return resp, nil
		}
	}
	for _, gap := range report.TestGaps {
		if !matches(gap.QualifiedName, gap.FilePath) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            gap.QualifiedName,
			Type:          "test_gap",
			Title:         gap.QualifiedName,
			Summary:       gap.FilePath,
			Path:          gap.FilePath,
			QualifiedName: gap.QualifiedName,
			FilePath:      gap.FilePath,
			TestCoverage:  "missing",
		})
		if limit > 0 && len(resp.Results) >= limit {
			return resp, nil
		}
	}
	for _, priority := range report.ReviewPriorities {
		if !matches(priority.QualifiedName, priority.Reason) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            priority.QualifiedName,
			Type:          "review_priority",
			Title:         priority.QualifiedName,
			Summary:       priority.Reason,
			QualifiedName: priority.QualifiedName,
			RiskScore:     priority.RiskScore,
		})
		if limit > 0 && len(resp.Results) >= limit {
			return resp, nil
		}
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	return resp, nil
}

func collectCommunityContextResults(query string, limit int) (GraphQueryResponse, error) {
	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        "community_context",
		Query:         query,
		Provider:      "crg",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	root := crgRepoRoot()
	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		resp.Provider = "crg-unavailable"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, nil
	}
	communities, err := bridge.ListCommunities(0, "size")
	if err != nil {
		return resp, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	matches := func(texts ...string) bool {
		if q == "" {
			return true
		}
		for _, text := range texts {
			if strings.Contains(strings.ToLower(text), q) {
				return true
			}
		}
		return false
	}
	for _, community := range communities.Communities {
		if !matches(community.Name, community.Description, strings.Join(community.Members, " ")) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:         fmt.Sprintf("community:%d", community.ID),
			Type:       "community",
			Title:      community.Name,
			Summary:    fmt.Sprintf("size=%d cohesion=%.2f %s", community.Size, community.Cohesion, community.Description),
			SourceRefs: community.Members,
		})
		if limit > 0 && len(resp.Results) >= limit {
			break
		}
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	return resp, nil
}

func collectCodeBridgeResults(kgHomeDir, bridgeIntent, query string, limit int) (GraphQueryResponse, error) {
	if bridgeIntent == "change_analysis" {
		return collectChangeAnalysisResults(query, limit)
	}
	if bridgeIntent == "community_context" {
		return collectCommunityContextResults(query, limit)
	}
	store, err := openKGStore(kgHomeDir)
	if err != nil {
		return GraphQueryResponse{}, fmt.Errorf("open graph store: %w", err)
	}
	defer store.Close()

	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        bridgeIntent,
		Query:         query,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Provider:      "warm-graphstore",
	}
	switch bridgeIntent {
	case "symbol_lookup":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		resp.Results = collectGraphResults(nodes, "", limit)
	case "impact_radius":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		if len(nodes) == 0 {
			resp.Warnings = append(resp.Warnings, "no matching code symbols found")
			break
		}
		files := make([]string, 0, len(nodes))
		seen := make(map[string]bool)
		for _, node := range nodes {
			if node.FilePath == "" || seen[node.FilePath] {
				continue
			}
			seen[node.FilePath] = true
			files = append(files, node.FilePath)
		}
		impact, err := store.GetImpactRadius(files, 2, limit)
		if err != nil {
			return resp, err
		}
		resp.Results = collectGraphResults(impact.ChangedNodes, "changed_symbol", limit)
		if limit <= 0 || len(resp.Results) < limit {
			resp.Results = append(resp.Results, collectGraphResults(impact.ImpactedNodes, "impacted_symbol", limit-len(resp.Results))...)
		}
		if len(impact.ImpactedFiles) > 0 {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("impact radius spans %d files", len(impact.ImpactedFiles)))
		}
	case "tests_for":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		resp.Results, err = collectNeighborResults(store, nodes, graphstore.EdgeKindTestedBy, true, limit)
		if err != nil {
			return resp, err
		}
		if len(resp.Results) == 0 {
			resp.Results, err = collectNeighborResults(store, nodes, graphstore.EdgeKindTestedBy, false, limit)
			if err != nil {
				return resp, err
			}
		}
	case "callers_of":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		resp.Results, err = collectNeighborResults(store, nodes, graphstore.EdgeKindCalls, true, limit)
		if err != nil {
			return resp, err
		}
	case "callees_of":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		resp.Results, err = collectNeighborResults(store, nodes, graphstore.EdgeKindCalls, false, limit)
		if err != nil {
			return resp, err
		}
	case "symbol_decisions":
		nodes, err := findCodeNodes(store, query, limit)
		if err != nil {
			return resp, err
		}
		resp.Results, err = collectSymbolDecisionResults(store, nodes, limit)
		if err != nil {
			return resp, err
		}
	case "decision_symbols":
		resp.Results, err = collectDecisionSymbolResults(store, query, limit)
		if err != nil {
			return resp, err
		}
	default:
		return resp, fmt.Errorf("unknown code bridge intent %q", bridgeIntent)
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	return resp, nil
}

// ── Phase 5: KGAdapter interface ──────────────────────────────────────────────

// KGAdapter is the interface for pluggable graph query backends.
type KGAdapter interface {
	Name() string
	Query(query GraphQuery) (GraphQueryResponse, error)
	Health() (KGAdapterHealth, error)
	Available() bool
}

// KGAdapterHealth reports status for one adapter.
type KGAdapterHealth struct {
	AdapterName     string   `json:"adapter_name"`
	Available       bool     `json:"available"`
	LastQueryTime   string   `json:"last_query_time,omitempty"`
	LastQueryStatus string   `json:"last_query_status,omitempty"`
	NoteCount       int      `json:"note_count"`
	Warnings        []string `json:"warnings,omitempty"`
}

// LocalFileAdapter wraps the Phase 3 index-based search as a KGAdapter.
type LocalFileAdapter struct {
	kgHome        string
	lastQueryTime string
	lastStatus    string
}

func NewLocalFileAdapter(kgHome string) *LocalFileAdapter {
	return &LocalFileAdapter{kgHome: kgHome}
}

func (a *LocalFileAdapter) Name() string { return "local-file" }

func (a *LocalFileAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(a.kgHome, "self", "config.yaml"))
	return err == nil
}

func (a *LocalFileAdapter) Query(query GraphQuery) (GraphQueryResponse, error) {
	resp, err := executeQuery(a.kgHome, query)
	a.lastQueryTime = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		a.lastStatus = "error"
	} else {
		a.lastStatus = "ok"
	}
	return resp, err
}

func (a *LocalFileAdapter) Health() (KGAdapterHealth, error) {
	h := KGAdapterHealth{
		AdapterName:     a.Name(),
		Available:       a.Available(),
		LastQueryTime:   a.lastQueryTime,
		LastQueryStatus: a.lastStatus,
	}
	if !h.Available {
		h.Warnings = append(h.Warnings, "graph not initialized")
		return h, nil
	}
	// Count notes
	_ = walkNoteFiles(a.kgHome, func(_ string, _ fs.DirEntry) error {
		h.NoteCount++
		return nil
	})
	return h, nil
}

// collectAdapterHealth gathers health from all adapters and writes to ops/adapters/.
func collectAdapterHealth(kgHomeDir string, adapters []KGAdapter) []KGAdapterHealth {
	healthList := make([]KGAdapterHealth, 0, len(adapters))
	for _, adapter := range adapters {
		h, _ := adapter.Health()
		healthList = append(healthList, h)
	}
	// Write to ops/adapters/adapter-health.json
	adapterHealthPath := filepath.Join(kgHomeDir, "ops", "adapters", "adapter-health.json")
	if err := os.MkdirAll(filepath.Dir(adapterHealthPath), 0755); err == nil {
		if data, err := json.MarshalIndent(healthList, "", "  "); err == nil {
			_ = os.WriteFile(adapterHealthPath, data, 0644)
		}
	}
	return healthList
}

// ── Phase 5: Bridge endpoint ──────────────────────────────────────────────────

// executeBridgeQuery resolves a bridge intent, executes KG queries, merges results.
func executeBridgeQuery(kgHomeDir, bridgeIntent, query string) (GraphQueryResponse, error) {
	if isCodeBridgeIntent(bridgeIntent) {
		return collectCodeBridgeResults(kgHomeDir, bridgeIntent, query, 10)
	}
	queries, err := resolveBridgeQuery(bridgeIntent, query)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	adapter := NewLocalFileAdapter(kgHomeDir)
	if !adapter.Available() {
		return GraphQueryResponse{}, fmt.Errorf("KG not initialized at %s", kgHomeDir)
	}
	var responses []GraphQueryResponse
	for _, q := range queries {
		resp, _ := adapter.Query(q) // collect even on partial error
		responses = append(responses, resp)
	}
	merged := mergeBridgeResults(responses, bridgeIntent)
	merged.Provider = adapter.Name()
	// Update adapter health
	collectAdapterHealth(kgHomeDir, []KGAdapter{adapter})
	return merged, nil
}

// ── Phase 5: Bridge contract ──────────────────────────────────────────────────

// writeBridgeContract writes KG_HOME/self/schema/bridge-contract.yaml.
func writeBridgeContract(kgHomeDir string) error {
	schemaDir := filepath.Join(kgHomeDir, "self", "schema")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		return err
	}
	mappings := defaultBridgeMappings()
	intents := make([]string, 0, len(mappings))
	for _, m := range mappings {
		intents = append(intents, m.BridgeIntent)
	}
	type contract struct {
		SchemaVersion    int                   `yaml:"schema_version"`
		SupportedIntents []string              `yaml:"supported_intents"`
		IntentMappings   []BridgeIntentMapping `yaml:"intent_mappings"`
		ResponseVersion  int                   `yaml:"response_version"`
		Adapters         []string              `yaml:"adapters"`
	}
	c := contract{
		SchemaVersion:    1,
		SupportedIntents: intents,
		IntentMappings:   mappings,
		ResponseVersion:  1,
		Adapters:         []string{"local-file"},
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(schemaDir, "bridge-contract.yaml"), data, 0644)
}

// ── kg bridge subcommands ─────────────────────────────────────────────────────

func runKGBridgeQuery(cmd *cobra.Command, args []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized — run 'dot-agents kg setup' first")
	}
	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return fmt.Errorf("--intent is required (valid: %s)", strings.Join(sortedKeys(validBridgeIntents), ", "))
	}
	query := strings.Join(args, " ")

	resp, err := executeBridgeQuery(home, intent, query)
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Bridge Query: %s  [%s]", intent, query))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, summarize(r.Summary, 60)))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

func runKGBridgeHealth(cmd *cobra.Command, _ []string) error {
	home := kgHome()
	adapter := NewLocalFileAdapter(home)
	adapters := []KGAdapter{adapter}
	healthList := collectAdapterHealth(home, adapters)

	if Flags.JSON {
		data, _ := json.MarshalIndent(healthList, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("KG Bridge Health")
	for _, h := range healthList {
		status := ui.ColorText(ui.Green, "available")
		if !h.Available {
			status = ui.ColorText(ui.Red, "unavailable")
		}
		ui.Info(fmt.Sprintf("  Adapter: %s  [%s]", h.AdapterName, status))
		ui.Info(fmt.Sprintf("  Notes: %d", h.NoteCount))
		if h.LastQueryTime != "" {
			ui.Info(fmt.Sprintf("  Last query: %s  status=%s", h.LastQueryTime, h.LastQueryStatus))
		}
		for _, w := range h.Warnings {
			ui.Warn(w)
		}
	}
	return nil
}

func runKGBridgeMapping(_ *cobra.Command, _ []string) error {
	mappings := defaultBridgeMappings()
	if Flags.JSON {
		data, _ := json.MarshalIndent(mappings, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Bridge Intent Mapping")
	for _, m := range mappings {
		ui.Info(fmt.Sprintf("  %-20s → %s", m.BridgeIntent, strings.Join(m.KGIntents, " + ")))
	}
	return nil
}

// ── Command registration ──────────────────────────────────────────────────────

// ── Phase 6C: kg sync ─────────────────────────────────────────────────────────

// runKGSync is a thin wrapper: git pull (or push) followed by kg lint.
// It does not implement a custom sync protocol — git provides the transport.
func runKGSync(cmd *cobra.Command, _ []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized at %s — run 'dot-agents kg setup' first", home)
	}

	push, _ := cmd.Flags().GetBool("push")

	var gitArgs []string
	if push {
		gitArgs = []string{"-C", home, "push"}
	} else {
		gitArgs = []string{"-C", home, "pull"}
	}

	op := "pull"
	if push {
		op = "push"
	}

	ui.Info(fmt.Sprintf("Running git %s in %s ...", op, home))
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w", op, err)
	}

	if push {
		ui.Success("Graph pushed.")
		return nil
	}

	// After pull, run lint to surface any content drift
	ui.Info("Running kg lint after pull ...")
	report, err := runGraphLint(home)
	if err != nil {
		return fmt.Errorf("lint after sync: %w", err)
	}

	if report.ErrorCount > 0 || report.WarnCount > 0 {
		ui.InfoBox(
			fmt.Sprintf("Sync complete — lint found issues (%d errors, %d warnings)", report.ErrorCount, report.WarnCount),
			"Run 'dot-agents kg lint' for details",
		)
	} else {
		ui.Success(fmt.Sprintf("Sync complete — graph is clean (%d notes)", len(report.Results)+report.InfoCount))
	}
	return nil
}

// ── Phase B: CRG code-graph commands ─────────────────────────────────────────

// crgRepoRoot returns the nearest git repo root above the cwd, falling back to cwd.
func crgRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return dir
}

func runKGBuild(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	skipFlows, _ := cmd.Flags().GetBool("skip-flows")
	skipPost, _ := cmd.Flags().GetBool("skip-postprocess")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	ui.Info(fmt.Sprintf("Building code graph for %s ...", root))
	return bridge.Build(graphstore.BuildOptions{
		SkipFlows:       skipFlows,
		SkipPostprocess: skipPost,
	})
}

func runKGUpdate(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	base, _ := cmd.Flags().GetString("base")
	skipFlows, _ := cmd.Flags().GetBool("skip-flows")
	skipPost, _ := cmd.Flags().GetBool("skip-postprocess")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	ui.Info(fmt.Sprintf("Updating code graph for %s ...", root))
	return bridge.Update(graphstore.UpdateOptions{
		Base:            base,
		SkipFlows:       skipFlows,
		SkipPostprocess: skipPost,
	})
}

func runKGCodeStatus(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	status, err := bridge.Status()
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Code Graph Status")
	ui.Info(fmt.Sprintf("  Nodes:        %d", status.Nodes))
	ui.Info(fmt.Sprintf("  Edges:        %d", status.Edges))
	ui.Info(fmt.Sprintf("  Files:        %d", status.Files))
	ui.Info(fmt.Sprintf("  Languages:    %s", status.Languages))
	ui.Info(fmt.Sprintf("  Last updated: %s", status.LastUpdated))
	return nil
}

func runKGImpact(cmd *cobra.Command, args []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	base, _ := cmd.Flags().GetString("base")
	maxDepth, _ := cmd.Flags().GetInt("depth")
	maxResults, _ := cmd.Flags().GetInt("limit")

	var files []string
	if len(args) > 0 {
		files = args
	}

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.GetImpactRadius(graphstore.ImpactOptions{
		ChangedFiles: files,
		MaxDepth:     maxDepth,
		MaxResults:   maxResults,
		Base:         base,
	})
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Impact Radius")
	ui.Info(result.Summary)
	if len(result.ChangedNodes) > 0 {
		ui.Section("Changed nodes")
		for _, n := range result.ChangedNodes {
			if n.Kind == "File" {
				continue // file-level nodes are noisy
			}
			ui.Bullet("warn", fmt.Sprintf("[%s] %s", n.Kind, n.Name))
		}
	}
	if len(result.ImpactedNodes) > 0 {
		ui.Section("Impacted nodes")
		for _, n := range result.ImpactedNodes {
			if n.Kind == "File" {
				continue
			}
			ui.Bullet("found", fmt.Sprintf("[%s] %s", n.Kind, n.Name))
		}
	}
	if len(result.ImpactedFiles) > 0 {
		ui.Section("Impacted files")
		for _, f := range result.ImpactedFiles {
			ui.Bullet("found", f)
		}
	}
	if result.Truncated {
		ui.Info(fmt.Sprintf("  (results truncated — %d total impacted)", result.TotalImpacted))
	}
	return nil
}

func runKGFlows(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	limit, _ := cmd.Flags().GetInt("limit")
	sortBy, _ := cmd.Flags().GetString("sort")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.ListFlows(limit, sortBy)
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Execution Flows  [%s]", result.Summary))
	if len(result.Flows) == 0 {
		ui.Info("No flows detected. Run 'dot-agents kg postprocess' to detect flows.")
		return nil
	}
	for _, f := range result.Flows {
		ui.Bullet("found", fmt.Sprintf("[%s] %s (steps=%d, criticality=%.2f)", f.Kind, f.Name, f.StepCount, f.Criticality))
		if f.EntryPoint != "" {
			ui.Info(fmt.Sprintf("        entry: %s", f.EntryPoint))
		}
	}
	return nil
}

func runKGCommunities(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	minSize, _ := cmd.Flags().GetInt("min-size")
	sortBy, _ := cmd.Flags().GetString("sort")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	result, err := bridge.ListCommunities(minSize, sortBy)
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Code Communities  [%s]", result.Summary))
	for _, c := range result.Communities {
		ui.Bullet("found", fmt.Sprintf("[%s] %s (size=%d, cohesion=%.2f)", c.DominantLanguage, c.Name, c.Size, c.Cohesion))
		if c.Description != "" {
			ui.Info(fmt.Sprintf("        %s", c.Description))
		}
	}
	return nil
}

func runKGPostprocess(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	noFlows, _ := cmd.Flags().GetBool("no-flows")
	noCommunities, _ := cmd.Flags().GetBool("no-communities")
	noFTS, _ := cmd.Flags().GetBool("no-fts")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	ui.Info(fmt.Sprintf("Running post-processing on %s ...", root))
	return bridge.Postprocess(graphstore.PostprocessOptions{
		NoFlows:       noFlows,
		NoCommunities: noCommunities,
		NoFTS:         noFTS,
	})
}

func runKGChanges(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	base, _ := cmd.Flags().GetString("base")
	brief, _ := cmd.Flags().GetBool("brief")

	bridge, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return err
	}
	report, err := bridge.DetectChanges(graphstore.DetectChangesOptions{
		Base:  base,
		Brief: brief,
	})
	if err != nil {
		return err
	}
	if Flags.JSON {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Change Impact")
	ui.Info(report.Summary)
	if len(report.ChangedFunctions) > 0 {
		ui.Section("Changed symbols")
		for _, n := range report.ChangedFunctions {
			ui.Bullet("warn", fmt.Sprintf("[risk=%.2f] %s", n.RiskScore, n.QualifiedName))
		}
	}
	if len(report.TestGaps) > 0 {
		ui.Section("Test gaps")
		for _, g := range report.TestGaps {
			ui.Bullet("error", g.QualifiedName)
		}
	}
	if len(report.ReviewPriorities) > 0 {
		ui.Section("Review priorities")
		for _, p := range report.ReviewPriorities {
			ui.Bullet("found", fmt.Sprintf("[risk=%.2f] %s — %s", p.RiskScore, p.QualifiedName, p.Reason))
		}
	}
	return nil
}

// ── Phase D: Hot/cold note lifecycle ─────────────────────────────────────────

// graphstoreDBPath returns the path to the SQLite warm-layer database.
func graphstoreDBPath(kgHomeDir string) string {
	return filepath.Join(kgHomeDir, "ops", "graphstore.db")
}

// openKGStore opens (or creates) the warm-layer SQLite database.
func openKGStore(kgHomeDir string) (*graphstore.SQLiteStore, error) {
	return graphstore.OpenSQLite(graphstoreDBPath(kgHomeDir))
}

// noteToKGNote converts a GraphNote from the hot filesystem layer to a
// graphstore.KGNote for the warm database layer.
func noteToKGNote(note *GraphNote, filePath string) graphstore.KGNote {
	archivedAt := ""
	if note.Status == "archived" || note.Status == "superseded" {
		archivedAt = note.UpdatedAt
	}
	return graphstore.KGNote{
		ID:         note.ID,
		Title:      note.Title,
		NoteType:   note.Type,
		Status:     note.Status,
		Summary:    note.Summary,
		FilePath:   filePath,
		Version:    note.Version,
		ArchivedAt: archivedAt,
	}
}

// runKGWarm syncs all hot filesystem notes into the warm SQLite layer.
func runKGWarm(cmd *cobra.Command, _ []string) error {
	home := kgHome()
	noteTypeFilter, _ := cmd.Flags().GetString("type")

	store, err := openKGStore(home)
	if err != nil {
		return fmt.Errorf("open warm store: %w", err)
	}
	defer store.Close()

	allTypes := []string{"source", "entity", "concept", "synthesis", "decision", "repo", "session"}
	var typeList []string
	if noteTypeFilter != "" {
		if !isValidNoteType(noteTypeFilter) {
			return fmt.Errorf("invalid note type %q", noteTypeFilter)
		}
		typeList = []string{noteTypeFilter}
	} else {
		typeList = allTypes
	}
	subdirs := make([]string, len(typeList))
	for i, t := range typeList {
		subdirs[i] = noteSubdir(t)
	}

	var indexed, skipped int
	for _, sub := range subdirs {
		dir := filepath.Join(home, "notes", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // directory may not exist yet
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fpath := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(fpath)
			if err != nil {
				skipped++
				continue
			}
			note, _, err := parseGraphNote(data)
			if err != nil || note.ID == "" {
				skipped++
				continue
			}
			kn := noteToKGNote(note, fpath)
			if err := store.UpsertKGNote(kn); err != nil {
				skipped++
				continue
			}
			indexed++
		}
	}

	// Also walk _archived directory
	archivedDir := filepath.Join(home, "notes", "_archived")
	if entries, err := os.ReadDir(archivedDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fpath := filepath.Join(archivedDir, e.Name())
			data, err := os.ReadFile(fpath)
			if err != nil {
				skipped++
				continue
			}
			note, _, err := parseGraphNote(data)
			if err != nil || note.ID == "" {
				skipped++
				continue
			}
			kn := noteToKGNote(note, fpath)
			if kn.ArchivedAt == "" {
				kn.ArchivedAt = note.UpdatedAt // treat physical archive dir as archived
			}
			if err := store.UpsertKGNote(kn); err != nil {
				skipped++
				continue
			}
			indexed++
		}
	}

	_ = store.SetMetadata("last_warm_sync", time.Now().UTC().Format(time.RFC3339))

	ui.SuccessBox(
		fmt.Sprintf("Warm sync complete: %d notes indexed, %d skipped", indexed, skipped),
		"dot-agents kg link add <note-id> <symbol> — link a note to a code symbol",
		"dot-agents kg link list <note-id>         — list all symbol links for a note",
	)
	return nil
}

// runKGLinkAdd creates a note→symbol link.
func runKGLinkAdd(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: kg link add <note-id> <qualified-name>")
	}
	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "mentions"
	}
	validLinkKinds := map[string]bool{
		"mentions": true, "implements": true, "documents": true,
		"decides": true, "references": true,
	}
	if !validLinkKinds[kind] {
		return fmt.Errorf("invalid link kind %q: must be one of mentions|implements|documents|decides|references", kind)
	}

	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf("open warm store: %w", err)
	}
	defer store.Close()

	link := graphstore.NoteSymbolLink{
		NoteID:        args[0],
		QualifiedName: args[1],
		LinkKind:      kind,
	}
	id, err := store.UpsertNoteSymbolLink(link)
	if err != nil {
		return fmt.Errorf("create link: %w", err)
	}
	ui.Success(fmt.Sprintf("Link created (id=%d): %s -[%s]-> %s", id, args[0], kind, args[1]))
	return nil
}

// runKGLinkList shows all symbol links for a note.
func runKGLinkList(_ *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: kg link list <note-id>")
	}
	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf("open warm store: %w", err)
	}
	defer store.Close()

	links, err := store.GetLinksForNote(args[0])
	if err != nil {
		return fmt.Errorf("get links: %w", err)
	}
	if len(links) == 0 {
		ui.Info(fmt.Sprintf("No symbol links for note %q. Run 'kg warm' first if notes are not yet indexed.", args[0]))
		return nil
	}
	for _, l := range links {
		fmt.Printf("  [%d] %s -[%s]-> %s\n", l.ID, l.NoteID, l.LinkKind, l.QualifiedName)
	}
	return nil
}

// runKGLinkRemove deletes a note→symbol link by ID.
func runKGLinkRemove(_ *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: kg link remove <link-id>")
	}
	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
		return fmt.Errorf("invalid link ID %q: must be an integer", args[0])
	}
	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf("open warm store: %w", err)
	}
	defer store.Close()

	if err := store.DeleteNoteSymbolLink(id); err != nil {
		return fmt.Errorf("remove link: %w", err)
	}
	ui.Success(fmt.Sprintf("Link %d removed", id))
	return nil
}

// runKGWarmStats shows warm layer stats without doing a sync.
func runKGWarmStats(_ *cobra.Command, _ []string) error {
	store, err := openKGStore(kgHome())
	if err != nil {
		return fmt.Errorf("open warm store: %w", err)
	}
	defer store.Close()

	stats, err := store.GetStats()
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}
	lastSync, _ := store.GetMetadata("last_warm_sync")
	if lastSync == "" {
		lastSync = "never"
	}
	ui.InfoBox("Warm Layer Stats",
		fmt.Sprintf("Notes indexed:    %d", stats.NotesCount),
		fmt.Sprintf("Symbol links:     %d", stats.LinksCount),
		fmt.Sprintf("Code nodes:       %d", stats.TotalNodes),
		fmt.Sprintf("Code edges:       %d", stats.TotalEdges),
		fmt.Sprintf("Last warm sync:   %s", lastSync),
		fmt.Sprintf("DB path:          %s", graphstoreDBPath(kgHome())),
	)
	return nil
}

func NewKGCmd() *cobra.Command {
	kgCmd := &cobra.Command{
		Use:   "kg",
		Short: "Manage the local knowledge graph",
	}

	kgSetupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the knowledge graph at KG_HOME",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGSetup()
		},
	}

	kgHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show knowledge graph health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGHealth()
		},
	}

	kgIngestCmd := &cobra.Command{
		Use:   "ingest [file]",
		Short: "Ingest a raw source into the knowledge graph",
		RunE:  runKGIngest,
	}
	kgIngestCmd.Flags().Bool("all", false, "Process all pending sources in the inbox")
	kgIngestCmd.Flags().String("title", "", "Override source title")
	kgIngestCmd.Flags().String("type", "markdown", "Source type (markdown|text|pdf|url|transcript|meeting_notes|repo_doc)")
	kgIngestCmd.Flags().Bool("dry-run", false, "Show what would be created without writing")

	kgQueueCmd := &cobra.Command{
		Use:   "queue",
		Short: "List pending sources in the inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGQueue()
		},
	}

	kgQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Query the knowledge graph by intent",
		RunE:  runKGQuery,
	}
	kgQueryCmd.Flags().String("intent", "", fmt.Sprintf("Query intent (required): %s", strings.Join(sortedKeys(validQueryIntents), "|")))
	kgQueryCmd.Flags().Int("limit", 10, "Max results to return")
	kgQueryCmd.Flags().String("scope", "", "Optional scope filter")

	kgLintCmd := &cobra.Command{
		Use:   "lint",
		Short: "Check graph integrity and knowledge quality",
		RunE:  runKGLint,
	}
	kgLintCmd.Flags().String("check", "", "Run only one check (broken_links|orphan_pages|missing_source_refs|stale_pages|index_drift|oversize_pages|contradictions)")

	kgMaintainCmd := &cobra.Command{
		Use:   "maintain",
		Short: "Graph maintenance operations",
	}

	kgReweaveCmd := &cobra.Command{
		Use:   "reweave",
		Short: "Repair broken links and add missing source_ref links",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGReweave(kgHome())
		},
	}

	kgMarkStaleCmd := &cobra.Command{
		Use:   "mark-stale",
		Short: "Mark notes not updated beyond threshold as stale",
		RunE: func(cmd *cobra.Command, args []string) error {
			days, _ := cmd.Flags().GetInt("days")
			return runKGMarkStale(kgHome(), time.Duration(days)*24*time.Hour)
		},
	}
	kgMarkStaleCmd.Flags().Int("days", 90, "Age threshold in days (default 90)")

	kgCompactCmd := &cobra.Command{
		Use:   "compact",
		Short: "Archive superseded and archived notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGCompact(kgHome())
		},
	}

	kgMaintainCmd.AddCommand(kgReweaveCmd, kgMarkStaleCmd, kgCompactCmd)

	// bridge subcommand tree
	kgBridgeCmd := &cobra.Command{
		Use:   "bridge",
		Short: "Query and inspect the KG bridge surface",
	}
	kgBridgeQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Execute a bridge intent query",
		RunE:  runKGBridgeQuery,
	}
	kgBridgeQueryCmd.Flags().String("intent", "", fmt.Sprintf("Bridge intent (required): %s", strings.Join(sortedKeys(validBridgeIntents), "|")))

	kgBridgeHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show adapter availability and health",
		RunE:  runKGBridgeHealth,
	}
	kgBridgeMappingCmd := &cobra.Command{
		Use:   "mapping",
		Short: "Show bridge intent to KG intent mapping",
		RunE:  runKGBridgeMapping,
	}
	kgBridgeCmd.AddCommand(kgBridgeQueryCmd, kgBridgeHealthCmd, kgBridgeMappingCmd)

	// sync subcommand (Phase 6C)
	kgSyncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync graph via git pull + lint (use --push to push)",
		RunE:  runKGSync,
	}
	kgSyncCmd.Flags().Bool("push", false, "Push current state instead of pulling")

	// Phase D: warm layer sync
	kgWarmCmd := &cobra.Command{
		Use:   "warm",
		Short: "Sync hot filesystem notes into the warm SQLite layer",
		RunE:  runKGWarm,
	}
	kgWarmCmd.Flags().String("type", "", "Only sync notes of this type (source|entity|concept|synthesis|decision|repo|session)")

	kgWarmStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show warm layer statistics",
		RunE:  runKGWarmStats,
	}
	kgWarmCmd.AddCommand(kgWarmStatsCmd)

	// Phase D: note→symbol links
	kgLinkCmd := &cobra.Command{
		Use:   "link",
		Short: "Manage note→code symbol cross-references",
	}
	kgLinkAddCmd := &cobra.Command{
		Use:   "add <note-id> <qualified-name>",
		Short: "Link a knowledge note to a code symbol",
		RunE:  runKGLinkAdd,
	}
	kgLinkAddCmd.Flags().String("kind", "mentions", "Link kind: mentions|implements|documents|decides|references")

	kgLinkListCmd := &cobra.Command{
		Use:   "list <note-id>",
		Short: "List all symbol links for a note",
		RunE:  runKGLinkList,
	}
	kgLinkRemoveCmd := &cobra.Command{
		Use:   "remove <link-id>",
		Short: "Remove a note→symbol link by ID",
		RunE:  runKGLinkRemove,
	}
	kgLinkCmd.AddCommand(kgLinkAddCmd, kgLinkListCmd, kgLinkRemoveCmd)

	// Phase B: CRG code-graph subcommands
	kgBuildCmd := &cobra.Command{
		Use:   "build",
		Short: "Full code graph build (re-parse all files via code-review-graph)",
		RunE:  runKGBuild,
	}
	kgBuildCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgBuildCmd.Flags().Bool("skip-flows", false, "Skip flow/community detection (faster)")
	kgBuildCmd.Flags().Bool("skip-postprocess", false, "Skip all post-processing (raw parse only)")

	kgUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Incremental code graph update (changed files only)",
		RunE:  runKGUpdate,
	}
	kgUpdateCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgUpdateCmd.Flags().String("base", "", "Git diff base (default: HEAD~1)")
	kgUpdateCmd.Flags().Bool("skip-flows", false, "Skip flow/community detection")
	kgUpdateCmd.Flags().Bool("skip-postprocess", false, "Skip all post-processing")

	kgCodeStatusCmd := &cobra.Command{
		Use:   "code-status",
		Short: "Show code graph stats (nodes, edges, languages)",
		RunE:  runKGCodeStatus,
	}
	kgCodeStatusCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")

	kgChangesCmd := &cobra.Command{
		Use:   "changes",
		Short: "Detect change impact in the current diff",
		RunE:  runKGChanges,
	}
	kgChangesCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgChangesCmd.Flags().String("base", "", "Git diff base (default: HEAD~1)")
	kgChangesCmd.Flags().Bool("brief", false, "Show brief summary only")

	// Phase C: impact, flows, communities, postprocess
	kgImpactCmd := &cobra.Command{
		Use:   "impact [file...]",
		Short: "Show blast radius for given files (or current diff)",
		RunE:  runKGImpact,
	}
	kgImpactCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgImpactCmd.Flags().String("base", "", "Git diff base (default: HEAD~1)")
	kgImpactCmd.Flags().Int("depth", 2, "Max hop depth for impact traversal")
	kgImpactCmd.Flags().Int("limit", 50, "Max impacted nodes to return")

	kgFlowsCmd := &cobra.Command{
		Use:   "flows",
		Short: "List detected execution flows",
		RunE:  runKGFlows,
	}
	kgFlowsCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgFlowsCmd.Flags().Int("limit", 20, "Max flows to show")
	kgFlowsCmd.Flags().String("sort", "criticality", "Sort by: criticality|size")

	kgCommunitiesCmd := &cobra.Command{
		Use:   "communities",
		Short: "List detected code communities",
		RunE:  runKGCommunities,
	}
	kgCommunitiesCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgCommunitiesCmd.Flags().Int("min-size", 0, "Only show communities with at least this many members")
	kgCommunitiesCmd.Flags().String("sort", "size", "Sort by: size|cohesion")

	kgPostprocessCmd := &cobra.Command{
		Use:   "postprocess",
		Short: "Rebuild flows, communities, and FTS index",
		RunE:  runKGPostprocess,
	}
	kgPostprocessCmd.Flags().String("repo", "", "Repository root (auto-detected from git)")
	kgPostprocessCmd.Flags().Bool("no-flows", false, "Skip flow detection")
	kgPostprocessCmd.Flags().Bool("no-communities", false, "Skip community detection")
	kgPostprocessCmd.Flags().Bool("no-fts", false, "Skip FTS rebuild")

	kgCmd.AddCommand(
		kgSetupCmd, kgHealthCmd, kgIngestCmd, kgQueueCmd, kgQueryCmd,
		kgLintCmd, kgMaintainCmd, kgBridgeCmd, kgSyncCmd, kgWarmCmd, kgLinkCmd,
		kgBuildCmd, kgUpdateCmd, kgCodeStatusCmd, kgChangesCmd,
		kgImpactCmd, kgFlowsCmd, kgCommunitiesCmd, kgPostprocessCmd,
	)
	return kgCmd
}
