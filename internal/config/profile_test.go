package config

import (
	"encoding/json"
	"testing"
)

func TestDecodeSelectorUnknownKeyIsError(t *testing.T) {
	if _, err := decodeSelector(json.RawMessage(`{"role":"reviewer","bogus":"x"}`)); err == nil {
		t.Fatal("expected unknown selector key to be a validation error (Decision 5 / R9)")
	}
}

func TestDecodeSelectorEmptyIsWildcard(t *testing.T) {
	sel, err := decodeSelector(nil)
	if err != nil {
		t.Fatalf("nil selector: %v", err)
	}
	if sel != (ProfileSelector{}) {
		t.Fatalf("nil selector should be zero/wildcard, got %+v", sel)
	}
	if !sel.matches(ProfileContext{Role: "anything"}) {
		t.Fatal("wildcard selector should match any context")
	}
}

func TestDecodeSelectorMalformed(t *testing.T) {
	if _, err := decodeSelector(json.RawMessage(`["not","an","object"]`)); err == nil {
		t.Fatal("expected malformed selector error")
	}
	if _, err := decodeSelector(json.RawMessage(`{"role":123}`)); err == nil {
		t.Fatal("expected type-mismatch selector error")
	}
}

func TestSelectorMatchesAndSpecificity(t *testing.T) {
	sel := ProfileSelector{AppType: "go-cli", Stage: "verify"}
	if !sel.matches(ProfileContext{AppType: "go-cli", Stage: "verify", Role: "x"}) {
		t.Fatal("exact match on present keys + wildcard role should match")
	}
	if sel.matches(ProfileContext{AppType: "go-cli", Stage: "review"}) {
		t.Fatal("stage mismatch should not match")
	}
	if sel.matches(ProfileContext{AppType: "py", Stage: "verify"}) {
		t.Fatal("app_type mismatch should not match")
	}
	if got := sel.specificity(); got != 2 {
		t.Fatalf("specificity = %d, want 2", got)
	}
	full := ProfileSelector{Role: "r", AppType: "a", Stage: "s", Harness: "h"}
	if got := full.specificity(); got != 4 {
		t.Fatalf("full specificity = %d, want 4", got)
	}
	if full.matches(ProfileContext{Role: "r", AppType: "a", Stage: "s", Harness: "other"}) {
		t.Fatal("harness mismatch should not match")
	}
	if (ProfileSelector{Role: "r"}).matches(ProfileContext{Role: "x"}) {
		t.Fatal("role mismatch should not match")
	}
}

func TestOverridePermissionsThreeStates(t *testing.T) {
	// Omitted (nil): no opinion — anything may change.
	var omitted *OverridePermissions
	if !omitted.mayChange(AuthRepo, "tools.allow") {
		t.Fatal("omitted permissions must allow (Decision 2: inherit-higher / no restriction)")
	}
	// Lockdown ({}): nothing may change.
	lockdown := NewOverridePermissions(nil)
	if lockdown.mayChange(AuthRepo, "tools.allow") {
		t.Fatal("explicit-empty {} must be lockdown (Decision 2)")
	}
	if lockdown.mayChange(AuthOrg, "model") {
		t.Fatal("lockdown denies even org")
	}
	// Allowlist: present scope may change listed fields; absent scope nothing.
	allow := NewOverridePermissions(map[AuthorityScope][]string{
		AuthRepo: {"tools.allow", "mcp"},
		AuthOrg:  {"*"},
	})
	if !allow.mayChange(AuthRepo, "tools.allow") {
		t.Fatal("listed field should be allowed")
	}
	if allow.mayChange(AuthRepo, "model") {
		t.Fatal("unlisted field for present scope must be denied (Decision 2)")
	}
	if allow.mayChange(AuthUser, "tools.allow") {
		t.Fatal("scope absent from map may change nothing (Decision 2)")
	}
	if !allow.mayChange(AuthOrg, "anything") {
		t.Fatal("'*' should grant every field")
	}
}

func TestOverridePermissionsJSONRoundTripPreservesPresence(t *testing.T) {
	// Lockdown must round-trip as {} (non-nil empty), distinct from omitted.
	lockdown := NewOverridePermissions(nil)
	data, err := json.Marshal(lockdown)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Fatalf("lockdown marshals to %q, want {}", data)
	}
	var back OverridePermissions
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.byScope == nil {
		t.Fatal("decoding {} must yield a non-nil empty map (lockdown), not nil (omitted)")
	}
	if back.mayChange(AuthRepo, "x") {
		t.Fatal("round-tripped lockdown should still be lockdown")
	}
}

func TestDecodeLayeringPolicyStrict(t *testing.T) {
	cases := map[string]string{
		"unknown key":        `{"bogus":1}`,
		"force_allow lock":   `{"locks":[{"field":"tools.allow","force_allow":["Edit"]}]}`,
		"unknown mode":       `{"mode":"merge"}`,
		"lock missing field": `{"locks":[{"deny":["Edit"]}]}`,
		"deny+value lock":    `{"locks":[{"field":"model","deny":["x"],"value":"y"}]}`,
		"bad lock selector":  `{"locks":[{"field":"tools.allow","deny":["Edit"],"selector":{"bad":"k"}}]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeLayeringPolicy(json.RawMessage(raw), AuthOrg); err == nil {
				t.Fatalf("expected fail-closed error for %s", name)
			}
		})
	}
}

func TestDecodeLayeringPolicyValid(t *testing.T) {
	raw := `{
		"precedence":["user","repo","org"],
		"mode":"replace",
		"override_permissions":{"repo":["tools.allow"]},
		"locks":[
			{"field":"tools.allow","deny":["Edit","Write"],"selector":{"role":"reviewer"}},
			{"field":"model","value":"sonnet"}
		]
	}`
	p, err := decodeLayeringPolicy(json.RawMessage(raw), AuthOrg)
	if err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if p.Scope != AuthOrg {
		t.Fatalf("scope = %q, want org (source-derived stamp)", p.Scope)
	}
	if p.Mode != PolicyModeReplace {
		t.Fatalf("mode = %q", p.Mode)
	}
	if len(p.Locks) != 2 || p.Locks[0].Owner != AuthOrg {
		t.Fatalf("locks not stamped with owner: %+v", p.Locks)
	}
	if p.Locks[0].kind() != "deny_lock" || p.Locks[1].kind() != "value_lock" {
		t.Fatalf("lock kinds wrong: %q %q", p.Locks[0].kind(), p.Locks[1].kind())
	}
}

func TestAgentsRCLayeringPolicyRoundTrip(t *testing.T) {
	raw := `{"layering_policy":{"precedence":["repo","org"],"override_permissions":{}}}`
	var rc AgentsRC
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	if rc.LayeringPolicy == nil {
		t.Fatal("layering_policy did not decode into the typed field")
	}
	if rc.LayeringPolicy.OverridePermissions == nil || rc.LayeringPolicy.OverridePermissions.byScope == nil {
		t.Fatal("explicit-empty override_permissions must survive as lockdown")
	}
	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatal(err)
	}
	var rc2 AgentsRC
	if err := json.Unmarshal(out, &rc2); err != nil {
		t.Fatalf("re-decode after marshal: %v", err)
	}
	if rc2.LayeringPolicy == nil {
		t.Fatal("layering_policy lost on marshal round-trip")
	}
}
