package store

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

const (
	defaultCacheSize = 256
	defaultCacheTTL  = 30 * time.Second
	defaultLimit     = 50
	maxLimit         = 500
	// sessionsKey is the LRU key for the memoized cross-root projection
	// (sessionsView). It shares the store's read cache with the per-root
	// snapshots, which key off rootKey's "root:" prefix, so it never collides.
	sessionsKey = "sessions"
)

// Filename patterns for the three on-disk artifacts the store reads. The record
// pattern deliberately excludes the *.score.yaml sidecars that share the
// iter-* prefix.
var (
	iterRecordRE = regexp.MustCompile(`^iter-(\d+)\.yaml$`)
	iterScoreRE  = regexp.MustCompile(`^iter-(\d+)\.score\.yaml$`)
	sessionScrRE = regexp.MustCompile(`^session-(.+)\.score\.yaml$`)
)

// DiskStore is the read-through Store implementation over iter-log roots and
// their score sidecars. It is safe for concurrent use.
type DiskStore struct {
	roots           []string
	cache           *lruCache
	logger          *slog.Logger
	subscriberCount func() int
}

// Option configures a DiskStore.
type Option func(*DiskStore)

// WithCache overrides the default LRU (size 256, TTL 30s).
func WithCache(size int, ttl time.Duration) Option {
	return func(s *DiskStore) { s.cache = newLRUCache(size, ttl) }
}

// WithLogger sets the structured logger used for skipped-file warnings.
func WithLogger(l *slog.Logger) Option {
	return func(s *DiskStore) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithSubscriberCounter injects the live SSE subscriber count reported by
// Health. The broker (t04) owns the real count; when unset Health reports 0.
func WithSubscriberCounter(fn func() int) Option {
	return func(s *DiskStore) { s.subscriberCount = fn }
}

// New builds a DiskStore over the given iter-log roots. Per spec OQ1 the store
// discovers nothing beyond the roots it is handed: multi-root is supported so a
// future wave can add historical roots, but v1's default config passes only the
// active root.
func New(roots []string, opts ...Option) *DiskStore {
	s := &DiskStore{
		roots:  append([]string(nil), roots...),
		cache:  newLRUCache(defaultCacheSize, defaultCacheTTL),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// CacheMetrics exposes the read-cache instrumentation.
func (s *DiskStore) CacheMetrics() CacheMetrics { return s.cache.metrics() }

// Evict drops the cached snapshot for one root and the aggregate projection
// that folds it in (broker per-root push hook). Dropping the projection too is
// load-bearing: a same-mtime content rewrite the fingerprint cannot see leaves
// the combined key unchanged, so without this the memo would keep serving the
// pre-eviction view even though the root snapshot was force-dropped.
func (s *DiskStore) Evict(root string) {
	s.cache.evict(rootKey(root))
	s.cache.evict(sessionsKey)
}

// EvictAll drops every cached snapshot (broker whole-cache push hook).
func (s *DiskStore) EvictAll() { s.cache.clear() }

// rootKey canonicalizes a root into the cache key. The write path keys by the
// store's configured (OS-native) roots while the broker's per-root Evict push
// arrives with the event bridge's logical, forward-slash root; ToSlash(Clean)
// collapses both to the same key so eviction matches the cached snapshot on
// every OS (on non-Windows it is a no-op). Without it a backslash write key on
// Windows would never match a forward-slash evict key and per-root eviction
// would silently no-op, serving stale reads.
func rootKey(root string) string { return "root:" + filepath.ToSlash(filepath.Clean(root)) }

// rootSnapshot is one iter-log root's fully-parsed read view: the iteration
// records, the two families of score sidecars, and the per-file mtimes used for
// last_update and cache invalidation.
type rootSnapshot struct {
	root          string
	records       []scoring.IterationRecord
	iterScores    map[int]scoring.PersistedScore
	sessionScores map[string]scoring.SessionScore
	fileMtime     map[string]time.Time
	newestMtime   time.Time
}

// sessionCtx locates a session's records within the root it was discovered in.
type sessionCtx struct {
	root    string
	snap    rootSnapshot
	records []scoring.IterationRecord
}

// snapshotFP returns the parsed view of root and the directory fingerprint it
// was resolved at, served from cache while the fingerprint is unchanged. The
// fingerprint covers EVERY file's name and mtime — not just the newest — so a
// backfilled score sidecar on an old iteration, a same-timestamp rewrite with a
// preserved (backdated) mtime, or a deletion of a non-newest file all
// invalidate, where a max-mtime key would serve stale data. Returning the
// fingerprint lets sessionsView fold it into the aggregate memo key off the
// same read, with no second directory scan.
func (s *DiskStore) snapshotFP(root string) (rootSnapshot, int64) {
	mtimes, newest, fp := s.readDirState(root)
	if v, ok := s.cache.get(rootKey(root), fp); ok {
		return v.(rootSnapshot), fp
	}
	snap := s.loadRoot(root, mtimes)
	snap.newestMtime = newest
	s.cache.put(rootKey(root), snap, fp)
	return snap, fp
}

// snapshot is snapshotFP without the fingerprint, for callers that only need
// the parsed view (GetIteration and the recompute overlay).
func (s *DiskStore) snapshot(root string) rootSnapshot {
	snap, _ := s.snapshotFP(root)
	return snap
}

// readDirState lists root once, returning per-file mtimes, the newest mtime
// (for last_update / health), and an FNV-1a fingerprint over every (name,
// mtime) pair. It reads via *File.Readdir, which returns FileInfo directly, so
// one lstat per entry feeds both the newest-mtime scan and the hash with no
// intermediate DirEntry wrapper — os.ReadDir would allocate a *unixDirent per
// file, the dominant read-through allocation the store pays on every request.
// Readdir does not sort, so entries are name-sorted explicitly to keep the hash
// deterministic (identical to the os.ReadDir ordering it replaces). A missing
// root yields an empty map, the zero time, and a zero fingerprint, same as an
// empty root — legitimately absent. Any other error (permission denied, I/O
// fault) degrades the same way but is logged, matching the decodeYAML /
// resilientRecords siblings below.
func (s *DiskStore) readDirState(root string) (map[string]time.Time, time.Time, int64) {
	f, err := os.Open(root)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("dashboard/store: skip unreadable root", "root", root, "error", err)
		}
		return map[string]time.Time{}, time.Time{}, 0
	}
	defer f.Close()
	infos, err := f.Readdir(-1)
	if err != nil {
		s.logger.Warn("dashboard/store: skip unreadable root", "root", root, "error", err)
		return map[string]time.Time{}, time.Time{}, 0
	}
	slices.SortFunc(infos, func(a, b os.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
	m := make(map[string]time.Time, len(infos))
	var newest time.Time
	h := fnv.New64a()
	var buf [8]byte
	for _, info := range infos {
		if info.IsDir() {
			continue
		}
		mt := info.ModTime()
		m[info.Name()] = mt
		if mt.After(newest) {
			newest = mt
		}
		_, _ = h.Write([]byte(info.Name()))
		binary.LittleEndian.PutUint64(buf[:], uint64(mt.UnixNano()))
		_, _ = h.Write(buf[:])
	}
	return m, newest, int64(h.Sum64())
}

// loadRoot parses every sidecar and iteration record in root. Corrupt files are
// skipped with a warning (spec R10 resilience): a single bad file never fails a
// list query.
func (s *DiskStore) loadRoot(root string, mtimes map[string]time.Time) rootSnapshot {
	snap := rootSnapshot{
		root:          root,
		iterScores:    map[int]scoring.PersistedScore{},
		sessionScores: map[string]scoring.SessionScore{},
		fileMtime:     mtimes,
	}
	var iterFiles []string
	for name := range mtimes {
		full := filepath.Join(root, name)
		switch {
		case iterRecordRE.MatchString(name):
			iterFiles = append(iterFiles, full)
		case iterScoreRE.MatchString(name):
			var ps scoring.PersistedScore
			if s.decodeYAML(full, &ps) {
				snap.iterScores[ps.Iteration] = ps
			}
		case sessionScrRE.MatchString(name):
			var ss scoring.SessionScore
			if s.decodeYAML(full, &ss) {
				snap.sessionScores[ss.SessionID] = ss
			}
		}
	}

	// Happy path: reuse the canonical loader (also folds historical.yaml). If a
	// single corrupt iter-*.yaml makes it error, fall back to a resilient
	// per-file parse that skips the bad file, honoring spec R10.
	if recs, err := scoring.LoadIterationLog(root); err == nil {
		snap.records = recs
	} else {
		s.logger.Warn("dashboard/store: iteration-log load failed, using resilient fallback",
			"root", root, "error", err)
		snap.records = s.resilientRecords(iterFiles)
	}
	return snap
}

// decodeYAML reads path into v, logging and skipping on any read/parse error.
func (s *DiskStore) decodeYAML(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Warn("dashboard/store: skip unreadable sidecar", "path", path, "error", err)
		return false
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		s.logger.Warn("dashboard/store: skip corrupt sidecar", "path", path, "error", err)
		return false
	}
	return true
}

// resilientRecords parses each iter-*.yaml independently, skipping any file that
// fails to parse, and returns them sorted ascending by iteration.
func (s *DiskStore) resilientRecords(iterFiles []string) []scoring.IterationRecord {
	recs := make([]scoring.IterationRecord, 0, len(iterFiles))
	for _, path := range iterFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			s.logger.Warn("dashboard/store: skip unreadable iter file", "path", path, "error", err)
			continue
		}
		rec, err := scoring.ParseIterationRecord(data)
		if err != nil {
			s.logger.Warn("dashboard/store: skip corrupt iter file", "path", path, "error", err)
			continue
		}
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Iteration < recs[j].Iteration })
	return recs
}

// sessionsView is the memoized cross-root projection: the addressable sessions
// grouped and sorted, the pre-built list-summary DTOs, and the aggregate counts
// Health reports. It is a pure function of the roots' snapshots, so it is cached
// under the combined directory fingerprint and reused verbatim while every root
// is unchanged — repeated dashboard reads then skip the re-group/sort/project.
type sessionsView struct {
	byID      map[string]sessionCtx
	summaries []Run
	iterCount int
	newest    time.Time
}

// sessionsView returns the memoized projection, rebuilding it only when the
// combined per-root fingerprint changes. Each root's fingerprint is folded from
// the same snapshotFP read that resolves its snapshot, so a warm hit costs one
// directory scan per root and no projection work; any add/change/delete to any
// root flips a fingerprint, flips the combined key, and forces a rebuild.
func (s *DiskStore) sessionsView() sessionsView {
	snaps := make([]rootSnapshot, len(s.roots))
	h := fnv.New64a()
	var buf [8]byte
	for i, root := range s.roots {
		var fp int64
		snaps[i], fp = s.snapshotFP(root)
		binary.LittleEndian.PutUint64(buf[:], uint64(fp))
		_, _ = h.Write(buf[:])
	}
	key := int64(h.Sum64())
	if v, ok := s.cache.get(sessionsKey, key); ok {
		return v.(sessionsView)
	}
	view := s.buildSessionsView(snaps)
	s.cache.put(sessionsKey, view, key)
	return view
}

// buildSessionsView groups every addressable session across all roots. Records
// with an empty session id are unaddressable (no sidecar can be named) and are
// skipped, matching internal/scoring.AggregateSessions. A session id colliding
// across roots keeps the first root and warns (API.md §1.6: session ids are
// globally unique). iterCount and newest span EVERY record (including the
// skipped empty-id ones) so Health reports the same totals as a per-root walk.
func (s *DiskStore) buildSessionsView(snaps []rootSnapshot) sessionsView {
	byID := make(map[string]sessionCtx)
	var iterCount int
	var newest time.Time
	for i, root := range s.roots {
		snap := snaps[i]
		iterCount += len(snap.records)
		if snap.newestMtime.After(newest) {
			newest = snap.newestMtime
		}
		groups := make(map[string][]scoring.IterationRecord)
		for _, rec := range snap.records {
			sid := rec.Agent.SessionID
			if sid == "" {
				continue
			}
			groups[sid] = append(groups[sid], rec)
		}
		for sid, recs := range groups {
			if _, dup := byID[sid]; dup {
				s.logger.Warn("dashboard/store: duplicate session id across roots, keeping first",
					"session_id", sid, "root", root)
				continue
			}
			sort.Slice(recs, func(i, j int) bool { return recs[i].Iteration < recs[j].Iteration })
			byID[sid] = sessionCtx{root: root, snap: snap, records: recs}
		}
	}
	summaries := make([]Run, 0, len(byID))
	for _, ctx := range byID {
		summaries = append(summaries, s.buildRun(ctx, false))
	}
	return sessionsView{byID: byID, summaries: summaries, iterCount: iterCount, newest: newest}
}

// ListRuns implements Store.
func (s *DiskStore) ListRuns(_ context.Context, f RunFilter) ([]RunSummary, error) {
	f = normalizeFilter(f)
	view := s.sessionsView()
	// Copy the memoized summaries before filter/sort: filterRuns reuses the
	// backing array (runs[:0]) and sortRuns reorders in place, so handing them
	// the cached slice would corrupt the memo and race concurrent readers.
	runs := make([]Run, len(view.summaries))
	copy(runs, view.summaries)
	runs = filterRuns(runs, f)
	sortRuns(runs, f.Sort, f.Order)
	return paginate(runs, f.Limit, f.Offset), nil
}

// GetRun implements Store.
func (s *DiskStore) GetRun(_ context.Context, sessionID string) (RunDetail, error) {
	ctx, ok := s.sessionsView().byID[sessionID]
	if !ok {
		return RunDetail{}, ErrNotFound
	}
	return s.buildRun(ctx, true), nil
}

// ListIterations implements Store.
func (s *DiskStore) ListIterations(_ context.Context, sessionID string) ([]IterationSummary, error) {
	ctx, ok := s.sessionsView().byID[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]IterationSummary, 0, len(ctx.records))
	for _, rec := range ctx.records {
		out = append(out, buildIteration(rec, ctx.snap, false))
	}
	return out, nil
}

// GetIteration implements Store. iterLogDir defaults to the active (first) root
// and, when non-empty, must resolve to one of the configured roots (see
// resolveRoot) — an unlisted path is rejected with ErrRootNotAllowed.
func (s *DiskStore) GetIteration(_ context.Context, iterLogDir string, n int) (IterationDetail, error) {
	root, err := s.resolveRoot(iterLogDir)
	if err != nil {
		return IterationDetail{}, err
	}
	snap := s.snapshot(root)
	for _, rec := range snap.records {
		if rec.Iteration == n {
			return buildIteration(rec, snap, true), nil
		}
	}
	return IterationDetail{}, ErrNotFound
}

// resolveRoot maps a requested iter_log_dir to one of the store's configured
// roots. An empty value selects the active (first) root. A non-empty value MUST
// match a configured root after path normalization (filepath.Clean collapses
// any ".." traversal), so a handler cannot coax the store into reading an
// arbitrary iteration-log-shaped directory outside its resolved roots.
func (s *DiskStore) resolveRoot(iterLogDir string) (string, error) {
	if iterLogDir == "" {
		if len(s.roots) == 0 {
			return "", ErrNotFound
		}
		return s.roots[0], nil
	}
	req := filepath.Clean(iterLogDir)
	for _, root := range s.roots {
		if filepath.Clean(root) == req {
			return root, nil
		}
	}
	return "", ErrRootNotAllowed
}

// Rubric implements Store.
func (s *DiskStore) Rubric(_ context.Context) (RubricDoc, error) {
	r := scoring.DefaultRubric()
	doc := RubricDoc{
		Version:     r.Version,
		Combination: string(r.Combination),
		Signals:     make([]RubricSignal, len(r.Signals)),
		Bands:       make([]RubricBand, len(r.Bands)),
	}
	for i, sig := range r.Signals {
		doc.Signals[i] = RubricSignal{
			ID:          string(sig.ID),
			Label:       sig.Label,
			Weight:      sig.Weight,
			Description: sig.Description,
			TwoWay:      sig.TwoWay,
		}
	}
	for i, b := range r.Bands {
		doc.Bands[i] = RubricBand{Name: b.Name, Min: b.Min}
	}
	return doc, nil
}

// Health implements Store.
func (s *DiskStore) Health(_ context.Context) (Health, error) {
	view := s.sessionsView()
	h := Health{
		Status:         "ok",
		RunCount:       len(view.byID),
		IterationCount: view.iterCount,
		RubricVersion:  scoring.DefaultRubric().Version,
		Roots:          append([]string(nil), s.roots...),
	}
	if !view.newest.IsZero() {
		h.LastIterLogMtime = rfc3339Ptr(view.newest)
	}
	if s.subscriberCount != nil {
		h.SubscriberCount = s.subscriberCount()
	}
	return h, nil
}

// buildRun assembles the Run DTO for one session. When detail is true the
// per_iteration array is populated (RunDetail); the list path omits it.
func (s *DiskStore) buildRun(ctx sessionCtx, detail bool) Run {
	recs := ctx.records
	last := recs[len(recs)-1] // records is non-empty and ascending
	run := Run{
		SessionID:        last.Agent.SessionID,
		Harness:          last.Agent.Harness,
		Model:            last.Agent.Model,
		Wave:             last.Wave,
		IterationCount:   len(recs),
		Band:             bandUnscored,
		IterLogDir:       ctx.root,
		FirstIteration:   intPtr(recs[0].Iteration),
		LastIteration:    intPtr(last.Iteration),
		LastUpdate:       s.sessionLastUpdate(ctx),
		MeanCacheHitRate: meanCacheHitRate(recs),
	}
	if ss, ok := ctx.snap.sessionScores[ctx.records[0].Agent.SessionID]; ok {
		run.RubricVersion = ss.RubricVersion
		run.Scored = ss.Scored
		run.Band = ss.Band
		if ss.Scored {
			run.Score = floatPtr(ss.Value)
		}
	}
	if detail {
		run.PerIteration = perIterationRefs(ctx)
	}
	return run
}

// perIterationRefs prefers the session sidecar's per-iteration list; absent
// that, it derives one ref per record from the per-iteration score sidecars.
func perIterationRefs(ctx sessionCtx) []IterScoreRef {
	if ss, ok := ctx.snap.sessionScores[ctx.records[0].Agent.SessionID]; ok && len(ss.PerIteration) > 0 {
		refs := make([]IterScoreRef, len(ss.PerIteration))
		for i, r := range ss.PerIteration {
			ref := IterScoreRef{Iteration: r.Iteration, Scored: r.Scored, Band: r.Band}
			if r.Scored {
				ref.Score = floatPtr(r.Value)
			} else {
				ref.Band = bandUnscored
			}
			refs[i] = ref
		}
		return refs
	}
	refs := make([]IterScoreRef, 0, len(ctx.records))
	for _, rec := range ctx.records {
		ref := IterScoreRef{Iteration: rec.Iteration, Band: bandUnscored}
		if ps, ok := ctx.snap.iterScores[rec.Iteration]; ok && ps.Scored {
			ref.Scored = true
			ref.Score = floatPtr(ps.Value)
			ref.Band = ps.Band
		}
		refs = append(refs, ref)
	}
	return refs
}

// sessionLastUpdate is the newest mtime among a session's iter records, their
// score sidecars, and the session sidecar; nil when none resolve.
func (s *DiskStore) sessionLastUpdate(ctx sessionCtx) *string {
	var newest time.Time
	consider := func(name string) {
		if mt, ok := ctx.snap.fileMtime[name]; ok && mt.After(newest) {
			newest = mt
		}
	}
	for _, rec := range ctx.records {
		consider(iterFileName(rec.Iteration))
		consider(iterScoreFileName(rec.Iteration))
	}
	consider(sessionScoreFileName(ctx.records[0].Agent.SessionID))
	if newest.IsZero() {
		return nil
	}
	return rfc3339Ptr(newest)
}

// buildIteration projects one iteration record + its score sidecar into the DTO.
// When detail is true the breakdown and verifier rows are included. The
// integrity / objective / integrity_observation_count / transcript_turn_count
// fields are recompute-sourced (t06), so the read-through store leaves them
// empty rather than fabricating them.
func buildIteration(rec scoring.IterationRecord, snap rootSnapshot, detail bool) Iteration {
	it := Iteration{
		Iteration:     rec.Iteration,
		SessionID:     rec.Agent.SessionID,
		SchemaVersion: normSchemaVersion(rec.SchemaVersion),
		Date:          rec.Date,
		Wave:          rec.Wave,
		TaskID:        rec.TaskID,
		Commit:        rec.Commit,
		Band:          bandUnscored,
		FilesChanged:  rec.FilesChanged,
		LinesAdded:    rec.LinesAdded,
		LinesRemoved:  rec.LinesRemoved,
		Retries:       rec.Impl.Retries,
	}
	if tu := rec.SessionTokens; tu != nil {
		it.TokenUsage = &TokenUsage{
			InputTokens:         tu.InputTokens,
			OutputTokens:        tu.OutputTokens,
			CacheReadTokens:     tu.CacheReadTokens,
			CacheCreationTokens: tu.CacheCreationTokens,
			CacheHitRate:        tu.CacheHitRate,
		}
	}
	if ps, ok := snap.iterScores[rec.Iteration]; ok {
		it.RubricVersion = ps.RubricVersion
		it.Scored = ps.Scored
		it.Band = ps.Band
		if ps.Scored {
			it.Score = floatPtr(ps.Value)
		}
		if detail {
			it.Breakdown = mapBreakdown(ps.Breakdown)
		}
	}
	if detail {
		it.Verifiers = mapVerifiers(rec.Verifiers)
	}
	return it
}

func mapBreakdown(rows []scoring.PersistedContribution) []BreakdownRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]BreakdownRow, len(rows))
	for i, r := range rows {
		out[i] = BreakdownRow{
			Signal:          string(r.Signal),
			Label:           r.Label,
			Present:         r.Present,
			SubScore:        r.SubScore,
			Detail:          r.Detail,
			NominalWeight:   r.NominalWeight,
			EffectiveWeight: r.EffectiveWeight,
			Contribution:    r.Contribution,
		}
	}
	return out
}

func mapVerifiers(vs []scoring.VerifierRecord) []Verifier {
	if len(vs) == 0 {
		return nil
	}
	out := make([]Verifier, len(vs))
	for i, v := range vs {
		out[i] = Verifier{
			Type:       v.Type,
			Status:     v.Status,
			GatePassed: v.GatePassed,
			TestsAdded: v.TestsAdded,
			Retries:    v.Retries,
		}
	}
	return out
}

// meanCacheHitRate averages cache_hit_rate over the records that captured token
// telemetry; nil when none did.
func meanCacheHitRate(recs []scoring.IterationRecord) *float64 {
	var sum float64
	var n int
	for _, rec := range recs {
		if rec.SessionTokens != nil {
			sum += rec.SessionTokens.CacheHitRate
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return floatPtr(sum / float64(n))
}

// normalizeFilter applies the API.md defaults so ListRuns is forgiving of the
// zero value; handlers own param validation and 400s.
func normalizeFilter(f RunFilter) RunFilter {
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Sort == "" {
		f.Sort = "last_update"
	}
	if f.Order == "" {
		f.Order = "desc"
	}
	return f
}

func filterRuns(runs []Run, f RunFilter) []Run {
	if f.Band == "" && f.Harness == "" {
		return runs
	}
	out := runs[:0]
	for _, r := range runs {
		if f.Band != "" && r.Band != f.Band {
			continue
		}
		if f.Harness != "" && r.Harness != f.Harness {
			continue
		}
		out = append(out, r)
	}
	return out
}

// sortRuns orders runs by the sort key in the given direction, breaking ties on
// session_id ascending for deterministic paging (API.md §3.1).
func sortRuns(runs []Run, key, order string) {
	sort.SliceStable(runs, func(i, j int) bool {
		c := cmpRuns(runs[i], runs[j], key)
		if c == 0 {
			return runs[i].SessionID < runs[j].SessionID
		}
		if order == "asc" {
			return c < 0
		}
		return c > 0
	})
}

func cmpRuns(a, b Run, key string) int {
	switch key {
	case "score":
		return cmpFloat(scoreSortVal(a), scoreSortVal(b))
	case "iteration_count":
		return cmpFloat(float64(a.IterationCount), float64(b.IterationCount))
	case "session_id":
		return strings.Compare(a.SessionID, b.SessionID)
	default: // last_update
		return cmpFloat(updateSortVal(a), updateSortVal(b))
	}
}

// scoreSortVal ranks an unscored run below any scored one.
func scoreSortVal(r Run) float64 {
	if r.Score == nil {
		return -1
	}
	return *r.Score
}

// updateSortVal ranks a run with no resolvable mtime oldest.
func updateSortVal(r Run) float64 {
	if r.LastUpdate == nil {
		return 0
	}
	t, err := time.Parse(time.RFC3339, *r.LastUpdate)
	if err != nil {
		return 0
	}
	return float64(t.Unix())
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func paginate(runs []Run, limit, offset int) []Run {
	if offset >= len(runs) {
		return []Run{}
	}
	end := offset + limit
	if end > len(runs) {
		end = len(runs)
	}
	return runs[offset:end]
}

func normSchemaVersion(v int) int {
	if v == 1 || v == 2 {
		return v
	}
	return 2
}

func iterFileName(n int) string             { return "iter-" + strconv.Itoa(n) + ".yaml" }
func iterScoreFileName(n int) string        { return "iter-" + strconv.Itoa(n) + ".score.yaml" }
func sessionScoreFileName(id string) string { return "session-" + id + ".score.yaml" }

func rfc3339Ptr(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }
