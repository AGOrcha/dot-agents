package crgbehavior

import (
	"path/filepath"
	"testing"
)

// The checked-in contract is what makes "we ran the gate" mean "we compared
// these behaviors". It must require every surface and — for now — waive none.
func TestCheckedInContractRequiresEverySurface(t *testing.T) {
	contract, err := LoadContract(filepath.Join("..", "..", DefaultContractPath))
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	for _, surface := range AllSurfaces() {
		if !containsString(contract.RequiredSurfaces, surface) {
			t.Fatalf("contract does not require %q, so a run could skip it silently", surface)
		}
	}
	if len(contract.RatifiedExceptions) != 0 {
		t.Fatalf("contract waives %d surface(s); every waiver needs a reviewed reason here",
			len(contract.RatifiedExceptions))
	}
}

// A contract the gate cannot enforce is worse than none: it would look like a
// coverage guarantee while asserting nothing.
func TestContractValidateRejectsUnenforceableContracts(t *testing.T) {
	base := func() Contract {
		return Contract{SchemaVersion: ContractSchemaVersion, Release: PinnedVersion,
			RequiredSurfaces: []string{SurfaceFlows, SurfaceRiskIndex}}
	}
	cases := map[string]func(*Contract){
		"wrong schema version": func(c *Contract) { c.SchemaVersion = 99 },
		"another release":      func(c *Contract) { c.Release = "2.2.0" },
		"no required surfaces": func(c *Contract) { c.RequiredSurfaces = nil },
		"unknown surface":      func(c *Contract) { c.RequiredSurfaces = append(c.RequiredSurfaces, "made_up") },
		"waives a non-required": func(c *Contract) {
			c.RatifiedExceptions = []RatifiedException{{Surface: SurfaceFTSSearch, Reason: "r", RatifiedBy: "b"}}
		},
		"waiver without reason": func(c *Contract) {
			c.RatifiedExceptions = []RatifiedException{{Surface: SurfaceFlows, RatifiedBy: "b"}}
		},
		"waiver without owner": func(c *Contract) {
			c.RatifiedExceptions = []RatifiedException{{Surface: SurfaceFlows, Reason: "r"}}
		},
		"duplicate waiver": func(c *Contract) {
			c.RatifiedExceptions = []RatifiedException{
				{Surface: SurfaceFlows, Reason: "r", RatifiedBy: "b"},
				{Surface: SurfaceFlows, Reason: "r", RatifiedBy: "b"},
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("Validate accepted a contract that %s", name)
			}
		})
	}
}

// Coverage is judged over what the run ACTUALLY exercised: a divergence still
// counts as exercised (the behavior was compared and disagreed), while a
// not-exercised surface carries its measured reason forward.
func TestContractCoverageSeparatesExercisedFromUnexercised(t *testing.T) {
	contract := Contract{SchemaVersion: ContractSchemaVersion, Release: PinnedVersion,
		RequiredSurfaces: []string{SurfaceFlows, SurfaceRiskIndex, SurfaceFTSSearch}}
	tasks := []TaskReport{{
		Surfaces: []Surface{
			{Name: SurfaceFlows, Status: StatusAgree},
			{Name: SurfaceRiskIndex, Status: StatusDiverge},
			{Name: SurfaceFTSSearch, Status: StatusNotExercised, Reason: "the commit changed no declaration"},
		},
	}}
	byName := map[string]SurfaceCoverage{}
	for _, c := range contract.Coverage(tasks) {
		byName[c.Surface] = c
	}
	if got := byName[SurfaceFlows]; got.Exercised != 1 || got.Diverged != 0 || !got.Satisfied {
		t.Fatalf("flows coverage = %+v, want exercised and satisfied", got)
	}
	if got := byName[SurfaceRiskIndex]; got.Exercised != 1 || got.Diverged != 1 || !got.Satisfied {
		t.Fatalf("risk_index coverage = %+v, want a divergence to still count as exercised", got)
	}
	got := byName[SurfaceFTSSearch]
	if got.Exercised != 0 || got.Satisfied {
		t.Fatalf("fts_search coverage = %+v, want unexercised and unsatisfied", got)
	}
	if len(got.Reasons) != 1 {
		t.Fatalf("fts_search reasons = %v, want the measured reason carried forward", got.Reasons)
	}
}

// A ratified waiver satisfies the surface and is attributed in the coverage
// row, so a reader sees WHO accepted the gap.
func TestContractCoverageAttributesRatifiedWaivers(t *testing.T) {
	contract := Contract{SchemaVersion: ContractSchemaVersion, Release: PinnedVersion,
		RequiredSurfaces:   []string{SurfaceFlows},
		RatifiedExceptions: []RatifiedException{{Surface: SurfaceFlows, Reason: "no flow in corpus", RatifiedBy: "t6"}}}
	coverage := contract.Coverage([]TaskReport{{Surfaces: []Surface{{Name: SurfaceFlows, Status: StatusNotExercised}}}})
	if len(coverage) != 1 || !coverage[0].Satisfied || coverage[0].Ratified == nil {
		t.Fatalf("coverage = %+v, want a satisfied, attributed waiver", coverage)
	}
	if coverage[0].Ratified.RatifiedBy != "t6" {
		t.Fatalf("waiver attribution = %q, want t6", coverage[0].Ratified.RatifiedBy)
	}
}

// The contract is the gate's coverage claim. A file the gate cannot read or
// cannot enforce must fail the load: falling back to the zero Contract would
// require no surface at all, and then a run that compared nothing would report
// full coverage.
func TestCiTLoadContractRejectsUnusableFiles(t *testing.T) {
	offBaseline := Contract{
		SchemaVersion:    ContractSchemaVersion,
		Release:          "2.2.0",
		RequiredSurfaces: []string{SurfaceFlows},
	}
	cases := []ciTLoadCase{
		{name: "no such file", want: "read corpus contract"},
		{name: "malformed json", body: "{\"required_surfaces\":", want: "parse corpus contract"},
		{name: "another release", body: ciTJSON(t, offBaseline), want: `targets release "2.2.0"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadContract(ciTStagedPath(t, "contract.json", tc))
			ciTWantError(t, "LoadContract", err, tc.want)
		})
	}
}
