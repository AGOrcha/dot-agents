package graphproj

import (
	"testing"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/graphstore"
	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

func sampleInputs() (*projection.Plan, *projection.TaskFile) {
	plan := &projection.Plan{SchemaVersion: 1, ID: "p", Title: "T", Status: "active", Summary: "s"}
	tf := &projection.TaskFile{SchemaVersion: 1, PlanID: "p", Tasks: []projection.Task{
		{ID: "t1", Title: "one", Status: "pending", DependsOn: []string{"t0"}, Notes: "n1", Owner: "o", VerificationRequired: true, WriteScope: []string{"a.go"}},
		{ID: "t2", Title: "two", Status: "completed", Owner: "o"},
	}}
	return plan, tf
}

// TestReingestUpdatesGraphNode is point 4: re-ingest must UPDATE THE GRAPH (node
// merge), not just re-parse a struct. We ingest, mutate the model (status +
// notes), re-ingest into the SAME store, and assert the graph node changed and
// reconstruction reflects it.
func TestReingestUpdatesGraphNode(t *testing.T) {
	plan, tf := sampleInputs()
	s := graphstore.New()
	IngestInto(s, Complete, plan, tf, nil)

	// The file is edited on disk: t1 status pending->in_progress, notes appended.
	tf.Tasks[0].Status = "in_progress"
	tf.Tasks[0].Notes = "n1 EDITED"

	// Re-ingest into the SAME store: PutNode merges by id.
	IngestInto(s, Complete, plan, tf, nil)

	n, ok := s.Node(taskNodeID("p", "t1"))
	if !ok {
		t.Fatal("t1 node missing after re-ingest")
	}
	if n.Fields["status"] != "in_progress" {
		t.Errorf("graph node status not updated: %v", n.Fields["status"])
	}
	if n.Fields["notes"] != "n1 EDITED" {
		t.Errorf("graph node notes not updated: %v", n.Fields["notes"])
	}
	// Reconstruction reflects the updated graph.
	rt := ReconstructTasks(s, "p", 1)
	if rt.Tasks[0].Status != "in_progress" || rt.Tasks[0].Notes != "n1 EDITED" {
		t.Errorf("reconstruction did not reflect graph update: %+v", rt.Tasks[0])
	}
	// t2 untouched.
	if rt.Tasks[1].Status != "completed" {
		t.Errorf("t2 changed unexpectedly: %v", rt.Tasks[1].Status)
	}
}

// TestReingestRewritesEdges proves a STRUCTURAL edit rewrites edges, not
// accumulates them: changing t1.depends_on from [t0] to [tA,tB] must leave the
// graph with exactly the new deps (old t0 edge gone).
func TestReingestRewritesEdges(t *testing.T) {
	plan, tf := sampleInputs()
	s := graphstore.New()
	IngestInto(s, Complete, plan, tf, nil)

	tf.Tasks[0].DependsOn = []string{"tA", "tB"}
	IngestInto(s, Complete, plan, tf, nil)

	deps := ReconstructTasks(s, "p", 1).Tasks[0].DependsOn
	if len(deps) != 2 || deps[0] != "tA" || deps[1] != "tB" {
		t.Errorf("edge rewrite failed: got depends_on=%v (expected [tA tB], old t0 must be gone)", deps)
	}
}

// TestReingestIdempotent proves re-ingesting the SAME model twice yields the
// same graph (no edge duplication) and the same reconstruction — the no-drift
// guarantee for the graph SOT.
func TestReingestIdempotent(t *testing.T) {
	plan, tf := sampleInputs()
	s := graphstore.New()
	IngestInto(s, Complete, plan, tf, nil)
	n1, e1 := s.Stats()
	IngestInto(s, Complete, plan, tf, nil)
	n2, e2 := s.Stats()
	if n1 != n2 || e1 != e2 {
		t.Errorf("re-ingest not idempotent: nodes %d->%d edges %d->%d", n1, n2, e1, e2)
	}
}
