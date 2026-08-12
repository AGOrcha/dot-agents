---
title: CLI Help Conventions
description: How to write da command help so that both humans and AI agents can operate the CLI from --help alone.
sidebar:
  order: 4
---

# CLI help conventions (dot-agents CLI)

**Status:** Contract, adopted with the agent-friendly-help pass.
**Scope:** How to author `Short`, `Long`, `Example`, and flag usage strings for
any command under `da`. Sibling contracts:
[Error Message Contract](ERROR_MESSAGE_CONTRACT.md) (failure wording) and
[Global Flag Contract](GLOBAL_FLAG_CONTRACT.md) (persistent root flags).

## Why this exists

This CLI is driven by AI agents at least as often as by people, and an agent
reaches for `--help` before it reaches for a doc, a rule file, or a skill. That
makes help text a **first-line source of truth**, not a courtesy summary.

The practical consequence: anything an agent must know to compose a correct
invocation belongs in `--help`. If it lives only in `docs/`, in a repo rule, or
in a skill's prose, then every consumer has to carry a private copy of it — and
those copies drift from the binary the moment a flag changes. Help text that is
complete lets the surrounding rules and skills get *shorter*, which is the point:
progressive disclosure, with the binary as the authority.

Two mechanisms carry most of the load, in this order:

1. **Flag design** — the value listing, the flag's name, and the wording of its
   rejection message. This is where an agent learns what is legal.
2. **Examples** — short, real invocations that show which flags travel together.
   Secondary to (1), and deliberately terse.

## Rule 1 — a closed-set flag declares its values once, via the helper

Any flag with a finite vocabulary must be registered through
[`cmdutil.RegisterEnum`](../commands/internal/cmdutil/enumflag.go) (or
`RegisterEnumFlag` for commands that read flags back off the command rather than
binding them to a variable):

```go
var verifyKindEnum = cmdutil.EnumSpec{
    Name:     "kind",
    Usage:    "Class of verification being recorded",
    Values:   []string{"test", "lint", "build", "format", "custom", "review"},
    Required: true,
    Note:     "review is the odd member: it takes --phase1-decision/--phase2-decision instead of --status",
}

cmdutil.RegisterEnum(verifyRecordCmd, &verifyKind, verifyKindEnum)
```

One `Values` slice then drives three consumers:

| Consumer | What it produces |
|---|---|
| `--help` | `Class of verification being recorded (one of: test\|lint\|build\|format\|custom\|review) (required)` |
| shell completion | the same candidates, `NoFileComp` |
| validation | `--kind must be one of test\|lint\|build\|format\|custom\|review (got "unit")` |

Because all three read one declaration, help can never advertise a value that
validation rejects, nor omit one it accepts. **Do not hand-write a value listing
into a usage string** — a hand-written list is a second copy, and the second
copy is the one that goes stale.

Corollaries:

- **Do not re-list the vocabulary in the runner.** If a runner needs the same
  set, call `spec.Contains(v)` or `spec.Validate(v)`. `EnumSpec` is the single
  source; `validTaskStatusList`, `validQueryIntents`, and
  `platform.SupportedPipelinePlatforms()` are examples of vocabularies that the
  specs read from rather than duplicate.
- **`Default` must be a member of `Values`.** `RegisterEnum` panics otherwise;
  that is a command-definition bug, caught by any test that builds the tree.
- **Absence is not a wrong value.** `Validate` accepts the empty string, so
  required-ness stays with `MarkFlagRequired` or the runner's own check, which
  can explain *why* the flag is needed. `Required: true` on the spec only adds
  the `(required)` marker to the help text.
- **No backticks in `Usage` or `Note`.** Cobra reads the first backtick-quoted
  word in a usage string as the flag's value-type name, so a stray backtick
  turns `--kind string` into `--kind review`. `UsageString` strips them, but
  write them out of the source anyway.

### Config-derived vocabularies

Some sets are not compiled in — `app_type` comes from
`execution_profile.by_app_type` in `.agentsrc.json`, and stage keys come from
`stage_profiles`. Listing values there would be wrong for the next repo. Use
`DynamicFrom` to name the command that prints the live set:

```go
var taskAppTypeEnum = cmdutil.EnumSpec{
    Name:        "app-type",
    Usage:       "App type that selects this task's verifier sequence",
    DynamicFrom: "da workflow app-types",
}
// renders: App type that selects this task's verifier sequence (values come from: da workflow app-types)
```

Such a spec validates nothing (the binary cannot adjudicate membership) and
registers no completions (an empty candidate list is worse than none). What it
must never do is leave the reader at a dead end: **"see the docs" is not an
acceptable pointer — name a command that prints the answer.**

## Rule 2 — every command an agent drives ships an `Example` block

`commands/help_conventions_test.go` pins the list of agent-driven command paths
and fails when one loses its examples. Add to that list when a new command joins
the loop's critical path.

Examples are help text, not a tutorial. Keep to 1–3 lines, use real plan/task
identifiers rather than `<placeholders>` where a concrete value reads more
clearly, and prefer showing **which flags travel together** over showing every
flag:

```go
Example: deps.ExampleBlock(
    "  # direct work: record the suite you ran",
    "  da workflow verify record --kind test --status pass --command \"go test ./...\" --summary \"all packages passed\"",
    "  # delegated work: task-scoped, writes the typed artifact the contract gates on",
    "  da workflow verify record --kind test --status pass --task t1 --verifier-type unit --summary \"go test ./...\"",
),
```

A comment line above an example is worth it when the same command has two
distinct operating modes (direct vs delegated, dry-run vs apply). Otherwise skip
it.

## Rule 3 — `Long` carries the operational knowledge, not the flag list

Cobra already prints every flag. `Long` is for what the flag list cannot say:

- which commands must run **before** or **after** this one,
- which of two modes a caller is in and how that changes the required flags,
- constraints beyond membership (a state machine, a mutual exclusion),
- what the command writes, when that is not obvious from the name.

`da workflow advance` is the reference example: its `Long` gives the status
vocabulary with a one-line gloss per member, states that the transition graph
constrains it further, and says explicitly that a delegated worker calls
`merge-back` instead. That is knowledge that previously lived only in skills.

Do **not** put in `Long`: a restatement of the flag usage strings, a changelog,
a design rationale, or a link with no inline answer.

## Rule 4 — write for any agent runtime, and for humans

Help text is read by agents from several model families and by people at a
terminal. Keep it neutral:

- No assumptions about a particular agent harness, model, or tool-calling
  convention. Naming a platform as *data* is fine (`--platform claude|codex|copilot|cursor`
  is a real vocabulary); addressing one in prose is not.
- No second person aimed at a specific runtime ("if you are an agent, …").
- Plain ASCII flag syntax that copies and pastes.

## Checklist for a new command

- [ ] Every closed-set flag registered via `cmdutil.RegisterEnum` / `RegisterEnumFlag`.
- [ ] Every config-derived flag carries `DynamicFrom` naming a real command.
- [ ] No hand-written value listing in any usage string.
- [ ] No backticks in `Usage` / `Note`.
- [ ] `Example` block present, 1–3 lines, real values.
- [ ] `Long` present when the command has ordering constraints or two modes.
- [ ] Added to `agentDrivenCommands` in `commands/help_conventions_test.go` if it
      is on the loop's critical path.
