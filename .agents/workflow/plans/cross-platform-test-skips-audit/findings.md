# Cross-Platform Test Skip Audit — Findings

Plan: `cross-platform-test-skips-audit`
Task: `catalogue`
Source command:
```
grep -rn 'runtime.GOOS == "windows"' --include="*_test.go"
```
Repo scan root: catalogue worktree (excluding `.agents/`).

## Headline counts

- **Total `runtime.GOOS == "windows"` grep hits:** 97
- **Skip sites (actual `t.Skip`):** 89
- **Parameterizing-only sites (`[not-a-skip]`, no migration):** 7
- One site (`commands/init_test.go:345`) uses `os.Geteuid() == 0 || runtime.GOOS == "windows"` as a guarded early-return helper; classified `[genuine-posix]`, not double-counted.

### Per-classification counts (skip sites only)

| Class | Count | Downstream task |
|---|---:|---|
| `[shortcut-permission]` — file unreadable (helper exists: `testutil.MakeFileUnreadable`) | 3 | `migrate-sites` (immediate) |
| `[shortcut-permission]` — dir unreadable / read-only parent (needs `MakeDirUnreadable`) | 15 | `perms-dir` then `migrate-sites` |
| `[shortcut-mode]` — file read-only via `chmod 0o444` (needs `MakeFileReadOnly`) | 0 | `perms-readonly` ships helper for future use; **no migration sites today** |
| `[shortcut-symlink]` — `os.Symlink` without privilege probe (needs `SymlinkOrSkip`) | 10 | `symlinks` then `migrate-sites` |
| `[genuine-posix]` — POSIX-only semantics; document, don't migrate | 61 | `exec-bit` (1 site is literal exec-bit + provides doc pattern) |
| **Total skip sites** | **89** | |
| `[not-a-skip]` parameterizers | 7 | none |

Migration workload for `migrate-sites`: **3 + 15 + 10 = 28 sites**.

## ACTION ITEM — `fsops-windows-tests` (new task recommended)

`internal/fsops/fsops_windows.go` declares **6** Windows-build-tagged functions and the package has **no `fsops_windows_test.go` file**. `fsops_default_test.go` carries `//go:build !windows`, so Windows coverage of `internal/fsops` is effectively **0%**.

Uncovered Windows-side symbols:

| Symbol | Notes |
|---|---|
| `systemExe` | Returns `<rel>.exe`. Trivial unit test. |
| `MkdirAll` | Wraps `os.MkdirAll`; perm bits a no-op on Windows — assert success paths + already-exists. |
| `mkdirAllComponents` | Multi-segment + already-exists branches. |
| `WriteFile` | Negative path (permission-denied) needs a Windows DACL fixture — exactly what `perms-dir` delivers. |
| `Remove` | Negative path (read-only parent) likewise needs `perms-dir`'s helper. |
| `RemoveAll` | Same. |

**Recommendation:** new task `fsops-windows-tests`:
- `write_scope`: `internal/fsops/fsops_windows_test.go`
- `depends_on`: `perms-dir`
- `verification_required`: true

Without this, `migrate-sites` lifts Windows coverage on consumers of fsops while the package itself stays a coverage-gate blind spot.

## ACTION ITEM (surface only) — io / seam test files lack build tags

Recent fault-injection seam layers (PR #59 landed, #61 / #65 / #67 in flight) introduce production code with Windows-relevant fault shapes. Their `*_test.go` files have no `//go:build` tag, so they run only on the CI host OS and never assert Windows fault semantics:

| Test file | Has build tag? | Production sibling |
|---|---|---|
| `internal/platform/io_test.go` (PR #59) | no | `internal/platform/io.go` |
| `commands/kg/seams_test.go` (PR #61) | no | `commands/kg/seams.go` |
| `commands/skills/seams_test.go` (PR #65) | no | `commands/skills/seams.go` |
| `commands/agents/seams_test.go` (PR #67) | no | `commands/agents/seams.go` |

Not fixing here. Track as follow-up: each io/seam test either (a) needs a `//go:build !windows` guard plus a paired `*_windows_test.go` for Windows-specific fault shapes, or (b) the fake-driven assertions must be explicitly verified OS-agnostic.

## Helper precondition verified

- `internal/testutil/perms.go` exports `MakeFileUnreadable(t, path)` (line 38), POSIX impl in `perms_unix.go` (chmod 0), Windows impl in `perms_windows.go` (UTF-16 + `CreateFile` + `LockFileEx` deny-read).
- `MakeDirUnreadable`, `MakeFileReadOnly`, `SymlinkOrSkip` do not yet exist (deliverables of `perms-dir`, `perms-readonly`, `symlinks`).

---

## Per-site rows

### `[shortcut-permission]` — file unreadability (3 sites)

Migrate to `testutil.MakeFileUnreadable` immediately.

| path:line | Func | Reason |
|---|---|---|
| `commands/workflow/graph_test.go:1009` | `TestReadGraphBridgeHealth_StatErrorPropagates` | "chmod unreadable not portable on windows" |
| `commands/workflow/prefs_test.go:881` | `TestSetLocalPreference_ReadFileNonNotExistError` | "chmod unreadable not portable on windows" |
| `internal/platform/render_manifest_test.go:231` | `TestWriteManagedFile_UnreadableExistingFileBlocksAndPreserves` | "requires POSIX permission semantics" — chmods existing target file unreadable to assert preserve-on-block |

### `[shortcut-permission]` — directory unreadable / read-only parent (15 sites)

Block on `perms-dir` delivering `MakeDirUnreadable`. These use `os.Chmod 0` (or remove `0200`) on a *directory* to deny child create / list / unlink. Windows `os.Chmod` only toggles the read-only attribute and Windows ignores it for directory child operations, so the fault is silently a no-op there today.

| path:line | Func | Reason |
|---|---|---|
| `internal/projectsync/journal_test.go:483` | `TestPromoteJournal_ListPendingReadFileSkip` | "chmod-0 unreadable semantics differ on Windows" |
| `internal/projectsync/journal_test.go:545` | `TestRecoverPendingPromote_CanonicalCopiedRemoveAllError` | "chmod read-only parent semantics differ on Windows" |
| `internal/projectsync/promote_journal_test.go:56` | `TestPromoteResource_RCSaveFailureRollsBackJournal` | "chmod-based read-only project dir not portable to Windows" |
| `internal/projectsync/promote_journal_test.go:100` | `TestMaterializePromoteSource_RemoveSourceFailure` | "chmod-based read-only parent not portable to Windows" |
| `internal/projectsync/promote_journal_test.go:127` | `TestClearExistingCanonical_StaleSymlinkRemoveError` | "chmod parent semantics differ on Windows" |
| `internal/projectsync/promote_journal_test.go:160` | `TestClearExistingCanonical_ForceRealDirRemoveError` | "chmod parent semantics differ on Windows" |
| `internal/projectsync/promote_journal_test.go:204` | `TestMaterializePromoteSource_RollbackCrossFsCopyFails` | "chmod parent semantics differ on Windows" |
| `internal/links/links_test.go:274` | `TestRemoveIfHardlinkedToAny_RemovalFailurePropagates` | "uses POSIX dir-perm to force a removal failure" |
| `internal/platform/resource_plan_test.go:586` | `TestRemoveManagedIntentTarget_DirectFileHardLinkRemovalFailureSurfaces` | "read-only-dir fault injection does not deny deletion on Windows" |
| `commands/add_test.go:1084` | `TestRestoreFromResourcesCounted_StatErrorIsPropagated` | "read-only-dir chmod does not deny stat on Windows" |
| `commands/hooks/remove_test.go:178` | `TestRunHooksRemoveBundleRemoveAllError` | "unix permission model required to block RemoveAll" |
| `commands/hooks/remove_test.go:222` | `TestRunHooksRemoveLegacyRemoveError` | "unix permission model required to block Remove" |
| `commands/workflow/testutil_test.go:951` | `chmodUnreadableDir` (helper) | "chmod unreadable not supported on windows" — also rewrite helper to delegate |
| `commands/workflow/fs_test.go:771` | `TestMergePlanDirCompareAndCopy_ShouldSkipErrorPropagates` | "chmod unreadable not supported on windows" |
| `commands/skills/cmd_test.go:479` | `TestAppendSkillToAgentsRC_SaveError` | "chmod-based read-only dir not portable on Windows" |

### `[shortcut-mode]` — file read-only via `chmod 0o444` (0 sites)

None found in this scan. All "read-only" idioms in the codebase are either read-only *parent dir* (dir-class above) or deny-read on a file (file-class above).

`perms-readonly` should still ship the helper for future use — it's a one-line cross-platform alias (`os.Chmod(path, 0o444)` honored on both OSes via the W bit → `FILE_ATTRIBUTE_READONLY` translation). Its current migration ledger is empty.

### `[shortcut-symlink]` — `os.Symlink` without privilege probe (10 sites)

Block on `symlinks` delivering `SymlinkOrSkip`. Replace `if runtime.GOOS == "windows" { t.Skip(...) }` with `testutil.SymlinkOrSkip(t)` so the test runs on Windows when Developer Mode is enabled.

| path:line | Func | Reason |
|---|---|---|
| `internal/links/symlink_remove_error_test.go:15` | `TestSymlink_DanglingButCorrectIsNoop` | "POSIX symlink primitive; Windows path covered by internal/linktest" |
| `internal/links/symlink_remove_error_test.go:39` | `TestSymlink_RemoveAllErrorBranches` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches_test.go:37` | `TestSymlink_ReplacesStalePointingElsewhere` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches_test.go:103` | `TestIsManagedLink_AbsoluteTargetMatch` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches_test.go:125` | `TestIsManagedLinkUnder_AbsolutePrefixBranch` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches2_test.go:14` | `TestSymlink_AlreadyCorrectIsNoop` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches2_test.go:68` | `TestIsManagedLink_And_Under_AbsoluteBranches` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches2_test.go:148` | `TestIsManagedLinkUnder_RelativeTargetAndNonLink` | "POSIX symlink primitive; …" |
| `internal/links/managed_link_branches2_test.go:181` | `TestHardlinkWithPolicy_OwnedAndEmptyDir` | "POSIX hardlink/symlink primitives" — mixed; verify probe covers both |
| `internal/links/managed_link_branches2_test.go:228` | `TestIsManagedLinkUnder_SiblingPrefixNotContained` | "POSIX symlink primitive; …" |

### `[genuine-posix]` — POSIX-only semantics; document, do not migrate (61 sites)

These assert behavior with no Windows analog: POSIX symlink semantics on file-managed-links (Windows file managed link is a hard link without a reparse point), `/dev/null`, `/usr/bin/{true,false}`, POSIX shell shims (`#!/bin/sh`), POSIX exec bit, ENOTDIR, EISDIR, removing-cwd. Keep skip + document in place; `exec-bit` task delivers the documentation pattern.

| path:line | Func | Reason |
|---|---|---|
| `internal/projectsync/projectsync_test.go:176` | `TestCopyTree` | Windows file managed link = hard link w/o reparse point; symlink-skip contract unobservable |
| `internal/projectsync/promote_test.go:233` | `TestCopyTree_CopiesFilesAndDirsSkipsSymlinks` | same |
| `internal/links/links_test.go:181` | `TestIsSymlinkUnder` | `IsSymlinkUnder` resolves a managed-link target; hard link has no resolvable target |
| `internal/links/links_test.go:368` | `TestRemoveIfSymlinkUnder` | same |
| `internal/links/managed_link_test.go:61` | `TestManagedLinkTarget_ResolvableVsHardlink` | POSIX symlink has resolvable target; Windows hard link doesn't |
| `internal/links/managed_link_test.go:88` | `TestIsManagedLinkUnder` | same as :61 |
| `internal/platform/coverage_gap_test.go:26` | `installFakeCLI` (helper) | fake CLI shim relies on POSIX shell (`#!/bin/sh`) |
| `internal/graphstore/crg_wrappers_test.go:20` | `makeFakeCRGEnv` (helper) | fake shell binaries POSIX-only |
| `internal/graphstore/crg_updatereport_test.go:38` | `TestCRGBridge_UpdateReport_NoMutation_WithFiles` | fake shell binary |
| `internal/graphstore/crg_updatereport_test.go:64` | `TestCRGBridge_UpdateReport_NoMutation_NoFiles` | fake shell binary |
| `internal/graphstore/discoverbin_test.go:37` | `TestDiscoverCRGBin_PATHFallback` | `exec.LookPath` requires `PATHEXT` ext on Windows; extension-less shell-shim non-executable |
| `internal/graphstore/crg_internal_test.go:14` | `TestCRGBridge_pythonBin_FallbackToPath` | `python3` fallback POSIX-only |
| `internal/graphstore/crg_internal_test.go:147` | `TestCRGBridge_run_NonZeroExit` | shell binary path differs |
| `internal/graphstore/crg_internal_test.go:168` | `TestCRGBridge_run_NonZeroExitNoStderr` | shell binary path |
| `internal/graphstore/crg_internal_test.go:183` | `TestCRGBridge_run_OK` | shell binary path |
| `internal/graphstore/crg_internal_test.go:202` | `TestCRGBridge_runCaptured_NonPythonBin` | shell binary path |
| `internal/graphstore/crg_internal_test.go:221` | `TestCRGBridge_runCaptured_Error` | shell binary path |
| `internal/graphstore/crg_internal_test.go:441` | `TestCRGBridge_BuildReport_FailedRun` | shell binary path |
| `internal/graphstore/crg_internal_test.go:460` | `TestCRGBridge_BuildReport_EmptyGraphAfterRun` | shell binary path |
| `internal/graphstore/crg_internal_test.go:480` | `TestCRGBridge_BuildReport_ReadyOutcome` | shell binary path |
| `internal/graphstore/crg_internal_test.go:505` | `TestCRGBridge_Build_PassthroughError` | shell binary path |
| `internal/graphstore/crg_buildreport_test.go:39` | `TestCRGBridge_BuildReport_ErrorOutcome` | shell binary path |
| `internal/graphstore/crg_buildreport_test.go:69` | `TestCRGBridge_BuildReport_BusyOrLockedOutcome` | shell binary path |
| `internal/scaffold/home/copy_test.go:89` | `TestCopyStarterEntryShSuffixSetsExecBit` | **Literal exec-bit 0o755 case — `exec-bit` task documents this** |
| `commands/testutil_test.go:20` | `seedAllPlatformInstallSignals` (helper) | PATH/shim seeding semantics differ |
| `commands/init_test.go:56` | `TestLinkCursorGlobalHooks_SeededInstallCreatesHardlink` | "seed helper skips on Windows" (transitive via helper above) |
| `commands/init_test.go:345` | `assertSidecarBackupWriteFailureSurfaces` (helper) | early-return on root OR Windows (chmod-dir ineffective on either) |
| `commands/agents/agents_test.go:132` | `TestImportAgentIn_ErrorRepoLocalMispointedSymlink` | POSIX symlink mispoint detection; no managed-link analogue |
| `commands/agents/agents_test.go:342` | `TestPromoteAgentIn_ErrorRepoLocalSymlinkMispoints` | same |
| `commands/agents/agents_test.go:435` | `TestRemoveAgentIn_DriftSymlinkWithoutManifestEntry` | same |
| `commands/agents/agents_test.go:515` | `TestRemoveAgentIn_ErrorPurgeCanonicalSymlink` | same |
| `commands/agents/coverage_test.go:236` | `TestEnsureImportRepoAgentsSlot_AlreadyCorrectSymlink` | same |
| `commands/agents/coverage_test.go:317` | `TestCleanupManagedAgentRepoPath_MispointedSymlinkErrors` | same |
| `commands/agents/coverage_test.go:727` | `TestEnsureImportRepoAgentsSlot_DanglingSymlink` | same |
| `commands/agents/seams_test.go:42` | `TestEnsureImportRepoAgentsSlot_ReadlinkErrorSeam` | needs real symlink so `os.Lstat` reports `os.ModeSymlink` |
| `commands/agents/seams_test.go:80` | `TestCleanupManagedAgentRepoPath_ReadlinkErrorSeam` | same |
| `commands/agents/seams_test.go:112` | `TestStdReadlinker_DelegatesToOSReadlink` | same |
| `commands/doctor_repair_e2e_test.go:137` | `TestDoctorRepairE2E_ReportsAndRestoresBrokenLink` | Windows hard link cannot dangle (nlink decrement preserves content) |
| `commands/import_test.go:1137` | `TestIsManagedSymlink` | symlink-to-file is hard link on Windows; `isManagedSymlink` correctly returns false there |
| `commands/import_test.go:1975` | `TestProcessImportCandidate_ManagedSourceIsNoop` | no-op decision uses Readlink target match; Windows hard link has no recoverable target |
| `commands/import_test.go:2218` | `TestIsManagedSymlink_RelativeDestInsideAgentsHome` | relative-dest Readlink branch inexpressible in managed-link model |
| `commands/hooks/remove_test.go:146` | `TestEnsureUnderHooksScopeTreeRelError` | `filepath.Rel` behavior differs on Windows (drive letters / UNC) |
| `commands/hooks/spec_test.go:58` | `TestFindHookSpecListError` | ENOTDIR path POSIX-only; Windows returns IsNotExist-equivalent |
| `commands/workflow/testutil_test.go:936` | `chmodUnreadable` (helper, file variant) | helper itself is a no-op on Windows; callers migrate to `MakeFileUnreadable` |
| `commands/workflow/fs_test.go:725` | `TestCopyWorkflowArtifact_DstCreateFailsWhenDstIsDir` | EISDIR semantics differ |
| `commands/workflow/fs_test.go:750` | `TestCopyWorkflowArtifact_IoCopyFailsForDirSource` | `io.Copy` from directory not portable on Windows |
| `commands/workflow/plan_check_scope_test.go:88` | `TestCollectCheckScopeChangedFiles_FromGitDiff_AppendsAndDedupes` | git diff path differs |
| `commands/workflow/plan_check_scope_test.go:130` | `TestCheckScopeGitDiffFiles_ReturnsModifiedFiles` | same |
| `commands/workflow/plan_check_scope_test.go:158` | `TestCheckScopeGitDiffFiles_FallbackToCached` | same |
| `commands/workflow/plan_check_scope_test.go:294` | `setFakeExe` (helper) | fake-exe via shell unsupported on Windows |
| `commands/workflow/graph_test.go:969` | `TestRunWorkflowGraphQueryViaKGBridge_JSONFlagPrependsAndBridgeFails` | relies on `/usr/bin/false` |
| `commands/workflow/graph_test.go:992` | `TestRunWorkflowGraphQueryViaKGBridge_SuccessReturnsNil` | relies on `/usr/bin/true` |
| `commands/workflow/prefs_test.go:859` | `TestSetLocalPreference_MkdirAllError` | file-as-dir collision (ENOTDIR) — semantics differ on Windows |
| `commands/kg/bridge_fault_test.go:287` | `TestCollectCodeBridgeResults_OpenStoreError` | requires `/dev/null` as non-directory path component |
| `commands/kg/kg_test.go:88` | `crgShellShimSkip` (helper) | POSIX shell CRG shim non-executable on Windows |
| `commands/kg/sync_code_warm_link_test.go:741` | `TestRunKGLinkAdd_OpenStoreError` | `/dev/null` non-dir path |
| `commands/kg/sync_code_warm_link_test.go:755` | `TestRunKGLinkList_OpenStoreError` | same |
| `commands/kg/sync_code_warm_link_test.go:768` | `TestRunKGLinkRemove_OpenStoreError` | same |
| `commands/kg/sync_code_warm_link_test.go:782` | `TestRunKGWarm_OpenStoreError` | same |
| `commands/kg/sync_code_warm_link_test.go:796` | `TestRunKGWarmStats_OpenStoreError` | same |
| `commands/kg/sync_code_warm_link_test.go:1891` | `TestCrgRepoRoot_GetwdErrorFallsBackToDot` | removing cwd not reliably observable on Windows |
| `commands/skills/promote_test.go:170` | `TestPromoteSkillIn_ErrorRepoLocalSymlinkMispoints` | POSIX symlink mispoint detection; no managed-link analogue |

### `[not-a-skip]` — parameterizes value by OS (7 sites; no migration)

These set a value conditionally and run the test on both OSes — correctly cross-platform already.

| path:line | Func | What it parameterizes |
|---|---|---|
| `internal/config/paths_test.go:91` | `TestExpandPath_AbsolutePassThrough` | absolute-path literal (`/already/abs` vs `C:\already\abs`) |
| `internal/graphstore/crg_venv_discovery_test.go:16` | `TestVenvBinSubdirs_OrderByOS` | expected first subdir (`bin` vs `Scripts`) |
| `internal/graphstore/crg_venv_discovery_test.go:46` | `TestVenvExeCandidates_CoversLayouts` | requires `.exe` candidates on Windows |
| `internal/graphstore/crg_venv_discovery_test.go:55` | `crgBinFileName` (helper) | adds `.exe` suffix on Windows |
| `internal/graphstore/crg_venv_discovery_test.go:119` | `TestPythonBin_ResolvesSiblingThenFallback` | python binary name (`python3` vs `python.exe`) |
| `internal/graphstore/crg_venv_discovery_test.go:134` | `TestPythonBin_ResolvesSiblingThenFallback` | fallback python name (`python3` vs `python`) |
| `commands/install_test.go:389` | `buildFakeGit` (helper) | appends `.exe` to built binary path on Windows |

## Downstream task readout

| Task | Migration sites it unblocks |
|---|---:|
| `perms-dir` | 15 (plus enables `fsops-windows-tests`) |
| `perms-readonly` | 0 (ship helper; no current sites) |
| `symlinks` | 10 |
| `exec-bit` | 1 literal exec-bit site + provides documentation pattern for the other 60 `[genuine-posix]` sites |
| `migrate-sites` | 28 total (3 file-perm immediate + 15 dir-perm post-perms-dir + 10 symlink post-symlinks) |
