package crgbehavior

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// probe opens a seeded fixture and runs the capability probe directly.
func probe(t *testing.T, seed string, rel Release) (SchemaReport, error) {
	t.Helper()
	db, err := sql.Open(sqliteDriver, newGraphDB(t, seed))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return ProbeSchema(db, rel)
}

// A complete, built graph reports every table populated, so every surface it
// backs is genuinely exercised.
func TestProbeSchemaReportsPopulatedTables(t *testing.T) {
	report, err := probe(t, twoFlowSeed, testRelease())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if report.SchemaVersion != PinnedSchemaVersion {
		t.Fatalf("schema version = %d, want %d", report.SchemaVersion, PinnedSchemaVersion)
	}
	for _, c := range report.Capabilities {
		if c.State != CapPopulated {
			t.Fatalf("%s = %s (rows=%d), want populated", c.Table, c.State, c.Rows)
		}
	}
	if uncomputed := report.UncomputedSurfaces(); len(uncomputed) != 0 {
		t.Fatalf("a fully populated graph reported uncomputed surfaces: %v", uncomputed)
	}
}

// An optional view the release did not materialize and one it materialized but
// left empty are DIFFERENT facts, and neither is a divergence. Both must
// disable exactly the surfaces they back.
func TestProbeSchemaDistinguishesMissingFromEmptyViews(t *testing.T) {
	seed := twoFlowSeed + "\nDROP TABLE flow_snapshots;\nDELETE FROM risk_index;\n"
	report, err := probe(t, seed, testRelease())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	missing, ok := report.Of(tableFlowSnapshots)
	if !ok || missing.State != CapTableMissing {
		t.Fatalf("flow_snapshots = %+v, want table_missing", missing)
	}
	empty, ok := report.Of(tableRiskIndex)
	if !ok || empty.State != CapEmpty || empty.Rows != 0 {
		t.Fatalf("risk_index = %+v, want empty with zero rows", empty)
	}
	if missing.Usable() {
		t.Fatal("a missing table must not be readable")
	}
	if !empty.Usable() {
		t.Fatal("an existing empty table is readable — it just has no rows")
	}
	uncomputed := report.UncomputedSurfaces()
	for _, surface := range []string{SurfaceFlowSnapshots, SurfaceRiskIndex, SurfaceRiskDetail} {
		if _, ok := uncomputed[surface]; !ok {
			t.Fatalf("surface %q was not reported as uncomputed: %v", surface, uncomputed)
		}
	}
	if !strings.Contains(uncomputed[SurfaceRiskIndex], PinnedVersion) {
		t.Fatalf("an uncomputed surface must name the detected release: %q", uncomputed[SurfaceRiskIndex])
	}
}

// A column the gate reads that is not present can never be an uncomputed view:
// the read would fail, and a failed read must never look like an empty answer.
// It is fatal whether the table is required or optional.
func TestProbeSchemaFailsOnAMissingColumn(t *testing.T) {
	cases := map[string]string{
		"optional view": "\nDROP TABLE risk_index;\nCREATE TABLE risk_index (node_id INTEGER, qualified_name TEXT);\n",
		"required base": "\nDROP TABLE edges;\nCREATE TABLE edges (kind TEXT, source_qualified TEXT, target_qualified TEXT, file_path TEXT, line INTEGER);\n",
	}
	for name, mutation := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := probe(t, twoFlowSeed+mutation, testRelease())
			if !errors.Is(err, ErrSchemaIncompatible) {
				t.Fatalf("probe error = %v, want ErrSchemaIncompatible", err)
			}
		})
	}
}

// Required schema is the gate's plumbing floor: absent, empty, or at the wrong
// version, there is nothing to compare and the run must say so rather than
// report an unavailable view.
func TestProbeSchemaFailsOnRequiredSchemaProblems(t *testing.T) {
	cases := map[string]string{
		"missing required table": "\nDROP TABLE edges;\n",
		"empty required table":   "\nDELETE FROM nodes;\n",
		"wrong schema version":   "\nUPDATE metadata SET value='8' WHERE key='schema_version';\n",
		"no schema version row":  "\nDELETE FROM metadata WHERE key='schema_version';\n",
		"unreadable version":     "\nUPDATE metadata SET value='not-a-number' WHERE key='schema_version';\n",
		"no metadata table":      "\nDROP TABLE metadata;\n",
	}
	for name, mutation := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := probe(t, twoFlowSeed+mutation, testRelease()); !errors.Is(err, ErrSchemaIncompatible) {
				t.Fatalf("probe error = %v, want ErrSchemaIncompatible", err)
			}
		})
	}
}

// An arbitrary SQL failure is a FAILURE. Absorbing it into "that view is
// unavailable" is how a broken environment produced a green run.
func TestProbeSchemaSurfacesArbitrarySQLFailures(t *testing.T) {
	db, err := sql.Open(sqliteDriver, newGraphDB(t, twoFlowSeed))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	db.Close() // every query now fails for a reason that is not a schema fact
	_, err = ProbeSchema(db, testRelease())
	switch {
	case err == nil:
		t.Fatal("a dead connection produced no error")
	case errors.Is(err, ErrSchemaIncompatible):
		t.Fatalf("a connection failure was misreported as a schema fact: %v", err)
	}
}

// A release fixture naming something that is not a SQL identifier is rejected
// at load (see TestReleaseRejectsANonIdentifierTableName); countRows keeps the
// same allowlist as defence in depth so no table name can reach SQL text.
func TestCountRowsRefusesANonIdentifier(t *testing.T) {
	db, err := sql.Open(sqliteDriver, newGraphDB(t, twoFlowSeed))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := countRows(db, "nodes; DROP TABLE nodes"); err == nil {
		t.Fatal("countRows accepted a non-identifier table name")
	}
}

// --- vsT ---------------------------------------------------------------

// vsTUncountableIndexSeed removes the FTS index's backing store while leaving
// the index itself in the schema: the table is present and declares exactly
// the columns the gate reads, but nothing can be read out of it.
const vsTUncountableIndexSeed = `
PRAGMA writable_schema=ON;
DROP TABLE nodes_fts_data;
PRAGMA writable_schema=OFF;
`

// vsTUnreadableViewSeed is a graph object whose definition cannot be resolved.
const vsTUnreadableViewSeed = `
CREATE VIEW vsT_broken AS SELECT a FROM vsT_gone;
`

// vsTWantProbeFailure requires a probe failure that names the read which
// broke and is NOT classified as a schema fact.
func vsTWantProbeFailure(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("probe error = %v, want one naming %q", err, want)
	}
	if errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("a broken store was misreported as a schema fact: %v", err)
	}
}

// Every capability state has to explain itself: the reason is what a consumer
// reads when a surface is reported as not exercised, and it must tell "the
// release computed nothing" apart from "this graph is wrong".
func TestVsTCapabilityReasonExplainsEveryState(t *testing.T) {
	cases := map[string]struct {
		capability Capability
		want       string
	}{
		"a populated table needs no excuse": {
			Capability{Table: tableFlows, State: CapPopulated}, ""},
		"an empty table names the release that computed nothing": {
			Capability{Table: tableFlows, State: CapEmpty},
			PackageName + " " + PinnedVersion + " materializes flows but computed no rows for this graph"},
		"a missing table names the release that does not have it": {
			Capability{Table: tableFlows, State: CapTableMissing},
			PackageName + " " + PinnedVersion + " graph has no flows table"},
		"a column-incomplete table names the columns": {
			Capability{Table: tableFlows, State: CapColumnMissing, MissingColumns: []string{"criticality", "depth"}},
			"flows exists but lacks column(s) criticality, depth"},
		"an unclassified state is reported verbatim": {
			Capability{Table: tableFlows, State: CapabilityState("quarantined")}, "quarantined"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.capability.Reason(); got != tc.want {
				t.Fatalf("Reason() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The report answers only for tables it actually probed. Inventing a zero
// capability for anything else would read as "present, and not populated" at
// every call site, disabling surfaces on the strength of a typo.
func TestVsTSchemaReportOfOnlyAnswersForProbedTables(t *testing.T) {
	report, err := probe(t, twoFlowSeed, testRelease())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if _, ok := report.Of(tableFlows); !ok {
		t.Fatalf("a probed table is missing from the report: %+v", report.Capabilities)
	}
	capability, ok := report.Of("nodes_fts_data")
	if ok || capability.Table != "" || capability.State != "" {
		t.Fatalf("Of(unprobed) = %+v, %v; want the zero capability and false", capability, ok)
	}
}

// A table the probe cannot even count is a broken environment, not a view the
// release left uncomputed. The two demand opposite verdicts, so the SQL
// failure is returned verbatim.
func TestVsTProbeSchemaSurfacesAnUncountableTable(t *testing.T) {
	_, err := probe(t, twoFlowSeed+vsTUncountableIndexSeed, testRelease())
	vsTWantProbeFailure(t, err, "count nodes_fts rows")
}

// A table whose definition cannot be read is not a table with missing
// columns: the probe must fail rather than classify a column list it never
// obtained, because a missing column is always fatal and would be blamed on
// the release instead of on the graph.
func TestVsTProbeSchemaSurfacesAnUnreadableTableDefinition(t *testing.T) {
	rel := testRelease()
	rel.Tables = append(rel.Tables, TableSpec{Name: "vsT_broken", Columns: []string{"a"}})
	_, err := probe(t, twoFlowSeed+vsTUnreadableViewSeed, rel)
	vsTWantProbeFailure(t, err, "read vsT_broken columns")
}

// The probe classifies rather than guesses, and whatever it cannot classify is
// a FAILURE. A store that answers the schema version and then cannot be
// interrogated must never come back as a graph with missing tables — that is
// how a broken environment produced a green run.
//
// The sequence is scripted because a real SQLite file cannot be coaxed into
// it: every way of breaking the schema listing also breaks the version read
// that precedes it on the same connection.
func TestVsTProbeSchemaSurfacesAStoreItCannotInterrogate(t *testing.T) {
	version := vsTScriptedAnswer{Match: "FROM metadata", Rows: [][]driver.Value{{"9"}}}
	tables := vsTScriptedAnswer{Match: "sqlite_master", Rows: [][]driver.Value{{"nodes"}}}
	rel := Release{SchemaVersion: PinnedSchemaVersion, Tables: []TableSpec{
		{Name: "nodes", Required: true, Columns: []string{"id"}}}}
	cases := map[string]struct {
		script vsTScriptedStore
		want   string
	}{
		"the table list cannot be read": {
			vsTScriptedStore{version, {Match: "sqlite_master", Err: errors.New("disk I/O error")}},
			"list graph tables"},
		"a table name cannot be decoded": {
			vsTScriptedStore{version, {Match: "sqlite_master", Rows: [][]driver.Value{{nil}}}},
			"scan graph table name"},
		"a column name cannot be decoded": {
			vsTScriptedStore{version, tables, {Match: "pragma_table_info", Rows: [][]driver.Value{{nil}}}},
			"scan nodes column"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ProbeSchema(vsTOpenScripted(t, tc.script), rel)
			vsTWantProbeFailure(t, err, tc.want)
		})
	}
}

// vsTScriptedStore is a database/sql store that answers each query from a
// script. It is the only seam that can fail ONE of the probe's reads.
type vsTScriptedStore []vsTScriptedAnswer

// vsTScriptedAnswer is what the store answers for a query containing Match.
type vsTScriptedAnswer struct {
	Match string
	Err   error
	Rows  [][]driver.Value
}

// vsTOpenScripted opens the scripted store as a *sql.DB.
func vsTOpenScripted(t *testing.T, script vsTScriptedStore) *sql.DB {
	t.Helper()
	db := sql.OpenDB(script)
	t.Cleanup(func() { db.Close() })
	return db
}

func (s vsTScriptedStore) Connect(context.Context) (driver.Conn, error) { return s, nil }
func (s vsTScriptedStore) Driver() driver.Driver                        { return s }
func (s vsTScriptedStore) Open(string) (driver.Conn, error)             { return s, nil }
func (s vsTScriptedStore) Close() error                                 { return nil }
func (s vsTScriptedStore) Begin() (driver.Tx, error)                    { return nil, errors.New("vsT: read-only store") }

func (s vsTScriptedStore) Prepare(query string) (driver.Stmt, error) {
	for _, answer := range s {
		if strings.Contains(query, answer.Match) {
			return vsTScriptedStmt(answer), nil
		}
	}
	return nil, fmt.Errorf("vsT: the probe issued an unscripted query: %s", query)
}

// vsTScriptedStmt answers one scripted query.
type vsTScriptedStmt vsTScriptedAnswer

func (s vsTScriptedStmt) Close() error  { return nil }
func (s vsTScriptedStmt) NumInput() int { return -1 }

func (s vsTScriptedStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("vsT: read-only store")
}

func (s vsTScriptedStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	return &vsTScriptedRows{rows: s.Rows}, nil
}

// vsTScriptedRows replays one scripted answer's rows.
type vsTScriptedRows struct {
	rows [][]driver.Value
	next int
}

func (r *vsTScriptedRows) Columns() []string { return []string{"value"} }
func (r *vsTScriptedRows) Close() error      { return nil }

func (r *vsTScriptedRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
