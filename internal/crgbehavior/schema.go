package crgbehavior

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrReleaseMismatch reports that the bridge driven by this run is not the
// pinned release. It is a hard failure: a comparison against an unpinned build
// certifies nothing.
var ErrReleaseMismatch = errors.New("crgbehavior: bridge is not the pinned code-review-graph release")

// ErrSchemaIncompatible reports that the bridge's graph.db does not carry the
// pinned release's REQUIRED schema — a missing base table, a missing column, or
// a schema_version other than the pinned one. This is a release/plumbing
// incompatibility, NOT a behavior divergence, and it is never downgraded to
// "that view was not computed".
var ErrSchemaIncompatible = errors.New("crgbehavior: bridge graph schema is incompatible with the pinned release")

// CapabilityState classifies one table of the bridge's schema. The four states
// are deliberately distinct because they demand different verdicts:
//
//   - CapPopulated    — the table exists with every column the gate reads and
//     holds rows: the surfaces it feeds are exercised.
//   - CapEmpty        — the table EXISTS with the right columns but holds zero
//     rows: the release computed no data for that view. Optional views may be
//     legitimately uncomputed; the surfaces are reported as NOT EXERCISED with
//     the detected release, never as agreeing.
//   - CapTableMissing — no such table in sqlite_master. For a required table
//     that is a schema incompatibility; for an optional view it means the
//     release does not materialize it at all.
//   - CapColumnMissing — the table exists but lacks a column the gate reads.
//     This is ALWAYS a plumbing failure, never an uncomputed view: reading a
//     column that is not there cannot be mistaken for an empty answer.
type CapabilityState string

const (
	CapPopulated     CapabilityState = "populated"
	CapEmpty         CapabilityState = "empty"
	CapTableMissing  CapabilityState = "table_missing"
	CapColumnMissing CapabilityState = "column_missing"
)

// Capability is the probe result for one table.
type Capability struct {
	Table          string          `json:"table"`
	Required       bool            `json:"required"`
	State          CapabilityState `json:"state"`
	MissingColumns []string        `json:"missing_columns,omitempty"`
	Rows           int             `json:"rows"`
	Surfaces       []string        `json:"surfaces,omitempty"`
}

// Usable reports whether the gate may read this table at all.
func (c Capability) Usable() bool {
	return c.State == CapPopulated || c.State == CapEmpty
}

// Reason explains a non-populated capability in the report's own words.
func (c Capability) Reason() string {
	switch c.State {
	case CapPopulated:
		return ""
	case CapEmpty:
		return fmt.Sprintf("%s %s materializes %s but computed no rows for this graph",
			PackageName, PinnedVersion, c.Table)
	case CapTableMissing:
		return fmt.Sprintf("%s %s graph has no %s table", PackageName, PinnedVersion, c.Table)
	case CapColumnMissing:
		return fmt.Sprintf("%s exists but lacks column(s) %s", c.Table, strings.Join(c.MissingColumns, ", "))
	default:
		return string(c.State)
	}
}

// SchemaReport is the full capability probe of one bridge graph: the schema
// version it declares and the state of every table the gate reads. It is
// persisted in the JSON artifact so a run's evidence records which surfaces
// were actually backed by data.
type SchemaReport struct {
	SchemaVersion int          `json:"schema_version"`
	Capabilities  []Capability `json:"capabilities"`
}

// Of returns one table's capability.
func (s SchemaReport) Of(table string) (Capability, bool) {
	for _, c := range s.Capabilities {
		if c.Table == table {
			return c, true
		}
	}
	return Capability{}, false
}

// UncomputedSurfaces maps every surface disabled by a non-populated table to
// the reason it is disabled.
func (s SchemaReport) UncomputedSurfaces() map[string]string {
	out := map[string]string{}
	for _, c := range s.Capabilities {
		if c.State == CapPopulated {
			continue
		}
		for _, surface := range c.Surfaces {
			out[surface] = c.Reason()
		}
	}
	return out
}

// rowCountSQL is the row-count statement for every table the pinned release
// writes.
//
// The statements are LITERAL. SQL cannot bind a table name, and building the
// query by concatenation — even behind an identifier allowlist — puts the
// decision about what is safe in the probe instead of in the source. The gate
// certifies ONE release with ONE known schema (PinnedSchemaVersion), so the
// set of countable tables is closed: a fixture naming a table that is not here
// is a fixture defect the probe reports, never a query the probe invents.
//
// TestSchemaCountsEveryReleaseTable keeps this in lockstep with the checked-in
// release fixture.
var rowCountSQL = map[string]string{
	tableMetadata:           `SELECT COUNT(*) FROM "metadata"`,
	tableNodes:              `SELECT COUNT(*) FROM "nodes"`,
	tableEdges:              `SELECT COUNT(*) FROM "edges"`,
	tableFlows:              `SELECT COUNT(*) FROM "flows"`,
	tableFlowMemberships:    `SELECT COUNT(*) FROM "flow_memberships"`,
	tableFlowSnapshots:      `SELECT COUNT(*) FROM "flow_snapshots"`,
	tableCommunities:        `SELECT COUNT(*) FROM "communities"`,
	tableCommunitySummaries: `SELECT COUNT(*) FROM "community_summaries"`,
	tableRiskIndex:          `SELECT COUNT(*) FROM "risk_index"`,
	tableNodesFTS:           `SELECT COUNT(*) FROM "nodes_fts"`,
}

// ProbeSchema reads the bridge graph's declared schema version and the state of
// every table the pinned release's fixture describes, BEFORE any view is read.
//
// The probe is the gate's release-compatibility boundary. It classifies rather
// than guesses:
//
//   - schema_version absent or != the pinned version → ErrSchemaIncompatible.
//   - a REQUIRED table or column missing               → ErrSchemaIncompatible.
//   - an optional view missing / empty                 → reported capability.
//   - any other SQL error                              → returned verbatim.
//
// The last clause matters most: an arbitrary SQLite failure (a corrupt page, a
// locked database, an FTS5 module the driver cannot load) is a FAILURE. It is
// never absorbed into "that view is unavailable", which is how a broken
// environment previously produced a green run.
func ProbeSchema(db *sql.DB, rel Release) (SchemaReport, error) {
	version, err := readSchemaVersion(db)
	if err != nil {
		return SchemaReport{}, err
	}
	if version != rel.SchemaVersion {
		return SchemaReport{}, fmt.Errorf("%w: graph declares schema_version %d, the pinned release writes %d",
			ErrSchemaIncompatible, version, rel.SchemaVersion)
	}
	present, err := tableNames(db)
	if err != nil {
		return SchemaReport{}, err
	}
	report := SchemaReport{SchemaVersion: version}
	var incompatible []string
	for _, spec := range rel.Tables {
		tableCap, err := probeTable(db, spec, present)
		if err != nil {
			return SchemaReport{}, err
		}
		switch {
		case tableCap.State == CapColumnMissing:
			// A column the gate reads that is not there can never be an
			// "uncomputed view": reading it would fail, and a failed read must
			// not be reported as an empty answer. It is a schema mismatch
			// against the pinned release whether the table is required or not.
			incompatible = append(incompatible, tableCap.Reason())
		case spec.Required && tableCap.State == CapTableMissing:
			incompatible = append(incompatible, tableCap.Reason())
		case spec.Required && tableCap.State == CapEmpty && spec.Name != tableMetadata:
			incompatible = append(incompatible, fmt.Sprintf(
				"required table %s is empty — the graph was never built", spec.Name))
		}
		report.Capabilities = append(report.Capabilities, tableCap)
	}
	if len(incompatible) > 0 {
		sort.Strings(incompatible)
		return SchemaReport{}, fmt.Errorf("%w: %s", ErrSchemaIncompatible, strings.Join(incompatible, "; "))
	}
	return report, nil
}

// tableMetadata is the release's key/value table; it holds the schema version
// and is allowed to hold nothing else.
const tableMetadata = "metadata"

// readSchemaVersion reads `metadata.schema_version`. A graph without it is not
// a graph this gate can reason about.
func readSchemaVersion(db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRow(`SELECT value FROM metadata WHERE key = 'schema_version'`).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("%w: graph metadata declares no schema_version", ErrSchemaIncompatible)
	case err != nil && isMissingTableErr(err, tableMetadata):
		return 0, fmt.Errorf("%w: graph has no metadata table", ErrSchemaIncompatible)
	case err != nil:
		return 0, fmt.Errorf("crgbehavior: read graph schema_version: %w", err)
	}
	version, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil {
		return 0, fmt.Errorf("%w: graph schema_version %q is not a number", ErrSchemaIncompatible, raw)
	}
	return version, nil
}

// tableNames lists every table and view the graph holds.
func tableNames(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table','view')`)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: list graph tables: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan graph table name: %w", err)
		}
		out[name] = true
	}
	return out, rowsErr(rows, "sqlite_master")
}

// probeTable classifies one table.
func probeTable(db *sql.DB, spec TableSpec, present map[string]bool) (Capability, error) {
	tableCap := Capability{Table: spec.Name, Required: spec.Required, Surfaces: spec.Surfaces}
	if !present[spec.Name] {
		tableCap.State = CapTableMissing
		return tableCap, nil
	}
	columns, err := tableColumns(db, spec.Name)
	if err != nil {
		return Capability{}, err
	}
	for _, want := range spec.Columns {
		if !columns[want] {
			tableCap.MissingColumns = append(tableCap.MissingColumns, want)
		}
	}
	if len(tableCap.MissingColumns) > 0 {
		sort.Strings(tableCap.MissingColumns)
		tableCap.State = CapColumnMissing
		return tableCap, nil
	}
	rows, err := countRows(db, spec.Name)
	if err != nil {
		return Capability{}, err
	}
	tableCap.Rows = rows
	if rows == 0 {
		tableCap.State = CapEmpty
		return tableCap, nil
	}
	tableCap.State = CapPopulated
	return tableCap, nil
}

// tableColumns reads a table's columns through the bindable pragma
// table-valued function, so no table name is ever interpolated into SQL here.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read %s columns: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan %s column: %w", table, err)
		}
		out[name] = true
	}
	return out, rowsErr(rows, table)
}

// countRows returns a table's row count, using the literal statement declared
// for that table. A table the pinned release is not known to write is refused
// rather than counted through a query assembled at run time.
func countRows(db *sql.DB, table string) (int, error) {
	query, ok := rowCountSQL[table]
	if !ok {
		return 0, fmt.Errorf("crgbehavior: release fixture declares table %q, which %s %s is not known to write",
			table, PackageName, PinnedVersion)
	}
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		return 0, fmt.Errorf("crgbehavior: count %s rows: %w", table, err)
	}
	return n, nil
}

// isMissingTableErr reports whether err is SQLite's "no such table" for table.
func isMissingTableErr(err error, table string) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") && strings.Contains(msg, strings.ToLower(table))
}
