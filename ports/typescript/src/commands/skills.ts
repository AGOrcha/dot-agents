/**
 * `dot-agents skills` — Manage skills in ~/.agents/skills/.
 *
 * Supports list and new subcommands. Aligned with commands/skills.go.
 */

import { mkdir, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";
import { listBucket, type BucketEntry } from "../lib/projectsync.js";

export interface SkillsOptions {
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export interface SkillEntry {
  name: string;
  description?: string;
  hasSkillMd: boolean;
}

export interface SkillsListResult {
  scope: string;
  skills: SkillEntry[];
}

/**
 * List skills in the given scope (default: global).
 */
export async function runSkillsList(scope = "global", opts: SkillsOptions = {}): Promise<SkillsListResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const entries = await listBucket(home, scope, { bucket: "skills", manifestName: "SKILL.md" });
  const skills: SkillEntry[] = entries.map((e: BucketEntry) => ({
    name: e.name,
    description: e.description,
    hasSkillMd: e.hasManifest,
  }));
  return { scope, skills };
}

/** Create a new skill directory scaffold. */
export async function runSkillsNew(
  skillName: string,
  scope = "global",
  opts: SkillsOptions = {},
): Promise<{ created: boolean; path: string; alreadyExists: boolean }> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const skillPath = join(home, "skills", scope, skillName);

  try {
    await stat(skillPath);
    return { created: false, path: skillPath, alreadyExists: true };
  } catch {
    // doesn't exist — create it
  }

  await mkdir(skillPath, { recursive: true });
  const skillMd = `---
name: "${skillName}"
description: ""
---

# ${skillName}

<!-- Describe what this skill does -->
`;
  await writeFile(join(skillPath, "SKILL.md"), skillMd, "utf8");
  return { created: true, path: skillPath, alreadyExists: false };
}
