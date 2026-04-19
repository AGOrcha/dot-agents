# CI Smoke Suite Hardening

Status: Active

## Outcome

Turn CI from a mostly compile-and-basic-smoke gate into a layered verification pipeline that:

- validates the built CLI binary instead of launcher shims
- runs with isolated `HOME` and `AGENTS_HOME`
- covers meaningful workflow, KG, and resource-lifecycle command paths
- validates release packaging before release time
- keeps heavier environment-sensitive integration checks in an intentional lane

## Audited Gap

The current workflows and smoke scripts prove some baseline behavior, but they still leave major gaps around:

- formatting, vet, and build hygiene
- built-binary authenticity
- runner-state isolation
- broader CLI surface coverage
- workflow and KG smoke confidence
- release and PR parity

## Scope

This plan turns the older Markdown-only hardening note into a canonical executable bundle. The first waves should:

1. establish the baseline built-binary and toolchain checks
2. isolate smoke environments
3. expand command-surface smokes for workflow, KG, and resource lifecycle
4. align release packaging checks with PR CI
5. explicitly place heavy environment-sensitive tests into a separate lane instead of leaving them as forgotten debt

## Exit Condition

The plan is complete when the repo’s main CI paths validate the shipped binary in isolated homes, cover the highest-risk command families, and catch packaging drift before `auto-release.yml` is the first place it fails.
