// Package codegraph is the kg-native code-graph backend
// (graph-backend-adapter-contract §11): an in-process replacement for the
// legacy Python `code-review-graph` subprocess bridge.
//
// It has two halves. `scan.go` ingests a repository's source into the
// normalized symbol corpus the kg-native `crg` adapter models
// (internal/adapters/builtin/crg), and `engine.go` persists that corpus into
// the published graphstore contract and answers the eight §11.1 parity rows —
// build, update, status, impact-radius, flows, communities, postprocess and
// detect-changes — with NO subprocess of any kind.
//
// The package lives here rather than inside internal/graphstore because it
// depends on the crg adapter, and the crg adapter depends on graphstore; the
// dependency only points one way.
package codegraph

import (
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
	"strings"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
)

// languageGo is the only language the kg-native ingester parses today. The
// legacy Python bridge used Tree-sitter and covered several languages; that
// delta is documented in docs/crg-bridge-consumer-audit.md rather than hidden.
const languageGo = "go"

// kindFunction / kindType are the two `symbol` note kinds the crg adapter
// schema declares (schema.yaml enum: Function | Type).
const (
	kindFunction = "Function"
	kindType     = "Type"
)

// Edge kinds emitted by the ingester. They match the crg adapter's declared
// edge_types so the adapter's parity-verified derivations (flows, communities,
// risk index) read them back without translation.
const (
	edgeCalls    = "CALLS"
	edgeImports  = "IMPORTS"
	edgeTestedBy = "TESTED_BY"
)

// skipDirs are directory names never walked during ingestion: VCS metadata,
// vendored or installed third-party trees, and build output.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, ".venv": true,
	"dist": true, "build": true, ".dot-agents": true, ".code-review-graph": true,
}

// Decl is one declared symbol plus the persistence detail the corpus symbol
// shape does not carry: the package-local name (so the store can rebuild the
// qualified name from parent + name) and the declaration's end line.
type Decl struct {
	Symbol crg.Symbol
	// Local is the name within the package, e.g. `Status` or `CRGBridge.Status`.
	Local string
	// LineEnd is the last line of the declaration.
	LineEnd int
}

// SourceFile is one parsed source file's contribution to the corpus: the
// symbols it declares plus the references those symbols make. Ingestion is
// file-scoped so an incremental update can replace exactly one file's rows.
type SourceFile struct {
	// Path is the repo-relative, slash-separated file path.
	Path string
	// PkgPath is the qualifier every symbol in this file is named under.
	PkgPath string
	// FileHash is the content hash of the whole file (change detection).
	FileHash string
	// IsTest reports whether this is a _test.go file.
	IsTest bool
	// Decls are the symbols declared in this file.
	Decls []Decl
	// References are the resolved edges whose source symbol lives in this file.
	References []crg.Reference
}

// Symbols returns the corpus symbols this file declares.
func (f SourceFile) Symbols() []crg.Symbol {
	out := make([]crg.Symbol, 0, len(f.Decls))
	for _, d := range f.Decls {
		out = append(out, d.Symbol)
	}
	return out
}

// Scan walks root, parses every Go source file, and returns the per-file
// ingestion units plus the flattened corpus at commit. Reference resolution is
// repo-global (a call in one file resolves against symbols declared in
// another), which is why the whole repo is always parsed even when only a few
// files are being rewritten.
//
// Unparseable files are skipped rather than failing the scan: a repository
// mid-edit must still produce a usable graph (the bridge behaved the same way).
func Scan(root, commit string) ([]SourceFile, crg.Corpus, error) {
	paths, err := goFiles(root)
	if err != nil {
		return nil, crg.Corpus{}, err
	}
	units := parseUnits(root, paths)
	idx := buildIndex(units)
	files := make([]SourceFile, 0, len(units))
	corpus := crg.Corpus{Commit: commit}
	for _, u := range units {
		sf := SourceFile{
			Path: u.path, PkgPath: u.pkgPath, FileHash: u.hash,
			IsTest: u.isTest, Decls: u.decls,
		}
		sf.References = resolveFileReferences(u, idx)
		files = append(files, sf)
		corpus.Symbols = append(corpus.Symbols, sf.Symbols()...)
		corpus.References = append(corpus.References, sf.References...)
	}
	return files, corpus, nil
}

// goFiles returns every repo-relative .go file path under root, sorted, with
// skipDirs and hidden directories pruned.
func goFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("codegraph: walk %s: %w", root, err)
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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

// unit is one parsed file with everything reference resolution needs.
type unit struct {
	path    string
	pkgPath string
	hash    string
	isTest  bool
	fset    *token.FileSet
	file    *ast.File
	imports map[string]string // local alias -> repo-relative package path
	decls   []Decl
	// declRefs pairs each declared symbol with the raw references in its body.
	declRefs []declRef
}

// declRef is one declaration's unresolved reference set.
type declRef struct {
	qualified string
	calls     []rawRef
	types     []rawRef
}

// rawRef is an unresolved reference: either a bare identifier (pkg == "") or a
// selector expression's package/receiver qualifier plus member name.
type rawRef struct {
	pkg  string
	name string
}

// parseUnits parses each path under root into a unit, dropping files that fail
// to read or parse.
func parseUnits(root string, paths []string) []*unit {
	fset := token.NewFileSet()
	units := make([]*unit, 0, len(paths))
	for _, rel := range paths {
		if u := parseUnit(fset, root, rel); u != nil {
			units = append(units, u)
		}
	}
	return units
}

// parseUnit reads and parses one file, returning nil when it cannot be used.
func parseUnit(fset *token.FileSet, root, rel string) *unit {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	file, err := parser.ParseFile(fset, rel, data, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	u := &unit{
		path:    rel,
		pkgPath: packagePathOf(rel, file.Name.Name),
		hash:    hashBytes(data),
		isTest:  strings.HasSuffix(rel, "_test.go"),
		fset:    fset,
		file:    file,
		imports: inRepoImports(file),
	}
	collectDeclarations(u, data)
	return u
}

// packagePathOf returns the qualifier a file's symbols are named under: the
// repo-relative directory (so `internal/graphstore.NewCRGBridge` is unique
// across packages that share a short name), falling back to the package name
// for files at the repository root.
func packagePathOf(rel, pkgName string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return pkgName
	}
	return dir
}

// inRepoImports maps each import's local alias to its repo-relative package
// path. Only imports that look like they belong to this module are kept — a
// stdlib or third-party import can never resolve to an in-repo symbol, so
// keeping it would only create ambiguity.
func inRepoImports(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		alias := importAlias(spec, path)
		if alias == "" {
			continue
		}
		out[alias] = path
	}
	return out
}

// importAlias returns the local name an import is referenced by: the explicit
// alias when present, else the last path segment. Blank and dot imports yield
// no usable qualifier.
func importAlias(spec *ast.ImportSpec, path string) string {
	if spec.Name != nil {
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return ""
		}
		return spec.Name.Name
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// collectDeclarations records the file's declared symbols and each
// declaration's raw reference set.
func collectDeclarations(u *unit, src []byte) {
	for _, decl := range u.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			u.addDecl(funcLocalName(d), d, src)
		case *ast.GenDecl:
			u.addTypeDecls(d, src)
		}
	}
}

// addTypeDecls records every type spec in a `type (...)` declaration group.
func (u *unit) addTypeDecls(d *ast.GenDecl, src []byte) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		if ts, ok := spec.(*ast.TypeSpec); ok {
			u.addDecl(ts.Name.Name, ts, src)
		}
	}
}

// funcLocalName is a function's package-local name. Methods are named
// `<Receiver>.<Method>` so they never collide with a package-level function of
// the same name.
func funcLocalName(d *ast.FuncDecl) string {
	if recv := receiverTypeName(d); recv != "" {
		return recv + "." + d.Name.Name
	}
	return d.Name.Name
}

// declKind maps an AST declaration node to the crg symbol kind it becomes.
func declKind(node ast.Node) string {
	if _, ok := node.(*ast.FuncDecl); ok {
		return kindFunction
	}
	return kindType
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

// addDecl appends a declared symbol and the raw references found in its
// subtree.
func (u *unit) addDecl(local string, node ast.Node, src []byte) {
	sym := crg.Symbol{
		QualifiedName: u.pkgPath + "." + local,
		Kind:          declKind(node),
		Language:      languageGo,
		FilePath:      u.path,
		LineStart:     u.fset.Position(node.Pos()).Line,
		ContentHash:   hashNode(u.fset, src, node),
	}
	u.decls = append(u.decls, Decl{
		Symbol:  sym,
		Local:   local,
		LineEnd: u.fset.Position(node.End()).Line,
	})
	calls, types := rawReferences(node)
	u.declRefs = append(u.declRefs, declRef{qualified: sym.QualifiedName, calls: calls, types: types})
}

// rawReferences walks a declaration and returns its call references and its
// non-call identifier/selector references (candidate type usages), in
// first-seen order with duplicates removed.
func rawReferences(node ast.Node) (calls, types []rawRef) {
	callSeen, typeSeen := map[rawRef]bool{}, map[rawRef]bool{}
	called := map[ast.Expr]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			called[call.Fun] = true
			if ref, ok := exprRef(call.Fun); ok && !callSeen[ref] {
				callSeen[ref] = true
				calls = append(calls, ref)
			}
		}
		return true
	})
	ast.Inspect(node, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok || called[expr] {
			return true
		}
		if ref, ok := exprRef(expr); ok && !typeSeen[ref] && !callSeen[ref] {
			typeSeen[ref] = true
			types = append(types, ref)
		}
		return true
	})
	return calls, types
}

// exprRef reduces an expression to a raw reference when it is a bare
// identifier or a `qualifier.Member` selector; anything else is not a
// resolvable symbol reference.
func exprRef(expr ast.Expr) (rawRef, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return rawRef{name: e.Name}, true
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return rawRef{pkg: x.Name, name: e.Sel.Name}, true
		}
	}
	return rawRef{}, false
}

// symbolIndex resolves reference candidates to declared qualified names.
type symbolIndex struct {
	// kindByQualified maps a qualified name to its symbol kind.
	kindByQualified map[string]string
	// byMember maps an unqualified member name to every qualified name whose
	// last dot-separated segment matches it. Only unique matches resolve.
	byMember map[string][]string
	// testFiles records which qualified names were declared in a _test.go file.
	testDecl map[string]bool
}

// buildIndex indexes every declared symbol for reference resolution.
func buildIndex(units []*unit) *symbolIndex {
	idx := &symbolIndex{
		kindByQualified: map[string]string{},
		byMember:        map[string][]string{},
		testDecl:        map[string]bool{},
	}
	for _, u := range units {
		for _, d := range u.decls {
			qual := d.Symbol.QualifiedName
			idx.kindByQualified[qual] = d.Symbol.Kind
			member := qual[strings.LastIndex(qual, ".")+1:]
			idx.byMember[member] = append(idx.byMember[member], qual)
			if u.isTest {
				idx.testDecl[qual] = true
			}
		}
	}
	for member := range idx.byMember {
		sort.Strings(idx.byMember[member])
	}
	return idx
}

// resolve turns one raw reference into a declared qualified name. Resolution is
// deliberately conservative: a same-package or aliased-import hit wins, and a
// bare member name resolves only when exactly one declaration carries it, so an
// ambiguous name produces no edge rather than a wrong one.
func (idx *symbolIndex) resolve(ref rawRef, u *unit) (string, bool) {
	if ref.pkg == "" {
		if qual := u.pkgPath + "." + ref.name; idx.kindByQualified[qual] != "" {
			return qual, true
		}
		return idx.uniqueMember(ref.name)
	}
	if imp, ok := u.imports[ref.pkg]; ok {
		if qual, found := idx.resolveInImport(imp, ref.name); found {
			return qual, true
		}
	}
	if qual := u.pkgPath + "." + ref.pkg + "." + ref.name; idx.kindByQualified[qual] != "" {
		return qual, true
	}
	return idx.uniqueMember(ref.name)
}

// resolveInImport resolves `member` inside an imported package path. The import
// path is a full module path while symbols are keyed by repo-relative directory,
// so the longest matching suffix of the import path is tried.
func (idx *symbolIndex) resolveInImport(importPath, member string) (string, bool) {
	segments := strings.Split(importPath, "/")
	for i := range segments {
		candidate := strings.Join(segments[i:], "/") + "." + member
		if idx.kindByQualified[candidate] != "" {
			return candidate, true
		}
	}
	return "", false
}

// uniqueMember resolves a bare member name when exactly one symbol declares it.
func (idx *symbolIndex) uniqueMember(name string) (string, bool) {
	quals := idx.byMember[name]
	if len(quals) != 1 {
		return "", false
	}
	return quals[0], true
}

// resolveFileReferences turns one file's raw declaration references into typed
// edges: CALLS to functions, IMPORTS to types, and TESTED_BY from a production
// symbol to the test function that exercises it.
func resolveFileReferences(u *unit, idx *symbolIndex) []crg.Reference {
	var out []crg.Reference
	for _, dr := range u.declRefs {
		out = append(out, resolveDeclReferences(u, idx, dr)...)
	}
	return out
}

// resolveDeclReferences resolves a single declaration's references.
func resolveDeclReferences(u *unit, idx *symbolIndex, dr declRef) []crg.Reference {
	seen := map[crg.Reference]bool{}
	var out []crg.Reference
	add := func(ref crg.Reference) {
		if ref.From == ref.To || seen[ref] {
			return
		}
		seen[ref] = true
		out = append(out, ref)
	}
	for _, raw := range dr.calls {
		addResolved(idx, u, dr, raw, kindFunction, edgeCalls, add)
	}
	for _, raw := range dr.types {
		addResolved(idx, u, dr, raw, kindType, edgeImports, add)
	}
	return out
}

// addResolved resolves one raw reference and emits the edge for it. A reference
// made from a test declaration to a non-test symbol is inverted into a
// TESTED_BY edge (production symbol → test), which is the direction the crg
// adapter's schema declares.
func addResolved(idx *symbolIndex, u *unit, dr declRef, raw rawRef, wantKind, edgeKind string, add func(crg.Reference)) {
	target, ok := idx.resolve(raw, u)
	if !ok || idx.kindByQualified[target] != wantKind {
		return
	}
	if u.isTest && !idx.testDecl[target] {
		add(crg.Reference{Kind: edgeTestedBy, From: target, To: dr.qualified})
		return
	}
	add(crg.Reference{Kind: edgeKind, From: dr.qualified, To: target})
}

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
