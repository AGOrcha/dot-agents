# work-store-projection (prototype)

Answers the **KG-as-SOT D1′ question** from the `work-tracking-storage-abstraction`
spec: *if the graph is SOT for plan/task structure + state and
`.agents/workflow/**/{PLAN,TASKS,SLICES}.yaml` is a projection, what must the
graph schema store for that projection to be **lossless**?*

Self-contained module (`prototype/work-store-projection/`, own `go.mod`).

## The experiment routes through a REAL graph (not a mirror struct)

```
YAML  --parse-->  typed model  --ingest(profile)-->  graphstore (nodes + edges)
                                                          | readback (nodes+edges ONLY)
                                                          v
YAML  <--serialize--  reconstructed model  <--reconstruct--+
```

Reconstruction uses **only what the graph holds** (node fields + edges) — never a
retained parse struct. So a field the graph schema doesn't persist is *gone* at
readback. Two schema profiles are compared:

- **`SchemaV4`** — the SHIPPED `internal/adapters/builtin/sdd-register/ingest.go`
  field set. This is the field-dropping **negative control**.
- **`Complete`** — the minimal extension that persists every typed field (scalars
  as node fields, ordered lists as ordinal-carrying edges, container order as a
  node ordinal). The delta v4->complete is the **schema-completeness gap**.

```
go run . --graph-loss <plans-root>   # PRIMARY: v4 vs complete field loss
go run . --roundtrip  <plan-dir>     # secondary: serializer fidelity (one plan)
go run . --sweep      <plans-root>   # secondary: serializer fidelity (all)
go test ./...
```

## Field catalog — what schema-v4 stores vs drops (the deliverable)

| File | v4 stores | of M | v4 DROPS |
|---|---:|---:|---|
| PLAN.yaml | 4 | 12 | `schema_version, created_at, updated_at, owner, success_criteria, verification_strategy, current_focus_task, default_app_type` |
| TASKS.yaml | 4 | 10 | `depends_on`* , `blocks`, `owner`, `write_scope`, `verification_required`, `notes` |
| SLICES.yaml | **0** | 9 | everything — sdd-register has **no slice node type** |

\* `depends_on` becomes unordered edges with the target normalized to a node id —
the **order and the bare-vs-qualified literal form are not recoverable**. The
shipped graph also stores no task/slice **ordinal**, so container order is lost too.

## Real-tree result (35 plans, 351 tasks, 17 slice entries)

- **schema-v4 graph round-trip -> 1943 field-losses.** By field: `owner=380,
  notes=332, write_scope=324, verification_required=317, depends_on=276,
  blocks=104`, plus plan-level timestamps/criteria and **all 17 slices dropped
  wholesale**.
- **complete graph round-trip -> 0 losses**, and reconstruct->serialize reproduces
  the **same bytes** as the direct struct path (no extra churn).
- **Schema-completeness gap = 1943.**

## What `Complete` requires (and the honest second-order finding)

To be lossless the graph must store: every plan/task/slice **scalar as a node
field**, every **list (`write_scope`/`depends_on`/`blocks`) as ordinal-carrying
edges** (order + literal form), **container order** (task/slice position) as a
node ordinal, and a **slice node type + `contains_slice`/`slice_parent` edges**.

At that point the graph stores *every typed YAML field* — i.e. the graph is the
YAML in node form. So for the **working/structural view, graph-as-SOT buys
queryability + typed edges, not data the YAML lacked.** D1′ is achievable, but
only with a schema that mirrors the full typed surface; a lean "structural index"
graph (today's schema-v4) is **not** a lossless projection.

## Proofs

| Test | File | Claim |
|---|---|---|
| `TestGraphV4LosesFieldsOnRealTree` | `graphproj/graphloss_test.go` | the shipped graph loses fields on the real tree (quantified; named fields) |
| `TestGraphCompleteIsLosslessOnRealTree` | same | the extended graph round-trips every field with 0 loss |
| `TestGraphCompleteReconstructsByteFaithful` | same | graph-reconstructed bytes == struct-serialized bytes (no extra churn) |
| `TestReingestUpdatesGraphNode` / `…RewritesEdges` / `…Idempotent` | `graphproj/reingest_test.go` | re-ingest UPDATES the graph (node merge / edge rewrite), idempotent |
| H-roundtrip / H-churn / H-reingest (serializer half) | `projection/*_test.go` | the canonical serializer is deterministic, churn-free, edit-survivable — see the SCOPE NOTE in `projection/model.go` |

## What this proves vs what it can't

- **Proves:** the shipped schema-v4 graph is a **lossy** projection of the work
  YAML (1943 real losses, with the dropped-field list); a complete schema CAN be
  lossless; and that complete schema is essentially the full YAML surface in node
  form.
- **Can't prove:** that the *production* SDK store / DSL evaluator behave like
  this in-memory `graphstore` (shapes mirrored, not the real backend); nor
  anything about prose artifacts (`design.md`, `.plan.md`) — those stay
  git-canonical by D1′ and are out of scope.
- **Earlier-version caveat:** the original `projection/*` proofs (struct ->
  yaml.Marshal -> struct) only test the serializer; they are NOT the D1′ proof
  (the model mirrors the YAML 1:1, so "lossless for typed fields" was
  tautological). They are retained as the YAML-write half. The graph round-trip
  above is the corrected experiment.
