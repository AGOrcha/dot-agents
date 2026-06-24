# Lesson: SSH keepalive for slow pre-push hooks

## Pattern

`git push` repeatedly fails with `Connection to github.com closed by remote
host.` **after** the pre-push hook prints all-green, and the branch never lands
on the remote — even though `ssh -T git@github.com` authenticates instantly and
the commit content is valid.

## Root cause

`git push` opens the SSH transport to GitHub **before** running the pre-push
hook. This repo's pre-push hook is slow (build/vet on POSIX+Windows + `go test
./...` 95% coverage gate + a **containerized sonar-scanner**) — minutes. While
the hook runs, the already-negotiated SSH connection sits idle, and GitHub's
server-side idle timeout closes it. By the time the hook exits 0 and git tries
to send the pack, the connection is gone. Exit code can still surface as 0 when
piped through `| tail`, masking the failure — always verify the branch actually
landed with `git ls-remote origin refs/heads/<branch>`.

## Rule

For any push in this repo (slow pre-push hook), force SSH keepalives so the
connection survives the hook:

```sh
GIT_SSH_COMMAND="ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=30 -o TCPKeepAlive=yes" \
  git push -u origin <branch>
```

`ServerAliveInterval=15` sends a keepalive every 15s during the long hook so the
server never sees the connection as idle. Do **not** reach for `--no-verify` to
dodge the hook — that is a hard project constraint; keepalive is the correct fix.

## How to apply

- This is now scripted: `bash scripts/precommit-mandate.sh push [<remote>] [<branch>]`
  sets the keepalive `GIT_SSH_COMMAND` and verifies the ref landed (ls-remote SHA ==
  local HEAD), failing loudly if not. Prefer it over a bare `git push` for this repo.
- After any push, confirm the ref landed: `git ls-remote origin refs/heads/<branch>`.
  Do not trust a piped exit code alone.
- If a push shows green hooks then `Connection ... closed by remote host`, it's
  this — re-run with the `GIT_SSH_COMMAND` keepalive above, don't keep retrying
  bare `git push` (each retry re-runs the multi-minute hook and loses the same race).
- Consider setting these options permanently in `~/.ssh/config` for
  `Host github.com` so every push inherits them.

Sibling of [[parallel-worker-branch-drift]] (both are push-time hook interactions).
