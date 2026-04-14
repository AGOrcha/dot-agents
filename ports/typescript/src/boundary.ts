/**
 * Phase 4 advanced-surface boundary strings for CLI help and tests.
 * Canonical narrative: docs/TYPESCRIPT_PORT_BOUNDARY.md
 */

/** Matches documented Phase 4 option id (see TASKS.yaml phase-4-advanced-surface-decision). */
export const CHOSEN_PHASE4_OPTION = 2 as const;

/** Short label for logs and help. */
export const PHASE4_DECISION_LABEL =
  "Phase 4: selected workflow read-only surfaces (optional future); kg and workflow writes = Go-only";

/** Substrings tests require in the concatenated help block (stable contract). */
export const BOUNDARY_HELP_SUBSTRINGS = [
  "kg/*",
  "workflow writes",
  "checkpoint",
  "merge-back",
  "orchestration",
  "TYPESCRIPT_PORT_BOUNDARY.md",
] as const;

/**
 * Lines appended to `dot-agents-ts --help` so users see the boundary without opening docs.
 */
export function boundaryHelpLines(): string[] {
  return [
    "",
    "Boundary (Phase 4 — workflow / KG / orchestration):",
    `  Decision: option ${CHOSEN_PHASE4_OPTION} — read-only workflow may be added later; not full parity.`,
    "  Implemented now: Stage 1 commands only (init, add, refresh, status, doctor, skills, agents, hooks).",
    "  Use the Go dot-agents CLI for: kg/*, workflow writes (checkpoint, advance, merge-back, fanout, …),",
    "  and orchestration. See docs/TYPESCRIPT_PORT_BOUNDARY.md.",
  ];
}
