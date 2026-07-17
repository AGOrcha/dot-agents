---
schema_version: 1
task_id: t3-lock-and-wire-pass2
parent_plan_id: package-artifact-install
title: Record kind:artifact lock unit and wire packages pass-2 into install/refresh
summary: |-
    ROUND-2 REWORK. Re-review confirmed the content-digest integrity core sound
    (all refs, offline, add/delete-sensitive) and found three more defects. All
    fixed with fail-before-fix tests; the tracked residual is documented, not
    fixed.

    #1 BLOCKER — config verify integrity bypass. unitDigestMismatchOnly required
    the staleness reasons to be EXACTLY [ReasonUnitDigest], so a tamper co-occurring
    with any inputs/declared-set drift downgraded to a warn (OK=true). FIX:
    reasonsContainUnitDigest — a unit-digest mismatch is ALWAYS a hard FAIL; the
    co-occurring drift is reported in the detail string. Test:
    TestVerifyStaleness_TamperPlusInputsDriftStillFails asserts BOTH reasons fire
    then FAIL.

    #2 HIGH — destructive refresh on fetch failure. On a hydration error refresh
    still called exact ProjectResolvedUnits with the EMPTY set, whose one-to-zero
    prune deleted the project's installed package links — a transient fetch failure
    destroyed installed packages. FIX: refreshOneProject skips shared-target
    projection entirely on a hydration error (leaving all prior links intact; the
    additive per-platform CreateLinks still runs). install already returns on error
    so it was never affected. Test:
    TestRunRefresh_PackageFetchFailureLeavesExistingLinksIntact installs a package,
    breaks the source, refreshes, asserts the link survives.

    #3 HIGH — lost-update window. Both pass-1 (resolver.writeUnitsLock) and pass-2
    (commitArtifactLock) read-modified-wrote the shared "units" section with the
    read OUTSIDE the flush lock, so a concurrent write between read and flush was
    overwritten by a stale whole-section snapshot. FIX: new agentslock.Update — a
    serialized read-modify-write that holds the advisory lock across open-read +
    mutate + atomic-write. Flush was refactored to share the post-acquire
    writeLocked body (byte-for-byte equivalent; no behavior change). Both pass-1 and
    pass-2 now do their units RMW under Update. Tests: TestUpdate_* (agentslock
    concurrency + abort-on-error) and
    TestCommitArtifactLock_InterleavedWithPass1PreservesBothKeys (real pass-2
    interleaved with a pass-1-shaped writer; race-clean; both keys survive).

    WHY agentslock.Update (scope note): a correct cross-process serialized RMW
    cannot be built from the existing Open/Flush surface — Flush re-acquires the
    advisory lock, so a caller cannot hold it across the read. Update is a new
    ADDITIVE primitive in internal/agentslock (outside the task write_scope, same
    category as the earlier sanctioned materialize.go export); no existing
    agentslock behavior changed. Flagging for review-lens scrutiny given it lands
    outside the original grant.

    TRACKED, NOT FIXED THIS ROUND (per the coordinator): (a) the residual
    verify→link TOCTOU — a same-user process replacing a 0444 file in the writable
    CAS dir between verifyProjectionInputs and the symlink; (b) the orphaned first
    CAS entry on a mid-fetch failure (materialize succeeds for pkg 1, pkg 2 fails →
    pkg 1's store entry is left un-referenced; harmless under the permanent CAS
    gitignore + deferred GC, but not cleaned). Both need invocation-time
    verification or truly-immutable storage — out of scope here.

    Verification: full go test ./... green; -race green on agentslock/config/
    lifecycle/commands-config; gofmt + vet + fsguard clean.
files_changed:
    - .agents/active/delegation/t3-lock-and-wire-pass2.yaml
    - .agents/active/iteration-log/iter-3.yaml
    - .agents/active/verification/t3-lock-and-wire-pass2/unit.result.yaml
verification_result:
    status: pass
    summary: ""
integration_notes: ""
created_at: "2026-07-16T00:19:05Z"
---

## Summary

ROUND-2 REWORK. Re-review confirmed the content-digest integrity core sound
(all refs, offline, add/delete-sensitive) and found three more defects. All
fixed with fail-before-fix tests; the tracked residual is documented, not
fixed.

#1 BLOCKER — config verify integrity bypass. unitDigestMismatchOnly required
the staleness reasons to be EXACTLY [ReasonUnitDigest], so a tamper co-occurring
with any inputs/declared-set drift downgraded to a warn (OK=true). FIX:
reasonsContainUnitDigest — a unit-digest mismatch is ALWAYS a hard FAIL; the
co-occurring drift is reported in the detail string. Test:
TestVerifyStaleness_TamperPlusInputsDriftStillFails asserts BOTH reasons fire
then FAIL.

#2 HIGH — destructive refresh on fetch failure. On a hydration error refresh
still called exact ProjectResolvedUnits with the EMPTY set, whose one-to-zero
prune deleted the project's installed package links — a transient fetch failure
destroyed installed packages. FIX: refreshOneProject skips shared-target
projection entirely on a hydration error (leaving all prior links intact; the
additive per-platform CreateLinks still runs). install already returns on error
so it was never affected. Test:
TestRunRefresh_PackageFetchFailureLeavesExistingLinksIntact installs a package,
breaks the source, refreshes, asserts the link survives.

#3 HIGH — lost-update window. Both pass-1 (resolver.writeUnitsLock) and pass-2
(commitArtifactLock) read-modified-wrote the shared "units" section with the
read OUTSIDE the flush lock, so a concurrent write between read and flush was
overwritten by a stale whole-section snapshot. FIX: new agentslock.Update — a
serialized read-modify-write that holds the advisory lock across open-read +
mutate + atomic-write. Flush was refactored to share the post-acquire
writeLocked body (byte-for-byte equivalent; no behavior change). Both pass-1 and
pass-2 now do their units RMW under Update. Tests: TestUpdate_* (agentslock
concurrency + abort-on-error) and
TestCommitArtifactLock_InterleavedWithPass1PreservesBothKeys (real pass-2
interleaved with a pass-1-shaped writer; race-clean; both keys survive).

WHY agentslock.Update (scope note): a correct cross-process serialized RMW
cannot be built from the existing Open/Flush surface — Flush re-acquires the
advisory lock, so a caller cannot hold it across the read. Update is a new
ADDITIVE primitive in internal/agentslock (outside the task write_scope, same
category as the earlier sanctioned materialize.go export); no existing
agentslock behavior changed. Flagging for review-lens scrutiny given it lands
outside the original grant.

TRACKED, NOT FIXED THIS ROUND (per the coordinator): (a) the residual
verify→link TOCTOU — a same-user process replacing a 0444 file in the writable
CAS dir between verifyProjectionInputs and the symlink; (b) the orphaned first
CAS entry on a mid-fetch failure (materialize succeeds for pkg 1, pkg 2 fails →
pkg 1's store entry is left un-referenced; harmless under the permanent CAS
gitignore + deferred GC, but not cleaned). Both need invocation-time
verification or truly-immutable storage — out of scope here.

Verification: full go test ./... green; -race green on agentslock/config/
lifecycle/commands-config; gofmt + vet + fsguard clean.

## Integration Notes


