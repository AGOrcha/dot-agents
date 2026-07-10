#!/usr/bin/env bash
# guard-commands — block destructive commands before shell execution.
#
# The rm-analysis below is the STANDARD robust algorithm, kept in lockstep with
# the omp-native TypeScript variant (omp/guard-rm.ts). It refuses a recursive/
# force `rm` whose target can expand to EMPTY — an empty path operand deletes the
# CURRENT DIRECTORY in shells where "" is not a no-op (the omp embedded shell;
# 2026-07-10 payout workspace-deletion incident) — plus catastrophic literal
# roots. Explicit non-empty literal paths (rm -rf dist, rm -rf "$VAR/sub") pass.
set -u

payload="$(cat || true)"

# Extract the bash command. Handle .input.command (omp), .tool_input.command
# (claude), and .command. Fall back to scanning the raw payload if jq is absent.
cmd=""
if command -v jq >/dev/null 2>&1; then
	cmd="$(printf '%s' "$payload" | jq -r '[.. | objects | .command? // empty] | map(select(type=="string")) | .[0] // empty' 2>/dev/null || true)"
fi
[ -z "$cmd" ] && cmd="$payload"

block() {
	printf 'blocked by guard-commands: %s\n' "$1" >&2
	exit 2
}

# `rm` as a command word, carrying a recursive/force flag cluster.
rmpre='(^|[^A-Za-z0-9_/.-])rm[[:space:]]+(-[A-Za-z]*[rRf][A-Za-z]*[[:space:]]+)+'
bnd='([[:space:]]|;|&|[|]|$)'

# 1) empty-quote target: rm -rf ""  or  rm -rf ''
if grep -Eq "${rmpre}[\"'][\"']${bnd}" <<<"$cmd"; then
	block "rm recursive/force on an empty-string argument — deletes the CURRENT DIRECTORY in this shell. Use an explicit non-empty literal path."
fi
# 2) command-substitution target: rm -rf "$(...)" / rm -rf $(...) / rm -rf `...`
if grep -Eq "${rmpre}[\"']?([\$][(]|\`)" <<<"$cmd"; then
	block "rm recursive/force on a command-substitution target (\$(...) or backticks) — if it expands to empty it deletes the CURRENT DIRECTORY. Compute and verify the path first."
fi
# 3) bare-variable target: rm -rf $VAR / rm -rf "${VAR}"  (whole operand a var)
if grep -Eq "${rmpre}[\"']?[\$][{]?[A-Za-z_][A-Za-z0-9_]*[}]?[\"']?${bnd}" <<<"$cmd"; then
	block "rm recursive/force on a bare variable target that can expand to empty — deletes the CURRENT DIRECTORY. Guard it (\${VAR:?}) or use a literal path."
fi
# 4) catastrophic literal root: rm -rf / ~ . .. * $HOME $PWD
if grep -Eq "${rmpre}[\"']?([/~*]|[.][.]?|[\$]HOME|[\$]PWD)[\"']?${bnd}" <<<"$cmd"; then
	block "rm recursive/force on filesystem root / home / cwd."
fi
# 5) no explicit target: rm -rf  then a separator or end of line
if grep -Eq "(^|[^A-Za-z0-9_/.-])rm[[:space:]]+(-[A-Za-z]*[rRf][A-Za-z]*[[:space:]]*)+([;&|]|$)" <<<"$cmd"; then
	block "rm recursive/force with no explicit path operand — deletes the CURRENT DIRECTORY. Pass an explicit literal path."
fi

# Other exact destructive patterns (substring, case-insensitive) over the payload.
for pattern in \
	'git push --force origin main' \
	'git push --force origin master' \
	'DROP DATABASE' \
	'DROP TABLE' \
	'truncate' \
	':(){ :|:& };:'; do
	if grep -Fqi "$pattern" <<<"$payload"; then
		block "matched forbidden pattern: $pattern"
	fi
done

exit 0
