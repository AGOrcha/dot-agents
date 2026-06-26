# work-store-projection (prototype)

Validates the **KG-as-SOT "Group C" fork** (spec
`work-tracking-storage-abstraction` §6, OQ6 / D1′): if the graph is the source
of truth for plan/task **structure + state** and the committed
`.agents/workflow/**/{PLAN,TASKS}.yaml` are a **projection**, then regenerating
the YAML from a typed model must be **lossless** and **churn-free**, and a
hand-edited file must **re-ingest cleanly** (editing the file IS the act).

This is a self-contained module (`prototype/work-store-projection/`, own
`go.mod`) — it does not couple to the main module. It studies the shipped
`internal/adapters/builtin/sdd-register/ingest.go` parse approach but
re-implements a minimal typed model.

## Run

```
go run . --roundtrip <plan-dir>     # one plan: grade + unified diff per file
go run . --sweep <plans-root>       # every plan: per-file grade table + aggregate
go test ./...                       # the proofs
```

## What the proofs establish (run over the REAL `.agents/workflow/plans/` tree)

| Proof | File | Claim |
|---|---|---|
| H-roundtrip | `roundtrip_test.go` | every file is byte-identical, semantic-equal, normalized, or an **explicitly-named** lossy case — no silent loss |
| H-churn | `churn_test.go` | serializing is deterministic AND a fixed point (regen of regen == regen) for every file — the no-git-noise guarantee |
| H-reingest | `reingest_test.go` | regenerate → hand-edit the file bytes → re-ingest reflects the edit (incl. the `: `-in-notes schema-usage case) |
| Negative control | `churn_test.go` | a naive (key-reordering) serializer produces **17x** the churn of the canonical one — proving struct-order key emission is load-bearing |
| Mutation sensitivity | `mutation_test.go` | a serializer that drops `notes` is caught; key-reorder registers as churn — the proofs are not coverage theater |

## Fidelity grades

- **byte-identical** — zero git churn (the canonical serializer == what `da`
  itself writes, which is plain `yaml.Marshal`, default 4-space indent).
- **semantic-equal** — same parsed structure, cosmetic byte diff (comments,
  blank lines, hand-applied non-canonical indent/quoting). One-time reflow,
  clean thereafter.
- **normalized** — regen materializes an **absent** schema key to its zero value
  (a task that omits `blocks:` gains `blocks: []`). No information lost.
- **lossy** — a non-schema key or a YAML comment carrying data was dropped. True
  loss; the dropped keys are named.

## Result on the real tree (34 plans, 71 files)

55 byte-identical, 13 semantic-equal, 1 normalized, **2 lossy**. The 2 lossy
files dropped non-schema top-level keys (`coherence_note`, `spec_ref`,
plan-level `notes`) that the canonical PLAN/TASKS schema cannot hold. See the
findings section in the PR description / report.
