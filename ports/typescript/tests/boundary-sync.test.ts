/**
 * Boundary sync test.
 *
 * Invokes the static check-boundary parser against the live repo state and
 * asserts there are no violations. This is what CI runs to catch drift
 * between the Go cobra command tree and the TypeScript port.
 *
 * If this test fails, look at the printed CheckFailure list — each entry
 * names the file you need to edit and the spec section to update.
 */

import { describe, expect, it } from "vitest";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { readFileSync } from "node:fs";

import {
  runCheck,
  parseRootConstructors,
  parseTSCommandTree,
  parseGoCommandTree,
  loadSpec,
} from "../scripts/check-boundary.js";

const TEST_DIR = dirname(fileURLToPath(import.meta.url));
// tests/ → ports/typescript/ → ports/ → repo root
const REPO_ROOT = resolve(TEST_DIR, "..", "..", "..");

describe("boundary-sync check", () => {
  it("returns zero violations against the current repo state", () => {
    const result = runCheck(REPO_ROOT);

    if (result.failures.length > 0) {
      // Surface the diagnostics directly so CI logs are actionable.
      const diag = result.failures
        .map((f, i) => `  ${i + 1}. [${f.category}] ${f.message}`)
        .join("\n");
      throw new Error(
        `boundary-sync expected zero failures but got ${result.failures.length}:\n${diag}`,
      );
    }

    expect(result.failures).toEqual([]);
  });

  it("loads the boundary spec with all required fields", () => {
    const spec = loadSpec(REPO_ROOT);
    expect(spec.version).toBeGreaterThan(0);
    expect(spec.stage1_commands.length).toBeGreaterThan(0);
    expect(typeof spec.stage1_flag_lock).toBe("object");
    expect(Array.isArray(spec.phase4_optouts)).toBe(true);
    expect(Array.isArray(spec.phase5_deferred)).toBe(true);
    expect(Array.isArray(spec.stage2_deferred_subitems)).toBe(true);
  });

  it("discovers all 19 Go top-level constructors from commands/root.go", () => {
    const rootSrc = readFileSync(
      join(REPO_ROOT, "commands", "root.go"),
      "utf8",
    );
    const ctors = parseRootConstructors(rootSrc);
    expect(ctors).toContain("NewInitCmd");
    expect(ctors).toContain("NewKGCmd");
    expect(ctors).toContain("NewWorkflowCmd");
    expect(ctors.length).toBeGreaterThanOrEqual(19);
  });

  it("parses the TS top-level command set including all 8 Stage 1 cmds", () => {
    const ts = parseTSCommandTree(REPO_ROOT);
    for (const stage1 of [
      "init",
      "add",
      "refresh",
      "status",
      "doctor",
      "skills",
      "agents",
      "hooks",
    ]) {
      expect(ts.topLevel).toContain(stage1);
    }
  });

  it("parses the Go command tree and recovers each Stage 1 command name", () => {
    const cmds = parseGoCommandTree(REPO_ROOT);
    const names = cmds.map((c) => c.name);
    for (const stage1 of [
      "init",
      "add",
      "refresh",
      "status",
      "doctor",
      "skills",
      "agents",
      "hooks",
    ]) {
      expect(names).toContain(stage1);
    }
  });
});
