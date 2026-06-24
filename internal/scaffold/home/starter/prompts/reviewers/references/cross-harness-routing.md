# Cross-harness routing contract

Reference for the `cross-harness-adversarial` reviewer profile
(`reviewers/cross-harness-adversarial.md` + repo overlay
`reviewers/cross-harness-adversarial.project.md`). Captures the two load-bearing
invariants so they survive edits to the prose.

## Principle: active platforms, not my-setup

The reviewer routes the adversarial lens to a **harness discovered on the host**, never to a
hardcoded engine. The same discipline the project applies to platform detection
(`internal/platform/cliprobe.go` — bounded `exec.LookPath` per candidate, each platform's
`IsInstalled`) applies here: *what is installed on THIS machine* decides the reviewer, so the profile
works for any contributor, not just the author. A profile that names one developer's tool as "the
reviewer" is broken by definition.

## The routing contract

```
running_engine  := the harness hosting this session   (excluded from selection)
available       := { agent CLI in {claude,cursor,codex,opencode,copilot} : binary resolves on PATH }
candidates      := available \ { running_engine }
review_engine   := first(candidates, preference codex>cursor>opencode>copilot>claude)   if candidates ≠ ∅
                   SKIP (graceful, non-blocking)                                        if candidates = ∅
```

- **Detect** the running engine from its env marker (`CLAUDECODE`, `CURSOR_SESSION_ID`,
  `CODEX_SESSION_ID`, `OPENCODE_SESSION_ID`).
- **Select** a *different* brain. One alternate is enough; the value is the disagreement, not a quorum.
- **Dispatch** a read-only adversarial brief via the alternate's headless mode (`codex exec`,
  `cursor agent --print`, `opencode run`, `copilot -p`, `claude --print`).
- **Route findings** via output-to-PR-comment: `gh pr comment <pr> --body-file <file>`, body prefixed
  `engine: <review_engine> (host: <running_engine>)`. No PR ⇒ write to
  `.agents/active/review/<task_id>-cross-harness-adversarial.md`.
- **Reconcile** in the running orchestrator/verifier — confirm the alternate's findings against the
  diff before adopting them; a second brain can also be wrong.

## Graceful degradation

No alternate harness present ⇒ emit one LOW skip note and a `pass` verdict tagged
`[SKIPPED: no alternate harness]`. Never hard-fail, and never silently self-review under the
cross-harness label — that would erase the "different brain" guarantee the profile exists to provide.

## stage_profiles.reviewer registration (coordinated step — NOT done here)

This profile is the prompt files only. To make it dispatchable as a lens, a coordinated change must
add it to `stage_profiles.reviewer` in `.agentsrc.json`, mirroring the existing `adversarial` entry:

```jsonc
"cross-harness-adversarial": {
  "label": "Cross-harness adversarial lens",
  "prompt_files": [
    "reviewers/reviewer.base.md",
    "reviewers/cross-harness-adversarial.md",
    "reviewers/cross-harness-adversarial.project.md"
  ]
}
```

The lens may then be referenced from a `stage_profiles.review` list (alongside or in place of
`adversarial`) for the staged-runtime review stage.
