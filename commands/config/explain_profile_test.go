package config

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// profileRepo is a repo-local manifest carrying an execution_profile + a
// layering_policy, so explain's profile-context surface has something to resolve.
func profileRepo() map[string]any {
	return map[string]any{
		"project": "demo",
		"execution_profile": map[string]any{
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					"relevance": map[string]any{
						"verify": map[string]any{"core": []any{"go-test"}, "noise": []any{"chatty"}},
					},
					"topology": map[string]any{"executors": 2},
				},
			},
		},
		"layering_policy": map[string]any{
			"override_permissions": map[string]any{"repo": []any{"*"}},
			"locks": []any{
				map[string]any{"field": "tools.allow", "deny": []any{"Edit"}, "selector": map[string]any{"role": "reviewer"}},
			},
		},
	}
}

func runProfileExplain(t *testing.T, opts *runExplainOptions) string {
	t.Helper()
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		return snapResult(flatSnapshot(t, nil, profileRepo())), nil
	})
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	return opts.stdout.(*bytes.Buffer).String()
}

func TestExplainProfileContextHuman(t *testing.T) {
	opts := mustOptions("/x")
	opts.appType = "go-cli"
	out := runProfileExplain(t, opts)
	for _, want := range []string{"Effective profile bundle", "app_type=go-cli", "digest", "contributing refs", "execution-profile:go-cli"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q:\n%s", want, out)
		}
	}
}

func TestExplainProfileContextShowsBindingLock(t *testing.T) {
	opts := mustOptions("/x")
	opts.role = "reviewer"
	out := runProfileExplain(t, opts)
	if !strings.Contains(out, "binding locks") || !strings.Contains(out, "tools.allow") {
		t.Fatalf("reviewer context should surface the deny-lock:\n%s", out)
	}
}

func TestExplainProfileContextJSON(t *testing.T) {
	opts := mustOptions("/x")
	opts.appType = "go-cli"
	opts.jsonOut = true
	out := runProfileExplain(t, opts)
	var resolved cfg.ResolvedProfile
	if err := json.Unmarshal([]byte(out), &resolved); err != nil {
		t.Fatalf("profile --json did not decode: %v\n%s", err, out)
	}
	if resolved.Digest == "" || len(resolved.Contributing) == 0 {
		t.Fatalf("json output missing digest/contributing: %+v", resolved)
	}
	if resolved.PolicyMode != cfg.PolicyModeNarrow {
		t.Fatalf("policy mode = %q, want narrow", resolved.PolicyMode)
	}
}

// valueLockRepo carries a value-lock + a same-scope conflict + a replace policy
// so the human renderer's value-lock, conflict, replaced-by, and permission
// branches are all exercised.
func valueLockRepo() map[string]any {
	return map[string]any{
		"project": "demo",
		"layering_policy": map[string]any{
			"mode":                 "replace",
			"override_permissions": map[string]any{"repo": []any{"*"}},
			"locks": []any{
				map[string]any{"field": "model", "value": "sonnet"},
			},
		},
	}
}

func TestExplainProfileContextValueLockAndReplace(t *testing.T) {
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		return snapResult(flatSnapshot(t, nil, valueLockRepo())), nil
	})
	opts := mustOptions("/x")
	opts.appType = "go-cli"
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	for _, want := range []string{"value-lock", "model", "replaced by repo", "override permissions"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestExplainProfileContextResolveError(t *testing.T) {
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		return snapResult(flatSnapshot(t, nil, map[string]any{
			"project":         "demo",
			"layering_policy": map[string]any{"mode": "bogus"},
		})), nil
	})
	opts := mustOptions("/x")
	opts.appType = "go-cli"
	if err := runExplain(opts, nil, testDeps()); err == nil {
		t.Fatal("expected a malformed-policy resolve error to surface")
	}
}

func TestExplainProfileContextValidationGuards(t *testing.T) {
	cases := []func(*runExplainOptions){
		func(o *runExplainOptions) { o.appType = "go-cli"; o.all = true },
		func(o *runExplainOptions) { o.role = "r"; o.valueOnly = true },
		func(o *runExplainOptions) { o.stage = "verify"; o.flags = true },
	}
	for i, mut := range cases {
		opts := mustOptions("/x")
		mut(opts)
		// A profile-context combo with --all/--value-only/--flags is a usage error;
		// also test the field-path-arg guard.
		err := runExplain(opts, nil, testDeps())
		if err == nil {
			t.Fatalf("case %d: expected a usage error for an invalid profile-context combo", i)
		}
	}
	// Field-path argument with a profile context is rejected.
	opts := mustOptions("/x")
	opts.appType = "go-cli"
	if err := runExplain(opts, []string{"repo_id"}, testDeps()); err == nil {
		t.Fatal("expected a usage error when a field path is passed with a profile context")
	}
}
