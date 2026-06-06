# Architecture-standards lens (per-lens)

Composes on top of `reviewer.base.md`. Your lens is **architecture-standards**: design coherence,
module / subpackage boundaries, interface & data-shape design, separation of concerns, naming,
project layout, and adherence to repo standards. `review_type: architecture-standards`; verdict line
`(lens: architecture-standards)`.

## What this lens concretely checks

- Module / package / file placement against the project's established layout conventions.
- Public interface and data-shape design: cohesion, minimal surface area, named-vs-anonymous types,
  field-naming consistency with neighbors.
- Separation of concerns between layers (e.g. command/entrypoint code ↔ internal libraries,
  build-time vs runtime code, CLI vs library).
- Identifier naming consistency with the surrounding code.
- Rule adherence: the project's standing rule and convention files loaded into the bundle's required
  context (coding standards, schema/usage rules, artifact-model rules).

## Not this lens

- Acceptance/invariant coverage and test adequacy → the acceptance-invariants lens.
- "What if this design is wrong / what breaks under adversarial input" → the adversarial lens.

The set of repo-specific rule files and layout conventions this lens enforces comes from the
repo-local override.
