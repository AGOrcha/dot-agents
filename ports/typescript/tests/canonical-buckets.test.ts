/**
 * Canonical store bucket list — must stay aligned with internal/platform/buckets.go.
 */

import { describe, expect, it } from "vitest";
import { CANONICAL_BUCKET_SPECS } from "../src/platforms/canonical-buckets.js";

describe("CANONICAL_BUCKET_SPECS", () => {
  it("matches Go platform.CanonicalStoreBucketSpecs order and markers", () => {
    expect(CANONICAL_BUCKET_SPECS.map((s) => s.name)).toEqual([
      "rules",
      "settings",
      "mcp",
      "skills",
      "agents",
      "hooks",
      "commands",
      "output-styles",
      "ignore",
      "modes",
      "plugins",
      "themes",
      "prompts",
    ]);
    const skills = CANONICAL_BUCKET_SPECS.find((s) => s.name === "skills");
    expect(skills?.countDirs).toBe(true);
    expect(skills?.markerFile).toBe("SKILL.md");
    const plugins = CANONICAL_BUCKET_SPECS.find((s) => s.name === "plugins");
    expect(plugins?.countDirs).toBe(true);
    expect(plugins?.markerFile).toBe("PLUGIN.yaml");
    const rules = CANONICAL_BUCKET_SPECS.find((s) => s.name === "rules");
    expect(rules?.countDirs).toBe(false);
  });
});
