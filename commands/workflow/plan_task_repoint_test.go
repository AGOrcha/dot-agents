package workflow

import "testing"

const otherTaskID = "t-other"

func TestRepointRefMapsBareAndQualifiedIDs(t *testing.T) {
	const planID, oldID, newID = "plan-a", "t-old", "t-new"
	oldQ, newQ := planID+"/"+oldID, planID+"/"+newID
	cases := []struct {
		name     string
		ref      string
		remove   bool
		wantRef  string
		wantKeep bool
	}{
		{"bare old rewritten", oldID, false, newID, true},
		{"bare old removed", oldID, true, "", false},
		{"qualified old rewritten", oldQ, false, newQ, true},
		{"qualified old removed", oldQ, true, "", false},
		{"unrelated passthrough", otherTaskID, false, otherTaskID, true},
		{"unrelated passthrough on remove", otherTaskID, true, otherTaskID, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRef, gotKeep := repointRef(tc.ref, oldID, oldQ, newID, newQ, tc.remove)
			if gotRef != tc.wantRef || gotKeep != tc.wantKeep {
				t.Fatalf("repointRef(%q, remove=%v) = (%q, %v), want (%q, %v)", tc.ref, tc.remove, gotRef, gotKeep, tc.wantRef, tc.wantKeep)
			}
		})
	}
}

func refRecord(planID, taskID string, order int) stateRefTaskRecord {
	return stateRefTaskRecord{SchemaVersion: 1, PlanID: planID, Order: order, Task: CanonicalTask{ID: taskID}}
}

func TestResolveRepointRefTasks(t *testing.T) {
	const plan, oldID, newID = "plan-a", "t-old", "t-new"
	base := &CanonicalTaskFile{SchemaVersion: 1, PlanID: plan, Tasks: []CanonicalTask{{ID: oldID}}}
	rename := taskRepointInputs{PlanID: plan, OldID: oldID, NewID: newID, Operation: "rename"}

	// No prior ref snapshot: the working-copy tf passes through unchanged.
	if got, err := resolveRepointRefTasks(base, nil, rename); err != nil || got != base {
		t.Fatalf("empty ref must pass tf through: got=%p want=%p err=%v", got, base, err)
	}

	// Old task still on the ref: the rename applies to the projected snapshot.
	present := []stateRefTaskRecord{refRecord(plan, oldID, 0), refRecord(plan, otherTaskID, 1)}
	if got, err := resolveRepointRefTasks(base, present, rename); err != nil || got == nil {
		t.Fatalf("rename on a present old task: got=%v err=%v", got, err)
	}

	// Only the new id present (a concurrent writer already repointed it):
	// dependencies are repointed, no error.
	newOnly := []stateRefTaskRecord{refRecord(plan, newID, 0), refRecord(plan, otherTaskID, 1)}
	if _, err := resolveRepointRefTasks(base, newOnly, rename); err != nil {
		t.Fatalf("new-only ref must repoint deps without error: %v", err)
	}

	// Neither id present: not found.
	none := []stateRefTaskRecord{refRecord(plan, otherTaskID, 0)}
	if _, err := resolveRepointRefTasks(base, none, rename); err == nil {
		t.Fatal("missing old and new ids must error")
	}

	// Both present under a rename: applyTaskRepoint's conflict error propagates.
	both := []stateRefTaskRecord{refRecord(plan, oldID, 0), refRecord(plan, newID, 1)}
	if _, err := resolveRepointRefTasks(base, both, rename); err == nil {
		t.Fatal("renaming onto an existing new id must error")
	}
}
