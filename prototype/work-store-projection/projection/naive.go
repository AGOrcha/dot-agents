package projection

import yaml "go.yaml.in/yaml/v3"

// NaiveSerializeTasks is the NEGATIVE CONTROL serializer. It proves that
// churn-free round-trip is NOT free — it depends on the canonical serializer's
// discipline (struct-order keys, default indent, preserved block scalars).
//
// The naive path routes the parsed task file through a generic map, which YAML
// emits with SORTED keys (alphabetical) instead of struct-declaration order.
// That reorders every field of every task and reflows block scalars, producing
// large diffs against the original — quantified by DiffLineCount in the tests.
//
// The delta between this and SerializeTasks IS the finding: the canonical
// serializer's key-order + indent rules are load-bearing for churn-freedom.
func NaiveSerializeTasks(tf *TaskFile) ([]byte, error) {
	generic := toGeneric(tf)
	return yaml.Marshal(generic)
}

// toGeneric converts the typed task file into map[string]any so that the YAML
// encoder uses map semantics (sorted keys) rather than struct field order.
func toGeneric(tf *TaskFile) map[string]any {
	tasks := make([]any, 0, len(tf.Tasks))
	for _, t := range tf.Tasks {
		tasks = append(tasks, map[string]any{
			"id":                    t.ID,
			"title":                 t.Title,
			"status":                t.Status,
			"depends_on":            toAnySlice(t.DependsOn),
			"blocks":                toAnySlice(t.Blocks),
			"owner":                 t.Owner,
			"write_scope":           toAnySlice(t.WriteScope),
			"verification_required": t.VerificationRequired,
			"notes":                 t.Notes,
			"app_type":              t.AppType,
		})
	}
	return map[string]any{
		"schema_version": tf.SchemaVersion,
		"plan_id":        tf.PlanID,
		"tasks":          tasks,
	}
}

func toAnySlice(s []string) []any {
	if s == nil {
		return nil
	}
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
