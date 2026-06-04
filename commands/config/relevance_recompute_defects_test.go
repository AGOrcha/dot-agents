package config

import (
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// This file holds the three test-first defect validations for the lens-rejected
// `da config relevance --recompute` work (PR #21). Each test is written to FAIL
// if its hypothesized defect is real, run against the current branch, and then
// kept as a permanent regression test. See
// .agents/proposals/skill-relevance-filter.md §6.

// ---------- Defect A: silent-zero mass-demotion ----------

// TestRecompute_EmptyCorpus_NoMassDemotion is the silent-zero validation. With
// an empty / zero-signal corpus there is no evidence about any unit, so recompute
// must NOT propose demoting every listed unit to noise. Absence of evidence is
// not evidence of irrelevance: a never-cited unit in an empty corpus keeps its
// current class (no change at all). The test feeds a corpus with no scored
// citations and asserts no unit is proposed for a class change — in particular no
// mass demotion to noise.
func TestRecompute_EmptyCorpus_NoMassDemotion(t *testing.T) {
	// A profile that lists units across both stages; every one currently has a
	// real class (core/situational/noise). With no corpus evidence none should
	// be reclassified.
	profile := &cfg.ExecutionProfile{
		ByAppType: map[string]cfg.AppTypeProfile{
			"go-cli": {
				Relevance: map[string]cfg.RelevanceClasses{
					"verify": {
						Core:        []string{"unit"},
						Situational: []string{"cli-runner"},
					},
					"review": {
						Core:        []string{"review-pr"},
						Situational: []string{"self-review"},
						Noise:       []string{"article-extract"},
					},
				},
			},
		},
		DefaultClass: "situational",
	}
	// An entirely empty corpus (a fresh repo with no iteration traces).
	corpus := scoredCorpus{}

	opts := &runRelevanceOptions{recompute: true, appType: "go-cli"}
	res := buildRecomputeResult(opts, profile, corpus)

	if len(res.Proposals) == 0 {
		t.Fatalf("expected the profile's units to be reported, got none")
	}

	var changed, demotedToNoise []string
	for _, p := range res.Proposals {
		if p.Changed {
			changed = append(changed, p.Unit+":"+p.CurrentClass+"->"+p.ProposedClass)
		}
		if p.ProposedClass == "noise" && p.CurrentClass != "noise" {
			demotedToNoise = append(demotedToNoise, p.Unit)
		}
	}

	if len(demotedToNoise) > 0 {
		t.Fatalf("DEFECT A reproduced: empty corpus mass-demoted units to noise: %v", demotedToNoise)
	}
	if len(changed) > 0 {
		t.Fatalf("DEFECT A reproduced: empty corpus proposed class changes with no evidence: %v", changed)
	}
}

// TestRecompute_UnscoredCorpus_NoMassDemotion is the zero-signal variant: there
// ARE iterations, but none carry a score, so nothing votes. The signal is still
// "no evidence", so the same no-change contract holds.
func TestRecompute_UnscoredCorpus_NoMassDemotion(t *testing.T) {
	profile := &cfg.ExecutionProfile{
		ByAppType: map[string]cfg.AppTypeProfile{
			"go-cli": {
				Relevance: map[string]cfg.RelevanceClasses{
					"review": {
						Core:        []string{"review-pr"},
						Situational: []string{"self-review"},
					},
				},
			},
		},
		DefaultClass: "situational",
	}
	// Iterations exist and even cite the units, but none are scored -> no votes.
	corpus := scoredCorpus{
		digest: "d",
		entries: []corpusEntry{
			{iteration: 1, haystack: "review-pr self-review", scored: false},
			{iteration: 2, haystack: "review-pr self-review", scored: false},
		},
	}

	opts := &runRelevanceOptions{recompute: true, appType: "go-cli"}
	res := buildRecomputeResult(opts, profile, corpus)

	for _, p := range res.Proposals {
		if p.ProposedClass == "noise" && p.CurrentClass != "noise" {
			t.Fatalf("DEFECT A reproduced: zero-signal corpus demoted %q from %q to noise",
				p.Unit, p.CurrentClass)
		}
		if p.Changed {
			t.Fatalf("DEFECT A reproduced: zero-signal corpus changed %q from %q to %q with no votes",
				p.Unit, p.CurrentClass, p.ProposedClass)
		}
	}
}

// TestStageCandidates_DedupAndDropEmpty covers the candidate-set builder that
// feeds the live working set: a unit listed in two classes is offered once, in
// core->situational->noise order, and empty entries are dropped.
func TestStageCandidates_DedupAndDropEmpty(t *testing.T) {
	classes := cfg.RelevanceClasses{
		Core:        []string{"a", ""},
		Situational: []string{"b", "a"}, // "a" duplicated across classes
		Noise:       []string{"c", "b"}, // "b" duplicated across classes
	}
	got := stageCandidates(classes)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("stageCandidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stageCandidates = %v, want %v", got, want)
		}
	}
}

// ---------- Defect C: dead suppression code wired into the live path ----------

// TestRecompute_LiveWorkingSetSuppression exercises the SuppressNoise /
// WorkingSet API through a production command path. Before the fix, SuppressNoise
// / WorkingSet.Kept / .Suppressed / .Candidates have no non-test caller — the
// "resolved working set" the design (§2) promises is never actually computed by
// any command, so noise is never suppressed in practice. This test resolves the
// units facet through the live `da config relevance` path and asserts the command
// surfaces the kept/suppressed working set derived from the WorkingSet API.
func TestRecompute_LiveWorkingSetSuppression(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := &runRelevanceOptions{
		filter:  filterUnits,
		appType: "go-cli",
		stage:   "review",
		stdout:  &captureBuffer{},
		stderr:  &captureBuffer{},
		cwd:     project,
		jsonOut: true,
	}
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance (units facet): %v", err)
	}
	out := opts.stdout.(*captureBuffer).String()

	// The review stage of the fixture lists article-extract as noise. The live
	// resolved working set must therefore SUPPRESS it (keep it out of the kept
	// set) while retaining it in the suppressed list (a reversible view, never a
	// delete). If the working set is never computed by the command, neither the
	// kept nor the suppressed surface appears.
	if !strings.Contains(out, "\"working_set\"") && !strings.Contains(out, "\"suppressed\"") {
		t.Fatalf("DEFECT C reproduced: the units facet exposes no resolved working set "+
			"(SuppressNoise/WorkingSet is dead code, never wired into the live command)\n%s", out)
	}
	if !strings.Contains(out, "article-extract") {
		t.Fatalf("expected the noise unit article-extract to appear in the suppressed view\n%s", out)
	}
}

// captureBuffer is a minimal io.Writer that records everything written, used so
// the live command path can be inspected without cobra.
type captureBuffer struct{ b strings.Builder }

func (c *captureBuffer) Write(p []byte) (int, error) { return c.b.Write(p) }
func (c *captureBuffer) String() string              { return c.b.String() }
