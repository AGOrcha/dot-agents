package graphproj

import (
	"sort"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/graphstore"
	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

// ReconstructPlan rebuilds a Plan PURELY from the graph node (no parse struct).
// Fields the profile did not persist come back as zero values — that IS the
// loss. The plan node is found by id.
func ReconstructPlan(s *graphstore.Store, planID string) *projection.Plan {
	n, ok := s.Node(planNodeID(planID))
	if !ok {
		return nil
	}
	p := &projection.Plan{
		ID:      str(n, "plan_id"),
		Title:   str(n, "title"),
		Status:  str(n, "status"),
		Summary: str(n, "summary"),
		// Complete-only fields (zero under v4):
		SchemaVersion:        intf(n, "schema_version"),
		CreatedAt:            str(n, "created_at"),
		UpdatedAt:            str(n, "updated_at"),
		Owner:                str(n, "owner"),
		SuccessCriteria:      str(n, "success_criteria"),
		VerificationStrategy: str(n, "verification_strategy"),
		CurrentFocusTask:     str(n, "current_focus_task"),
		DefaultAppType:       str(n, "default_app_type"),
	}
	return p
}

// ReconstructTasks rebuilds a TaskFile from the graph: the contains_task edges
// give the task set + order, each task node gives its fields, and list edges
// give write_scope/depends_on/blocks. v4 task nodes lack most fields and have no
// ordered list edges, so those come back empty.
func ReconstructTasks(s *graphstore.Store, planID string, schemaVersion int) *projection.TaskFile {
	tf := &projection.TaskFile{SchemaVersion: schemaVersion, PlanID: planID}
	for _, n := range childrenByOrdinal(s, edgeContainsTask, planNodeID(planID)) {
		tf.Tasks = append(tf.Tasks, projection.Task{
			ID:                   str(n, "task_id"),
			Title:                str(n, "title"),
			Status:               str(n, "status"),
			AppType:              str(n, "app_type"),
			Owner:                str(n, "owner"),
			VerificationRequired: boolf(n, "verification_required"),
			Notes:                str(n, "notes"),
			DependsOn:            readOrderedList(s, edgeDependsOn, n.ID),
			Blocks:               readOrderedList(s, edgeBlocks, n.ID),
			WriteScope:           readOrderedList(s, edgeWriteScope, n.ID),
		})
	}
	return tf
}

// ReconstructSlices rebuilds a SliceFile from contains_slice edges + slice nodes
// (complete profile only; v4 has no slice nodes so this returns an empty file).
func ReconstructSlices(s *graphstore.Store, planID string, schemaVersion int) *projection.SliceFile {
	sf := &projection.SliceFile{SchemaVersion: schemaVersion, PlanID: planID}
	for _, n := range childrenByOrdinal(s, edgeContainsSlc, planNodeID(planID)) {
		sf.Slices = append(sf.Slices, projection.Slice{
			ID:                str(n, "slice_id"),
			ParentTaskID:      str(n, "parent_task_id"),
			Title:             str(n, "title"),
			Summary:           str(n, "summary"),
			Status:            str(n, "status"),
			VerificationFocus: str(n, "verification_focus"),
			Owner:             str(n, "owner"),
			DependsOn:         readOrderedList(s, edgeSliceDepends, n.ID),
			WriteScope:        readOrderedList(s, edgeWriteScope, n.ID),
		})
	}
	return sf
}

// childrenByOrdinal returns the child nodes reached from `from` via edgeType,
// ordered by each child's _ordinal field. Containment edges form an unordered
// SET; the explicit ordinal is what restores the original file order.
func childrenByOrdinal(s *graphstore.Store, edgeType, from string) []*graphstore.Node {
	var nodes []*graphstore.Node
	for _, e := range s.OutEdges(edgeType, from) {
		if n, ok := s.Node(e.To); ok {
			nodes = append(nodes, n)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodeOrdinal(nodes[i]) < nodeOrdinal(nodes[j])
	})
	return nodes
}

func nodeOrdinal(n *graphstore.Node) int {
	if v, ok := n.Fields[ordinalField].(int); ok {
		return v
	}
	return 0
}

// readOrderedList reverses writeOrderedList: it reads the "<ord>|<literal>"
// targets, sorts by ordinal, and returns the literals in order. Returns nil
// when there are no such edges (v4 lists, or a genuinely empty list).
func readOrderedList(s *graphstore.Store, edgeType, from string) []string {
	edges := s.OutEdges(edgeType, from)
	if len(edges) == 0 {
		return nil
	}
	type item struct {
		ord int
		val string
	}
	items := make([]item, 0, len(edges))
	for _, e := range edges {
		ord, val := splitOrdinal(e.To)
		items = append(items, item{ord, val})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ord < items[j].ord })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.val
	}
	return out
}

func splitOrdinal(target string) (int, string) {
	idx := strings.IndexByte(target, '|')
	if idx < 0 {
		return 0, target
	}
	ord, _ := strconv.Atoi(target[:idx])
	return ord, target[idx+1:]
}

func str(n *graphstore.Node, key string) string {
	if v, ok := n.Fields[key].(string); ok {
		return v
	}
	return ""
}

func intf(n *graphstore.Node, key string) int {
	if v, ok := n.Fields[key].(int); ok {
		return v
	}
	return 0
}

func boolf(n *graphstore.Node, key string) bool {
	if v, ok := n.Fields[key].(bool); ok {
		return v
	}
	return false
}
