package graphstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeMCPBridge struct {
	buildCalls  int
	updateCalls int
	postCalls   int
	statusSeq   []*CRGStatus
	statusIdx   int

	buildErr    error
	updateErr   error
	postErr     error
	statusErr   error
	impactErr   error
	impact      *CRGImpactResult
	detectErr   error
	detect      *CRGChangeReport
	communities *CommunitiesResult
}

func (f *fakeMCPBridge) Build(opts BuildOptions) error {
	f.buildCalls++
	return f.buildErr
}

func (f *fakeMCPBridge) Update(opts UpdateOptions) error {
	f.updateCalls++
	return f.updateErr
}

func (f *fakeMCPBridge) Status() (*CRGStatus, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if len(f.statusSeq) == 0 {
		return &CRGStatus{}, nil
	}
	idx := f.statusIdx
	if idx >= len(f.statusSeq) {
		idx = len(f.statusSeq) - 1
	} else {
		f.statusIdx++
	}
	return f.statusSeq[idx], nil
}

func (f *fakeMCPBridge) GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error) {
	if f.impactErr != nil {
		return nil, f.impactErr
	}
	if f.impact == nil {
		return &CRGImpactResult{}, nil
	}
	return f.impact, nil
}

func (f *fakeMCPBridge) ListFlows(limit int, sortBy string) (*FlowsResult, error) {
	return &FlowsResult{}, nil
}

func (f *fakeMCPBridge) ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error) {
	if f.communities != nil {
		return f.communities, nil
	}
	return &CommunitiesResult{}, nil
}

func (f *fakeMCPBridge) Postprocess(opts PostprocessOptions) error {
	f.postCalls++
	return f.postErr
}

func (f *fakeMCPBridge) PostprocessReport(opts PostprocessOptions) (*CRGOperationReport, error) {
	if err := f.Postprocess(opts); err != nil {
		return nil, err
	}
	return &CRGOperationReport{Status: statusOK, Summary: PostprocessSummary()}, nil
}

func (f *fakeMCPBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	if f.detectErr != nil {
		return nil, f.detectErr
	}
	if f.detect != nil {
		return f.detect, nil
	}
	return &CRGChangeReport{}, nil
}

func runMCPServeOnce(t *testing.T, srv *MCPServer, req string) rpcResponse {
	t.Helper()
	reader, writer := io.Pipe()
	defer reader.Close()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(reader, &out)
	}()

	if _, err := io.WriteString(writer, req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var resp rpcResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v\nraw: %s", err, out.String())
	}
	return resp
}

// runMCPCallExpectErrorCode dispatches req through the server and
// asserts the response carries a non-nil JSON-RPC error with the given
// code. Collapses the duplicated 3-line "resp := runMCPServeOnce; if
// resp.Error == nil || resp.Error.Code != X" pattern present at 7+
// call sites in this file (invalid-params -32602, method-not-found
// -32601, parse-error -32700).
func runMCPCallExpectErrorCode(t *testing.T, srv *MCPServer, req string, wantCode int) {
	t.Helper()
	resp := runMCPServeOnce(t, srv, req)
	if resp.Error == nil || resp.Error.Code != wantCode {
		t.Fatalf("expected error code %d, got %+v", wantCode, resp.Error)
	}
}

// decodeResultMap marshals resp.Result back to JSON and unmarshals it
// into a generic map for assertions. Collapses the duplicated 3-line
// "rb, _ := json.Marshal(resp.Result); var p map[string]any; _ =
// json.Unmarshal(rb, &p)" pattern present at 4 call sites in this file.
func decodeResultMap(t *testing.T, resp rpcResponse) map[string]any {
	t.Helper()
	rb, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(rb, &p); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return p
}

// TestKGServeParseError verifies that malformed JSON returns a -32700 parse
// error response.
func TestKGServeParseError(t *testing.T) {
	srv := &MCPServer{}
	reader, writer := io.Pipe()
	defer reader.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- srv.Serve(reader, &out) }()
	_, _ = io.WriteString(writer, "not json{")
	_ = writer.Close()
	<-done

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var resp rpcResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode parse-error response: %v raw: %s", err, out.String())
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error, got %+v", resp.Error)
	}
}

// TestKGServeNotificationNoResponse verifies a JSON-RPC notification (no id)
// produces no response.
func TestKGServeNotificationNoResponse(t *testing.T) {
	srv := &MCPServer{}
	reader, writer := io.Pipe()
	defer reader.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- srv.Serve(reader, &out) }()
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","method":"tools/list","params":{}}`)
	_ = writer.Close()
	<-done
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected no response for notification, got %q", out.String())
	}
}

// TestBuildRPCResponse_TypedError preserves typed *rpcError on the response.
func TestBuildRPCResponse_TypedError(t *testing.T) {
	id := json.RawMessage(`1`)
	re := &rpcError{Code: -32603, Message: "boom"}
	resp := buildRPCResponse(id, nil, re)
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Errorf("expected typed error preserved, got %+v", resp.Error)
	}
}

// TestBuildRPCResponse_PlainError wraps non-rpcError as code -32603.
func TestBuildRPCResponse_PlainError(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := buildRPCResponse(id, nil, io.EOF)
	if resp.Error == nil || resp.Error.Code != -32603 {
		t.Errorf("expected -32603 for non-rpc error, got %+v", resp.Error)
	}
}

// TestBuildRPCResponse_OK populates result on success.
func TestBuildRPCResponse_OK(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := buildRPCResponse(id, json.RawMessage(`{"ok":true}`), nil)
	if resp.Error != nil {
		t.Errorf("expected nil error, got %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Errorf("expected non-nil result")
	}
}

// TestRPCError_Error covers the *rpcError.Error stringer (nil and populated).
func TestRPCError_Error(t *testing.T) {
	var nilErr *rpcError
	if nilErr.Error() != "" {
		t.Errorf("nil rpcError.Error must be empty")
	}
	e := &rpcError{Message: "boom"}
	if e.Error() != "boom" {
		t.Errorf("got %q", e.Error())
	}
}

// TestDefaultKGHome and TestDefaultGraphstoreDBPath cover the path helpers.
func TestDefaultKGHome_EnvOverride(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/kg-test-home")
	if defaultKGHome() != "/tmp/kg-test-home" {
		t.Errorf("expected env override to win")
	}
}

// TestDefaultKGHome_OverrideBypassesHomeResolution covers the KG_HOME
// override short-circuiting home resolution even when $HOME is unresolvable.
func TestDefaultKGHome_OverrideBypassesHomeResolution(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/kg-test-home")
	t.Setenv("HOME", "")
	if defaultKGHome() != "/tmp/kg-test-home" {
		t.Errorf("expected env override to win")
	}
}

// TestDefaultKGHome_UnresolvableHomeHardFails covers the remediation for
// the UserHomeDir-swallow class (top-risk #2 duplicate site): no KG_HOME
// override and an unresolvable $HOME must hard-fail via defaultKGHomeExit
// instead of silently falling back to a relative "knowledge-graph" path.
func TestDefaultKGHome_UnresolvableHomeHardFails(t *testing.T) {
	t.Setenv("KG_HOME", "")
	t.Setenv("HOME", "")
	// os.UserHomeDir resolves via USERPROFILE (then HOMEDRIVE+HOMEPATH) on
	// Windows, so clear those too to force the failure cross-platform.
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	orig := defaultKGHomeExit
	var gotErr error
	defaultKGHomeExit = func(err error) { gotErr = err }
	defer func() { defaultKGHomeExit = orig }()

	if got := defaultKGHome(); got != "" {
		t.Errorf("expected empty result after hard-fail hook fires, got %q", got)
	}
	if gotErr == nil {
		t.Fatal("expected defaultKGHomeExit to be invoked with a non-nil error")
	}
}

// TestDefaultKGHomeExit_DefaultHookHardFailsAndExits drives the real
// (unstubbed) defaultKGHomeExit hook — the "print + os.Exit(1)" body
// itself, as opposed to TestDefaultKGHome_UnresolvableHomeHardFails above
// which overrides the hook to observe the error without exiting. Since
// os.Exit(1) would kill this test binary, the body runs in a re-exec'd
// child process (the stdlib TestHelperProcess pattern; see
// internal/events/producer_test.go for the precedent in this repo).
func TestDefaultKGHomeExit_DefaultHookHardFailsAndExits(t *testing.T) {
	if os.Getenv("KG_HOME_EXIT_HELPER") == "1" {
		// Child process: defaultKGHomeExit is never overridden here, so this
		// calls straight into the production print+exit body.
		defaultKGHome()
		t.Fatal("defaultKGHome should have exited via defaultKGHomeExit before returning")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDefaultKGHomeExit_DefaultHookHardFailsAndExits$")
	cmd.Env = append(os.Environ(),
		"KG_HOME_EXIT_HELPER=1",
		"KG_HOME=",
		"HOME=",
		"USERPROFILE=",
		"HOMEDRIVE=",
		"HOMEPATH=",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected the child process to exit non-zero via os.Exit(1), got err=%v, stderr=%s", err, stderr.String())
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d (stderr=%s)", exitErr.ExitCode(), stderr.String())
	}
	msg := stderr.String()
	if !strings.Contains(msg, "cannot resolve home directory") || !strings.Contains(msg, "$KG_HOME") {
		t.Errorf("expected actionable message on stderr mentioning $KG_HOME, got: %q", msg)
	}
}

func TestDefaultGraphstoreDBPath(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/kg-test-home")
	p := defaultGraphstoreDBPath()
	if !strings.HasSuffix(p, filepath.Join("ops", "graphstore.db")) {
		t.Errorf("unexpected path: %s", p)
	}
}

// ── CRG internal helpers ─────────────────────────────────────────────────────

func TestParseCRGBuildOutput_FullBuildLine(t *testing.T) {
	out := []byte("INFO: starting\nFull build: 4 files, 16 nodes, 29 edges (postprocess=full)\n")
	rep := parseCRGBuildOutput(out)
	if rep.BuildType != crgBuildTypeFull {
		t.Fatalf("build_type = %q, want %q", rep.BuildType, crgBuildTypeFull)
	}
	if rep.FilesParsed == nil || *rep.FilesParsed != 4 ||
		rep.TotalNodes == nil || *rep.TotalNodes != 16 ||
		rep.TotalEdges == nil || *rep.TotalEdges != 29 {
		t.Fatalf("counters = %+v, want 4/16/29", rep)
	}
	if rep.Summary != FullBuildSummary(4, 16, 29) {
		t.Errorf("summary = %q, want upstream's sentence", rep.Summary)
	}
}

func TestParseCRGBuildOutput_CountsErrors(t *testing.T) {
	rep := parseCRGBuildOutput([]byte("Full build: 1 files, 1 nodes, 0 edges (postprocess=none)\nErrors: 2\n"))
	if rep.Errors == nil || len(*rep.Errors) != 2 {
		t.Fatalf("errors = %v, want two entries", rep.Errors)
	}
}

// TestParseCRGBuildOutput_UnrecognisedOutput pins the fail-loud behaviour: an
// output the parser does not recognise yields NO counters, so a future CLI
// format change is visible rather than silently producing wrong numbers.
func TestParseCRGBuildOutput_UnrecognisedOutput(t *testing.T) {
	rep := parseCRGBuildOutput([]byte("nothing here\n"))
	if rep.TotalNodes != nil || rep.FilesParsed != nil || rep.FilesUpdated != nil {
		t.Fatalf("unrecognised output produced counters: %+v", rep)
	}
	if rep.Summary != "nothing here" {
		t.Errorf("summary = %q, want the raw transcript", rep.Summary)
	}
}

func TestIsCRGBusyLockedError(t *testing.T) {
	if !isCRGBusyLockedError(errFmt("database is locked")) {
		t.Error("should detect 'database is locked'")
	}
	if !isCRGBusyLockedError(errFmt("server busy")) {
		t.Error("should detect 'busy'")
	}
	if isCRGBusyLockedError(nil) {
		t.Error("nil err should be false")
	}
	if isCRGBusyLockedError(errFmt("other error")) {
		t.Error("non-matching err should be false")
	}
}

func TestIsCRGUnbuiltError(t *testing.T) {
	if !isCRGUnbuiltError(errFmt("no such table: nodes")) {
		t.Error("should detect 'no such table'")
	}
	if !isCRGUnbuiltError(errFmt("missing schema")) {
		t.Error("should detect 'missing'")
	}
	if isCRGUnbuiltError(nil) {
		t.Error("nil err should be false")
	}
	if isCRGUnbuiltError(errFmt("other")) {
		t.Error("non-matching err should be false")
	}
}

func TestClassifyCRGRunError_BusyLocked(t *testing.T) {
	got := classifyCRGRunError("build", errFmt("database is locked"), nil)
	if !strings.Contains(got.Error(), "busy or locked") {
		t.Errorf("got %v", got)
	}
}

func TestClassifyCRGRunError_FallbackToOutput(t *testing.T) {
	got := classifyCRGRunError("build", errFmt("boom"), []byte("the real output"))
	if !strings.Contains(got.Error(), "the real output") {
		t.Errorf("got %v", got)
	}
}

func TestClassifyCRGRunError_EmptyOutput(t *testing.T) {
	got := classifyCRGRunError("update", errFmt("boom"), nil)
	if !strings.Contains(got.Error(), "boom") {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeCRGUpdatedAt(t *testing.T) {
	if got := normalizeCRGUpdatedAt("  "); got != "" {
		t.Errorf("blank input must normalize to the empty (JSON null) value: got %q", got)
	}
	if got := normalizeCRGUpdatedAt("2026-04-11T00:49:52"); got != "2026-04-11T00:49:52" {
		t.Errorf("rfc3339-ish passthrough: got %q", got)
	}
	if got := normalizeCRGUpdatedAt("1712797792.5"); !strings.Contains(got, "T") {
		t.Errorf("numeric should normalize, got %q", got)
	}
	if got := normalizeCRGUpdatedAt("garbage"); got != "garbage" {
		t.Errorf("garbage passthrough: got %q", got)
	}
}

func TestApplyCRGStatusError_BusyLocked(t *testing.T) {
	st := &CRGStatus{}
	applyCRGStatusError(st, errFmt("database is locked"))
	if st.State != string(CRGReadinessBusyOrLocked) {
		t.Errorf("got %q", st.State)
	}
}

func TestApplyCRGStatusError_Unbuilt(t *testing.T) {
	st := &CRGStatus{State: string(CRGReadinessUnbuilt)}
	applyCRGStatusError(st, errFmt("no such table: foo"))
	if st.State != string(CRGReadinessUnbuilt) {
		t.Errorf("got %q", st.State)
	}
}

func TestApplyCRGStatusError_Generic(t *testing.T) {
	st := &CRGStatus{}
	applyCRGStatusError(st, errFmt("totally unexpected"))
	if st.State != string(CRGReadinessError) {
		t.Errorf("got %q", st.State)
	}
}

func TestIsPythonEntrypoint_MissingFile(t *testing.T) {
	if isPythonEntrypoint("/no/such/file") {
		t.Error("missing file should return false")
	}
}

func TestIsPythonEntrypoint_Shebang(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	_ = os.WriteFile(p, []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0o755)
	if !isPythonEntrypoint(p) {
		t.Error("python shebang should return true")
	}
}

func TestIsPythonEntrypoint_NoShebang(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	_ = os.WriteFile(p, []byte("not a script\n"), 0o644)
	if isPythonEntrypoint(p) {
		t.Error("expected false")
	}
}

func TestUnmarshalSkippingLogPrefix_DirectJSON(t *testing.T) {
	var v map[string]any
	if err := unmarshalSkippingLogPrefix([]byte(`{"a":1}`), &v); err != nil {
		t.Fatalf("err: %v", err)
	}
	if v["a"] == nil {
		t.Errorf("unexpected: %v", v)
	}
}

func TestUnmarshalSkippingLogPrefix_WithLogLines(t *testing.T) {
	raw := []byte("INFO: something\n{\"a\":1}\n")
	var v map[string]any
	if err := unmarshalSkippingLogPrefix(raw, &v); err != nil {
		t.Fatalf("err: %v", err)
	}
	if v["a"] == nil {
		t.Errorf("got %v", v)
	}
}

func TestSplitPGStatements(t *testing.T) {
	got := splitPGStatements("SELECT 1; SELECT 2;; SELECT 3;")
	if len(got) != 3 {
		t.Errorf("expected 3 statements, got %d: %v", len(got), got)
	}
}

func TestPGEncodeExtra_Empty(t *testing.T) {
	got, err := pgEncodeExtra(nil)
	if err != nil || got != "{}" {
		t.Errorf("nil: got %q err=%v", got, err)
	}
}

func TestPGEncodeExtra_Populated(t *testing.T) {
	got, err := pgEncodeExtra(map[string]any{"k": "v"})
	if err != nil || !strings.Contains(got, "\"k\"") {
		t.Errorf("populated: got %q err=%v", got, err)
	}
}

func TestEncodeExtra_Empty(t *testing.T) {
	got, err := encodeExtra(nil)
	if err != nil || got != "{}" {
		t.Errorf("nil: got %q err=%v", got, err)
	}
}

func TestEncodeExtra_Populated(t *testing.T) {
	got, err := encodeExtra(map[string]any{"x": 1})
	if err != nil || !strings.Contains(got, "\"x\"") {
		t.Errorf("populated: got %q err=%v", got, err)
	}
}

func TestDecodeExtra_Empty(t *testing.T) {
	if m := decodeExtra(""); m != nil {
		t.Errorf("empty: got %v", m)
	}
	if m := decodeExtra("{}"); m != nil {
		t.Errorf("empty object: got %v", m)
	}
}

func TestDecodeExtra_Populated(t *testing.T) {
	if m := decodeExtra(`{"k":"v"}`); m["k"] != "v" {
		t.Errorf("got %v", m)
	}
}

// TestMakeQualified_WithParent pins the nested identity on upstream's shape:
// the file path stays in the identity, so two receivers of the same name in
// different files never collapse into one node.
func TestMakeQualified_WithParent(t *testing.T) {
	got := makeQualified(NodeInfo{Name: "Bar", ParentName: "Foo", FilePath: "f.go"})
	if got != "f.go::Foo.Bar" {
		t.Errorf("got %q", got)
	}
}

func TestMakeQualified_FilePath(t *testing.T) {
	got := makeQualified(NodeInfo{Name: "Bar", FilePath: "f.go"})
	if got != "f.go::Bar" {
		t.Errorf("got %q", got)
	}
}

// TestMakeQualified_FileNodeIsBarePath pins the File arm: every IMPORTS_FROM
// and file-sourced CONTAINS edge names a file by its bare path, so the File
// node's identity must be that path and not "<path>::<path>".
func TestMakeQualified_FileNodeIsBarePath(t *testing.T) {
	got := makeQualified(NodeInfo{Kind: NodeKindFile, Name: "f.go", FilePath: "dir/f.go"})
	if got != "dir/f.go" {
		t.Errorf("got %q", got)
	}
}

// ── impact internal helpers ──────────────────────────────────────────────────

// fakeImpactStore satisfies impactStoreView for computeImpactRadius tests.
type fakeImpactStore struct {
	nodes map[string]*GraphNode
	edges []GraphEdge
}

func (f *fakeImpactStore) GetNode(qn string) (*GraphNode, error) {
	return f.nodes[qn], nil
}
func (f *fakeImpactStore) GetEdgesAmong(qns []string) ([]GraphEdge, error) {
	return f.edges, nil
}

func TestComputeImpactRadius_AssemblesResult(t *testing.T) {
	store := &fakeImpactStore{
		nodes: map[string]*GraphNode{
			"A": {QualifiedName: "A", FilePath: "a.go"},
			"B": {QualifiedName: "B", FilePath: "b.go"},
		},
		edges: []GraphEdge{{SourceQualified: "A", TargetQualified: "B"}},
	}
	seeds := map[string]bool{"A": true}
	fwd := map[string][]string{"A": {"B"}}
	rev := map[string][]string{"B": {"A"}}
	got, err := computeImpactRadius(seeds, fwd, rev, 2, 100, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ChangedNodes) != 1 || got.ChangedNodes[0].QualifiedName != "A" {
		t.Errorf("changed: %v", got.ChangedNodes)
	}
	if len(got.ImpactedNodes) != 1 || got.ImpactedNodes[0].QualifiedName != "B" {
		t.Errorf("impacted: %v", got.ImpactedNodes)
	}
	if len(got.ImpactedFiles) != 1 || got.ImpactedFiles[0] != "b.go" {
		t.Errorf("files: %v", got.ImpactedFiles)
	}
}

func TestResolveImpactNodes_ExcludeSet(t *testing.T) {
	store := &fakeImpactStore{nodes: map[string]*GraphNode{
		"A": {QualifiedName: "A", FilePath: "a.go"},
		"B": {QualifiedName: "B", FilePath: "b.go"},
	}}
	got := resolveImpactNodes(map[string]bool{"A": true, "B": true}, map[string]bool{"A": true}, store)
	if len(got) != 1 || got[0].QualifiedName != "B" {
		t.Errorf("got %v", got)
	}
}

func TestResolveImpactNodes_SkipsMissing(t *testing.T) {
	store := &fakeImpactStore{nodes: map[string]*GraphNode{}}
	got := resolveImpactNodes(map[string]bool{"missing": true}, nil, store)
	if len(got) != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestUniqueImpactFiles_Dedup(t *testing.T) {
	got := uniqueImpactFiles([]GraphNode{
		{FilePath: "a.go"}, {FilePath: "b.go"}, {FilePath: "a.go"},
	})
	if len(got) != 2 {
		t.Errorf("expected 2, got %v", got)
	}
}

func TestAppendUnvisited_SkipsVisited(t *testing.T) {
	visited := map[string]bool{"a": true}
	impacted := map[string]bool{}
	got := appendUnvisited(nil, []string{"a", "b", "c"}, visited, impacted)
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
	if !impacted["b"] || !impacted["c"] || impacted["a"] {
		t.Errorf("impacted: %v", impacted)
	}
}

// errFmt is a tiny helper to construct an error from a literal string.
func errFmt(msg string) error {
	return fmt.Errorf("%s", msg)
}
