package codegraph

import (
	"encoding/json"
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// The two MUTATING tools of the release surface: build_or_update_graph_tool
// and run_postprocess_tool.
//
// Both are thin adapters. The lifecycle work — scanning, the incremental diff,
// the three post-processing levels and the result dict's field set — belongs to
// Engine.BuildReport / UpdateReport / PostprocessReport and to
// graphstore.CRGOperationReport. A handler here does exactly two things: lower
// the release's argument set onto the option struct, and project the report
// into the response payload.
//
// Everything else the release does around these two tools is deliberately NOT
// here, because it is somebody else's single source of truth:
//
//   - the release attaches its `_graph` provenance envelope to both results
//     (main.py:141, main.py:179 -> tools/_common.with_provenance) and no
//     `_hints` block, which is exactly what Engine.CallTool already does;
//   - the implicit full-rebuild rules (an empty graph forces a full rebuild,
//     and an AUTOMATIC base that resolves to nothing falls back to one) live
//     in UpdateReport, which is why the incremental branch below calls it
//     unconditionally instead of pre-empting it with its own emptiness check.
//     A second copy of those rules here could disagree with the first, and the
//     disagreement would surface as a `build_type` that contradicts the work
//     actually performed;
//   - a fully specified embedding_provider/embedding_model pair is passed
//     straight through. It is not silently dropped and it does not raise: for
//     a graph this backend built, the release's own refresh is a no-op.
//     embeddings.refresh_embeddings returns None before it resolves a provider
//     when the graph holds no vectors, and the native store creates upstream's
//     `embeddings` table without ever populating it — so the release adds no
//     `warnings`, no `embeddings_refreshed` and no `embeddings_purged` there,
//     and neither do we. Supplying only ONE half is the release's warning, and
//     graphstore.EmbeddingRefreshWarnings owns that string.

func init() {
	RegisterTool("build_or_update_graph_tool", buildOrUpdateGraphTool)
	RegisterTool("run_postprocess_tool", runPostprocessTool)
}

// buildOrUpdateGraphTool is the release's build_or_update_graph_tool.
//
// `full_rebuild` selects the path, and nothing else does: the release's other
// two routes to a full build are decided inside the update, from state this
// handler cannot see (whether the graph holds nodes, and whether an automatic
// base resolves), and the report it returns already says `build_type: "full"`
// when either fired.
func buildOrUpdateGraphTool(e *Engine, args crgrelease.Args) (any, error) {
	var (
		report *graphstore.CRGOperationReport
		err    error
	)
	if args.Bool("full_rebuild") {
		report, err = e.BuildReport(graphstore.BuildOptions{
			RepoRoot:          args.String("repo_root"),
			Postprocess:       args.String("postprocess"),
			RecurseSubmodules: lifecycleTriStateBool(args, "recurse_submodules"),
			EmbeddingProvider: args.String("embedding_provider"),
			EmbeddingModel:    args.String("embedding_model"),
		})
	} else {
		// `base` is a three-state argument, and the third state is
		// observable: absent resolves the diff base to the commit the
		// graph was last built at (never HEAD~1), whereas an explicit
		// empty string is a ref that matches nothing, so the diff is
		// empty and `base_resolved` echoes "". OptString reports which
		// of the two the caller sent.
		base, baseSet := args.OptString("base")
		report, err = e.UpdateReport(graphstore.UpdateOptions{
			RepoRoot:          args.String("repo_root"),
			Base:              base,
			BaseSet:           baseSet,
			Postprocess:       args.String("postprocess"),
			RecurseSubmodules: lifecycleTriStateBool(args, "recurse_submodules"),
			EmbeddingProvider: args.String("embedding_provider"),
			EmbeddingModel:    args.String("embedding_model"),
		})
	}
	if err != nil {
		return nil, err
	}
	return lifecycleReportPayload(args.Tool(), report)
}

// runPostprocessTool is the release's run_postprocess_tool: a standalone
// recomputation of the derived views over a graph that already exists.
//
// The three step flags are bound as PRESENT booleans rather than left nil.
// PostprocessOptions treats nil as "the release's default", and every one of
// these parameters carries `default: true` in the published schema, so the
// bound value is always an answer the caller (or the schema on the caller's
// behalf) actually gave — there is no unset state left to defer.
func runPostprocessTool(e *Engine, args crgrelease.Args) (any, error) {
	report, err := e.PostprocessReport(graphstore.PostprocessOptions{
		RepoRoot:          args.String("repo_root"),
		Flows:             new(args.Bool("flows")),
		Communities:       new(args.Bool("communities")),
		FTS:               new(args.Bool("fts")),
		EmbeddingProvider: args.String("embedding_provider"),
		EmbeddingModel:    args.String("embedding_model"),
	})
	if err != nil {
		return nil, err
	}
	return lifecycleReportPayload(args.Tool(), report)
}

// lifecycleReportPayload projects a lifecycle report into the release's result
// dict.
//
// It goes through the report's own JSON encoding rather than copying ~30
// fields by hand, because WHICH KEYS ARE PRESENT is the contract here — a
// no-changes early return, postprocess="none", postprocess="minimal" and a
// standalone post-process each carry a different field set — and
// CRGOperationReport's pointer-plus-omitempty tags are where that is already
// decided. A hand-written projection would be a second, drifting copy of the
// same rule set, and the drift would be invisible: a field added to the report
// would simply never reach a caller.
func lifecycleReportPayload(tool string, report *graphstore.CRGOperationReport) (map[string]any, error) {
	if report == nil {
		return nil, fmt.Errorf("codegraph: %s produced no report", tool)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("codegraph: encode %s report: %w", tool, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, fmt.Errorf("codegraph: decode %s report: %w", tool, err)
	}
	return payload, nil
}

// lifecycleTriStateBool lowers one of the release's `bool | null` parameters
// onto an option struct's *bool. Only `recurse_submodules` is genuinely
// tri-state: null there does not mean False, it means "consult
// CRG_RECURSE_SUBMODULES", so collapsing it to a bool would override an
// environment the caller deliberately left in charge.
func lifecycleTriStateBool(args crgrelease.Args, name string) *bool {
	value, set := args.OptBool(name)
	if !set {
		return nil
	}
	return new(value)
}
