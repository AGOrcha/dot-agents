---
schema_version: 1
task_id: t1b-fetch-materialization-bounds
parent_plan_id: package-artifact-install
title: Bound raw fetch/clone materialization before normalizer caps (go-git in-memory clone + directory enumeration)
summary: |-
    Bounded both t1b DoS residuals in the raw fetch/clone layer, before BundleLimits (t1's normalizer caps) ever run.

    (a) go-git in-memory clone: gitArtifactFetcher now clones via gitCloneShallowQuotaBounded
    (fetcher_git_artifact.go) instead of gitCloneShallow — a fresh temp dir behind a
    byte-quota-guarded billy.Filesystem wrapper (quotaFilesystem/quotaFile/quotaTracker,
    gitArtifactCloneQuotaBytes = 256 MiB, applied independently to both the object store and
    the checkout). A hostile/oversized upstream now fails closed against a fixed disk ceiling
    instead of exhausting host memory via go-git's default memory.NewStorage()+memfs. The
    returned artifactCloneFS exposes a Cleanup() the caller (FetchArtifact) defers once done
    reading; a test-injected cloner is unaffected (no Cleanup method -> no-op type assertion).

    (b) directory enumeration:
      - local (fetcher_local_artifact.go): readConfinedDir's unbounded fh.ReadDir(-1) is now
        streamConfinedDir, reading in dirReadBatchSize(1024) batches so a flat-entry flood is
        rejected within one batch's over-read instead of after the whole directory is buffered.
      - git (fetcher_git_artifact.go): the tree-layout walk no longer uses wfs.ReadDir on the
        checked-out worktree (billy.Filesystem has no bounded/batched ReadDir at all). It now
        reads directly from the COMMITTED tree/blob objects via gitCommittedTreeWalker
        (object.TreeWalker, which streams one entry at a time), with each file's declared
        blob.Size checked against MaxFileBytes before Reader() is ever called
        (readCommittedBlobFile). This also closes a latent worktree-vs-committed-tree content
        divergence: content now comes from the same object rejectGitSubmodules already trusts.

    Verification: go test ./internal/config/... and full go test ./... green; go build ./...
    clean; gofmt/go vet clean. New adversarial tests: an oversized committed blob inside a
    tree-layout pull (TestGitArtifactFetcherTreeLayoutRejectsOversizedBlob, end-to-end via
    FetchArtifact with real DefaultBundleLimits) and entry-count floods on both paths
    (TestGitCommittedTreeWalkerRejectsEntryFlood, TestLocalRootWalkerRejectsEntryFlood) plus a
    batch-boundary correctness regression guard (TestLocalRootWalkerStreamsAcrossBatchBoundary)
    and a direct unit test of the clone-quota mechanism (TestQuotaFilesystemRejectsOverBudgetWrite).

    Surprising: the existing gitArtifactFetcher tests (populatedRepoFS/committedRepoFS) built a
    REAL on-disk git fixture repo purely for HEAD/tree scaffolding, paired with an INDEPENDENT
    memfs the test populated by hand with the actual expected content/shape (nested dirs,
    dotfiles, symlinks) -- the committed tree and the worktree fixture deliberately diverged.
    Switching the tree-layout content source to the committed tree broke that shortcut for the
    ~4 tests whose assertions depended on memfs-only content; added a general
    buildCommittedTree/gitTreeFixtureCloner test helper (low-level git object-graph
    construction, same technique the existing submoduleCloner already used) and rewrote those
    tests to commit real matching content instead. Single-file (blob) reads and the top-level
    directory-vs-file Lstat classification were deliberately left on the worktree/wfs path
    (unchanged, no test impact) since neither has a directory-listing flood surface -- only the
    tree-layout enumeration path needed the committed-tree switch.
files_changed: []
verification_result:
    status: pass
    summary: |-
        write_scope untouched: fetcher.go was not modified (gitCloneShallow/gitFetcher, the
        layer-tier clone, is unchanged and out of this task's concern per the bundle's success
        criteria -- layer.json reads use maxLayerBytes, a separate bound). The new
        gitCloneShallowQuotaBounded/quotaFilesystem plumbing lives entirely in
        fetcher_git_artifact.go. gitArtifactCloneQuotaBytes (256 MiB) is a hardcoded const, not
        currently surfaced as a config knob -- flag if a lower/configurable ceiling is wanted before
        t6-oci-consume's untrusted-remote gate. t3/t2's delegation contracts in this worktree's
        .agents/active/delegation/ were stale (missing t1b's, generated in the primary worktree by
        workflow fanout but never committed) -- copied t1b-fetch-materialization-bounds.yaml over so
        `workflow verify record --task` could resolve it; the primary worktree still has it
        untracked, worth a `git add` there when convenient.
integration_notes: |-
    write_scope untouched: fetcher.go was not modified (gitCloneShallow/gitFetcher, the
    layer-tier clone, is unchanged and out of this task's concern per the bundle's success
    criteria -- layer.json reads use maxLayerBytes, a separate bound). The new
    gitCloneShallowQuotaBounded/quotaFilesystem plumbing lives entirely in
    fetcher_git_artifact.go. gitArtifactCloneQuotaBytes (256 MiB) is a hardcoded const, not
    currently surfaced as a config knob -- flag if a lower/configurable ceiling is wanted before
    t6-oci-consume's untrusted-remote gate. t3/t2's delegation contracts in this worktree's
    .agents/active/delegation/ were stale (missing t1b's, generated in the primary worktree by
    workflow fanout but never committed) -- copied t1b-fetch-materialization-bounds.yaml over so
    `workflow verify record --task` could resolve it; the primary worktree still has it
    untracked, worth a `git add` there when convenient.
created_at: "2026-07-16T01:00:46Z"
---

## Summary

Bounded both t1b DoS residuals in the raw fetch/clone layer, before BundleLimits (t1's normalizer caps) ever run.

(a) go-git in-memory clone: gitArtifactFetcher now clones via gitCloneShallowQuotaBounded
(fetcher_git_artifact.go) instead of gitCloneShallow — a fresh temp dir behind a
byte-quota-guarded billy.Filesystem wrapper (quotaFilesystem/quotaFile/quotaTracker,
gitArtifactCloneQuotaBytes = 256 MiB, applied independently to both the object store and
the checkout). A hostile/oversized upstream now fails closed against a fixed disk ceiling
instead of exhausting host memory via go-git's default memory.NewStorage()+memfs. The
returned artifactCloneFS exposes a Cleanup() the caller (FetchArtifact) defers once done
reading; a test-injected cloner is unaffected (no Cleanup method -> no-op type assertion).

(b) directory enumeration:
  - local (fetcher_local_artifact.go): readConfinedDir's unbounded fh.ReadDir(-1) is now
    streamConfinedDir, reading in dirReadBatchSize(1024) batches so a flat-entry flood is
    rejected within one batch's over-read instead of after the whole directory is buffered.
  - git (fetcher_git_artifact.go): the tree-layout walk no longer uses wfs.ReadDir on the
    checked-out worktree (billy.Filesystem has no bounded/batched ReadDir at all). It now
    reads directly from the COMMITTED tree/blob objects via gitCommittedTreeWalker
    (object.TreeWalker, which streams one entry at a time), with each file's declared
    blob.Size checked against MaxFileBytes before Reader() is ever called
    (readCommittedBlobFile). This also closes a latent worktree-vs-committed-tree content
    divergence: content now comes from the same object rejectGitSubmodules already trusts.

Verification: go test ./internal/config/... and full go test ./... green; go build ./...
clean; gofmt/go vet clean. New adversarial tests: an oversized committed blob inside a
tree-layout pull (TestGitArtifactFetcherTreeLayoutRejectsOversizedBlob, end-to-end via
FetchArtifact with real DefaultBundleLimits) and entry-count floods on both paths
(TestGitCommittedTreeWalkerRejectsEntryFlood, TestLocalRootWalkerRejectsEntryFlood) plus a
batch-boundary correctness regression guard (TestLocalRootWalkerStreamsAcrossBatchBoundary)
and a direct unit test of the clone-quota mechanism (TestQuotaFilesystemRejectsOverBudgetWrite).

Surprising: the existing gitArtifactFetcher tests (populatedRepoFS/committedRepoFS) built a
REAL on-disk git fixture repo purely for HEAD/tree scaffolding, paired with an INDEPENDENT
memfs the test populated by hand with the actual expected content/shape (nested dirs,
dotfiles, symlinks) -- the committed tree and the worktree fixture deliberately diverged.
Switching the tree-layout content source to the committed tree broke that shortcut for the
~4 tests whose assertions depended on memfs-only content; added a general
buildCommittedTree/gitTreeFixtureCloner test helper (low-level git object-graph
construction, same technique the existing submoduleCloner already used) and rewrote those
tests to commit real matching content instead. Single-file (blob) reads and the top-level
directory-vs-file Lstat classification were deliberately left on the worktree/wfs path
(unchanged, no test impact) since neither has a directory-listing flood surface -- only the
tree-layout enumeration path needed the committed-tree switch.

## Integration Notes

write_scope untouched: fetcher.go was not modified (gitCloneShallow/gitFetcher, the
layer-tier clone, is unchanged and out of this task's concern per the bundle's success
criteria -- layer.json reads use maxLayerBytes, a separate bound). The new
gitCloneShallowQuotaBounded/quotaFilesystem plumbing lives entirely in
fetcher_git_artifact.go. gitArtifactCloneQuotaBytes (256 MiB) is a hardcoded const, not
currently surfaced as a config knob -- flag if a lower/configurable ceiling is wanted before
t6-oci-consume's untrusted-remote gate. t3/t2's delegation contracts in this worktree's
.agents/active/delegation/ were stale (missing t1b's, generated in the primary worktree by
workflow fanout but never committed) -- copied t1b-fetch-materialization-bounds.yaml over so
`workflow verify record --task` could resolve it; the primary worktree still has it
untracked, worth a `git add` there when convenient.

## Review Follow-up (commit 14a9e0be)

The t1b review found the pre-materialization bound did NOT hold for two go-git-internal
paths that materialize into memory BEFORE the quota-guarded storer sees a write. Both are
now addressed in fetcher_git_artifact.go; a genuine residual is documented, not silent.

DELTA / PACK RESOLUTION (fetch-time). gitCloneShallowQuotaBounded now requests a partial
clone: filter=blob:limit=<per-file-cap+1> (packp.FilterBlobLimit). A filter-capable server
(the real untrusted-remote case) omits over-budget blobs from the pack, so the client never
receives or delta-resolves them; an omitted blob reads back as missing and fails closed.
go-git returns transport.ErrFilterNotSupported for servers without partial-clone support
(notably go-git's own file:// transport); on that sentinel we fall back to an unfiltered but
STILL quota-bounded clone (cloneQuotaBoundedOnce factors the single attempt).

FLAT-TREE DECODE. object.GetTree materializes a whole tree object's entry slice into memory,
so the prior object.TreeWalker walk could OOM decoding one flat directory of millions of
entries before the post-decode MaxEntries cap acts. Replaced with a manual recursive walk
(gitCommittedTreeWalker now takes storer.EncodedObjectStorer + subtree hash) that gates each
tree object's plaintext size via s.EncodedObjectSize BEFORE object.GetTree, and each blob's
size before object.GetBlob (readCommittedBlob). Submodule rejection folded inline;
committedSubtreeHashAt resolves the subtree hash via FindEntry (decodes only ancestors, not
the flat target) and preserves the old rejectGitSubmodules front-door error cases.
repo.Storer threaded through cloneAndResolve.

DOCUMENTED RESIDUAL (availability-only, NOT integrity — digest/pin still gate acceptance):
go-git v6 resolves the received packfile, including in-memory delta reconstruction and
decode of NON-blob objects, BEFORE the quota storer sees a write, and exposes no hook to
bound that in-memory resolution. blob:limit is the only fetch-time object-size control
go-git v6 exposes and applies to BLOBS against FILTER-CAPABLE servers only. So a malicious
all-delta-chain pack over non-blob objects, or a filter-incapable server, can still be
resolved in memory before the quota fires. A harder bound needs a transport-byte cap or a
shell-git partial-clone fallback for untrusted sources. Captured in the
gitCloneShallowQuotaBounded doc comment (RESIDUAL block). Coordinator to track as follow-up.

Verification: go test ./... green; go build ./... clean; gofmt/go vet clean; -race clean on
the git artifact paths. New test TestGitCommittedTreeWalkerRejectsFlatTreeBeforeDecode proves
the tree-object-size gate fires BEFORE decode. Existing real-path clone tests
(&gitArtifactFetcher{} + file:// fixture) exercise the filter->ErrFilterNotSupported->
unfiltered fallback end-to-end.

NOTE: the workflow CLI would not re-record this merge-back (delegation already marked
completed after the first closeout); per the coordinator's standing instruction this
follow-up is appended here + committed, and the coordinator closes out from main.
