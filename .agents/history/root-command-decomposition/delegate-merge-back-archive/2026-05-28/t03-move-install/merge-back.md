---
schema_version: 1
task_id: t03-move-install
parent_plan_id: root-command-decomposition
title: Move install command into commands/lifecycle/
summary: |-
    Moved install command to commands/lifecycle/install.go per t03.

    commands/lifecycle/install.go holds the full ~600-line implementation with
    exported entry points (NewInstallCmd, RunInstall, RunInstallGenerate,
    RegisterInstallProject, FindProjectByPath, LinkResourceFromSources,
    CloneGitSource, ShouldUseCachedGitSource, InstallDeps, StdInstallDeps).

    commands/lifecycle/deps.go extends GlobalFlags with DryRun/Force/Verbose
    and adds package-var mirrors (Flags, Version, Commit, Describe,
    ErrorWithHintsFn) per t01 SHAPE.md decision to preserve package-var seams.

    commands/install.go is now a thin shim: wires NewInstallCmd to lifecycle,
    syncs lifecycle package vars from commands.Flags / Version / etc. on every
    cross-package call, and exposes lower-case forwarders (runInstall,
    runInstallGenerate, registerInstallProject, findProjectByPath,
    linkResourceFromSources, cloneGitSource, shouldUseCachedGitSource) plus
    type aliases (installDeps = lifecycle.InstallDeps,
    stdInstallDeps = lifecycle.StdInstallDeps) so seams_test.go keeps
    compiling until t11 re-homes the seam tests.

    commands/install_test.go trimmed to fakeInstallDeps (still referenced by
    seams_test.go) and the cobra-construction test. The 1600-line install
    test suite moved to commands/lifecycle/install_test.go with exported
    names.

    No new internal/projectsync or internal/platform imports appear in
    commands/install.go.

    PR: https://github.com/NikashPrakash/dot-agents/pull/79
    Branch: feature/t03-move-install
    Commit: b1f7fd37
    Worktree: .agents/worktrees/t03-move-install
files_changed: []
verification_result:
    status: pass
    summary: |-
        Ready for parent review. PR open at #79 (do not merge per bundle).

        Verification: go build ./... + go vet ./... + gofmt -l . + go test ./...
        -race -count=1 -timeout 300s all green except TestCRGBridgeFreshBuildRealCRG
        in internal/graphstore. That test fails on fresh worktrees because they
        lack the .venv shipping with the primary checkout — pre-existing
        environmental issue, unrelated to this change (confirmed by running the
        same test on master where it passes, and by re-running graphstore with
        -skip TestCRGBridgeFreshBuildRealCRG where it passes).

        Follow-up tasks already on the plan:
        - t11: split seams_test.go so install-specific seam tests move to
          commands/lifecycle/ — at that point the lower-case forwarders in
          commands/install.go become dead.
        - t13: delete commands/install.go entirely and switch commands/root.go
          to import lifecycle.NewInstallCmd directly.
integration_notes: |-
    Ready for parent review. PR open at #79 (do not merge per bundle).

    Verification: go build ./... + go vet ./... + gofmt -l . + go test ./...
    -race -count=1 -timeout 300s all green except TestCRGBridgeFreshBuildRealCRG
    in internal/graphstore. That test fails on fresh worktrees because they
    lack the .venv shipping with the primary checkout — pre-existing
    environmental issue, unrelated to this change (confirmed by running the
    same test on master where it passes, and by re-running graphstore with
    -skip TestCRGBridgeFreshBuildRealCRG where it passes).

    Follow-up tasks already on the plan:
    - t11: split seams_test.go so install-specific seam tests move to
      commands/lifecycle/ — at that point the lower-case forwarders in
      commands/install.go become dead.
    - t13: delete commands/install.go entirely and switch commands/root.go
      to import lifecycle.NewInstallCmd directly.
created_at: "2026-05-25T14:18:25Z"
---

## Summary

Moved install command to commands/lifecycle/install.go per t03.

commands/lifecycle/install.go holds the full ~600-line implementation with
exported entry points (NewInstallCmd, RunInstall, RunInstallGenerate,
RegisterInstallProject, FindProjectByPath, LinkResourceFromSources,
CloneGitSource, ShouldUseCachedGitSource, InstallDeps, StdInstallDeps).

commands/lifecycle/deps.go extends GlobalFlags with DryRun/Force/Verbose
and adds package-var mirrors (Flags, Version, Commit, Describe,
ErrorWithHintsFn) per t01 SHAPE.md decision to preserve package-var seams.

commands/install.go is now a thin shim: wires NewInstallCmd to lifecycle,
syncs lifecycle package vars from commands.Flags / Version / etc. on every
cross-package call, and exposes lower-case forwarders (runInstall,
runInstallGenerate, registerInstallProject, findProjectByPath,
linkResourceFromSources, cloneGitSource, shouldUseCachedGitSource) plus
type aliases (installDeps = lifecycle.InstallDeps,
stdInstallDeps = lifecycle.StdInstallDeps) so seams_test.go keeps
compiling until t11 re-homes the seam tests.

commands/install_test.go trimmed to fakeInstallDeps (still referenced by
seams_test.go) and the cobra-construction test. The 1600-line install
test suite moved to commands/lifecycle/install_test.go with exported
names.

No new internal/projectsync or internal/platform imports appear in
commands/install.go.

PR: https://github.com/NikashPrakash/dot-agents/pull/79
Branch: feature/t03-move-install
Commit: b1f7fd37
Worktree: .agents/worktrees/t03-move-install

## Integration Notes

Ready for parent review. PR open at #79 (do not merge per bundle).

Verification: go build ./... + go vet ./... + gofmt -l . + go test ./...
-race -count=1 -timeout 300s all green except TestCRGBridgeFreshBuildRealCRG
in internal/graphstore. That test fails on fresh worktrees because they
lack the .venv shipping with the primary checkout — pre-existing
environmental issue, unrelated to this change (confirmed by running the
same test on master where it passes, and by re-running graphstore with
-skip TestCRGBridgeFreshBuildRealCRG where it passes).

Follow-up tasks already on the plan:
- t11: split seams_test.go so install-specific seam tests move to
  commands/lifecycle/ — at that point the lower-case forwarders in
  commands/install.go become dead.
- t13: delete commands/install.go entirely and switch commands/root.go
  to import lifecycle.NewInstallCmd directly.
