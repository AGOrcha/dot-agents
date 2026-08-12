package kg

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
)

// Closed-set flag vocabularies for the `da kg` surface, declared once each and
// registered through cmdutil.RegisterEnumFlag so help, completion, and
// validation cannot drift apart. See docs/CLI_HELP_CONVENTIONS.md.

// kgQueryIntentEnum lists the note-graph intents `kg query` can answer. The
// vocabulary is derived from validQueryIntents so adding an intent there shows
// up in `--help` with no second edit.
var kgQueryIntentEnum = cmdutil.EnumSpec{
	Name:     "intent",
	Usage:    "Which question to ask the note graph",
	Values:   cmdutil.SortedValues(sortedKeys(validQueryIntents)),
	Required: true,
}

// kgBridgeIntentEnum lists the bridge intents, derived from the bridge mapping
// table so a new mapping is documented the moment it is registered.
var kgBridgeIntentEnum = cmdutil.EnumSpec{
	Name:     "intent",
	Usage:    "Which question to ask through the graph bridge",
	Values:   cmdutil.SortedValues(sortedKeys(validBridgeIntents)),
	Required: true,
}

// kgNoteTypeValues is the note-type vocabulary shared by ingest and warm-sync.
var kgNoteTypeValues = []string{"source", "entity", "concept", "synthesis", "decision", "repo", "session"}

// kgIngestTypeEnum classifies the raw source being ingested. It is a different
// axis from note type: these are input formats, not graph roles.
var kgIngestTypeEnum = cmdutil.EnumSpec{
	Name:    "type",
	Usage:   "Format of the source being ingested",
	Values:  []string{"markdown", "text", "pdf", "url", "transcript", "meeting_notes", "repo_doc"},
	Default: "markdown",
}

// kgWarmTypeEnum narrows a warm-store sync to one note type.
var kgWarmTypeEnum = cmdutil.EnumSpec{
	Name:   "type",
	Usage:  "Only sync notes of this type",
	Values: kgNoteTypeValues,
	Note:   "omit to sync every type",
}

// kgLinkKindEnum is the note→symbol relationship vocabulary.
var kgLinkKindEnum = cmdutil.EnumSpec{
	Name:    "kind",
	Usage:   "Relationship the link asserts between note and symbol",
	Values:  []string{"mentions", "implements", "documents", "decides", "references"},
	Default: "mentions",
}

// kgLintCheckEnum names the integrity checks `kg lint` runs.
var kgLintCheckEnum = cmdutil.EnumSpec{
	Name:   "check",
	Usage:  "Run only this one check",
	Values: []string{"broken_links", "orphan_pages", "missing_source_refs", "stale_pages", "index_drift", "oversize_pages", "contradictions"},
	Note:   "omit to run every check",
}

// kgFlowSortEnum orders the flow listing.
var kgFlowSortEnum = cmdutil.EnumSpec{
	Name:    "sort",
	Usage:   "Order flows by",
	Values:  []string{"criticality", "size"},
	Default: "criticality",
}

// kgCommunitySortEnum orders the community listing.
var kgCommunitySortEnum = cmdutil.EnumSpec{
	Name:    "sort",
	Usage:   "Order communities by",
	Values:  []string{"size", "cohesion"},
	Default: "size",
}
