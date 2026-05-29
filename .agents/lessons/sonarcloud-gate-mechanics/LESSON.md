# Lesson: SonarCloud free-tier gate mechanics + fixing gotchas

A cluster of SonarCloud behaviors that repeatedly cost cycles this session.
Internalize them before touching anything Sonar-gated.

## 1. Free tier = no branch analysis → local scanner evaluates as **main**

SonarCloud branch analysis (`sonar.branch.name`) is a **paid** feature. On this
free-tier project it errors `Project not found` / `branch ... not found`. So:

- The local pre-push `sonar-scanner` (scripts/precommit-mandate.sh) runs with NO
  `-Dsonar.branch.name` and analyzes the **working tree as the main branch**,
  evaluating against **main's** quality gate.
- You therefore CANNOT branch-scope the local gate. The only way to stop a
  branch inheriting a red gate is to **keep main green**.
- The MCP `get_component_measures` / `get_project_quality_gate_status` (no
  `pullRequest`) reflect main's last analysis — which the local push overwrites.
- CI (`SonarSource/sonarqube-scan-action`) does proper PR analysis (base↔head
  diff) — that's the authoritative per-PR gate. PR analysis IS free.

## 2. Editing a file resurfaces its OLD issues as NEW-code

SonarCloud attributes issues on **changed/shifted lines** to the new-code
period. So adding one line to a file makes every pre-existing issue near it
count as "new" and hit `new_*_rating` / `new_security_hotspots_reviewed`. A
tiny edit to a debt-laden file (e.g. tests/Dockerfile.sandbox) can fail the gate
on a dozen "older" findings. Expect to clean the whole file you touch.

## 3. A "fix" can trade one finding for another — verify it nets out

- `ADD --chown=user ...` clears docker:S7026 (curl→ADD) but introduces
  **docker:S6504** ("write permissions on the copied resource"). Drop `--chown`
  (root-owned 0644, still readable) to clear both.
- `wget --https-only` does NOT satisfy shell:S6506's "disable redirects" — add
  `--max-redirect=0` where redirects aren't needed; `curl` wants
  `--proto "=https" --tlsv1.2`.
- `npm install --ignore-scripts` clears S6505 but can break a build whose deps
  have a needed postinstall — confirm the build tolerates it.

## 4. CE "Task finished abnormally" ≠ gate failure — retry it

The containerized scanner on an arm64 host runs an emulated amd64 image; the
SonarCloud Compute-Engine step intermittently fails with `CE Task finished
abnormally with status: FAILED` (~half the time). That is **infra**, not a
quality signal — retry. A genuine `QUALITY GATE STATUS: FAILED` is NOT retried;
surface its conditions. (scripts/precommit-mandate.sh now does both.)

## 5. Hotspots (server-side review) vs issues (fix in code)

`new_security_hotspots_reviewed` must be 100%. Hotspots are cleared by either a
code fix OR marking Reviewed→Safe/Acknowledged via the API (persists across
re-analyses). Issues (`new_violations`) must be fixed or accepted. Prefer code
fixes; review-as-safe only when the construct is intended (e.g. a CLI resolving
user binaries via PATH — go:S4036).

## 6. execabs is deprecated (Go 1.19+) — do not reintroduce

`golang.org/x/sys/execabs` (the CWD/PATH-injection-safe exec wrapper the old git
tooling used) was folded into stdlib `os/exec` in Go 1.19 (`exec.ErrDot`). This
repo is Go 1.26, so `os/exec` already has the guard; adding execabs trades a
hotspot for an SA1019 deprecation. This is the justification for clearing
go:S4036 PATH hotspots as Reviewed→Safe.

## How to apply

Always verify with `git ls-remote` that a push landed (a piped exit code lies),
keep main's gate at zero (review/fix promptly), and after touching a Sonar-gated
file re-query `search_sonar_issues_in_projects` / `search_security_hotspots`
(pullRequest=N) to confirm you didn't resurface or trade findings. Sibling of
[[ssh-keepalive-for-slow-pre-push-hook]] (same pre-push pipeline).
