package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/credstore"
	"github.com/AGOrcha/dot-agents/internal/events"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// fakePRSourceLister is a hermetic prSourceLister for tests — no producer
// cycle, gh binary, or network.
type fakePRSourceLister struct {
	prs []openPR
	err error
}

func (f fakePRSourceLister) ListOpenPRs(string) ([]openPR, error) { return f.prs, f.err }

// fakeFetcher is a hermetic events.Fetcher: it returns canned bytes (or an
// error) regardless of the fetch spec, so the producer-backed lister runs with
// no exec/http I/O.
type fakeFetcher struct {
	out []byte
	err error
}

func (f fakeFetcher) Fetch(context.Context, events.FetchSpec) ([]byte, error) {
	return f.out, f.err
}

func TestQualifyDepID(t *testing.T) {
	if got := qualifyDepID("t1", "planA"); got != "planA/t1" {
		t.Fatalf("intra-plan qualify = %q", got)
	}
	if got := qualifyDepID("planB/t2", "planA"); got != "planB/t2" {
		t.Fatalf("cross-plan qualify must be unchanged = %q", got)
	}
}

func TestSplitQualifiedDep(t *testing.T) {
	p, tk := splitQualifiedDep("planA/t1")
	if p != "planA" || tk != "t1" {
		t.Fatalf("split = %q/%q", p, tk)
	}
	p, tk = splitQualifiedDep("bareid")
	if p != "" || tk != "bareid" {
		t.Fatalf("split no-slash = %q/%q", p, tk)
	}
}

func TestResolveBaseNoDepsIsMaster(t *testing.T) {
	res, err := resolveBase(baseResolutionInput{TaskID: "t", PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster || res.BasePR != 0 || res.Lineage != nil {
		t.Fatalf("expected bare master, got %+v", res)
	}
}

func TestResolveBaseAllDepsMergedIsMaster(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "completed"},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("merged dep should base off master, got %q", res.BaseBranch)
	}
}

func TestResolveBaseSingleAwaitingDep(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/d1" || res.BasePR != 205 || res.BaseTask != "p/d1" {
		t.Fatalf("single-dep base wrong: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "p/d1" {
		t.Fatalf("expected lineage certificate, got %+v", res.Lineage)
	}
}

func TestResolveBaseSingleAwaitingSubStatus(t *testing.T) {
	for _, status := range []string{"awaiting_agent_review", "awaiting_owner_review"} {
		in := baseResolutionInput{
			TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
			InFlight: map[string]inFlightTask{
				"p/d1": {Status: status, PRBranch: "feature/d1", PRNumber: 9},
			},
		}
		res, err := resolveBase(in)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if res.BaseBranch != "feature/d1" {
			t.Fatalf("%s: base = %q", status, res.BaseBranch)
		}
	}
}

func TestResolveBaseMultiDepSameBranch(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1", "d2"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/shared", PRNumber: 1},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/shared", PRNumber: 1},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("same-branch multi-dep should not conflict: %v", err)
	}
	if res.BaseBranch != "feature/shared" {
		t.Fatalf("base = %q", res.BaseBranch)
	}
}

func TestResolveBaseMultiDepDistinctBranchesRefuses(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d2", "d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/d2", PRNumber: 206},
		},
	}
	_, err := resolveBase(in)
	var conflict *multiDepConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected multiDepConflict, got %v", err)
	}
	// Conflict set must be sorted and name both deps.
	if got := conflict.conflictTasks; len(got) != 2 || got[0] != "p/d1" || got[1] != "p/d2" {
		t.Fatalf("conflict set = %+v", got)
	}
	if !strings.Contains(conflict.Error(), "--base-branch") {
		t.Fatalf("error should prompt --base-branch: %q", conflict.Error())
	}
}

func TestResolveBaseExplicitBaseShortCircuits(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1", "d2"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/d2", PRNumber: 206},
		},
		ExplicitBase: "feature/d2",
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("explicit base must override conflict: %v", err)
	}
	if res.BaseBranch != "feature/d2" || res.BasePR != 206 || res.BaseTask != "p/d2" {
		t.Fatalf("explicit base resolution wrong: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "p/d2" {
		t.Fatalf("explicit lineage = %+v", res.Lineage)
	}
}

func TestResolveBaseExplicitBaseUnknownBranch(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
		},
		ExplicitBase: "feature/manual-merge",
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/manual-merge" || res.BasePR != 0 || res.BaseTask != "" {
		t.Fatalf("unknown explicit branch should carry no PR: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "" {
		t.Fatalf("lineage should record manual rationale, got %+v", res.Lineage)
	}
}

func TestResolveBaseSkipsDepWithoutBranch(t *testing.T) {
	// Dep is awaiting_review but has no PR branch (the producer found no open
	// PR) — it must not count as an in-flight base, so resolution falls back to
	// master.
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "", PRNumber: 0},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("branchless awaiting dep should fall back to master, got %q", res.BaseBranch)
	}
}

func TestBuildInFlightMapJoinsPRs(t *testing.T) {
	depStatus := map[string]string{"p/d1": "awaiting_review", "p/d2": "in_progress"}
	depBranch := map[string]string{"p/d1": "feature/d1", "p/d2": "feature/d2"}
	openPRs := []openPR{{Number: 205, Branch: "feature/d1"}}
	got := buildInFlightMap(depStatus, depBranch, openPRs)

	if f := got["p/d1"]; f.PRBranch != "feature/d1" || f.PRNumber != 205 {
		t.Fatalf("d1 join = %+v", f)
	}
	// d2 has a branch name but no open PR → no PR metadata attached.
	if f := got["p/d2"]; f.PRBranch != "" || f.PRNumber != 0 || f.Status != "in_progress" {
		t.Fatalf("d2 should have no PR metadata: %+v", f)
	}
}

func TestBuildInFlightMapIgnoresBlankPRHead(t *testing.T) {
	got := buildInFlightMap(
		map[string]string{"p/d1": "awaiting_review"},
		map[string]string{"p/d1": "feature/d1"},
		[]openPR{{Number: 5, Branch: ""}},
	)
	if f := got["p/d1"]; f.PRNumber != 0 {
		t.Fatalf("blank PR head must not match: %+v", f)
	}
}

// ─── producer-backed open-PR source (the gh-decoupled seam) ──────────────────

func TestOpenPRsFromEnvelopesProjectsPRKinds(t *testing.T) {
	prOpened, err := events.NewEnvelope(
		events.KindPROpened, "github", "205", time.Now(),
		[]byte(`{"number":205,"branch":"feature/d1"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	// A non-PR envelope must be ignored, not decoded.
	other, err := events.NewEnvelope(
		"event.metric.cpu", "src", "k", time.Now(), []byte(`{"number":9,"branch":"nope"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openPRsFromEnvelopes([]events.Envelope{prOpened, other})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 205 || got[0].Branch != "feature/d1" {
		t.Fatalf("projected open PRs = %+v", got)
	}
}

func TestOpenPRsFromEnvelopesDropsEmptyEntry(t *testing.T) {
	// An event.pr.* envelope with neither number nor branch cannot back a base
	// and must be dropped.
	env, err := events.NewEnvelope(
		events.KindPROpened, "github", "k", time.Now(), []byte(`{"number":0,"branch":""}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openPRsFromEnvelopes([]events.Envelope{env})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty PR entry must be dropped, got %+v", got)
	}
}

func TestOpenPRsFromEnvelopesDecodeError(t *testing.T) {
	// A malformed PR payload must fail loud, not silently resolve to master.
	env := events.Envelope{
		Type:           events.KindPROpened,
		Source:         "github",
		IdempotencyKey: "bad",
		Payload:        []byte(`{"number":"not-a-number"}`),
	}
	if _, err := openPRsFromEnvelopes([]events.Envelope{env}); err == nil {
		t.Fatal("expected decode error on malformed PR payload")
	}
}

func TestProducerPRSourceListerProjectsGHShape(t *testing.T) {
	// The default `gh` pr_source config interpreted by the generic engine: a gh
	// `pr list --json` document maps onto canonical openPR entries with no
	// gh-specific code in the resolver. The default gh map projects
	// statusCheckRollup (the producer's rollup derive consumes it), so the
	// fixture carries that field exactly as real `gh pr list --json` emits it.
	doc := `[{"number":205,"title":"t","headRefName":"feature/d1","baseRefName":"master","state":"OPEN","mergeable":"MERGEABLE","url":"https://x/205","statusCheckRollup":[]}]`
	lister := producerPRSourceLister{
		source:  events.DefaultGHPRSource(),
		fetcher: fakeFetcher{out: []byte(doc)},
	}
	got, err := lister.ListOpenPRs("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 205 || got[0].Branch != "feature/d1" {
		t.Fatalf("gh-shape projection = %+v", got)
	}
}

func TestProducerPRSourceListerNonGitHubShapeZeroGo(t *testing.T) {
	// Done-criterion 1: adding a non-GitHub PR source is a pr_source config
	// block (exec/http + field map) with NO Go change. A GitLab-like JSON shape
	// maps onto the same canonical openPR via config alone.
	gitlab := events.PRSourceConfig{
		Producer: "exec",
		List: events.FetchBlock{
			Argv: []string{"glab", "mr", "list", "--json"},
			Each: ".",
			Map: map[string]string{
				"number": ".iid",
				"branch": ".source_branch",
			},
		},
	}
	doc := `[{"iid":42,"source_branch":"feature/gl-1"}]`
	lister := producerPRSourceLister{source: gitlab, fetcher: fakeFetcher{out: []byte(doc)}}
	got, err := lister.ListOpenPRs("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 42 || got[0].Branch != "feature/gl-1" {
		t.Fatalf("non-github shape projection = %+v", got)
	}
}

func TestProducerPRSourceListerFetchError(t *testing.T) {
	lister := producerPRSourceLister{
		source:  events.DefaultGHPRSource(),
		fetcher: fakeFetcher{err: errors.New("gh not installed")},
	}
	if _, err := lister.ListOpenPRs("x"); err == nil {
		t.Fatal("expected producer-cycle error to propagate")
	}
}

func TestProducerPRSourceListerInvalidConfig(t *testing.T) {
	// An empty list map yields no valid producer config — the lister must error
	// rather than panic.
	lister := producerPRSourceLister{
		source:  events.PRSourceConfig{Producer: "gh", List: events.FetchBlock{Argv: []string{"x"}}},
		fetcher: fakeFetcher{out: []byte(`[]`)},
	}
	if _, err := lister.ListOpenPRs("x"); err == nil {
		t.Fatal("expected invalid pr_source config error")
	}
}

func TestNewProducerPRSourceListerUsesGHDefault(t *testing.T) {
	l := newProducerPRSourceLister()
	if l.source.Producer != "gh" {
		t.Fatalf("production lister should default to gh, got %q", l.source.Producer)
	}
	// The fetcher must be the auth-aware DefaultFetcher (D5 seam wired into the
	// live path), not a nil fetcher that would issue unauthenticated http
	// requests. Exec (`gh`) fetches bypass the client; http fetches flow through
	// the AuthRoundTripper.
	df, ok := l.fetcher.(events.DefaultFetcher)
	if !ok {
		t.Fatalf("production lister fetcher should be events.DefaultFetcher, got %T", l.fetcher)
	}
	if df.Client == nil {
		t.Fatal("production fetcher must carry an http.Client so the auth seam is reachable")
	}
	if _, ok := df.Client.Transport.(*events.AuthRoundTripper); !ok {
		t.Fatalf("production fetcher client transport must be *events.AuthRoundTripper (D5 seam wired), got %T", df.Client.Transport)
	}
}

// TestNewPRAuthRoundTripperWiresSeam proves the production AuthRoundTripper is
// built from the DA_SERVICE_PROXY env (proxy path) with the direct-load fallback
// loader attached — i.e. the seam is configured, not left empty.
func TestNewPRAuthRoundTripperWiresSeam(t *testing.T) {
	t.Setenv(daServiceProxyEnv, "http://localhost:8765")
	rt := newPRAuthRoundTripper()
	if rt.ProxyBase != "http://localhost:8765" {
		t.Fatalf("proxy base from env = %q", rt.ProxyBase)
	}
	if rt.Loader == nil {
		t.Fatal("direct-load fallback loader must be attached even on the proxy path")
	}
}

func TestNewPRAuthRoundTripperFallsBackWhenNoProxy(t *testing.T) {
	t.Setenv(daServiceProxyEnv, "")
	rt := newPRAuthRoundTripper()
	if rt.ProxyBase != "" {
		t.Fatalf("absent proxy env must select direct-load fallback (empty ProxyBase), got %q", rt.ProxyBase)
	}
	if rt.Loader == nil {
		t.Fatal("fallback path requires a credential loader")
	}
}

// TestCredstoreHostLoaderMissIsUnauthenticated proves a not-found credential is a
// clean miss (empty token, nil error) so the request proceeds unauthenticated
// rather than failing the base-resolution cycle.
func TestCredstoreHostLoaderMissIsUnauthenticated(t *testing.T) {
	// Hermetic loader: no env credential, no plaintext file, no keyring → miss.
	t.Setenv("DA_CREDENTIAL_EXAMPLE_COM", "")
	loader := credstore.NewLoader(credstore.WithKeyring(nil), credstore.WithStorePath(filepath.Join(t.TempDir(), "absent.json")))
	got := credstoreHostLoader(loader)
	token, err := got("example.com")
	if err != nil {
		t.Fatalf("not-found credential must be a clean miss, got err %v", err)
	}
	if token != "" {
		t.Fatalf("miss must yield empty token, got %q", token)
	}
	// Empty host short-circuits to no-auth.
	if token, err := got(""); err != nil || token != "" {
		t.Fatalf("empty host = (%q, %v)", token, err)
	}
}

// hardErrResolver is a credstore.OIDCResolver that fails with a non-not-found
// error, exercising the hard-error propagation path of credstoreHostLoader.
type hardErrResolver struct{ err error }

func (r hardErrResolver) Resolve(string) (string, error) { return "", r.err }

// TestCredstoreHostLoaderPropagatesHardError proves a hard resolver error (not a
// clean not-found miss) surfaces, so a real auth failure is not silently swallowed
// into an unauthenticated request.
func TestCredstoreHostLoaderPropagatesHardError(t *testing.T) {
	boom := errors.New("vault unreachable")
	loader := credstore.NewLoader(
		credstore.WithKeyring(nil),
		credstore.WithStorePath(filepath.Join(t.TempDir(), "absent.json")),
		credstore.WithResolver(hardErrResolver{err: boom}),
	)
	got := credstoreHostLoader(loader)
	if _, err := got("example.com"); !errors.Is(err, boom) {
		t.Fatalf("hard resolver error must propagate, got %v", err)
	}
}

// TestCredstoreHostLoaderResolvesToken proves the happy path: a host with a
// configured credential yields a bearer token for the AuthRoundTripper to attach.
func TestCredstoreHostLoaderResolvesToken(t *testing.T) {
	t.Setenv("DA_CREDENTIAL_API_GITHUB_COM", "tok-123")
	loader := credstore.NewLoader(credstore.WithKeyring(nil), credstore.WithStorePath(filepath.Join(t.TempDir(), "absent.json")))
	got := credstoreHostLoader(loader)
	token, err := got("api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "tok-123" {
		t.Fatalf("resolved token = %q", token)
	}
}

// TestBranchMatchesTaskUppercaseBoundary covers an uppercase-alnum neighbour so
// the bound rejects "T1" extended by an uppercase letter ("T1B"), and accepts it
// when bounded — exercising the uppercase arm of isBranchWordByte.
func TestBranchMatchesTaskUppercaseBoundary(t *testing.T) {
	if branchMatchesTask("feature/T1B", "T1") {
		t.Fatal("uppercase-extended token must not match")
	}
	if !branchMatchesTask("feature/T1", "T1") {
		t.Fatal("uppercase id bounded at string end must match")
	}
}

// TestAuthAwareFetcherExecBypassesTransport proves the wired seam does not break
// the default `gh` exec path: an exec fetch returns its stdout unchanged, the
// HTTP transport (and thus the auth seam) is never consulted.
func TestAuthAwareFetcherExecBypassesTransport(t *testing.T) {
	f := newAuthAwareFetcher()
	out, err := f.Fetch(context.Background(), events.FetchSpec{Argv: []string{"printf", "[]"}})
	if err != nil {
		t.Fatalf("exec fetch via auth-aware fetcher: %v", err)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("exec fetch output = %q", out)
	}
}

// ─── §4.2 bundle scope injection ─────────────────────────────────────────────

func baseTestBundle() *delegationBundleYAML {
	b := &delegationBundleYAML{SchemaVersion: 1, DelegationID: "del-x", PlanID: "p", TaskID: "t"}
	b.Scope.WriteScope = []string{"a.go"}
	return b
}

func TestMarshalBundleWithBaseInjectsScopeFields(t *testing.T) {
	res := &baseResolution{
		BaseBranch: "feature/d1",
		BasePR:     205,
		BaseTask:   "p/d1",
		Lineage:    &lineageCertificate{SourceTasks: []string{"p/d1"}, SelectedTask: "p/d1", Rationale: "x"},
	}
	data, err := marshalBundleWithBase(baseTestBundle(), res)
	if err != nil {
		t.Fatal(err)
	}
	var rt map[string]any
	if err := yaml.Unmarshal(data, &rt); err != nil {
		t.Fatal(err)
	}
	scope, ok := rt["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope not a mapping: %T", rt["scope"])
	}
	if scope["base_branch"] != "feature/d1" || scope["base_pr"] != 205 || scope["base_task"] != "p/d1" {
		t.Fatalf("scope base fields = %+v", scope)
	}
	if scope["lineage"] == nil {
		t.Fatalf("expected lineage in scope: %+v", scope)
	}
	// write_scope must survive the injection.
	if ws, _ := scope["write_scope"].([]any); len(ws) != 1 || ws[0] != "a.go" {
		t.Fatalf("write_scope clobbered: %+v", scope["write_scope"])
	}
}

func TestMarshalBundleWithBasePlainMasterUnchanged(t *testing.T) {
	plain, err := yamlMarshal(baseTestBundle())
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: baseRefMaster})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("bare master must not augment scope:\n%s", got)
	}
	// nil resolution is also unchanged.
	gotNil, err := marshalBundleWithBase(baseTestBundle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotNil) != string(plain) {
		t.Fatalf("nil resolution must not augment scope:\n%s", gotNil)
	}
}

func TestMarshalBundleWithBaseMasterWithPRIsLayered(t *testing.T) {
	// A master base that still carries a PR/lineage is recorded (observability).
	res := &baseResolution{BaseBranch: baseRefMaster, BasePR: 7}
	data, err := marshalBundleWithBase(baseTestBundle(), res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "base_pr: 7") {
		t.Fatalf("master+PR should record base_pr:\n%s", data)
	}
}

func TestMarshalBundleWithBaseMarshalError(t *testing.T) {
	prev := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected marshal error to propagate")
	}
}

func TestMarshalBundleWithBaseReparseError(t *testing.T) {
	// First yamlMarshal call (struct → bytes) succeeds with bytes that are not
	// a valid YAML mapping, forcing the re-parse/injection path to fail.
	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("\tnot: [valid"), nil
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected re-parse error")
	}
}

func TestMarshalBundleWithBaseNonMappingRoot(t *testing.T) {
	prev := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return []byte("- just\n- a\n- list\n"), nil }
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected non-mapping root error")
	}
}

func TestBaseResolutionIsLayered(t *testing.T) {
	cases := []struct {
		name string
		res  *baseResolution
		want bool
	}{
		{"nil", nil, false},
		{"empty-branch", &baseResolution{}, false},
		{"bare-master", &baseResolution{BaseBranch: baseRefMaster}, false},
		{"master-with-pr", &baseResolution{BaseBranch: baseRefMaster, BasePR: 3}, true},
		{"master-with-lineage", &baseResolution{BaseBranch: baseRefMaster, Lineage: &lineageCertificate{}}, true},
		{"feature-branch", &baseResolution{BaseBranch: "feature/x"}, true},
	}
	for _, c := range cases {
		if got := baseResolutionIsLayered(c.res); got != c.want {
			t.Fatalf("%s: layered = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReplaceMappingValueErrors(t *testing.T) {
	scope := bundleScopeWithBase{WriteScope: []string{"a.go"}}
	// Non-mapping root.
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	if err := replaceMappingValue(scalar, "scope", scope); err == nil {
		t.Fatal("expected non-mapping error")
	}
	// Missing key.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("foo: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if err := replaceMappingValue(&doc, "scope", scope); err == nil {
		t.Fatal("expected missing-key error")
	}
	// Happy path: existing key is replaced.
	var ok yaml.Node
	if err := yaml.Unmarshal([]byte("scope: {}\n"), &ok); err != nil {
		t.Fatal(err)
	}
	if err := replaceMappingValue(&ok, "scope", scope); err != nil {
		t.Fatalf("replace existing key: %v", err)
	}
}

func TestDocumentMappingShapes(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if documentMapping(&doc) == nil {
		t.Fatal("expected mapping for document node")
	}
	if documentMapping(&yaml.Node{Kind: yaml.ScalarNode}) != nil {
		t.Fatal("scalar must not be a mapping")
	}
}

// ─── delegation.go fanout base-resolution integration ────────────────────────

func TestDepBranchForTask(t *testing.T) {
	prs := []openPR{
		{Number: 205, Branch: "feature/config-v2-d1"},
		{Number: 206, Branch: ""},
	}
	if got := depBranchForTask("d1", prs); got != "feature/config-v2-d1" {
		t.Fatalf("matched branch = %q", got)
	}
	if got := depBranchForTask("nope", prs); got != "" {
		t.Fatalf("no-match should be empty, got %q", got)
	}
}

// TestDepBranchForTaskBoundedMatch locks in the fix for the unbounded-substring
// hazard: a short task id must NOT resolve to a sibling's PR branch just because
// the id appears as a substring (e.g. "t1" inside "feature/t10"). Matching is
// bounded by token edges, so the wrong base can never be silently selected.
func TestDepBranchForTaskBoundedMatch(t *testing.T) {
	cases := []struct {
		name   string
		taskID string
		prs    []openPR
		want   string
	}{
		{
			name:   "suffix segment matches",
			taskID: "d1",
			prs:    []openPR{{Number: 1, Branch: "feature/config-v2-d1"}},
			want:   "feature/config-v2-d1",
		},
		{
			name:   "exact branch equals id",
			taskID: "feature/d1",
			prs:    []openPR{{Number: 1, Branch: "feature/d1"}},
			want:   "feature/d1",
		},
		{
			name:   "hyphenated task id matches whole token",
			taskID: "lpf-d-base-resolution",
			prs:    []openPR{{Number: 1, Branch: "feature/lpf-d-base-resolution"}},
			want:   "feature/lpf-d-base-resolution",
		},
		{
			name:   "trailing digit must NOT match (t1 vs t10)",
			taskID: "t1",
			prs:    []openPR{{Number: 1, Branch: "feature/t10"}},
			want:   "",
		},
		{
			name:   "trailing alpha must NOT match (t1 vs t1b)",
			taskID: "t1",
			prs:    []openPR{{Number: 1, Branch: "feature/lpf-t1b"}},
			want:   "",
		},
		{
			name:   "leading alnum must NOT match (task-2 inside subtask-2)",
			taskID: "task-2",
			prs:    []openPR{{Number: 1, Branch: "feature/subtask-2"}},
			want:   "",
		},
		{
			name:   "prefers the correctly-bounded PR over a substring sibling",
			taskID: "t1",
			prs: []openPR{
				{Number: 1, Branch: "feature/t10-other"},
				{Number: 2, Branch: "feature/t1"},
			},
			want: "feature/t1",
		},
		{
			name:   "empty task id never matches",
			taskID: "",
			prs:    []openPR{{Number: 1, Branch: "feature/anything"}},
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := depBranchForTask(c.taskID, c.prs); got != c.want {
				t.Fatalf("depBranchForTask(%q) = %q, want %q", c.taskID, got, c.want)
			}
		})
	}
}

func TestCollectDepStatusAndBranch(t *testing.T) {
	repo := setupTestProject(t)
	// Mark task-002 awaiting_review so it is a candidate dep.
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	openPRs := []openPR{{Number: 206, Branch: "feature/task-002"}}

	status, branch := collectDepStatusAndBranch(repo, "plan-001", []string{"task-002", "missing"}, openPRs)
	if status["plan-001/task-002"] != "awaiting_review" {
		t.Fatalf("dep status = %+v", status)
	}
	if branch["plan-001/task-002"] != "feature/task-002" {
		t.Fatalf("dep branch = %+v", branch)
	}
	// Unknown dep must be absent (no status entry → not in-flight).
	if _, ok := status["plan-001/missing"]; ok {
		t.Fatalf("missing dep should not be recorded: %+v", status)
	}
}

func TestCollectDepStatusAndBranchUnloadablePlan(t *testing.T) {
	repo := setupTestProject(t)
	// Cross-plan dep into a plan that does not exist → loadErr branch; the dep
	// is silently skipped (treated as not-in-flight).
	status, branch := collectDepStatusAndBranch(repo, "plan-001", []string{"ghost-plan/x"}, nil)
	if len(status) != 0 || len(branch) != 0 {
		t.Fatalf("unloadable plan dep must be skipped: %+v / %+v", status, branch)
	}
}

func TestResolveFanoutBaseSingleDep(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	lister := fakePRSourceLister{prs: []openPR{{Number: 206, Branch: "feature/task-002"}}}

	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/task-002" || res.BasePR != 206 || res.BaseTask != "plan-001/task-002" {
		t.Fatalf("resolved base = %+v", res)
	}
}

func TestResolveFanoutBaseProducerErrorFallsBackToMaster(t *testing.T) {
	repo := setupTestProject(t)
	lister := fakePRSourceLister{err: errors.New("pr source unavailable")}
	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("producer error should fall back to master, got %q", res.BaseBranch)
	}
}

func TestResolveFanoutBaseProducerErrorHonorsExplicitBase(t *testing.T) {
	repo := setupTestProject(t)
	lister := fakePRSourceLister{err: errors.New("pr source down")}
	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "feature/manual", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/manual" {
		t.Fatalf("explicit base must survive producer error, got %q", res.BaseBranch)
	}
}

func TestResolveFanoutBaseMultiDepConflict(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	lister := fakePRSourceLister{prs: []openPR{
		{Number: 205, Branch: "feature/task-001"},
		{Number: 206, Branch: "feature/task-002"},
	}}
	_, err := resolveFanoutBase(repo, "plan-001", "task-new", []string{"task-001", "task-002"}, "", lister)
	var conflict *multiDepConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected multiDepConflict, got %v", err)
	}
}

func newBaseFlagCmd(t *testing.T, base string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "fanout"}
	cmd.Flags().String("base-branch", "", "")
	if base != "" {
		if err := cmd.Flags().Set("base-branch", base); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// TestFanoutCmdRegistersBaseBranchFlag locks in finding-3: the --base-branch flag
// fanoutResolveBase reads MUST be registered on the real fanout command, else the
// CLI override path is unreachable and GetString silently returns "". This drives
// the flag off the actual command builder, not a synthetic test command.
func TestFanoutCmdRegistersBaseBranchFlag(t *testing.T) {
	cmd := newWorkflowFanoutCmd()
	flag := cmd.Flags().Lookup("base-branch")
	if flag == nil {
		t.Fatal("fanout command must register --base-branch (fanoutResolveBase reads it)")
	}
	// The override must be readable through the same accessor fanoutResolveBase
	// uses, proving the registration actually feeds the resolution path.
	if err := cmd.Flags().Set("base-branch", "feature/manual"); err != nil {
		t.Fatalf("set --base-branch: %v", err)
	}
	if got, _ := cmd.Flags().GetString("base-branch"); got != "feature/manual" {
		t.Fatalf("registered flag must be readable as set, got %q", got)
	}
}

// TestFanoutResolveBaseReadsRegisteredFlag exercises the real command's flag end
// to end: a --base-branch override set on newWorkflowFanoutCmd() reaches
// fanoutResolveBase and short-circuits resolution to that base. Before the flag
// was registered this path was dead (the override was silently dropped).
func TestFanoutResolveBaseReadsRegisteredFlag(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultPRSourceLister
	defaultPRSourceLister = fakePRSourceLister{prs: []openPR{
		{Number: 205, Branch: "feature/task-001"},
		{Number: 206, Branch: "feature/task-002"},
	}}
	t.Cleanup(func() { defaultPRSourceLister = prev })

	cmd := newWorkflowFanoutCmd()
	if err := cmd.Flags().Set("base-branch", "feature/task-001"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	// Two distinct in-flight deps would otherwise conflict; the registered flag
	// override sequences them.
	res, err := fanoutResolveBase(cmd, repo, "plan-001", "task-new", []string{"task-001", "task-002"})
	if err != nil {
		t.Fatalf("registered --base-branch must override conflict: %v", err)
	}
	if res.BaseBranch != "feature/task-001" || res.BasePR != 205 {
		t.Fatalf("flag-driven resolution = %+v", res)
	}
}

func TestFanoutResolveBaseSingleDep(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultPRSourceLister
	defaultPRSourceLister = fakePRSourceLister{prs: []openPR{{Number: 206, Branch: "feature/task-002"}}}
	t.Cleanup(func() { defaultPRSourceLister = prev })

	res, err := fanoutResolveBase(newBaseFlagCmd(t, ""), repo, "plan-001", "task-001", []string{"task-002"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/task-002" {
		t.Fatalf("base = %q", res.BaseBranch)
	}
}

func TestFanoutResolveBaseConflictRefuses(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultPRSourceLister
	defaultPRSourceLister = fakePRSourceLister{prs: []openPR{
		{Number: 205, Branch: "feature/task-001"},
		{Number: 206, Branch: "feature/task-002"},
	}}
	t.Cleanup(func() { defaultPRSourceLister = prev })

	_, err := fanoutResolveBase(newBaseFlagCmd(t, ""), repo, "plan-001", "task-new", []string{"task-001", "task-002"})
	if err == nil || !strings.Contains(err.Error(), "base-branch resolution refused") {
		t.Fatalf("expected refusal error, got %v", err)
	}
}

func TestFanoutResolveBaseExplicitFlagOverridesConflict(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultPRSourceLister
	defaultPRSourceLister = fakePRSourceLister{prs: []openPR{
		{Number: 205, Branch: "feature/task-001"},
		{Number: 206, Branch: "feature/task-002"},
	}}
	t.Cleanup(func() { defaultPRSourceLister = prev })

	res, err := fanoutResolveBase(newBaseFlagCmd(t, "feature/task-001"), repo, "plan-001", "task-new", []string{"task-001", "task-002"})
	if err != nil {
		t.Fatalf("explicit flag must override conflict: %v", err)
	}
	if res.BaseBranch != "feature/task-001" || res.BasePR != 205 {
		t.Fatalf("explicit resolution = %+v", res)
	}
}

func TestFanoutBaseSummary(t *testing.T) {
	withPR := baseResolution{BaseBranch: "feature/d1", BasePR: 205, BaseTask: "p/d1"}
	if got := fanoutBaseSummary(withPR); got != "feature/d1 (PR #205, from p/d1)" {
		t.Fatalf("summary = %q", got)
	}
	bare := baseResolution{BaseBranch: baseRefMaster}
	if got := fanoutBaseSummary(bare); got != baseRefMaster {
		t.Fatalf("bare summary = %q", got)
	}
}

func TestSaveDelegationBundleWithBase(t *testing.T) {
	repo := t.TempDir()
	res := &baseResolution{BaseBranch: "feature/d1", BasePR: 205, BaseTask: "p/d1"}
	if err := saveDelegationBundleWithBase(repo, baseTestBundle(), res); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", "del-x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "base_branch: feature/d1") {
		t.Fatalf("persisted bundle missing base_branch:\n%s", data)
	}
}

func TestSaveDelegationBundleWithBaseEmptyID(t *testing.T) {
	b := &delegationBundleYAML{}
	if err := saveDelegationBundleWithBase(t.TempDir(), b, nil); err == nil {
		t.Fatal("expected empty delegation_id error")
	}
}

func TestSaveDelegationBundleWithBaseMkdirError(t *testing.T) {
	prev := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	t.Cleanup(func() { osMkdirAll = prev })
	if err := saveDelegationBundleWithBase(t.TempDir(), baseTestBundle(), nil); err == nil {
		t.Fatal("expected mkdir error to propagate")
	}
}

func TestPersistFanoutBundleWithBaseSaveError(t *testing.T) {
	prev := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	t.Cleanup(func() { osMkdirAll = prev })
	contract := &DelegationContract{ParentTaskID: "t"}
	err := persistFanoutBundleWithBase(t.TempDir(), contract, baseTestBundle(), nil)
	if err == nil || !strings.Contains(err.Error(), "save delegation bundle") {
		t.Fatalf("expected wrapped save error, got %v", err)
	}
}
