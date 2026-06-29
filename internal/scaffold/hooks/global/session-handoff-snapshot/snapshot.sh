#!/bin/sh

# session-handoff-snapshot: PreCompact hook.
#
# Before the context window is compacted, capture a fresh deterministic
# live-state snapshot of the session-handoff journal (`da workflow journal
# snapshot`). The snapshot is written to durable, non-git-tracked state that
# SURVIVES compaction, so the post-compaction session can re-inject verified
# state instead of re-grounding from scratch (spec D5/R2).
#
# BEST-EFFORT CONTRACT: a snapshot failure must NEVER block compaction. Every
# path returns success; failures are reported on stderr only and the hook
# always exits 0. The snapshot itself prints nothing into the (about to be
# discarded) context — its job is to write the durable file, not to talk.

set -u

project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"

main() {
  # The workflow surface ships in da 0.4.0+. On installs without the journal
  # command, skip silently rather than warning about a command that is simply
  # not present yet.
  if command -v da >/dev/null 2>&1 && da workflow journal --help >/dev/null 2>&1; then
    if (
      cd "$project_dir" &&
      da workflow journal snapshot >/dev/null 2>&1
    ); then
      return 0
    fi
    printf 'session-handoff-snapshot warning: da workflow journal snapshot failed; compaction not blocked\n' >&2
  fi
  return 0
}

main || true

exit 0
