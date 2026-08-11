package lifecycle

// ImportCandidate is the exported, lifecycle-facing shape of an import
// candidate: the project name plus the source root + source file path
// from which a canonical import is produced. Lifted from
// commands/import.go in plan root-command-decomposition t02b.
//
// The shape is intentionally identical to the unexported importCandidate
// struct in commands/import.go; the function-var seam below
// (CanonicalImportOutputs) converts between the two when bridging into
// the canonicalImportOutputs machinery that still lives in import.go
// until t06 moves the whole import command. Once t06 lands, the
// commands/import.go duplicate can be deleted and the lifecycle shape
// becomes the only one.
type ImportCandidate struct {
	Project    string
	SourceRoot string
	SourcePath string
	DestRel    string
	// AgentsHome is the agents home whose existing canonical hook bundles
	// establish provenance for a hook-shaped source: an entry that home
	// already renders is a render output, not a hook to capture. Empty
	// means "no home in play" — nothing can claim ownership and every
	// parsed entry is treated as hand-authored.
	AgentsHome string
}

// ImportOutput is the exported, lifecycle-facing shape of a canonical
// import emission: the destination-relative path and content bytes,
// plus the optional origin platform id used by RFC §6 alternate naming.
// Lifted from commands/import.go in t02b alongside ImportCandidate.
type ImportOutput struct {
	DestRel string
	Content []byte
	Origin  string
}

// CanonicalImportOutputs is wired at init time by commands/import.go.
// Returns ([]ImportOutput, handled, err); handled=false means the
// candidate is not a canonical resource (not a hook bundle / settings
// file the canonical importer recognizes) and the caller should fall
// back to legacy per-file restore. Stub default returns "not handled"
// so the lifecycle package builds standalone and runs without the
// commands package's init() (e.g. in commands/lifecycle/... tests that
// do not exercise canonical-import).
var CanonicalImportOutputs = func(c ImportCandidate) ([]ImportOutput, bool, error) {
	return nil, false, nil
}
