---
schema_version: 1
task_id: t08-move-status
parent_plan_id: root-command-decomposition
title: Move status command (incl. linkcount build-tagged helpers) into commands/lifecycle/
summary: |-
    Moved commands/status.go (1500+ LOC) and the build-tagged
    linkcount_{unix,windows}.go helpers into commands/lifecycle/. Root files
    are now thin shims (linkcount delegates to lifecycle.HasMultipleHardLinks;
    status.go wires NewStatusCmd via lifecycle.NewStatusCmd with the JSON
    flag threaded as a func() bool closure so lifecycle stays import-cycle
    free).

    Per SHAPE.md OD-2: HasMultipleHardLinks is exported during the t08->t09
    window for doctor.go (and add.go) consumers; managedLinkBroken /
    resolveLinkDest were duplicated from doctor.go into lifecycle/status.go;
    printAudit / printSymlinkDirAudit / countClaudeRules / RunStatus /
    StatusConfigLoader are exported so out-of-scope doctor.go and seams_test.go
    keep resolving. All exports collapse back in t09/t11.

    Closure-based JSON seam needed because lifecycle.GlobalFlags (t02) lacks
    JSON and deps.go is outside t08 write scope. The root shim re-binds RunE
    in the commands package so internal/globalflagcov (which only loads
    ./commands) can still resolve the Flags.JSON handler read.
files_changed: []
verification_result:
    status: pass
    summary: |-
        Open PR; do not auto-merge - coordinate with t09 (depends on this) and
        t04/t03/t05/t06/t07 (parallel siblings under root-command-decomposition).
        t09 owner should pick up the export reversal: collapse the
        managedLinkBroken/resolveLinkDest duplicates, lowercase HasMultipleHardLinks
        and the four lifecycle helper exports (PrintAudit, PrintSymlinkDirAudit,
        CountClaudeRules, RunStatus, StatusConfigLoader). After t13 strips the
        root shims, internal/globalflagcov/static.go's loadCommandPackages call
        needs ./commands/lifecycle added to its load set (tracked as a follow-up
        in the t13 PR description per the t08 NewStatusCmd doc comment).
integration_notes: |-
    Open PR; do not auto-merge - coordinate with t09 (depends on this) and
    t04/t03/t05/t06/t07 (parallel siblings under root-command-decomposition).
    t09 owner should pick up the export reversal: collapse the
    managedLinkBroken/resolveLinkDest duplicates, lowercase HasMultipleHardLinks
    and the four lifecycle helper exports (PrintAudit, PrintSymlinkDirAudit,
    CountClaudeRules, RunStatus, StatusConfigLoader). After t13 strips the
    root shims, internal/globalflagcov/static.go's loadCommandPackages call
    needs ./commands/lifecycle added to its load set (tracked as a follow-up
    in the t13 PR description per the t08 NewStatusCmd doc comment).
created_at: "2026-05-25T14:26:30Z"
---

## Summary

Moved commands/status.go (1500+ LOC) and the build-tagged
linkcount_{unix,windows}.go helpers into commands/lifecycle/. Root files
are now thin shims (linkcount delegates to lifecycle.HasMultipleHardLinks;
status.go wires NewStatusCmd via lifecycle.NewStatusCmd with the JSON
flag threaded as a func() bool closure so lifecycle stays import-cycle
free).

Per SHAPE.md OD-2: HasMultipleHardLinks is exported during the t08->t09
window for doctor.go (and add.go) consumers; managedLinkBroken /
resolveLinkDest were duplicated from doctor.go into lifecycle/status.go;
printAudit / printSymlinkDirAudit / countClaudeRules / RunStatus /
StatusConfigLoader are exported so out-of-scope doctor.go and seams_test.go
keep resolving. All exports collapse back in t09/t11.

Closure-based JSON seam needed because lifecycle.GlobalFlags (t02) lacks
JSON and deps.go is outside t08 write scope. The root shim re-binds RunE
in the commands package so internal/globalflagcov (which only loads
./commands) can still resolve the Flags.JSON handler read.

## Integration Notes

Open PR; do not auto-merge - coordinate with t09 (depends on this) and
t04/t03/t05/t06/t07 (parallel siblings under root-command-decomposition).
t09 owner should pick up the export reversal: collapse the
managedLinkBroken/resolveLinkDest duplicates, lowercase HasMultipleHardLinks
and the four lifecycle helper exports (PrintAudit, PrintSymlinkDirAudit,
CountClaudeRules, RunStatus, StatusConfigLoader). After t13 strips the
root shims, internal/globalflagcov/static.go's loadCommandPackages call
needs ./commands/lifecycle added to its load set (tracked as a follow-up
in the t13 PR description per the t08 NewStatusCmd doc comment).
