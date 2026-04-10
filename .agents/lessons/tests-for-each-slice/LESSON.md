# Tests For Each Slice

## Pattern

When implementation work lands in slices, each slice needs its own automated coverage before being treated as done.

## Trigger

- adding a new command group
- adding new scaffolded runtime assets such as shell hooks
- changing behavior that is exercised outside pure Go helpers

## Rule

- Do not close an implementation slice with only manual verification if the slice introduced testable behavior.
- Add focused tests in the same slice for the most important new behavior and failure mode.
- For scaffolded scripts or generated assets, prefer tests that execute the installed artifact rather than only inspecting source text.

## Application Here

The workflow automation work initially added and refined shell hook behavior without automated execution coverage. That gap was corrected by adding direct execution tests for the scaffolded hook scripts.
