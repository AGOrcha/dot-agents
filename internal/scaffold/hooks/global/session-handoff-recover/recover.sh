#!/bin/sh

# session-handoff-recover: SessionStart(matcher: compact) hook.
#
# When a session resumes into a freshly compacted context window, re-inject the
# VERIFIED recovery view (`da workflow journal recover`): the durable snapshot +
# replayed events, each item re-verified against current reality (git/gh) so the
# fresh context never trusts stale claims as facts (spec D7/R2/R4). The hook
# fires only for the `compact` SessionStart source (the manifest matcher), so a
# normal startup/resume/clear is untouched.
#
# The recovery view is printed on stdout: a SessionStart hook's stdout is added
# to the session context, which is exactly how the recovered state re-enters the
# new window.
#
# BEST-EFFORT CONTRACT: a recovery failure must NEVER block session start. Every
# path returns success; failures are reported on stderr only and the hook always
# exits 0. A missing journal (nothing to recover) is silent.

set -u

project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"

main() {
  # The workflow surface ships in da 0.4.0+. On installs without the journal
  # command, skip silently rather than warning about a command that is simply
  # not present yet.
  if command -v da >/dev/null 2>&1 && da workflow journal --help >/dev/null 2>&1; then
    if (
      cd "$project_dir" &&
      da workflow journal recover
    ); then
      return 0
    fi
    printf 'session-handoff-recover warning: da workflow journal recover failed; session start not blocked\n' >&2
  fi
  return 0
}

main || true

exit 0
