// Package dogfood runs the TTRPG adapter hard test
// (graph-backend-adapter-contract §13.3 "Hard test"). It lives under
// internal/ (reachable by `go test ./...`) rather than under the
// .agents/sandbox/ tree (which `./...` skips because Go ignores dot-prefixed
// dirs), and imports the sandbox bootstrap package + reads the sandbox corpus,
// schema, and oracle by relative path.
//
// What it proves end-to-end:
//   - the TTRPG schema.yaml loads and validates against the SHIPPED adapter
//     contract (internal/kg/registry) — same loader the `none` adapter uses;
//   - the bootstrap skill (sandbox bootstrap-skill/bootstrap.go) parses the
//     10-session corpus and writes it through the `da-adapter-sdk` SDK ONLY,
//     producing the exact note/edge counts in oracle.yaml;
//   - the named queries in queries.yaml, run with §5 v1 semantics, return the
//     exact results in oracle.yaml.
//
// This substitutes the human "DM-validated results" step with a machine oracle
// so the build is verifiable with no human in the loop (the live DM dogfood is
// explicitly deferred).
package dogfood

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	bootstrap "github.com/AGOrcha/dot-agents/.agents/sandbox/ttrpg-adapter/bootstrap-skill"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"

	"go.yaml.in/yaml/v3"
)

// sandboxDir resolves the sandbox adapter dir relative to this test file, so
// the test is path-independent of the cwd `go test` chooses.
func sandboxDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// internal/adapters/sdk/dogfood/dogfood_test.go → repo root is 4 up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, ".agents", "sandbox", "ttrpg-adapter")
}

// --- oracle.yaml shape --------------------------------------------------------

type oracle struct {
	Bootstrap struct {
		SessionsParsed int            `yaml:"sessions_parsed"`
		NoteCount      int            `yaml:"note_count"`
		EdgeCount      int            `yaml:"edge_count"`
		NotesByType    map[string]int `yaml:"notes_by_type"`
		EdgesByType    map[string]int `yaml:"edges_by_type"`
	} `yaml:"bootstrap"`
	Queries struct {
		CharacterLocation struct {
			Rows []map[string]string `yaml:"rows"`
		} `yaml:"character_location"`
		CharactersInRegion map[string]struct {
			IDs []string `yaml:"ids"`
		} `yaml:"characters_in_region"`
		FactionMembers map[string]struct {
			IDs []string `yaml:"ids"`
		} `yaml:"faction_members"`
		ReachableLocations struct {
			FromIronhold struct {
				DestHops map[string]int `yaml:"dest_hops"`
			} `yaml:"from_ironhold"`
		} `yaml:"reachable_locations"`
		EventsSince struct {
			All struct {
				Count int `yaml:"count"`
			} `yaml:"all"`
			Since7 struct {
				IDs []string `yaml:"ids"`
			} `yaml:"since_7"`
		} `yaml:"events_since"`
		FactionMemberCount struct {
			Counts map[string]int `yaml:"counts"`
		} `yaml:"faction_member_count"`
		LivingCharactersHostileSeat struct {
			LivingIDs                  []string `yaml:"living_ids"`
			HostileFactionNamesPresent int      `yaml:"hostile_faction_names_present"`
		} `yaml:"living_characters_hostile_seat"`
		OpenQuests struct {
			IDs []string `yaml:"ids"`
		} `yaml:"open_quests"`
	} `yaml:"queries"`
}

func loadOracle(t *testing.T, dir string) oracle {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "oracle.yaml"))
	if err != nil {
		t.Fatalf("read oracle: %v", err)
	}
	var o oracle
	if err := yaml.Unmarshal(data, &o); err != nil {
		t.Fatalf("parse oracle: %v", err)
	}
	return o
}

// --- the hard test ------------------------------------------------------------

// TestSchemaLoadsAgainstShippedContract proves the TTRPG schema validates with
// the same registry loader the built-in `none` adapter uses (§4). This is the
// "adapter loads from a local sandbox source" deliverable.
func TestSchemaLoadsAgainstShippedContract(t *testing.T) {
	dir := sandboxDir(t)
	data, err := os.ReadFile(filepath.Join(dir, "schema.yaml"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	s, err := registry.LoadSchema(data)
	if err != nil {
		t.Fatalf("schema does not validate against shipped contract: %v", err)
	}
	if s.Name != "ttrpg" {
		t.Fatalf("schema name = %q, want ttrpg", s.Name)
	}
	if len(s.NoteTypes) != 7 {
		t.Fatalf("note types = %d, want 7 (§13.3)", len(s.NoteTypes))
	}
	if len(s.EdgeTypes) != 13 {
		t.Fatalf("edge types = %d, want 13 (§13.3)", len(s.EdgeTypes))
	}
	// Resolve the schema through a registry, the way `da` resolves a backend
	// ref — proves the sandbox-sourced adapter is registry-resolvable.
	reg := registry.New()
	if err := reg.Register(schemaAdapter{s}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Resolve("ttrpg@^1.0"); err != nil {
		t.Fatalf("resolve ttrpg@^1.0: %v", err)
	}
}

// schemaAdapter wraps a loaded Schema as a registry.Adapter so the sandbox
// schema can be resolved exactly like a built-in. ImpactRadius is the §13.1
// no-op identity (the real impact-radius DSL execution is the dsl package's
// job, a sibling task).
type schemaAdapter struct{ s registry.Schema }

func (a schemaAdapter) Name() string            { return a.s.Name }
func (a schemaAdapter) Schema() registry.Schema { return a.s }
func (a schemaAdapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// TestBootstrapHardTest is the §13.3 hard test: bootstrap the 10-session corpus
// through the SDK and assert the exact note/edge counts in oracle.yaml.
func TestBootstrapHardTest(t *testing.T) {
	dir := sandboxDir(t)
	o := loadOracle(t, dir)

	store := sdk.NewMemStore()
	s := sdk.For("ttrpg", store)
	res, err := bootstrap.Run(s, filepath.Join(dir, "corpus"))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if res.SessionsParsed != o.Bootstrap.SessionsParsed {
		t.Errorf("sessions parsed = %d, want %d", res.SessionsParsed, o.Bootstrap.SessionsParsed)
	}
	if res.NoteCount != o.Bootstrap.NoteCount {
		t.Errorf("note count = %d, want %d", res.NoteCount, o.Bootstrap.NoteCount)
	}
	if res.EdgeCount != o.Bootstrap.EdgeCount {
		t.Errorf("edge count = %d, want %d", res.EdgeCount, o.Bootstrap.EdgeCount)
	}
	if !reflect.DeepEqual(res.NotesByType, o.Bootstrap.NotesByType) {
		t.Errorf("notes_by_type = %v, want %v", res.NotesByType, o.Bootstrap.NotesByType)
	}
	if !reflect.DeepEqual(res.EdgesByType, o.Bootstrap.EdgesByType) {
		t.Errorf("edges_by_type = %v, want %v", res.EdgesByType, o.Bootstrap.EdgesByType)
	}

	// The bootstrap fired one session.recorded predicate per session (§8.4.1
	// declare_predicate_fired), proving the SDK predicate surface is exercised.
	if got := len(s.FiredPredicates()); got != o.Bootstrap.SessionsParsed {
		t.Errorf("fired predicates = %d, want %d", got, o.Bootstrap.SessionsParsed)
	}
}

// TestNamedQueriesHardTest runs each queries.yaml named query with v1 §5
// semantics (implemented as Go runners — the DSL compiler is a sibling task)
// and asserts the exact results in oracle.yaml. This is the "named queries
// return expected results" half of the §13.3 hard test.
func TestNamedQueriesHardTest(t *testing.T) {
	dir := sandboxDir(t)
	o := loadOracle(t, dir)

	store := sdk.NewMemStore()
	s := sdk.For("ttrpg", store)
	if _, err := bootstrap.Run(s, filepath.Join(dir, "corpus")); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	t.Run("Q1_character_location", func(t *testing.T) {
		rows := runCharacterLocation(t, s, "char:mara")
		want := o.Queries.CharacterLocation.Rows
		if len(rows) != len(want) {
			t.Fatalf("rows = %v, want %v", rows, want)
		}
		for i, w := range want {
			if rows[i]["name"] != w["name"] || rows[i]["stated_location"] != w["stated_location"] {
				t.Fatalf("row %d = %v, want %v", i, rows[i], w)
			}
		}
	})

	t.Run("Q2_characters_in_region", func(t *testing.T) {
		for region, exp := range o.Queries.CharactersInRegion {
			got := runCharactersInRegion(t, s, region)
			assertIDs(t, "Q2 "+region, got, exp.IDs)
		}
	})

	t.Run("Q3_faction_members", func(t *testing.T) {
		cases := map[string]string{"wardens": "fac:wardens", "cinder": "fac:cinder"}
		for key, facID := range cases {
			got := runFactionMembers(t, s, facID)
			assertIDs(t, "Q3 "+key, got, o.Queries.FactionMembers[key].IDs)
		}
	})

	t.Run("Q4_reachable_locations", func(t *testing.T) {
		got := runReachableLocations(t, s, "loc:ironhold", 3)
		want := o.Queries.ReachableLocations.FromIronhold.DestHops
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("reachable = %v, want %v", got, want)
		}
	})

	t.Run("Q5_events_since", func(t *testing.T) {
		all := runEventsSince(t, s, nil)
		if len(all) != o.Queries.EventsSince.All.Count {
			t.Fatalf("events (all) = %d, want %d", len(all), o.Queries.EventsSince.All.Count)
		}
		seven := 7
		assertIDs(t, "Q5 since_7", runEventsSince(t, s, &seven), o.Queries.EventsSince.Since7.IDs)
	})

	t.Run("Q6_faction_member_count", func(t *testing.T) {
		got := runFactionMemberCount(t, s)
		if !reflect.DeepEqual(got, o.Queries.FactionMemberCount.Counts) {
			t.Fatalf("counts = %v, want %v", got, o.Queries.FactionMemberCount.Counts)
		}
	})

	t.Run("Q7_living_characters_hostile_seat", func(t *testing.T) {
		living, hostilePresent := runLivingHostileSeat(t, s)
		assertIDs(t, "Q7 living", living, o.Queries.LivingCharactersHostileSeat.LivingIDs)
		if hostilePresent != o.Queries.LivingCharactersHostileSeat.HostileFactionNamesPresent {
			t.Fatalf("hostile names present = %d, want %d", hostilePresent, o.Queries.LivingCharactersHostileSeat.HostileFactionNamesPresent)
		}
	})

	t.Run("Q8_open_quests", func(t *testing.T) {
		assertIDs(t, "Q8", runOpenQuests(t, s), o.Queries.OpenQuests.IDs)
	})
}

func assertIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	w := append([]string(nil), want...)
	sort.Strings(w)
	if !reflect.DeepEqual(got, w) {
		t.Fatalf("%s ids = %v, want %v", label, got, w)
	}
}
