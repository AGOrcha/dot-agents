/**
 * Phase 4 boundary: docs + CLI help must agree that TS does not implement kg or
 * workflow writes; option 2 (read-only workflow as optional future) is explicit.
 */

import { describe, expect, it } from "vitest";

import {
  BOUNDARY_HELP_SUBSTRINGS,
  CHOSEN_PHASE4_OPTION,
  PHASE4_DECISION_LABEL,
  boundaryHelpLines,
} from "../src/boundary.js";

describe("Phase 4 boundary module", () => {
  it("locks option 2 as the chosen Phase 4 alternative", () => {
    expect(CHOSEN_PHASE4_OPTION).toBe(2);
  });

  it("exposes a stable decision label for tooling", () => {
    expect(PHASE4_DECISION_LABEL).toContain("Phase 4");
    expect(PHASE4_DECISION_LABEL.toLowerCase()).toContain("workflow");
    expect(PHASE4_DECISION_LABEL.toLowerCase()).toContain("go-only");
  });

  it("includes required substrings in help lines (positive + negative clarity)", () => {
    const text = boundaryHelpLines().join("\n");
    for (const s of BOUNDARY_HELP_SUBSTRINGS) {
      expect(text).toContain(s);
    }
    expect(text).toMatch(/not full parity/i);
    expect(text).toMatch(/option\s+2/i);
  });
});
