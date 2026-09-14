package codegraph

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// languageContractFS holds the pinned upstream release's indexed-source
// inventory. It is embedded rather than read from testdata because the
// native-vs-bridge routing decision is made at RUNTIME, and a shipped binary
// has no testdata directory. `go:embed` cannot reach outside the package
// directory, so the file is a byte-for-byte duplicate of
// testdata/crg-release/v2.3.8/languages.json; capability_test.go compares the
// two so they cannot drift.
//
//go:embed contract/v2.3.8/languages.json
var languageContractFS embed.FS

// LanguageNative is the ONE language the kg-native scanner extracts. Every
// other native/bridge decision in this file is derived from it — the set of
// native file extensions is "the extensions upstream maps to this language",
// not a second hard-coded list that could drift from the scanner.
const LanguageNative = languageGo

// LanguageUnindexed is the bucket for files the pinned upstream release does
// not index at all. It is deliberately NOT an upstream language name: those
// files are neither native nor bridge work, so they cannot force routing
// either way. Reporting them keeps the diagnostic's three totals summing to
// the number of files walked.
const LanguageUnindexed = "unindexed"

// LanguageShebangScript buckets extension-LESS files whose first bytes are
// `#!`. Upstream routes those through SHEBANG_INTERPRETER_TO_LANGUAGE
// (parser.detect_language probes 256 bytes whenever the suffix lookup misses
// and the path has no suffix at all), so a repository of extension-less
// scripts is upstream-indexed work the native scanner would silently drop.
//
// The bucket exists because the pinned inventory records extensions, not
// interpreters: we can prove such a file IS script source without being able
// to name its language. Counting it as bridge work is the safe direction —
// upstream returns nil for an interpreter it does not map, so at worst this
// routes a repository to a bridge that would have handled it identically.
const LanguageShebangScript = "shebang-script"

// Routing decisions returned by CapabilityReport.Routing.
const (
	// RoutingNative means the kg-native engine can serve the whole repository.
	RoutingNative = "native"
	// RoutingBridge means at least one file is in a language upstream indexes
	// and the native scanner does not, so the repository must be served by the
	// retained Python `code-review-graph` bridge.
	RoutingBridge = "bridge"
)

// languageContract is the generated extension→language inventory of the pinned
// upstream release.
type languageContract struct {
	// Languages maps each upstream language to the extensions it claims.
	Languages map[string][]string `json:"languages"`
	// Extensions is the same relation inverted: the lookup the scanner does.
	Extensions map[string]string `json:"extensions"`
}

var (
	upstream        = mustLanguageContract()
	nativeLanguages = map[string]bool{LanguageNative: true}
)

func mustLanguageContract() languageContract {
	const name = "contract/v2.3.8/languages.json"
	data, err := languageContractFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("codegraph: embedded language contract %s missing: %v", name, err))
	}
	var contract languageContract
	if err := json.Unmarshal(data, &contract); err != nil {
		panic(fmt.Sprintf("codegraph: decode embedded language contract %s: %v", name, err))
	}
	if len(contract.Extensions) == 0 {
		panic("codegraph: embedded language contract has no extensions")
	}
	if _, ok := contract.Languages[LanguageNative]; !ok {
		panic(fmt.Sprintf("codegraph: embedded language contract does not know %q", LanguageNative))
	}
	return contract
}

// SourceCapability is one language's presence in a repository plus who can
// serve it.
type SourceCapability struct {
	// Language is the upstream language identifier, or LanguageUnindexed for
	// files no upstream language claims.
	Language string `json:"language"`
	// Extensions are the extensions actually OBSERVED under the scanned root
	// for this language, sorted — not every extension the language can claim.
	Extensions []string `json:"extensions"`
	// Files is how many files were counted for this language.
	Files int `json:"files"`
	// Native reports whether the kg-native scanner extracts this language.
	Native bool `json:"native"`
	// UpstreamSupported reports whether the pinned upstream release indexes it.
	UpstreamSupported bool `json:"upstream_supported"`
}

// CapabilityReport answers "can the kg-native backend serve this repository?"
// for one repository root.
type CapabilityReport struct {
	// Root is the scanned repository root, as given.
	Root string `json:"root"`
	// Languages is the per-language breakdown, sorted by language name.
	Languages []SourceCapability `json:"languages"`
	// NativeFiles, BridgeFiles and UnsupportedFiles partition every file the
	// walk counted: extracted natively, indexed only by upstream, and indexed
	// by neither.
	NativeFiles      int `json:"native_files"`
	BridgeFiles      int `json:"bridge_files"`
	UnsupportedFiles int `json:"unsupported_files"`
	// FullyNative is the routing predicate. It is deliberately conservative:
	// a single file in a language upstream indexes and the native scanner does
	// not is enough to fall back to the bridge, and a repository with nothing
	// native to extract is not "fully native" either.
	FullyNative bool `json:"fully_native"`
}

// ScanCapability walks root and classifies every file by who can index it.
//
// The walk uses the ingester's own pruning rules (skipDirEntry: hidden
// directories, VCS metadata, vendored trees, build output), so the diagnostic
// and the ingester always agree on which files count — a vendored Python tree
// no more forces bridge routing than it contributes nodes to the graph.
//
// Classification is by lowercased file extension against the pinned upstream
// inventory, plus the one case an extension cannot answer: an extension-less
// file beginning with `#!` is script source upstream routes by interpreter,
// so it is counted as bridge work (see LanguageShebangScript).
func ScanCapability(root string) (CapabilityReport, error) {
	if _, err := os.Stat(root); err != nil {
		return CapabilityReport{}, fmt.Errorf("codegraph: capability scan %s: %w", root, err)
	}
	scan := newCapabilityScan(root)
	if err := walkDir(root, scan.visit); err != nil {
		return CapabilityReport{}, fmt.Errorf("codegraph: capability scan %s: %w", root, err)
	}
	return scan.report(), nil
}

// capabilityScan accumulates one walk's tallies: how many files each language
// claimed, which extensions were actually observed for it, and the running
// native/bridge/unsupported partition.
type capabilityScan struct {
	root             string
	counts           map[string]int
	observed         map[string]map[string]bool
	nativeFiles      int
	bridgeFiles      int
	unsupportedFiles int
}

func newCapabilityScan(root string) *capabilityScan {
	return &capabilityScan{
		root:     root,
		counts:   map[string]int{},
		observed: map[string]map[string]bool{},
	}
}

// visit is the walk callback: it prunes with the ingester's own rules so the
// diagnostic and the ingester always agree on which files count, and hands
// every regular file to countFile.
func (s *capabilityScan) visit(path string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return nil // unreadable subtree — skip, exactly as ingestion does
	}
	if d.IsDir() {
		return skipDirEntry(s.root, path, d)
	}
	if !d.Type().IsRegular() {
		return nil
	}
	s.countFile(path, d.Name())
	return nil
}

// countFile classifies one regular file by who can index it and folds it into
// the tallies.
func (s *capabilityScan) countFile(path, name string) {
	// Upstream lowercases a file's suffix before the lookup (".R" → ".r"),
	// so a case-different extension is the same language here too.
	ext := strings.ToLower(filepath.Ext(name))
	language, supported := upstream.Extensions[ext]
	if !supported && ext == "" && hasShebang(path) {
		language, supported = LanguageShebangScript, true
	}
	switch {
	case !supported:
		language = LanguageUnindexed
		s.unsupportedFiles++
	case nativeLanguages[language]:
		s.nativeFiles++
	default:
		s.bridgeFiles++
	}
	s.counts[language]++
	if ext != "" {
		exts := s.observed[language]
		if exts == nil {
			exts = map[string]bool{}
			s.observed[language] = exts
		}
		exts[ext] = true
	}
}

// report renders the accumulated tallies as the language-sorted report.
func (s *capabilityScan) report() CapabilityReport {
	report := CapabilityReport{
		Root:             s.root,
		Languages:        make([]SourceCapability, 0, len(s.counts)),
		NativeFiles:      s.nativeFiles,
		BridgeFiles:      s.bridgeFiles,
		UnsupportedFiles: s.unsupportedFiles,
	}
	for language, files := range s.counts {
		report.Languages = append(report.Languages, SourceCapability{
			Language:          language,
			Extensions:        sortedKeys(s.observed[language]),
			Files:             files,
			Native:            nativeLanguages[language],
			UpstreamSupported: language != LanguageUnindexed,
		})
	}
	sort.Slice(report.Languages, func(i, j int) bool {
		return report.Languages[i].Language < report.Languages[j].Language
	})
	report.FullyNative = report.BridgeFiles == 0 && report.NativeFiles > 0
	return report
}

// BridgeLanguages returns the upstream-indexed languages the native scanner
// cannot extract, sorted. These are exactly the languages that force bridge
// routing, which is why the routing reason can name them.
func (r CapabilityReport) BridgeLanguages() []string {
	var out []string
	for _, lang := range r.Languages {
		if lang.UpstreamSupported && !lang.Native && lang.Files > 0 {
			out = append(out, lang.Language)
		}
	}
	return out
}

// Routing is the backend this repository must be served by.
func (r CapabilityReport) Routing() string {
	if r.FullyNative {
		return RoutingNative
	}
	return RoutingBridge
}

// Reason explains the routing decision in one line, naming the languages that
// forced it so the operator can act on the report rather than re-derive it.
func (r CapabilityReport) Reason() string {
	if r.FullyNative {
		return fmt.Sprintf(
			"every indexed file (%d) is %s, which the kg-native scanner extracts",
			r.NativeFiles, LanguageNative,
		)
	}
	if bridged := r.BridgeLanguages(); len(bridged) > 0 {
		return fmt.Sprintf(
			"%d file(s) in %s are indexed by code-review-graph but not extracted by the kg-native scanner",
			r.BridgeFiles, strings.Join(bridged, ", "),
		)
	}
	return fmt.Sprintf(
		"no %s source found under the repository root, so the kg-native scanner has nothing to extract",
		LanguageNative,
	)
}

// hasShebang reports whether path's first two bytes are `#!`. That is exactly
// the precondition upstream's shebang probe applies before it consults the
// interpreter table, so a false here means upstream cannot route the file
// either. Unreadable files answer false: the ingester skips them too.
func hasShebang(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == '#' && head[1] == '!'
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
