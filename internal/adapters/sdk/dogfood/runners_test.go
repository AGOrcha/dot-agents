package dogfood

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// fieldStatedLocation is the character note's stated_location ref field name,
// hoisted to a const so the literal is declared once (it is read in the bare-
// ref Q1 and the ref-traversal Q2 runners and their assertions).
const fieldStatedLocation = "stated_location"

// This file implements the queries.yaml named queries with v1 §5 DSL semantics
// as Go runners. They stand in for compiled DSL queries (the internal/kg/dsl
// compiler is a sibling task, not yet built — STATE NOTE in TASKS.yaml). Each
// runner is annotated with the §5 construct it models, and every one goes
// through sdk.Query so the token-scoped read boundary (§8.2) is exercised: a
// named query sees ONLY the ttrpg namespace, never cross-adapter data (§8.3).

// noteByID indexes a namespace view's notes by id for ref resolution.
func noteByID(v sdk.NamespaceView) map[string]sdk.Note {
	m := make(map[string]sdk.Note, len(v.Notes))
	for _, n := range v.Notes {
		m[n.ID] = n
	}
	return m
}

func strField(n sdk.Note, k string) (string, bool) {
	if n.Fields == nil {
		return "", false
	}
	v, ok := n.Fields[k]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// intField reads an int-typed field. JSON numbers decode to float64.
func intField(n sdk.Note, k string) (int, bool) {
	if n.Fields == nil {
		return 0, false
	}
	switch v := n.Fields[k].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

// Q1 — bare-ref read (§5.4.1 rule 1 / T5). RETURN c.name, c.stated_location:
// the bare ref reads the scalar ref id from the source row; NO JOIN.
func runCharacterLocation(t *testing.T, s *sdk.SDK, charID string) []map[string]string {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		var out []sdk.Row
		for _, c := range v.NotesByType("character") {
			if c.ID != charID {
				continue
			}
			name, _ := strField(c, "name")
			loc, _ := strField(c, fieldStatedLocation) // bare ref id, no resolution
			out = append(out, sdk.Row{"name": name, fieldStatedLocation: loc})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q1: %v", err)
	}
	res := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		res = append(res, map[string]string{
			"name":              r["name"].(string),
			fieldStatedLocation: r[fieldStatedLocation].(string),
		})
	}
	return res
}

// Q2 — ref field-traversal in WHERE, required source (§5.4.1 rule 2 / T6, T13).
// WHERE c.stated_location.region = $region resolves the ref (one LEFT JOIN) and
// filters; required-MATCH source keeps the predicate in WHERE, so characters
// with a NULL stated_location (failed join) are EXCLUDED (§5.4.2 test 1).
func runCharactersInRegion(t *testing.T, s *sdk.SDK, region string) []string {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		idx := noteByID(v)
		var out []sdk.Row
		for _, c := range v.NotesByType("character") {
			locID, ok := strField(c, fieldStatedLocation)
			if !ok { // NULL ref → LEFT JOIN NULL row → fails WHERE → excluded
				continue
			}
			loc, ok := idx[locID]
			if !ok {
				continue
			}
			if r, _ := strField(loc, "region"); r == region {
				out = append(out, sdk.Row{"id": c.ID})
			}
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q2: %v", err)
	}
	return ids(rows)
}

// Q3 — single edge traversal (T2). MATCH (c:character)-[:member_of]->(f).
func runFactionMembers(t *testing.T, s *sdk.SDK, facID string) []string {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		var out []sdk.Row
		for _, e := range v.EdgesByType("member_of") {
			if e.To == facID {
				out = append(out, sdk.Row{"id": e.From})
			}
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q3: %v", err)
	}
	return ids(rows)
}

// bfsMinHops returns, for each node reachable from start within maxHops, the
// minimum hop count to reach it (extracted from the Q4 runner to keep that
// runner's query closure flat). adj is the directed adjacency list.
func bfsMinHops(adj map[string][]string, start string, maxHops int) map[string]int {
	type qn struct {
		id  string
		hop int
	}
	best := map[string]int{}
	queue := []qn{{start, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.hop >= maxHops {
			continue
		}
		for _, nb := range adj[cur.id] {
			h := cur.hop + 1
			if prev, ok := best[nb]; !ok || h < prev {
				best[nb] = h
				queue = append(queue, qn{nb, h})
			}
		}
	}
	return best
}

// Q4 — variable-length pattern *1..maxHops (§5.1 / T3). BFS over connects_to,
// recording the MINIMUM hop count to each reachable destination; returns
// destination + hop_count (no paths-as-objects, §5.2 / T31).
func runReachableLocations(t *testing.T, s *sdk.SDK, startID string, maxHops int) map[string]int {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		adj := map[string][]string{}
		for _, e := range v.EdgesByType("connects_to") {
			adj[e.From] = append(adj[e.From], e.To)
		}
		var out []sdk.Row
		for dest, hop := range bfsMinHops(adj, startID, maxHops) {
			out = append(out, sdk.Row{"dest_id": dest, "hop_count": hop})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q4: %v", err)
	}
	res := map[string]int{}
	for _, r := range rows {
		res[r["dest_id"].(string)] = r["hop_count"].(int)
	}
	return res
}

// Q5 — coalesce param-normalization in WHERE (§5.1.1 / T23). WHERE e.session >=
// coalesce($min_session, 0): the function folds the optional param to a default
// BEFORE predicate evaluation; it is applied to the PARAM, never the field.
func runEventsSince(t *testing.T, s *sdk.SDK, minSession *int) []string {
	t.Helper()
	threshold := 0 // coalesce($min_session, 0) when param omitted
	if minSession != nil {
		threshold = *minSession
	}
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		var out []sdk.Row
		for _, e := range v.NotesByType("event") {
			if sess, ok := intField(e, "session"); ok && sess >= threshold {
				out = append(out, sdk.Row{"id": e.ID})
			}
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q5: %v", err)
	}
	return ids(rows)
}

// Q6 — count(*) aggregate in RETURN. Groups member_of edges by faction.
func runFactionMemberCount(t *testing.T, s *sdk.SDK) map[string]int {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		counts := map[string]int{}
		for _, e := range v.EdgesByType("member_of") {
			counts[e.To]++
		}
		var out []sdk.Row
		for f, c := range counts {
			out = append(out, sdk.Row{"faction": f, "count": c})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q6: %v", err)
	}
	res := map[string]int{}
	for _, r := range rows {
		res[r["faction"].(string)] = r["count"].(int)
	}
	return res
}

// hostileFactionName returns the name of the first hostile faction among
// factionIDs, or nil when none is hostile (extracted from the Q7 runner to
// flatten its query closure). It models the optional-join + hoisted f.stance
// predicate: a non-hostile or absent faction yields a NULL name.
func hostileFactionName(idx map[string]sdk.Note, factionIDs []string) any {
	for _, fID := range factionIDs {
		f, ok := idx[fID]
		if !ok {
			continue
		}
		if st, _ := strField(f, "stance"); st == "hostile" {
			name, _ := strField(f, "name")
			return name
		}
	}
	return nil
}

// indexMemberFactions groups member_of edges by source character id.
func indexMemberFactions(v sdk.NamespaceView) map[string][]string {
	memberFactions := map[string][]string{}
	for _, e := range v.EdgesByType("member_of") {
		memberFactions[e.From] = append(memberFactions[e.From], e.To)
	}
	return memberFactions
}

// Q7 — OPTIONAL MATCH source, predicate hoists to ON (§5.4.2 test 2 / T14).
// MATCH (c) OPTIONAL MATCH (c)-[:member_of]->(f) WHERE c.status='alive' AND
// f.stance='hostile'. The required predicate (c.status) filters source rows;
// the optional-source predicate (f.stance) hoists to ON, so ALL living
// characters are preserved with a NULL faction when they have no hostile one.
// Returns the living-character ids and the count of rows with a non-NULL
// hostile-faction name.
func runLivingHostileSeat(t *testing.T, s *sdk.SDK) (living []string, hostilePresent int) {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		idx := noteByID(v)
		memberFactions := indexMemberFactions(v)
		var out []sdk.Row
		for _, c := range v.NotesByType("character") {
			if status, _ := strField(c, "status"); status != "alive" {
				continue // required-source predicate filters the row out
			}
			out = append(out, sdk.Row{
				"id":           c.ID,
				"faction_name": hostileFactionName(idx, memberFactions[c.ID]),
			})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q7: %v", err)
	}
	for _, r := range rows {
		living = append(living, r["id"].(string))
		if r["faction_name"] != nil {
			hostilePresent++
		}
	}
	return living, hostilePresent
}

// Q8 — enum WHERE (T1) + ref-traversal for giver (T6). WHERE q.status='open'.
func runOpenQuests(t *testing.T, s *sdk.SDK) []string {
	t.Helper()
	rows, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		var out []sdk.Row
		for _, q := range v.NotesByType("quest") {
			if st, _ := strField(q, "status"); st == "open" {
				out = append(out, sdk.Row{"id": q.ID})
			}
		}
		return out
	})
	if err != nil {
		t.Fatalf("Q8: %v", err)
	}
	return ids(rows)
}

func ids(rows []sdk.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["id"].(string))
	}
	return out
}
