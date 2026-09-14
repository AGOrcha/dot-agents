// Package codegraph is the kg-native code-graph backend
// (graph-backend-adapter-contract §11): an in-process replacement for the
// legacy Python `code-review-graph` subprocess bridge.
//
// It has two halves. `scan.go` extracts a repository's Go sources into the
// upstream-shaped node and edge rows the published graphstore contract
// persists — identities, kinds, line ranges, parameter text and call/import/
// containment/test edges are the ones code-review-graph v2.3.8 produces for
// the same tree — and `engine.go` persists them and answers the §11.1 parity
// rows with NO subprocess of any kind.
//
// Extraction is deliberately upstream-faithful rather than "correct": upstream
// resolves a call against the DECLARATIONS OF THE CALLING FILE ONLY, so a call
// into another file of the same package, or through any package/receiver
// selector, keeps a BARE target name. Reproducing that is the contract; a
// smarter resolver would produce a different graph and break parity.
//
// The package lives here rather than inside internal/graphstore because it
// depends on the crg adapter, and the crg adapter depends on graphstore; the
// dependency only points one way.
package codegraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
)

// languageGo is the only language the kg-native ingester parses today. The
// legacy Python bridge used Tree-sitter and covered several languages; that
// delta is reported by ScanCapability (capability.go), which routes a
// repository with non-Go sources to the bridge instead of silently dropping
// them.
const languageGo = "go"

// walkDir is a seam for the directory walk. Production binds
// filepath.WalkDir; the walk callback swallows every per-entry error, so
// overriding this is the only way to reach goFiles' walk-failure arm.
var walkDir = filepath.WalkDir

// Node kinds, matching upstream's `nodes.kind` vocabulary for Go: a type
// declaration is a Class, a func or method is a Function, and a func whose
// name looks like a test is a Test. The File kind lives in namespace.go
// (nodeKindFile) because the node-kind filter there is its only other user.
const (
	kindClass    = "Class"
	kindFunction = "Function"
	kindTest     = "Test"
)

// Edge kinds emitted by the ingester — upstream's complete set for Go.
// INHERITS is absent on purpose: upstream's Go base-type extraction looks for
// a bare `type_identifier` directly under `field_declaration_list`, which
// tree-sitter-go never produces, so embedded structs and interface
// composition emit nothing.
const (
	edgeContains    = "CONTAINS"
	edgeCalls       = "CALLS"
	edgeImportsFrom = "IMPORTS_FROM"
	edgeTestedBy    = "TESTED_BY"
)

// qualSep separates the file-path component of a qualified name from the
// symbol path; memberSep separates a receiver from its method inside that
// symbol path: `<abs file path>::<Receiver>.<Method>`.
const (
	qualSep   = "::"
	memberSep = "."
)

// skipDirs are directory names never walked during ingestion: VCS metadata,
// vendored or installed third-party trees, and build output.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, ".venv": true,
	"dist": true, "build": true, ".dot-agents": true, ".code-review-graph": true,
}

// testRunnerNames are the xUnit/BDD runner names upstream promotes to a Test
// node when they are declared inside a test file. They are JS/Python idioms,
// but the predicate is language-agnostic upstream, so a Go `func test()` in a
// `_test.go` file is a Test node there and here.
var testRunnerNames = map[string]bool{
	"describe": true, "it": true, "test": true, "beforeEach": true,
	"afterEach": true, "beforeAll": true, "afterAll": true, "suite": true,
}

// Decl is one declared symbol plus the row detail the corpus symbol shape does
// not carry. Symbol.QualifiedName is the upstream identity
// (`<abs file path>::[<ParentName>.]<Name>`) and is what every edge endpoint
// naming this declaration uses.
type Decl struct {
	Symbol crg.Symbol
	// Name is the display name — `Expired`, not `Session.Expired`.
	Name string
	// ParentName is a method's receiver type, empty for a package-level symbol.
	ParentName string
	// Params is the source parameter list INCLUDING its parentheses. For a
	// method it is the RECEIVER list, because upstream reads the first
	// `parameter_list` child and the receiver comes first in the grammar.
	// Empty for a Class.
	Params string
	// LineEnd is the last line of the declaration.
	LineEnd int
	// IsTest reports whether this declaration is a test, by NAME — a helper in
	// a `_test.go` file is not a test, and a `TestFoo` in a production file is.
	IsTest bool
	// receiver is the method's receiver variable name, empty for a package-level
	// symbol or a method declared with an anonymous receiver. Only a call
	// through this name can resolve to a sibling method on the same type.
	receiver string
}

// Edge is one extracted graph edge. Source and Target are qualified names
// except where upstream leaves them bare: an unresolved CALLS target, a
// TESTED_BY source mirroring one, and an IMPORTS_FROM target, which is always
// the raw import path string.
type Edge struct {
	Kind   string
	Source string
	Target string
	Line   int
}

// SourceFile is one parsed source file's contribution to the graph. Extraction
// is file-scoped — exactly like upstream's, where resolution never leaves the
// file — so an incremental update can replace one file's rows in isolation.
type SourceFile struct {
	// Path is the ABSOLUTE, slash-separated file path. It is the node identity
	// upstream uses: `nodes.file_path`, the File node's whole qualified name,
	// and the path component of every symbol's qualified name.
	Path string
	// RelPath is the repo-relative, slash-separated path, for joining against
	// git's change lists.
	RelPath string
	// FileHash is the content hash of the whole file (change detection).
	FileHash string
	// IsTest reports whether the PATH looks like a test file.
	IsTest bool
	// LineCount is the File node's line_end: upstream counts newlines and adds
	// one, so a file ending in a newline reports one more than its text lines.
	LineCount int
	// Decls are the symbols declared in this file, in source order.
	Decls []Decl
	// Edges are the edges this file contributes, in upstream's emission order.
	Edges []Edge
}

// Symbols returns the corpus symbols this file declares.
func (f SourceFile) Symbols() []crg.Symbol {
	out := make([]crg.Symbol, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, d.Symbol)
	}
	return out
}

// References lowers this file's edges to the corpus reference shape. Corpus
// references carry no line, and Corpus.ToGraph drops any reference whose
// endpoints are not both declared symbols — which is how bare call targets,
// import paths and File-node containment fall out of the corpus projection
// without being hidden from the persisted graph.
func (f SourceFile) References() []crg.Reference {
	out := make([]crg.Reference, 0, len(f.Edges))
	for _, e := range f.Edges {
		out = append(out, crg.Reference{Kind: e.Kind, From: e.Source, To: e.Target})
	}
	return out
}

// Scan walks root, extracts every Go source file, and returns the per-file
// units plus the flattened corpus at commit.
//
// Unparseable files are skipped rather than failing the scan: a repository
// mid-edit must still produce a usable graph (the bridge behaved the same way).
func Scan(root, commit string) ([]SourceFile, crg.Corpus, error) {
	rels, err := goFiles(root)
	if err != nil {
		return nil, crg.Corpus{}, err
	}
	abs, err := absRoot(root)
	if err != nil {
		return nil, crg.Corpus{}, err
	}
	files := scanFiles(abs, rels)
	corpus := crg.Corpus{Commit: commit}
	for _, f := range files {
		corpus.Symbols = append(corpus.Symbols, f.Symbols()...)
		corpus.References = append(corpus.References, f.References()...)
	}
	return files, corpus, nil
}

// ScanPaths extracts exactly the named repo-relative paths, in the order
// given. A path that is absent, unreadable, not a Go file or unparseable is
// omitted from the result rather than reported: the incremental caller uses
// "in the graph but absent from the scan" as its purge signal, so a silent
// omission is the signal, not a failure. Only a root that cannot be made
// absolute — which would make every identity in the batch wrong — is an error.
//
// Scanning a subset is exact rather than approximate because upstream's Go
// resolution is file-local — there is no cross-file index to go stale.
func ScanPaths(root string, rels []string) ([]SourceFile, error) {
	abs, err := absRoot(root)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rels))
	wanted := make([]string, 0, len(rels))
	for _, rel := range rels {
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || seen[rel] {
			continue
		}
		seen[rel] = true
		wanted = append(wanted, rel)
	}
	return scanFiles(abs, wanted), nil
}

// absRoot resolves root to an absolute path without following symlinks, which
// is how upstream spells node identities: it normalizes separators and nothing
// else, so a repository reached through a symlink keeps the caller's spelling.
func absRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("codegraph: resolve root %s: %w", root, err)
	}
	return abs, nil
}

// scanFiles extracts each path under absRoot, dropping the ones that cannot be
// used.
func scanFiles(absRoot string, rels []string) []SourceFile {
	out := make([]SourceFile, 0, len(rels))
	for _, rel := range rels {
		if f, ok := scanFile(absRoot, rel); ok {
			out = append(out, f)
		}
	}
	return out
}

// goFiles returns every repo-relative .go file path under root, sorted, with
// skipDirs and hidden directories pruned.
func goFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("codegraph: walk %s: %w", root, err)
	}
	var out []string
	err := walkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree — skip, do not fail the whole scan
		}
		if d.IsDir() {
			return skipDirEntry(root, path, d)
		}
		if strings.HasSuffix(d.Name(), ".go") {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codegraph: walk %s: %w", root, err)
	}
	sort.Strings(out)
	return out, nil
}

// skipDirEntry decides whether a directory is walked. The root itself is
// always walked; hidden and known-vendored directories below it are pruned.
func skipDirEntry(root, path string, d fs.DirEntry) error {
	if path == root {
		return nil
	}
	name := d.Name()
	if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
		return filepath.SkipDir
	}
	return nil
}

// scanFile extracts one file, reporting false when it cannot be used.
func scanFile(absRoot, rel string) (SourceFile, bool) {
	rel = filepath.ToSlash(rel)
	data, err := os.ReadFile(filepath.Join(absRoot, filepath.FromSlash(rel)))
	if err != nil {
		return SourceFile{}, false
	}
	path := filepath.ToSlash(filepath.Join(absRoot, filepath.FromSlash(rel)))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
	if err != nil {
		return SourceFile{}, false
	}
	s := &fileScan{
		path:    path,
		src:     data,
		fset:    fset,
		defined: definedNames(file),
		dead:    deadGuardSpans(file),
	}
	s.extract(file)
	s.resolveCallTargets()
	s.appendTestedBy()
	return SourceFile{
		Path:      path,
		RelPath:   rel,
		FileHash:  hashBytes(data),
		IsTest:    isTestFile(path),
		LineCount: bytes.Count(data, []byte{'\n'}) + 1,
		Decls:     s.decls,
		Edges:     s.published(),
	}, true
}

// isTestFile reports whether a path looks like a test file, reproducing
// upstream's pattern list for the patterns a `.go` path can match: the
// `_test.go` suffix, or a `test/`, `tests/` or `__tests__/` path segment. The
// match is a plain substring search over the whole ABSOLUTE path, exactly as
// upstream's unanchored regexes are.
func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") ||
		strings.Contains(path, "test/") ||
		strings.Contains(path, "tests/") ||
		strings.Contains(path, "/__tests__/")
}

// isTestFunc reports whether a declaration name marks a test. Upstream keys on
// the NAME first, so `TestFoo` in a production file is a Test node and a
// `newFixture` helper in a `_test.go` file is not; a bare runner name counts
// only inside a test file.
func isTestFunc(name, path string) bool {
	switch {
	case strings.HasPrefix(name, "test_"),
		strings.HasPrefix(name, "Test"),
		strings.HasSuffix(name, "_test"),
		strings.HasSuffix(name, "_spec"),
		strings.Contains(name, ".test."),
		strings.Contains(name, ".spec."):
		return true
	}
	return testRunnerNames[name] && isTestFile(path)
}

// ─── per-file extraction ─────────────────────────────────────────────────────

// scanEdge is an edge plus the call-site evidence the per-file resolution pass
// consumes. The evidence is dropped before publication: upstream keeps it in
// `edges.extra`, but nothing downstream of this package reads it.
type scanEdge struct {
	Edge
	// receiver is the source text of a member call's operand — `auth` in
	// `auth.Login()`. Non-empty means the callee is a selector, which upstream
	// never resolves against file scope.
	receiver string
	// methodReceiver marks a call through the enclosing method's own receiver
	// variable, unshadowed. Only those resolve to a same-receiver method.
	methodReceiver bool
}

// fileScan accumulates one file's nodes and edges.
type fileScan struct {
	path    string
	src     []byte
	fset    *token.FileSet
	defined map[string]bool
	dead    []span
	decls   []Decl
	edges   []scanEdge
}

// declScope is the (enclosing class, enclosing function) pair in force for a
// subtree, plus the receiver evidence a method's body needs.
type declScope struct {
	end    token.Pos
	fn     string
	cls    string
	recv   string
	shadow []span
}

// extract walks the file once in source order, emitting nodes and edges the
// way upstream's recursive descent does: imports first (they lead the file),
// then each declaration's node and CONTAINS edge before the calls inside it.
func (s *fileScan) extract(file *ast.File) {
	scopes := []declScope{{end: file.End()}}
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		for len(scopes) > 1 && n.Pos() >= scopes[len(scopes)-1].end {
			scopes = scopes[:len(scopes)-1]
		}
		cur := scopes[len(scopes)-1]
		switch x := n.(type) {
		case *ast.GenDecl:
			switch x.Tok {
			case token.IMPORT:
				s.addImports(x)
				return false
			case token.TYPE:
				name, ok := typeDeclName(x)
				if !ok {
					return true // an all-alias group declares nothing
				}
				s.addClass(x, name, cur.cls)
				// Upstream recurses into a class with the class as the
				// enclosing scope and NO enclosing function, so a call inside
				// a type declaration is attributed to the file.
				scopes = append(scopes, declScope{end: x.End(), cls: name})
			}
		case *ast.FuncDecl:
			scopes = append(scopes, s.addFunc(x, cur.cls))
		case *ast.CallExpr:
			s.addCall(x, cur)
		}
		return true
	})
}

// addImports emits one IMPORTS_FROM edge per import spec, all carrying the
// line of the declaration keyword rather than of the spec: upstream reads the
// line off the `import_declaration` node, so every member of a grouped import
// block reports the `import (` line. The target is the raw import path —
// upstream has no Go module resolution, so it never rewrites one to a file.
func (s *fileScan) addImports(d *ast.GenDecl) {
	line := s.line(d.Pos())
	for _, spec := range d.Specs {
		is, ok := spec.(*ast.ImportSpec)
		if !ok || is.Path == nil || !strings.HasPrefix(is.Path.Value, `"`) {
			continue // upstream reads interpreted string literals only
		}
		path, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		s.addEdge(scanEdge{Edge: Edge{
			Kind: edgeImportsFrom, Source: s.path, Target: path, Line: line,
		}})
	}
}

// addClass emits a Class node for a type declaration and its containment edge.
// The node spans the whole declaration — for a `type (...)` group that is the
// keyword through the closing paren, named after the group's first non-alias
// spec, because that is the single node upstream's name lookup produces.
func (s *fileScan) addClass(d *ast.GenDecl, name, enclosing string) {
	qualified := s.qualify(name, enclosing)
	s.addDecl(d, Decl{Name: name, ParentName: enclosing}, kindClass, qualified)
	// A Class is always contained by its file, even when it is declared inside
	// a function body.
	s.addEdge(scanEdge{Edge: Edge{
		Kind: edgeContains, Source: s.path, Target: qualified, Line: s.line(d.Pos()),
	}})
}

// addFunc emits a Function or Test node for a func or method declaration and
// its containment edge, and returns the scope its body is walked under. A
// method is contained by its receiver TYPE, not by the file.
func (s *fileScan) addFunc(d *ast.FuncDecl, enclosing string) declScope {
	name := d.Name.Name
	cls := enclosing
	if recv := receiverTypeName(d); recv != "" {
		cls = recv
	}
	isTest := isTestFunc(name, s.path)
	kind := kindFunction
	if isTest {
		kind = kindTest
	}
	var recvVar string
	if d.Recv != nil && len(d.Recv.List) > 0 && len(d.Recv.List[0].Names) > 0 {
		recvVar = d.Recv.List[0].Names[0].Name
	}
	qualified := s.qualify(name, cls)
	s.addDecl(d, Decl{
		Name: name, ParentName: cls, Params: s.paramsText(d),
		IsTest: isTest, receiver: recvVar,
	}, kind, qualified)

	container := s.path
	if cls != "" {
		container = s.qualify(cls, "")
	}
	s.addEdge(scanEdge{Edge: Edge{
		Kind: edgeContains, Source: container, Target: qualified, Line: s.line(d.Pos()),
	}})

	scope := declScope{end: d.End(), fn: name, cls: cls, recv: recvVar}
	if recvVar != "" {
		scope.shadow = receiverShadowSpans(d.Body, recvVar)
	}
	return scope
}

// addCall emits a CALLS edge for one call site.
//
// Target resolution is upstream's, and it is deliberately weak. A call through
// any selector — a package qualifier, a receiver, a field — keeps the BARE
// member name, because the method lives on the operand's type and upstream
// does no type inference. A bare identifier resolves only against the calling
// file's own top-level declarations. Everything else stays bare and is left
// for the postprocess lane's evidence-backed resolution.
func (s *fileScan) addCall(c *ast.CallExpr, scope declScope) {
	if inSpans(s.dead, c.Pos()) {
		return // inside `if false { ... }`: never evaluated, so never a call
	}
	name, receiver, ok := s.callee(c.Fun)
	if !ok {
		return
	}
	caller := s.path
	if scope.fn != "" {
		caller = s.qualify(scope.fn, scope.cls)
	}
	edge := scanEdge{
		Edge:     Edge{Kind: edgeCalls, Source: caller, Target: name, Line: s.line(c.Pos())},
		receiver: receiver,
	}
	switch {
	case receiver != "":
		edge.methodReceiver = scope.recv != "" && receiver == scope.recv &&
			!inSpans(scope.shadow, c.Pos())
	case s.defined[name]:
		edge.Target = s.qualify(name, "")
	}
	s.addEdge(edge)
}

// callee reduces a call's callee expression to (member name, receiver text).
// Only a bare identifier and a selector are callees upstream recognises; a
// generic instantiation, an immediately-invoked literal or a call on a
// returned func produce no edge at all.
func (s *fileScan) callee(fun ast.Expr) (name, receiver string, ok bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, "", true
	case *ast.SelectorExpr:
		return f.Sel.Name, s.text(receiverOperand(f.X)), true
	}
	return "", "", false
}

// receiverOperand unwraps the parentheses and the single leading pointer
// dereference upstream unwraps before reading a receiver's source text, so
// `(*p).M()` reports `p` while `(&T{}).M()` reports `&T{}`.
func receiverOperand(x ast.Expr) ast.Expr {
	for {
		paren, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = paren.X
	}
	if star, ok := x.(*ast.StarExpr); ok {
		return star.X
	}
	return x
}

// addDecl records one declared symbol, filling in the fields derived from its
// AST node.
func (s *fileScan) addDecl(node ast.Node, d Decl, kind, qualified string) {
	d.Symbol = crg.Symbol{
		QualifiedName: qualified,
		Kind:          kind,
		Language:      languageGo,
		FilePath:      s.path,
		LineStart:     s.line(node.Pos()),
		ContentHash:   hashNode(s.fset, s.src, node),
	}
	d.LineEnd = s.line(node.End() - 1)
	s.decls = append(s.decls, d)
}

func (s *fileScan) addEdge(e scanEdge) { s.edges = append(s.edges, e) }

// qualify builds an upstream qualified name. It must stay byte-identical to
// graphstore's makeQualified, which derives the same name from the persisted
// NodeInfo — every edge endpoint depends on the two agreeing.
func (s *fileScan) qualify(name, parent string) string {
	if parent != "" {
		return s.path + qualSep + parent + memberSep + name
	}
	return s.path + qualSep + name
}

// line is the 1-based source line of a position.
func (s *fileScan) line(pos token.Pos) int { return s.fset.Position(pos).Line }

// text is the exact source text of an expression.
func (s *fileScan) text(x ast.Expr) string {
	return s.slice(s.offset(x.Pos()), s.offset(x.End()))
}

func (s *fileScan) offset(pos token.Pos) int { return s.fset.Position(pos).Offset }

func (s *fileScan) slice(start, end int) string {
	if start < 0 || end > len(s.src) || start >= end {
		return ""
	}
	return string(s.src[start:end])
}

// paramsText is the declaration's first parenthesised parameter list, verbatim
// and including its parentheses. For a method that is the RECEIVER list, which
// is what upstream's "first `parameter_list` child" rule selects; a type
// parameter list is a different grammar node and is skipped by both.
func (s *fileScan) paramsText(d *ast.FuncDecl) string {
	list := d.Type.Params
	if d.Recv != nil {
		list = d.Recv
	}
	if list == nil || !list.Opening.IsValid() || !list.Closing.IsValid() {
		return ""
	}
	return s.slice(s.offset(list.Opening), s.offset(list.Closing)+1)
}

// published drops the resolution evidence and returns the file's edges as the
// parser produced them, DUPLICATES INCLUDED. Two calls to the same unresolved
// name on one line are two extracted edges and one persisted row; upstream
// reports the extracted count and stores the collapsed row, so the collapse
// belongs to persistence (edgeInfosFor) and not here.
func (s *fileScan) published() []Edge {
	out := make([]Edge, 0, len(s.edges))
	for _, e := range s.edges {
		out = append(out, e.Edge)
	}
	return out
}

// ─── per-file resolution passes ──────────────────────────────────────────────

// declEntry is one candidate declaration for a bare call target.
type declEntry struct {
	qualified string
	parent    string
}

// resolveCallTargets is upstream's post-parse pass over one file's edges. It
// qualifies a bare CALLS target in two cases and only two: a call through the
// enclosing method's own receiver naming exactly one method on the same
// receiver type, and a bare name matching a declaration the file-scope
// pre-scan missed — a type declared inside a function body, which is a node
// but not a file-scope name.
func (s *fileScan) resolveCallTargets() {
	byName := make(map[string][]declEntry, len(s.decls))
	parentOf := make(map[string]string, len(s.decls))
	receiverOf := make(map[string]string, len(s.decls))
	for _, d := range s.decls {
		entry := declEntry{qualified: d.Symbol.QualifiedName, parent: d.ParentName}
		if !containsEntry(byName[d.Name], entry) {
			byName[d.Name] = append(byName[d.Name], entry)
		}
		parentOf[d.Symbol.QualifiedName] = d.ParentName
		if d.receiver != "" {
			receiverOf[d.Symbol.QualifiedName] = d.receiver
		}
	}
	for i := range s.edges {
		e := &s.edges[i]
		if e.Kind != edgeCalls {
			continue
		}
		if e.methodReceiver && e.receiver == receiverOf[e.Source] {
			var candidates []string
			for _, entry := range byName[e.Target] {
				if entry.parent == parentOf[e.Source] {
					candidates = append(candidates, entry.qualified)
				}
			}
			if len(candidates) == 1 {
				e.Target = candidates[0]
				continue
			}
		}
		if e.receiver == "" && !strings.Contains(e.Target, qualSep) {
			if entries := byName[e.Target]; len(entries) > 0 {
				e.Target = entries[0].qualified
			}
		}
	}
}

func containsEntry(entries []declEntry, want declEntry) bool {
	for _, e := range entries {
		if e == want {
			return true
		}
	}
	return false
}

// appendTestedBy mirrors every CALLS edge made by a test declaration back as a
// TESTED_BY edge from callee to test. Upstream appends them after the whole
// file is resolved, mirrors bare targets as-is, and does not deduplicate, so
// two calls to one helper from one test on different lines produce two edges.
func (s *fileScan) appendTestedBy() {
	tests := make(map[string]bool, len(s.decls))
	for _, d := range s.decls {
		if d.IsTest {
			tests[d.Symbol.QualifiedName] = true
		}
	}
	if len(tests) == 0 {
		return
	}
	calls := s.edges[:len(s.edges):len(s.edges)]
	for _, e := range calls {
		if e.Kind == edgeCalls && tests[e.Source] {
			s.addEdge(scanEdge{
				Edge: Edge{
					Kind: edgeTestedBy, Source: e.Target, Target: e.Source, Line: e.Line,
				},
				receiver:       e.receiver,
				methodReceiver: e.methodReceiver,
			})
		}
	}
}

// ─── file-scope pre-scan ─────────────────────────────────────────────────────

// definedNames is upstream's file-scope pre-scan: the names of the file's
// TOP-LEVEL func, method and type declarations. A method contributes its bare
// method name, a type group its first non-alias spec name, and `var`/`const`
// contribute nothing. A bare call to one of these names — and only these —
// resolves to `<file>::<name>`, which is why a method self-call written as a
// bare name resolves to a qualified name no node carries.
func definedNames(file *ast.File) map[string]bool {
	out := make(map[string]bool, len(file.Decls))
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out[d.Name.Name] = true
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			if name, ok := typeDeclName(d); ok {
				out[name] = true
			}
		}
	}
	return out
}

// typeDeclName is the name upstream gives a type declaration: the first spec
// that is not an alias. `type A = B` declares nothing at all there, so a group
// of nothing but aliases produces no node.
func typeDeclName(d *ast.GenDecl) (string, bool) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Assign.IsValid() {
			continue
		}
		return ts.Name.Name, true
	}
	return "", false
}

// receiverTypeName returns a method's receiver type name (pointer stars and
// type parameters stripped), or "" for a plain function.
func receiverTypeName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	return baseTypeName(d.Recv.List[0].Type)
}

// baseTypeName unwraps pointer and generic-instantiation wrappers to the
// underlying identifier name.
func baseTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return baseTypeName(t.X)
	case *ast.IndexExpr:
		return baseTypeName(t.X)
	case *ast.IndexListExpr:
		return baseTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// ─── position spans ──────────────────────────────────────────────────────────

// span is a half-open position range.
type span struct {
	start, end token.Pos
}

// inSpans reports whether pos falls inside any span. Spans are merged and
// sorted, so the last span starting at or before pos is the only candidate.
func inSpans(spans []span, pos token.Pos) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].start > pos }) - 1
	return i >= 0 && pos < spans[i].end
}

// mergeSpans sorts and coalesces overlapping spans, dropping empty ones.
func mergeSpans(spans []span) []span {
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	out := spans[:0]
	for _, s := range spans {
		if s.start >= s.end {
			continue
		}
		if n := len(out); n > 0 && s.start <= out[n-1].end {
			if s.end > out[n-1].end {
				out[n-1].end = s.end
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// deadGuardSpans returns the bodies of every `if false { ... }` in the file. A
// call there is never evaluated, so upstream emits no edge for it; only the
// consequence is dead, an `else` branch stays live.
func deadGuardSpans(file *ast.File) []span {
	var spans []span
	ast.Inspect(file, func(n ast.Node) bool {
		if stmt, ok := n.(*ast.IfStmt); ok && stmt.Body != nil && isFalseLiteral(stmt.Cond) {
			spans = append(spans, span{stmt.Body.Pos(), stmt.Body.End()})
		}
		return true
	})
	return mergeSpans(spans)
}

// isFalseLiteral reports whether a condition is the literal `false`, through
// any number of parentheses.
func isFalseLiteral(cond ast.Expr) bool {
	for {
		paren, ok := cond.(*ast.ParenExpr)
		if !ok {
			break
		}
		cond = paren.X
	}
	ident, ok := cond.(*ast.Ident)
	return ok && ident.Name == "false"
}

// receiverShadowSpans returns the ranges of a method body where name no longer
// refers to the receiver, reproducing upstream's lexical-shadow index: a func
// literal parameter or named result, a type-switch alias, a `var`/`const`/
// `type` declaration (shadowing from the declaration to the end of its scope),
// a `:=` binding (likewise, but only the FIRST one per scope, and never in the
// body's own top-level scope — a quirk of upstream's index, which pre-seeds
// that scope and so never treats a receiver redeclared there as shadowing),
// and a `range` or `select` receive binding.
func receiverShadowSpans(body *ast.BlockStmt, name string) []span {
	if body == nil || name == "" {
		return nil
	}
	sh := shadowScan{name: name, bound: map[span]bool{key(body): true}}
	var open []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		for len(open) > 0 && n.Pos() >= open[len(open)-1].End() {
			open = open[:len(open)-1]
		}
		scope := ast.Node(body)
		if len(open) > 0 {
			scope = open[len(open)-1]
		}
		sh.visit(n, scope)
		if isLexicalScope(n) {
			open = append(open, n)
		}
		return true
	})
	return mergeSpans(sh.spans)
}

// shadowScan accumulates shadow ranges for one receiver name.
type shadowScan struct {
	name string
	// bound records the scopes that already carry a binding range, which is
	// how upstream keeps the first `:=` per scope and ignores the rest.
	bound   map[span]bool
	spans   []span
	claimed []ast.Node // `:=` statements already handled by their parent
}

func (sh *shadowScan) visit(n ast.Node, scope ast.Node) {
	switch x := n.(type) {
	case *ast.FuncLit:
		if x.Body != nil && sh.signatureBinds(x.Type) {
			sh.add(span{x.Body.Pos(), x.Body.End()}, x.Body)
		}
	case *ast.TypeSwitchStmt:
		sh.typeSwitch(x)
	case *ast.IfStmt:
		sh.initStmt(x.Init, x)
	case *ast.SwitchStmt:
		sh.initStmt(x.Init, x)
	case *ast.ForStmt:
		sh.initStmt(x.Init, x)
	case *ast.CommClause:
		sh.initStmt(x.Comm, x)
	case *ast.RangeStmt:
		if x.Tok == token.DEFINE && x.Body != nil &&
			(identNamed(x.Key, sh.name) || identNamed(x.Value, sh.name)) {
			sh.add(span{x.Body.Pos(), x.Body.End()}, x.Body)
		}
	case *ast.ValueSpec:
		if identsNamed(x.Names, sh.name) {
			sh.add(span{x.End(), scope.End()}, scope)
		}
	case *ast.TypeSpec:
		if x.Name != nil && x.Name.Name == sh.name {
			sh.add(span{x.End(), scope.End()}, scope)
		}
	case *ast.AssignStmt:
		// A `:=` that is a plain statement binds for the rest of its scope,
		// but upstream records at most one binding per scope and treats the
		// method body's own scope as already bound.
		if sh.isClaimed(x) || x.Tok != token.DEFINE || !sh.lhsBinds(x) {
			return
		}
		if !sh.bound[key(scope)] {
			sh.add(span{x.End(), scope.End()}, scope)
		}
	}
}

// initStmt handles a `:=` in the init position of an `if`, `switch` or `for`,
// and the receive of a `select` case: the binding covers the whole statement,
// including its else branch and every case.
func (sh *shadowScan) initStmt(init ast.Stmt, owner ast.Node) {
	assign, ok := init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || !sh.lhsBinds(assign) {
		return
	}
	sh.claimed = append(sh.claimed, assign)
	if sh.bound[key(owner)] {
		return
	}
	sh.add(span{assign.End(), owner.End()}, owner)
}

// typeSwitch handles `switch v := x.(type)`, whose alias shadows inside every
// case body.
func (sh *shadowScan) typeSwitch(x *ast.TypeSwitchStmt) {
	assign, ok := x.Assign.(*ast.AssignStmt)
	if !ok || !sh.lhsBinds(assign) {
		return
	}
	sh.claimed = append(sh.claimed, assign)
	if x.Body == nil {
		return
	}
	for _, stmt := range x.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}
		if len(clause.Body) > 0 {
			sh.spans = append(sh.spans, span{clause.Body[0].Pos(), clause.End()})
		}
		sh.bound[key(clause)] = true
	}
}

func (sh *shadowScan) add(s span, scope ast.Node) {
	sh.spans = append(sh.spans, s)
	sh.bound[key(scope)] = true
}

func (sh *shadowScan) isClaimed(n ast.Node) bool {
	for _, c := range sh.claimed {
		if c == n {
			return true
		}
	}
	return false
}

// signatureBinds reports whether a func literal's parameters or named results
// bind the receiver name.
func (sh *shadowScan) signatureBinds(t *ast.FuncType) bool {
	if t == nil {
		return false
	}
	for _, list := range []*ast.FieldList{t.Params, t.Results} {
		if list == nil {
			continue
		}
		for _, field := range list.List {
			if identsNamed(field.Names, sh.name) {
				return true
			}
		}
	}
	return false
}

func (sh *shadowScan) lhsBinds(assign *ast.AssignStmt) bool {
	if assign == nil {
		return false
	}
	for _, lhs := range assign.Lhs {
		if identNamed(lhs, sh.name) {
			return true
		}
	}
	return false
}

func key(n ast.Node) span { return span{n.Pos(), n.End()} }

// isLexicalScope reports whether a node introduces one of the scopes upstream
// treats as a binding boundary: a block, a switch case, or a select case.
func isLexicalScope(n ast.Node) bool {
	switch n.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return true
	}
	return false
}

func identNamed(x ast.Expr, name string) bool {
	ident, ok := x.(*ast.Ident)
	return ok && ident.Name == name
}

func identsNamed(idents []*ast.Ident, name string) bool {
	for _, ident := range idents {
		if ident != nil && ident.Name == name {
			return true
		}
	}
	return false
}

// ─── hashing ─────────────────────────────────────────────────────────────────

// hashBytes returns the short content hash used for change detection.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// hashNode hashes a declaration's exact source text so the O5 source_mutation
// driver fires on a real content change rather than on any re-ingestion.
func hashNode(fset *token.FileSet, src []byte, node ast.Node) string {
	start := fset.Position(node.Pos()).Offset
	end := fset.Position(node.End()).Offset
	if start < 0 || end > len(src) || start >= end {
		return hashBytes(nil)
	}
	return hashBytes(src[start:end])
}
