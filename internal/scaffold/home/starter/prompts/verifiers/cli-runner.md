# CLI-runner verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **built-binary CLI smoke verification**: prove
the wired-up command-line binary actually runs and returns the **right content**, not just that the
code compiles in tests and exits `0`. `--kind custom`, `--verifier-type cli-runner`.

This is the end-to-end half of a CLI verifier sequence: a `unit` pass proves the code is correct;
this pass proves the **assembled binary** runs. It is a pre-merge verifier, not a post-merge PR
watcher.

## What to run

1. **Build fresh (always).** Build the CLI binary from the tree. A build failure is a terminal
   `--status fail` — the tree does not produce a working binary; record and stop.
2. **Smoke floor.** Run the project's CLI smoke harness (the script that exercises `--version` /
   `--help`, core subcommands, the live config + knowledge surfaces, dry-runs, and expected-failure
   cases). The floor must **assert on returned content** — a stable output substring or a `--json`
   field value — not merely a zero exit, so a command that runs but returns the wrong thing fails.
   Any failure here fails the pass even if the touched command works — a change must not regress a
   sibling command.
3. **Task-scoped invocations.** For each touched path that maps to a CLI command, exercise that
   command end-to-end against the freshly built binary:
   - **Positive:** the happy-path invocation exits `0` **and emits the expected output** (assert a
     stable substring or a `--json` field, not the whole stream).
   - **Negative:** where the change adds a failure mode, run the invalid invocation and assert a
     non-zero exit and a clear error message.

If the build or the smoke floor fails, you may skip the scoped invocations but must record
`--status fail` and explain.

## Record

`--command` is single-valued: pass the build/smoke entry line, and carry the full invocation story
(every command run, what each asserted, the first failure) in `--summary`.

```
da workflow verify record --kind custom --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type cli-runner \
  --command "<build + smoke entry line>" \
  --summary "<what built/ran, the content each invocation asserted, first failure, evidence term>"
```

A binary that builds and smokes clean but is missing the task's intended command — or returns the
wrong content for it — is `missing-feature`, not `ok`. The concrete build line, smoke-harness path,
content assertions, and the touched-path → command matrix come from the repo-local override.
