# Lesson: containerized sonar-scanner crashes on the git fsmonitor socket

## Symptom
The pre-push `sonar-scanner (containerized)` hook fails **intermittently** with:

```
Caused by: java.nio.file.NoSuchFileException: /usr/src/.git/fsmonitor--daemon.ipc
  at ...UnixFileSystemProvider.readAttributes
  at ...ProjectFilePreprocessor.processDirectory(ProjectFilePreprocessor.java:184)
  INFO  EXECUTION FAILURE
error: failed to push some refs
```

The commit lands; only the push (pre-push gate) fails. Re-running sometimes "works."

## Root cause
`scripts/precommit-mandate.sh` runs the scanner in a container with the repo
bind-mounted at `/usr/src` (`-v "$repo_root:/usr/src"`). The scanner walks the
whole tree. git's **fsmonitor daemon** keeps a Unix-domain socket at
`.git/fsmonitor--daemon.ipc` and churns it (creates/removes). The walker
`stat()`s that socket mid-walk; it vanishes → `NoSuchFileException` → the whole
scan aborts.

**Why intermittent:** it only fails when a daemon is *live* during the walk. An
editor's git integration keeps one alive, and `core.fsmonitor=false` does **not**
stop already-running orphan daemons (`git fsmonitor--daemon status` /
`pgrep -fl fsmonitor--daemon` reveal them). That's why a manual
`git fsmonitor--daemon stop` appeared to "fix" it — pure whack-a-mole.

## The fix that does NOT work
Bind-masking the socket in `docker run` — `-v /dev/null:/usr/src/.git/fsmonitor--daemon.ipc` — fails at container init:

```
error mounting "/dev/null" ... openat2 .../fsmonitor--daemon.ipc: operation not supported
```

**runc cannot use a socket as a mount target** (it `openat2`s the target; sockets
aren't openable). So the socket must be *removed*, not masked.

## The fix that works
In the launcher, before the `docker run`: stop the daemon and remove the socket.

```bash
git fsmonitor--daemon stop >/dev/null 2>&1 || true
rm -f "$repo_root/.git/fsmonitor--daemon.ipc" 2>/dev/null || true
[[ -n "${git_common_abs:-}" ]] && rm -f "$git_common_abs/fsmonitor--daemon.ipc" 2>/dev/null || true
```

fsmonitor is a perf cache (off by default here) that won't auto-restart during the
scan; `.git` stays fully readable for SCM/blame (do **not** reach for
`sonar.scm.disabled`). Handles normal checkouts and the worktree common dir.

## How to verify a fix like this in-situ
Reproduce the failure condition deliberately, then run the gate:
1. `git fsmonitor--daemon start` (socket appears: `ls -la .git/fsmonitor--daemon.ipc` → `srwx...`).
2. Push. If `sonar-scanner (containerized)` passes **with the daemon live**, the fix holds.

## Rules
- A bind-mounted working tree exposes volatile special files (sockets, lockfiles)
  to anything that walks it in a container. Don't assume `.git` is inert.
- You cannot `-v`-mask a Unix socket; remove the file instead.
- Fix recurring env flakes at the launcher/source, not by manual cleanup each time.

Sibling of [[sonarcloud-gate-mechanics]] and [[ssh-keepalive-for-slow-pre-push-hook]].
Landed in PR #228 (`scripts/precommit-mandate.sh`).

## Addendum (2026-06-02): orphan respawn + full daemon wedge

Two follow-on failure modes seen overnight, even with the launcher fix present:

1. **Orphan respawn loses the race.** The in-gate cleanup removes the socket
   right before `docker run`, but an editor's git integration **respawns** the
   fsmonitor daemon mid-scan, recreating the socket. Durable mitigation beyond
   the launcher fix: `git config core.fsmonitor false` **repo-locally** and
   `pkill -9 -f fsmonitor--daemon` before the scan. (Config alone does not kill
   orphans — must pkill.)

2. **Full Docker daemon wedge (distinct symptom).** Not the fast
   `NoSuchFileException` crash — instead the scanner produces **0 bytes for 20+
   min** and every `docker` command (`ps`, `kill`, `info`) hangs indefinitely.
   That is the Docker daemon itself wedged (recurring macOS flake, same family as
   the TCC/SSH issues), not the socket. No code fix; pushing is blocked until
   Docker is restarted. Don't `--no-verify`. Pattern that worked: a background
   watcher (`.agents/active/push-when-ready.sh`) that polls `docker info` (bounded
   via `perl -e 'alarm shift; exec @ARGV'` since macOS lacks `timeout`) and
   pushes queued branches through the real gate once the daemon recovers.
