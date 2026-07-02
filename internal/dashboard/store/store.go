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

// Evict drops the cached snapshot for one root (broker per-root push hook).
func (s *DiskStore) Evict(root string) { s.cache.evict(rootKey(root)) }

// EvictAll drops every cached snapshot (broker whole-cache push hook).
func (s *DiskStore) EvictAll() { s.cache.clear() }

func rootKey(root string) string { return "root:" + root }

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

// snapshot returns the parsed view of root, served from cache while the root's
// directory fingerprint is unchanged. The fingerprint covers EVERY file's name
// and mtime — not just the newest — so a backfilled score sidecar on an old
// iteration, a same-timestamp rewrite with a preserved (backdated) mtime, or a
// deletion of a non-newest file all invalidate, where a max-mtime key would
// serve stale data.
func (s *DiskStore) snapshot(root string) rootSnapshot {
	mtimes, newest, fp := readDirState(root)
	if v, ok := s.cache.get(rootKey(root), fp); ok {
		return v.(rootSnapshot)
	}
	snap := s.loadRoot(root, mtimes)
	snap.newestMtime = newest
	s.cache.put(rootKey(root), snap, fp)
	return snap
}

// readDirState lists root once, returning per-file mtimes, the newest mtime
// (for last_update / health), and an FNV-1a fingerprint over every (name,
// mtime) pair. os.ReadDir returns entries sorted by filename, so the hash is
// deterministic. A missing/unreadable root yields an empty map, the zero time,
// and a zero fingerprint.
func readDirState(root string) (map[string]time.Time, time.Time, int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return map[string]time.Time{}, time.Time{}, 0
	}
	m := make(map[string]time.Time, len(entries))
	var newest time.Time
	h := fnv.New64a()
	var buf [8]byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		m[e.Name()] = mt
		if mt.After(newest) {
			newest = mt
		}
		_, _ = h.Write([]byte(e.Name()))
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

// sessions groups every addressable session across all roots. Records with an
// empty session id are unaddressable (no sidecar can be named) and are skipped,
// matching internal/scoring.AggregateSessions. A session id colliding across
// roots keeps the first root and warns (API.md §1.6: session ids are globally
// unique).
func (s *DiskStore) sessions() map[string]sessionCtx {
	out := map[string]sessionCtx{}
	for _, root := range s.roots {
		snap := s.snapshot(root)
		groups := map[string][]scoring.IterationRecord{}
		for _, rec := range snap.records {
			sid := rec.Agent.SessionID
			if sid == "" {
				continue
			}
			groups[sid] = append(groups[sid], rec)
		}
		for sid, recs := range groups {
			if _, dup := out[sid]; dup {
				s.logger.Warn("dashboard/store: duplicate session id across roots, keeping first",
					"session_id", sid, "root", root)
				continue
			}
			sort.Slice(recs, func(i, j int) bool { return recs[i].Iteration < recs[j].Iteration })
			out[sid] = sessionCtx{root: root, snap: snap, records: recs}
		}
	}
	return out
}

// ListRuns implements Store.
func (s *DiskStore) ListRuns(_ context.Context, f RunFilter) ([]RunSummary, error) {
	f = normalizeFilter(f)
	sess := s.sessions()
	runs := make([]Run, 0, len(sess))
	for _, ctx := range sess {
		runs = append(runs, s.buildRun(ctx, false))
	}
	runs = filterRuns(runs, f)
	sortRuns(runs, f.Sort, f.Order)
	return paginate(runs, f.Limit, f.Offset), nil
}

// GetRun implements Store.
func (s *DiskStore) GetRun(_ context.Context, sessionID string) (RunDetail, error) {
	ctx, ok := s.sessions()[sessionID]
	if !ok {
		return RunDetail{}, ErrNotFound
	}
	return s.buildRun(ctx, true), nil
}

// ListIterations implements Store.
func (s *DiskStore) ListIterations(_ context.Context, sessionID string) ([]IterationSummary, error) {
	ctx, ok := s.sessions()[sessionID]
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
	sess := s.sessions()
	iterCount := 0
	var newest time.Time
	for _, root := range s.roots {
		snap := s.snapshot(root)
		iterCount += len(snap.records)
		if snap.newestMtime.After(newest) {
			newest = snap.newestMtime
		}
	}
	h := Health{
		Status:         "ok",
		RunCount:       len(sess),
		IterationCount: iterCount,
		RubricVersion:  scoring.DefaultRubric().Version,
		Roots:          append([]string(nil), s.roots...),
	}
	if !newest.IsZero() {
		h.LastIterLogMtime = rfc3339Ptr(newest)
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
