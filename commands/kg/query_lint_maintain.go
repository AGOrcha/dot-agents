package kg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

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
	// SparsityScore (0–100) indicates how incomplete the result set is.
	// 0 = fully evidenced, 100 = no evidence found.
	// Absent (omitted) when the query type does not support scoring.
	SparsityScore *int `json:"sparsity_score,omitempty"`
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

func runKGQuery(deps Deps, cmd *cobra.Command, args []string) error {
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

	if deps.Flags.JSON {
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

func runKGLint(deps Deps, cmd *cobra.Command, _ []string) error {
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

	if deps.Flags.JSON {
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
