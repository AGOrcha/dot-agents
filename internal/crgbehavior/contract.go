package crgbehavior

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// DefaultContractPath is the repo-relative path of the corpus contract.
const DefaultContractPath = "testdata/crg-behavior/contract.json"

// ContractSchemaVersion is the pinned contract format version.
const ContractSchemaVersion = 1

// RatifiedException is a REVIEWED, attributed decision that one required
// surface may go unexercised. It is the only way a required surface stops
// failing the gate: an exception must be written down, justified and owned,
// so "we never exercised flows" can never become a silent property of a
// particular corpus or a particular machine.
type RatifiedException struct {
	// Surface is the required surface this exception covers.
	Surface string `json:"surface"`
	// Reason states why the corpus cannot exercise it.
	Reason string `json:"reason"`
	// RatifiedBy attributes the decision (a plan/task id or a review).
	RatifiedBy string `json:"ratified_by"`
}

// Contract is the corpus contract: which comparison surfaces a criterion-2 run
// MUST actually exercise, and which unexercised surfaces have been explicitly
// ratified as acceptable.
//
// Without it, a run that skipped half its surfaces — because the corpus had no
// eligible task, or because the release computed no such view — reported the
// same verdict as a run that compared everything. The contract converts that
// into an explicit, reviewable claim.
type Contract struct {
	SchemaVersion int `json:"schema_version"`
	// Release pins the contract to one baseline; surfaces change between
	// releases, so a contract written for another release does not apply.
	Release string `json:"release"`
	// RequiredSurfaces must each be exercised by at least one corpus task.
	RequiredSurfaces []string `json:"required_surfaces"`
	// RatifiedExceptions waive individual required surfaces, with attribution.
	RatifiedExceptions []RatifiedException `json:"ratified_exceptions"`
}

// LoadContract reads and validates the corpus contract.
func LoadContract(path string) (Contract, error) {
	data, err := os.ReadFile(path) //nolint:gosec // pinned test corpus path
	if err != nil {
		return Contract{}, fmt.Errorf("crgbehavior: read corpus contract: %w", err)
	}
	var c Contract
	if err := json.Unmarshal(data, &c); err != nil {
		return Contract{}, fmt.Errorf("crgbehavior: parse corpus contract %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return Contract{}, err
	}
	return c, nil
}

// Validate rejects a contract the gate cannot enforce.
func (c Contract) Validate() error {
	if c.SchemaVersion != ContractSchemaVersion {
		return fmt.Errorf("crgbehavior: corpus contract schema_version %d, want %d",
			c.SchemaVersion, ContractSchemaVersion)
	}
	if c.Release != PinnedVersion {
		return fmt.Errorf("crgbehavior: corpus contract targets release %q, the gate certifies against %q",
			c.Release, PinnedVersion)
	}
	if len(c.RequiredSurfaces) == 0 {
		return fmt.Errorf("crgbehavior: corpus contract requires no surface — it would assert nothing")
	}
	known := setOf(AllSurfaces())
	for _, surface := range c.RequiredSurfaces {
		if !known[surface] {
			return fmt.Errorf("crgbehavior: corpus contract requires unknown surface %q (known: %s)",
				surface, strings.Join(AllSurfaces(), ", "))
		}
	}
	return c.validateExceptions()
}

// validateExceptions rejects an unattributed or inapplicable waiver.
func (c Contract) validateExceptions() error {
	required := setOf(c.RequiredSurfaces)
	seen := map[string]bool{}
	for _, e := range c.RatifiedExceptions {
		switch {
		case !required[e.Surface]:
			return fmt.Errorf("crgbehavior: corpus contract ratifies %q, which it does not require", e.Surface)
		case seen[e.Surface]:
			return fmt.Errorf("crgbehavior: corpus contract ratifies %q twice", e.Surface)
		case strings.TrimSpace(e.Reason) == "":
			return fmt.Errorf("crgbehavior: ratified exception for %q states no reason", e.Surface)
		case strings.TrimSpace(e.RatifiedBy) == "":
			return fmt.Errorf("crgbehavior: ratified exception for %q names no ratifier", e.Surface)
		}
		seen[e.Surface] = true
	}
	return nil
}

// Exception returns the ratified waiver for a surface, if any.
func (c Contract) Exception(surface string) (RatifiedException, bool) {
	for _, e := range c.RatifiedExceptions {
		if e.Surface == surface {
			return e, true
		}
	}
	return RatifiedException{}, false
}

// SurfaceCoverage is one required surface's corpus-wide outcome.
type SurfaceCoverage struct {
	Surface string `json:"surface"`
	// Exercised counts tasks where the surface actually ran an oracle.
	Exercised int `json:"exercised"`
	// Diverged counts tasks where the oracle disagreed or failed.
	Diverged int `json:"diverged"`
	// Reasons collects the distinct explanations for non-exercised tasks.
	Reasons []string `json:"reasons,omitempty"`
	// Ratified carries the waiver when the surface went unexercised and the
	// contract allows it.
	Ratified *RatifiedException `json:"ratified,omitempty"`
	// Satisfied is the contract verdict for this surface.
	Satisfied bool `json:"satisfied"`
}

// Coverage folds a finished run's tasks into the contract verdict: every
// required surface must have been exercised by at least one task, or carry a
// ratified exception. An unexercised, unratified required surface FAILS — that
// is the difference between "we compared these behaviors" and "we ran".
func (c Contract) Coverage(tasks []TaskReport) []SurfaceCoverage {
	exercised, diverged, reasons := tallySurfaces(tasks)
	out := make([]SurfaceCoverage, 0, len(c.RequiredSurfaces))
	for _, surface := range c.RequiredSurfaces {
		cov := SurfaceCoverage{
			Surface:   surface,
			Exercised: exercised[surface],
			Diverged:  diverged[surface],
			Reasons:   sortedSet(reasons[surface]),
			Satisfied: exercised[surface] > 0,
		}
		if !cov.Satisfied {
			if e, ok := c.Exception(surface); ok {
				waiver := e
				cov.Ratified, cov.Satisfied = &waiver, true
			}
		}
		out = append(out, cov)
	}
	return out
}

// tallySurfaces counts per-surface outcomes across a run.
func tallySurfaces(tasks []TaskReport) (exercised, diverged map[string]int, reasons map[string]map[string]bool) {
	exercised, diverged = map[string]int{}, map[string]int{}
	reasons = map[string]map[string]bool{}
	for _, t := range tasks {
		for _, s := range t.Surfaces {
			switch s.Status {
			case StatusAgree:
				exercised[s.Name]++
			case StatusDiverge, StatusFailed:
				exercised[s.Name]++
				diverged[s.Name]++
			case StatusNotExercised:
				if reasons[s.Name] == nil {
					reasons[s.Name] = map[string]bool{}
				}
				reasons[s.Name][s.Reason] = true
			}
		}
	}
	return exercised, diverged, reasons
}

// sortedSet returns a set's members in sorted order.
func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
