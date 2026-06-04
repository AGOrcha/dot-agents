package workflow

import "strings"

// This file defines the domain-adapter seam the §4.1 base-resolution algorithm
// depends on instead of git directly (spec §2.10, §4.4.2). `base` and `ready`
// are abstract seams, not git concepts:
//
//   - `ready` ("can a downstream consumer start before the upstream merges?") is
//     already sourced through the internal/events PR producer (prSourceLister) —
//     event.pr.* is ready-adapter #1, and a non-VCS domain swaps the producer.
//   - `base` ("the addressable thing to branch from") is the outputRef abstracted
//     here. Git PRs are base-adapter #1 (prBaseResolver); a content-addressed or
//     non-composable domain plugs in a different baseResolver with zero changes
//     to resolveBase.
//
// When a domain's outputs are not addressably layerable, LayerableBase reports
// false and the algorithm completion-gates to Trunk() — the pre-layering floor
// (spec §4.4.1), not a bug.

// outputRef is an addressable identifier for an upstream task's in-flight output
// that a downstream task can branch from before the upstream finalizes (spec
// §4.4.1). The git adapter sets Ref to the dep's PR head branch and PR to its
// number; a content-addressed adapter would set Ref to a digest and leave PR 0.
// An output with no addressable in-flight identity is not layerable, which forces
// the completion-gating floor.
type outputRef struct {
	Ref string // the addressable base (git: PR head branch; content: digest)
	PR  int    // adapter-specific backing handle (git: PR number; 0 when n/a)
}

// baseResolver is the domain adapter resolveBase depends on (spec §4.4.2). Trunk
// is the domain's no-layer base (git: master); LayerableBase maps a ready
// dependency to its addressable in-flight output, or reports not-layerable so the
// caller completion-gates to Trunk. Git is adapter #1; a non-VCS domain supplies
// its own without touching the §4.1 algorithm.
type baseResolver interface {
	Trunk() string
	LayerableBase(f inFlightTask) (outputRef, bool)
}

// prBaseResolver is the v1 git adapter (#1): Trunk is master; a dependency is
// layerable when it is in an awaiting_review status with an open PR branch —
// today's §4.1 step-3 behavior. The awaiting_review status it reads is itself
// produced by the internal/events PR producer, so both the base and ready seams
// are adapter-backed.
type prBaseResolver struct{}

// Trunk is the git no-layer base: master.
func (prBaseResolver) Trunk() string { return baseRefMaster }

// LayerableBase returns the dependency's open PR branch as its addressable
// output when the dep is in an awaiting_review status with a non-empty branch;
// otherwise it reports not-layerable so the caller completion-gates to master.
func (prBaseResolver) LayerableBase(f inFlightTask) (outputRef, bool) {
	if !awaitingReviewStatuses[f.Status] || strings.TrimSpace(f.PRBranch) == "" {
		return outputRef{}, false
	}
	return outputRef{Ref: f.PRBranch, PR: f.PRNumber}, true
}
