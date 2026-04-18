package kg

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

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

func runKGCodeStatus(deps Deps, cmd *cobra.Command, _ []string) error {
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
	if deps.Flags.JSON {
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

func runKGImpact(deps Deps, cmd *cobra.Command, args []string) error {
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
	if deps.Flags.JSON {
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

func runKGFlows(deps Deps, cmd *cobra.Command, _ []string) error {
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
	if deps.Flags.JSON {
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

func runKGCommunities(deps Deps, cmd *cobra.Command, _ []string) error {
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
	if deps.Flags.JSON {
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

func runKGChanges(deps Deps, cmd *cobra.Command, _ []string) error {
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
	if deps.Flags.JSON {
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
