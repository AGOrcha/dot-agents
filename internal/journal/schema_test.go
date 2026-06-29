package journal

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// expectedCommands is the canonical journaled set and its tier, kept here so the
// test fails loudly if a command is added/removed/retiered without an explicit
// review. It must match the registry exactly (asserted in TestRegistryMatchesExpected).
var expectedCommands = map[string]Tier{
	// Tier 1.
	CmdAdvance:            TierUnconditional,
	CmdStartTask:          TierUnconditional,
	CmdCloseTask:          TierUnconditional,
	CmdPlanCreate:         TierUnconditional,
	CmdPlanArchive:        TierUnconditional,
	CmdMergeBack:          TierUnconditional,
	CmdFanout:             TierUnconditional,
	CmdFoldBackCreate:     TierUnconditional,
	CmdFoldBackUpdate:     TierUnconditional,
	CmdDelegationCloseout: TierUnconditional,
	CmdContractCreate:     TierUnconditional,
	CmdPlanDeriveScope:    TierUnconditional,
	CmdVerifyRecord:       TierUnconditional,
	CmdCheckpoint:         TierUnconditional,
	CmdCommit:             TierUnconditional,
	CmdArchiveOrphans:     TierUnconditional,
	CmdSweepApply:         TierUnconditional,
	// Tier 2.
	CmdTaskAdd:        TierDelta,
	CmdTaskUpdate:     TierDelta,
	CmdPlanUpdate:     TierDelta,
	CmdPrefsSetLocal:  TierDelta,
	CmdPrefsSetShared: TierDelta,
	// KG.
	CmdKGIngest:          TierKG,
	CmdKGLinkAdd:         TierKG,
	CmdKGLinkRemove:      TierKG,
	CmdKGMaintainReweave: TierKG,
	CmdKGMaintainStale:   TierKG,
	CmdKGMaintainCompact: TierKG,
	CmdKGWarm:            TierKG,
	CmdKGBuild:           TierKG,
	CmdKGUpdate:          TierKG,
	CmdKGPostprocess:     TierKG,
	CmdKGSync:            TierKG,
	// Review.
	CmdReviewApprove: TierReview,
	CmdReviewReject:  TierReview,
}

// TestRegistryMatchesExpected pins the journaled set: every expected command
// resolves to its tier, and the registry holds nothing extra.
func TestRegistryMatchesExpected(t *testing.T) {
	for cmd, wantTier := range expectedCommands {
		spec, ok := Lookup(cmd)
		if !ok {
			t.Errorf("command %q missing from registry", cmd)
			continue
		}
		if spec.Tier() != wantTier {
			t.Errorf("command %q tier = %q, want %q", cmd, spec.Tier(), wantTier)
		}
		if spec.Command() != cmd {
			t.Errorf("spec.Command() = %q, want %q", spec.Command(), cmd)
		}
		if !IsJournaled(cmd) {
			t.Errorf("IsJournaled(%q) = false, want true", cmd)
		}
	}
	if len(registry) != len(expectedCommands) {
		t.Errorf("registry has %d commands, expected set has %d", len(registry), len(expectedCommands))
		for cmd := range registry {
			if _, ok := expectedCommands[cmd]; !ok {
				t.Errorf("registry has unexpected command %q", cmd)
			}
		}
	}
}

// TestTierToEventType verifies the tier→event_type classification: only Tier-2 is
// input_only; every other tier is a durable_delta.
func TestTierToEventType(t *testing.T) {
	cases := []struct {
		tier Tier
		want EventType
	}{
		{TierUnconditional, EventDurableDelta},
		{TierDelta, EventInputOnly},
		{TierKG, EventDurableDelta},
		{TierReview, EventDurableDelta},
	}
	for _, tc := range cases {
		got := CommandSpec{tier: tc.tier}.EventType()
		if got != tc.want {
			t.Errorf("tier %q EventType() = %q, want %q", tc.tier, got, tc.want)
		}
	}
}

// TestExcludedCommandsNotRegistered asserts the spec's Excluded set (config,
// hook-sentinel/outcome, score) and read-only/unknown commands resolve to nothing.
func TestExcludedCommandsNotRegistered(t *testing.T) {
	excluded := []string{
		"refresh",
		"config explain",
		"config verify",
		"config relevance",
		"kg setup",
		"workflow hook-sentinel",
		"workflow hook-outcome",
		"workflow score",
		"workflow status",
		"workflow drift", // read-only; only sweep --apply journals
		"",
		"not a real command",
	}
	for _, cmd := range excluded {
		if spec, ok := Lookup(cmd); ok {
			t.Errorf("excluded command %q resolved to tier %q, want not journaled", cmd, spec.Tier())
		}
		if IsJournaled(cmd) {
			t.Errorf("IsJournaled(%q) = true, want false", cmd)
		}
	}
}

// TestSchemaRoundTripAllCommands round-trips every command's typed Input and
// Observed through NewEvent and back, covering each registry factory and asserting
// the payloads survive the envelope's RawMessage hop losslessly.
func TestSchemaRoundTripAllCommands(t *testing.T) {
	for cmd, spec := range registry {
		t.Run(cmd, func(t *testing.T) {
			input := spec.NewInput()
			observed := spec.NewObserved()

			env, err := NewEvent(cmd, ActorMain, input, observed)
			if err != nil {
				t.Fatalf("NewEvent(%q): %v", cmd, err)
			}
			if env.Command != cmd {
				t.Errorf("env.Command = %q, want %q", env.Command, cmd)
			}
			if env.EventType != spec.EventType() {
				t.Errorf("env.EventType = %q, want %q", env.EventType, spec.EventType())
			}

			gotInput := spec.NewInput()
			if err := json.Unmarshal(env.Input, gotInput); err != nil {
				t.Fatalf("unmarshal input: %v", err)
			}
			if !reflect.DeepEqual(input, gotInput) {
				t.Errorf("input round-trip drift: got %+v want %+v", gotInput, input)
			}

			gotObserved := spec.NewObserved()
			if err := json.Unmarshal(env.Observed, gotObserved); err != nil {
				t.Fatalf("unmarshal observed: %v", err)
			}
			if !reflect.DeepEqual(observed, gotObserved) {
				t.Errorf("observed round-trip drift: got %+v want %+v", gotObserved, observed)
			}
		})
	}
}

// TestNewEventPopulatedPayloads round-trips populated payloads for representative
// commands, exercising the named JSON tags and the R8 locus split.
func TestNewEventPopulatedPayloads(t *testing.T) {
	t.Run("advance with in-open-pr locus", func(t *testing.T) {
		in := &AdvanceInput{Plan: "p", Task: "p2", Status: "completed", CommitState: "committed"}
		obs := &AdvanceObserved{
			FromStatus: "in_progress",
			ToStatus:   "completed",
			Committed:  true,
			HeadSHA:    "abc123",
			Locus:      &Locus{InOpenPR: &InOpenPRRef{PR: 90, Status: "completed"}},
		}
		env, err := NewEvent(CmdAdvance, ActorMain, in, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		if env.EventType != EventDurableDelta {
			t.Errorf("EventType = %q, want durable_delta", env.EventType)
		}
		got := &AdvanceObserved{}
		if err := json.Unmarshal(env.Observed, got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Locus == nil || got.Locus.InOpenPR == nil {
			t.Fatalf("locus.in_open_pr dropped: %+v", got.Locus)
		}
		if got.Locus.InOpenPR.PR != 90 || got.Locus.Canonical != nil {
			t.Errorf("locus round-trip wrong: %+v", got.Locus)
		}
	})

	t.Run("merge-back with canonical locus", func(t *testing.T) {
		obs := &MergeBackObserved{
			ArtifactPath: "merge-back.md",
			Verdict:      "accept",
			Committed:    true,
			Locus:        &Locus{Canonical: &CanonicalRef{Ref: "master@deadbeef"}},
		}
		env, err := NewEvent(CmdMergeBack, ActorLoopWorker, &MergeBackInput{Task: "p2"}, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		got := &MergeBackObserved{}
		if err := json.Unmarshal(env.Observed, got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Locus == nil || got.Locus.Canonical == nil || got.Locus.Canonical.Ref != "master@deadbeef" {
			t.Errorf("canonical locus round-trip wrong: %+v", got.Locus)
		}
	})

	t.Run("tier-2 task update is input_only with changed_fields", func(t *testing.T) {
		in := &DeltaInput{Plan: "p", Task: "p2", ChangedFields: NewChangedFields(map[string]string{"status": "in_progress"})}
		obs := &DeltaObserved{FieldsReplaced: []string{"status"}}
		env, err := NewEvent(CmdTaskUpdate, ActorMain, in, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		if env.EventType != EventInputOnly {
			t.Errorf("EventType = %q, want input_only", env.EventType)
		}
		got := &DeltaObserved{}
		if err := json.Unmarshal(env.Observed, got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got.FieldsReplaced) != 1 || got.FieldsReplaced[0] != "status" {
			t.Errorf("changed-fields delta wrong: %+v", got)
		}
	})

	t.Run("derive-scope records counts not bodies", func(t *testing.T) {
		obs := &DeriveScopeObserved{SidecarPath: "scope.yaml", Mode: "code", Confidence: "high", RequiredPaths: 2, Queries: 5}
		env, err := NewEvent(CmdPlanDeriveScope, ActorMain, &DeriveScopeInput{Plan: "p", Task: "p2", SeedSymbols: []string{"Foo"}}, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		got := &DeriveScopeObserved{}
		if err := json.Unmarshal(env.Observed, got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.RequiredPaths != 2 || got.Queries != 5 || got.Confidence != "high" {
			t.Errorf("derive-scope summary wrong: %+v", got)
		}
	})

	t.Run("kg ingest records counts + ids", func(t *testing.T) {
		obs := &KGIngestObserved{NotesCreated: 3, NotesUpdated: 1, NoteIDs: []string{"n1", "n2"}}
		env, err := NewEvent(CmdKGIngest, ActorMain, &KGIngestInput{All: true}, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		got := &KGIngestObserved{}
		if err := json.Unmarshal(env.Observed, got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.NotesCreated != 3 || got.NotesUpdated != 1 || len(got.NoteIDs) != 2 {
			t.Errorf("kg ingest counts/ids wrong: %+v", got)
		}
	})
}

// TestNewEventOmitsNilPayloads verifies that a nil interface and a typed-nil
// pointer both collapse to an absent payload.
func TestNewEventOmitsNilPayloads(t *testing.T) {
	t.Run("nil interface", func(t *testing.T) {
		env, err := NewEvent(CmdCommit, ActorMain, nil, nil)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		if env.Input != nil || env.Observed != nil {
			t.Errorf("nil payloads not dropped: input=%s observed=%s", env.Input, env.Observed)
		}
	})
	t.Run("typed-nil pointer", func(t *testing.T) {
		var in *CommitInput
		var obs *CommitObserved
		env, err := NewEvent(CmdCommit, ActorMain, in, obs)
		if err != nil {
			t.Fatalf("NewEvent: %v", err)
		}
		if env.Input != nil || env.Observed != nil {
			t.Errorf("typed-nil payloads not dropped: input=%s observed=%s", env.Input, env.Observed)
		}
	})
}

// TestNewEventUnknownCommand asserts a non-journaled command fails loudly.
func TestNewEventUnknownCommand(t *testing.T) {
	if _, err := NewEvent("config explain", ActorMain, nil, nil); err == nil {
		t.Fatal("NewEvent for excluded command: want error, got nil")
	}
}

// TestNewEventMarshalError surfaces a marshal failure from either payload.
func TestNewEventMarshalError(t *testing.T) {
	bad := make(chan int) // channels are not JSON-marshalable
	if _, err := NewEvent(CmdAdvance, ActorMain, bad, nil); err == nil {
		t.Error("NewEvent with unmarshalable input: want error")
	}
	if _, err := NewEvent(CmdAdvance, ActorMain, nil, bad); err == nil {
		t.Error("NewEvent with unmarshalable observed: want error")
	}
}

// TestNewFailedEvent verifies a failed event carries input, never a fabricated
// observed, and is typed EventFailed (R1) — and rejects non-journaled commands.
func TestNewFailedEvent(t *testing.T) {
	env, err := NewFailedEvent(CmdAdvance, ActorOrchestrator, &AdvanceInput{Plan: "p", Task: "p2"})
	if err != nil {
		t.Fatalf("NewFailedEvent: %v", err)
	}
	if env.EventType != EventFailed {
		t.Errorf("EventType = %q, want failed", env.EventType)
	}
	if env.Observed != nil {
		t.Errorf("failed event carries observed: %s", env.Observed)
	}
	if env.Input == nil {
		t.Error("failed event should carry input")
	}

	if _, err := NewFailedEvent("score", ActorMain, nil); err == nil {
		t.Error("NewFailedEvent for excluded command: want error")
	}
	if _, err := NewFailedEvent(CmdAdvance, ActorMain, make(chan int)); err == nil {
		t.Error("NewFailedEvent with unmarshalable input: want error")
	}
}

// TestFailedEventDropsObservedThroughEmit covers the failed-event-drops-observed
// interaction end to end: even if a caller hands Emit an observed on a failed
// event, the persisted line carries none (R1).
func TestFailedEventDropsObservedThroughEmit(t *testing.T) {
	repo := t.TempDir()
	env, err := NewFailedEvent(CmdCommit, ActorMain, &CommitInput{DryRun: true})
	if err != nil {
		t.Fatalf("NewFailedEvent: %v", err)
	}
	env.Observed = json.RawMessage(`{"head_sha":"should-be-dropped"}`) // caller mistake
	if err := Emit(repo, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	events := readEvents(t, repo)
	got := events[len(events)-1]
	if got.EventType != EventFailed {
		t.Errorf("EventType = %q, want failed", got.EventType)
	}
	if got.Observed != nil {
		t.Errorf("persisted failed event carries observed: %s", got.Observed)
	}
}

// TestLocusValidate enforces the R8 sum-type: exactly one arm set passes; both,
// neither, or (for a present Locus) empty is rejected; a nil Locus is allowed.
func TestLocusValidate(t *testing.T) {
	cases := []struct {
		name    string
		locus   *Locus
		wantErr bool
	}{
		{"nil locus is valid", nil, false},
		{"canonical only", &Locus{Canonical: &CanonicalRef{Ref: "master@x"}}, false},
		{"in-open-pr only", &Locus{InOpenPR: &InOpenPRRef{PR: 1, Status: "completed"}}, false},
		{"both arms set", &Locus{Canonical: &CanonicalRef{Ref: "master@x"}, InOpenPR: &InOpenPRRef{PR: 1}}, true},
		{"neither arm set", &Locus{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.locus.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestNewEventRejectsInvalidLocus asserts NewEvent fails loudly when an observed
// payload carries an invalid locus, rather than persisting an ambiguous record.
func TestNewEventRejectsInvalidLocus(t *testing.T) {
	bad := &AdvanceObserved{
		ToStatus: "completed",
		Locus:    &Locus{Canonical: &CanonicalRef{Ref: "master@x"}, InOpenPR: &InOpenPRRef{PR: 1}},
	}
	if _, err := NewEvent(CmdAdvance, ActorMain, &AdvanceInput{Plan: "p", Task: "t"}, bad); err == nil {
		t.Fatal("NewEvent with both-arm locus: want error, got nil")
	}

	// A valid single-arm locus on every locus-carrying observed passes.
	carriers := []any{
		&AdvanceObserved{Locus: &Locus{Canonical: &CanonicalRef{Ref: "m@x"}}},
		&CloseTaskObserved{Locus: &Locus{InOpenPR: &InOpenPRRef{PR: 2, Status: "completed"}}},
		&MergeBackObserved{ArtifactPath: "a", Locus: &Locus{Canonical: &CanonicalRef{Ref: "m@y"}}},
	}
	cmds := []string{CmdAdvance, CmdCloseTask, CmdMergeBack}
	for i, obs := range carriers {
		if _, err := NewEvent(cmds[i], ActorMain, nil, obs); err != nil {
			t.Errorf("NewEvent(%q) with valid locus: %v", cmds[i], err)
		}
	}

	// A typed-nil locus carrier must not panic and must validate clean, for every
	// carrier type (each has its own nil-receiver guard).
	var nilAdvance *AdvanceObserved
	var nilClose *CloseTaskObserved
	var nilMerge *MergeBackObserved
	nilCarriers := map[string]any{
		CmdAdvance:   nilAdvance,
		CmdCloseTask: nilClose,
		CmdMergeBack: nilMerge,
	}
	for cmd, obs := range nilCarriers {
		if _, err := NewEvent(cmd, ActorMain, nil, obs); err != nil {
			t.Errorf("NewEvent(%q) with typed-nil observed: %v", cmd, err)
		}
	}
}

// TestNewChangedFieldsBounded asserts the Tier-2 input reduces a raw value to
// {name, len, sha256-prefix} and never stores the value verbatim — even a large
// body collapses to fixed-size metadata.
func TestNewChangedFieldsBounded(t *testing.T) {
	if got := NewChangedFields(nil); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}

	body := strings.Repeat("secret-note-body ", 1000) // ~17KB body
	cf := NewChangedFields(map[string]string{
		"notes":  body,
		"status": "in_progress",
	})
	if len(cf) != 2 {
		t.Fatalf("got %d field deltas, want 2", len(cf))
	}
	// Sorted by name: "notes" before "status".
	if cf[0].Name != "notes" || cf[1].Name != "status" {
		t.Errorf("not sorted by name: %+v", cf)
	}
	if cf[0].Len != len(body) {
		t.Errorf("notes.len = %d, want %d", cf[0].Len, len(body))
	}
	if len(cf[0].SHA256) != changedFieldHashLen {
		t.Errorf("sha256 prefix len = %d, want %d", len(cf[0].SHA256), changedFieldHashLen)
	}

	// The body must not survive anywhere in the serialized input.
	in := &DeltaInput{Plan: "p", Task: "t", ChangedFields: cf}
	env, err := NewEvent(CmdTaskUpdate, ActorMain, in, &DeltaObserved{FieldsReplaced: []string{"notes", "status"}})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if strings.Contains(string(env.Input), "secret-note-body") {
		t.Fatalf("raw body leaked into journaled input: %s", env.Input)
	}
}

// TestContractCreateRoundTrip covers the newly registered contract create schema.
func TestContractCreateRoundTrip(t *testing.T) {
	obs := &ContractCreateObserved{
		ContractID:         "del-t-1",
		Mode:               "direct",
		Status:             "active",
		ContractPath:       ".agents/active/contracts/del-t-1.yaml",
		ResolvedWriteScope: []string{"internal/journal/"},
	}
	env, err := NewEvent(CmdContractCreate, ActorOrchestrator, &ContractCreateInput{Plan: "p", Task: "t", Mode: "direct"}, obs)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if env.EventType != EventDurableDelta {
		t.Errorf("EventType = %q, want durable_delta", env.EventType)
	}
	got := &ContractCreateObserved{}
	if err := json.Unmarshal(env.Observed, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ContractID != "del-t-1" || got.Mode != "direct" || len(got.ResolvedWriteScope) != 1 {
		t.Errorf("contract round-trip wrong: %+v", got)
	}
}
