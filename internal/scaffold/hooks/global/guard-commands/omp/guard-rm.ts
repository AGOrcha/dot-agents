// guard-rm — hard pre-execution block for destructive `rm` whose delete target
// can expand to EMPTY.
//
// Root cause it defends against (incident 2026-07-10): in this harness's
// embedded shell, `rm -rf ""` is NOT a no-op — an empty path operand makes `rm`
// recursive/force delete the CURRENT WORKING DIRECTORY. A loop worker ran
//     rm -rf "$(cat /dev/null)"; cd <worktree> && git status
// with cwd defaulted to ~/proj-docs/payout (it omitted an explicit cwd), so the
// empty command-substitution wiped the payout superproject (.git, .agents, all
// submodules) before the `cd` ever ran.
//
// The canonical docs example only refuses `rm -rf /|~|$HOME` — it would NOT have
// caught this. This guard additionally refuses the empty-expansion class: an
// empty-quote literal (""/''), a command substitution ($(...) / `...`), a bare
// variable ($VAR / "${VAR}"), or no operand at all. Explicit non-empty literal
// paths (rm -rf dist, rm -rf /tmp/x, rm -rf "$VAR/sub") pass through.

import type { HookAPI } from "@oh-my-pi/pi-coding-agent/extensibility/hooks";

const CATASTROPHIC: Record<string, true> = {
	"/": true,
	"/*": true,
	"~": true,
	"~/": true,
	".": true,
	"./": true,
	"..": true,
	"*": true,
	"$HOME": true,
	"${HOME}": true,
	"$PWD": true,
	"${PWD}": true,
};

// Returns a block reason if `command` contains a dangerous rm, else null.
export function findDangerousRm(command: string): string | null {
	// Segment into simple-commands so `rm` is judged as a command word, not as a
	// substring of an unrelated argument (echo / commit message / etc.).
	const segments = command.split(/\n|;|&&|\|\||\||&/);
	for (const rawSeg of segments) {
		// `rm` as the command: optional VAR=val prefixes, optional sudo/command.
		const m = rawSeg.match(
			/^\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*(?:sudo\s+)?(?:command\s+)?rm\s+(\S.*)$/,
		);
		if (!m) continue;
		const argStr = m[1];
		const tokens = argStr.split(/\s+/).filter((t) => t.length > 0);
		const flags = tokens.filter((t) => t.startsWith("-") && t !== "--");
		const destructive =
			flags.some((f) => /[rRf]/.test(f)) ||
			tokens.includes("--recursive") ||
			tokens.includes("--force");
		if (!destructive) continue;

		const padded = ` ${argStr} `;

		// Empty-quote operand: rm -rf ""  or  rm -rf ''
		if (/(?:^|\s)(?:""|'')(?:\s|$)/.test(padded)) {
			return 'Blocked: `rm` recursive/force with an empty-string argument ("" / \'\'). In this shell an empty target deletes the CURRENT DIRECTORY. Pass an explicit non-empty literal path.';
		}
		// Command substitution anywhere in the operands: $(...) or `...`
		if (/\$\(|`/.test(argStr)) {
			return "Blocked: `rm` recursive/force with a command-substitution target ($(...) or backticks). If it expands to empty it deletes the CURRENT DIRECTORY in this shell. Compute the path into a variable, verify it is non-empty, and delete an explicit literal path.";
		}
		// Bare variable operand (quoted or not) with NO literal path component:
		// "$VAR" / $VAR / "${VAR}" — but NOT "$VAR/sub" (has a literal suffix).
		if (/(?:^|\s)["']?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?["']?(?:\s|$)/.test(padded)) {
			return "Blocked: `rm` recursive/force on a bare variable target that can expand to empty (deletes the CURRENT DIRECTORY in this shell). Guard it (e.g. `${VAR:?must be set}`) or use an explicit literal path.";
		}

		const operands = tokens.filter((t) => !t.startsWith("-") && t !== "--");
		if (operands.length === 0) {
			return "Blocked: `rm` recursive/force with no explicit path operand (deletes the CURRENT DIRECTORY in this shell). Pass an explicit non-empty literal path.";
		}
		for (const op of operands) {
			const stripped = op.replace(/^["']/, "").replace(/["']$/, "");
			if (Object.hasOwn(CATASTROPHIC, stripped)) {
				return `Blocked: \`rm\` recursive/force on ${op} (filesystem root / home / cwd).`;
			}
		}
	}
	return null;
}

export default function guardRm(pi: HookAPI): void {
	pi.on("tool_call", (event) => {
		if (event.toolName !== "bash") return;
		const input: unknown = event.input;
		let command = "";
		if (typeof input === "object" && input !== null && "command" in input) {
			const c = input.command; // `in` narrowing → unknown, no cast
			if (typeof c === "string") command = c;
		}
		if (!command) return;
		const reason = findDangerousRm(command);
		if (reason) return { block: true, reason };
	});
}
