# UI-e2e verifier — dot-agents repo overlay (web app_type)

Composes after `verifiers/verifier.base.md` (the contract) and `verifiers/ui-e2e.md` (the kind) when
present; in this repo those upstream layers resolve empty, so this overlay is **self-sufficient** and
carries the full verifier contract on its own.

## Role

Drive the **built** UI in a real browser (Playwright) and prove the user-visible flows work
end-to-end against the running app — not the dev source, not a mock. This is the top of the web
verifier sequence (`unit` → `integration` → `ui-e2e`).

## How to run

1. **Build + serve the real artifact:** run the production build, then serve it (or start the app)
   and wait for readiness. Never point the browser at an unbuilt dev tree.
2. **Drive the flows:** with Playwright, exercise each acceptance flow from the task/spec — load,
   auth/login, the primary happy-path interaction(s), and at least one error/empty state. Assert on
   **user-visible truth**: rendered text/roles, URL after navigation, network/response success,
   element state — via role/label locators (`getByRole`, `getByLabel`), not brittle CSS/xpath.

## RAISED DELIVERY BAR — proof-of-delivery is MANDATORY (per spec)

A pass **MUST** produce and persist proof artifacts; a run without them is `--status partial` at best.

- **Proof directory (coordination ref):** `.agents/worktrees/_state/active/proof/<task_id>/`. Create
  it and write all artifacts there so the gate stage can attach/link them in the PR body.
- **Screenshots — one per KEY step:** take an explicit shot at each user-visible milestone:
  `await page.screenshot({ path: '.agents/worktrees/_state/active/proof/<task_id>/step-01-<name>.png' })`
  (e.g. `step-01-landing`, `step-02-login`, `step-03-submit`, `step-04-result`). Full-page for state
  proof.
- **Video — full run, always on:** record the entire session, not only failures:
  ```
  const context = await browser.newContext({
    recordVideo: { dir: '.agents/worktrees/_state/active/proof/<task_id>/video' },
  });
  // ...run the flow...
  await context.close();            // finalizes the .webm
  const video = '.agents/worktrees/_state/active/proof/<task_id>/video/' + /* page.video().path() basename */;
  ```
  Playwright config equivalent: `use: { video: 'on', screenshot: 'on', trace: 'on' }` with
  `outputDir` set to the proof dir. `video: 'on'` (not `retain-on-failure`) — proof of success is required.

## Record — link the proof in the verify record

```
da workflow verify record --kind custom --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type ui-e2e \
  --command "playwright test (built UI, video:on, per-step screenshots)" \
  --summary "<flows exercised + what each asserted; first failure; PROOF: screenshots .agents/worktrees/_state/active/proof/<task_id>/step-*.png ; video .../video/*.webm>"
```

`--kind custom` (the real surface accepts only `test|lint|build|format|custom|review` — a browser run
is not `go test`); the `ui-e2e` identity rides `--verifier-type ui-e2e`, writing the typed
`ui-e2e.result.yaml`. There is no `--artifact-path` flag, so **enumerate every proof path in
`--summary`** — the gate stage parses them out to link in the PR (proof-of-delivery). A run that
passes the flow but produces no screenshots + video did not meet the bar: record `--status partial`
and say the proof is missing.
