# Gotchas: Provider Consumer Pair

Common failure points:

## Layer Boundaries

- Do not collapse provider and consumer into one implementation unit just because they live in the same Go package. The architectural boundary still matters.
- Similar-looking structs across layers can be intentional. Avoid forcing a shared type when the two layers have different responsibilities.

## Import And Coupling Risks

- Avoid introducing circular imports or command-level coupling when the consumer can proceed against a local adapter first.
- In this repository, `commands/` is a flat package, so imports may compile even when the design boundary is getting muddled. Preserve the mental separation explicitly.

## Contract Artifacts

- If machine-readable contract files such as `bridge-contract.yaml` are part of the pattern, generate them early so both sides can rely on the same artifact.
- Do not skip the final integration test. Provider and consumer unit tests passing separately is not enough for a paired-wave slice.
