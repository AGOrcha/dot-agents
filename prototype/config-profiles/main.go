package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// main.go — da-config-explain-shaped demo.
//
//	go run . --role orchestrator --app-type go-cli --stage orchestrate --scope project
func main() {
	role := flag.String("role", "orchestrator", "agent role selector")
	appType := flag.String("app-type", "go-cli", "app_type selector")
	stage := flag.String("stage", "orchestrate", "stage selector")
	harness := flag.String("harness", "", "harness selector (optional)")
	scope := flag.String("scope", "project", "leaf scope of the chain")
	dir := flag.String("fixtures", "fixtures", "fixture directory")
	flag.Parse()

	src, err := LoadSourceSet(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load error:", err)
		os.Exit(1)
	}

	ctx := Context{
		Role:       *role,
		AppType:    *appType,
		Stage:      *stage,
		Harness:    *harness,
		ScopeChain: chainUpTo(Scope(*scope)),
	}
	res := Resolve(src, ctx)
	printExplain(ctx, res)
}

// chainUpTo builds the scope chain repo..<leaf>..org. The chain always includes
// the higher-authority scopes (user/team/org) because their policy binds.
func chainUpTo(leaf Scope) []Scope {
	full := []Scope{ScopeRepo, ScopeProject, ScopeUser, ScopeTeam, ScopeOrg}
	out := []Scope{}
	leafRank := authorityRank(leaf)
	for _, s := range full {
		// include scopes at or below the leaf for profile sourcing, plus all
		// higher-authority scopes for policy binding.
		if authorityRank(s) <= leafRank || authorityRank(s) >= authorityRank(ScopeUser) {
			out = append(out, s)
		}
	}
	return out
}

func printExplain(ctx Context, res Resolved) {
	fmt.Println("# config explain (prototype)")
	fmt.Printf("context: role=%s app_type=%s stage=%s harness=%q scope-chain=%v\n\n",
		ctx.Role, ctx.AppType, ctx.Stage, ctx.Harness, ctx.ScopeChain)

	fmt.Println("## effective policy")
	fmt.Printf("  precedence: %v\n", res.EffectivePolicy.Precedence)
	for _, lk := range res.EffectivePolicy.Locks {
		fmt.Printf("  lock: %s  (owner=%s)\n", lk.Raw, lk.Owner)
	}
	if len(res.EffectivePolicy.OverridePermissions) > 0 {
		fmt.Printf("  override_permissions: %v\n", res.EffectivePolicy.OverridePermissions)
	}
	fmt.Println()

	fmt.Println("## effective bundle")
	printList("  tools.allow", res.Bundle.Tools.Allow)
	printList("  tools.deny ", res.Bundle.Tools.Deny)
	printList("  skills.preload", res.Bundle.Skills.Preload)
	printList("  skills.allow  ", res.Bundle.Skills.Allow)
	printList("  skills.deny   ", res.Bundle.Skills.Deny)
	printList("  hooks", res.Bundle.Hooks)
	printList("  mcp  ", res.Bundle.MCP)
	if res.Bundle.Model != "" {
		fmt.Printf("  model: %s\n", res.Bundle.Model)
	}
	fmt.Println()

	fmt.Println("## contributing profiles")
	for _, r := range res.Contributing {
		fmt.Printf("  - %s\n", r)
	}
	fmt.Printf("\ndigest: %s\n", res.Digest)
}

func printList(label string, vals []string) {
	if len(vals) == 0 {
		return
	}
	fmt.Printf("%s: [%s]\n", label, strings.Join(vals, ", "))
}
