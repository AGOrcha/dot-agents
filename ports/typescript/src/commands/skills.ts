/**
 * `dot-agents skills` — Manage skills in ~/.agents/skills/.
 *
 * Supports list and new subcommands. Aligned with commands/skills.go.
 */

import type { Dirent } from "node:fs";
import { readdir, stat, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";

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

/** Read the description field from a SKILL.md front-matter (if present). */
async function readSkillDescription(skillMd: string): Promise<string | undefined> {
  try {
    const { readFile } = await import("node:fs/promises");
    const content = await readFile(skillMd, "utf8");
    // Simple frontmatter parse: look for description: value
    const match = content.match(/^description:\s*(.+)$/m);
    if (match) {
      return match[1].trim().replace(/^['"]|['"]$/g, "");
    }
  } catch {
    // ignore
  }
  return undefined;
}

/**
 * List skills in the given scope (default: global).
 */
export async function runSkillsList(scope = "global", opts: SkillsOptions = {}): Promise<SkillsListResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const skillsDir = join(home, "skills", scope);
  const skills: SkillEntry[] = [];

  let entries: Dirent[];
  try {
    entries = await readdir(skillsDir, { withFileTypes: true });
  } catch {
    return { scope, skills };
  }

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const skillPath = join(skillsDir, entry.name);
    const skillMd = join(skillPath, "SKILL.md");
    let hasSkillMd = false;
    let description: string | undefined;
    try {
      await stat(skillMd);
      hasSkillMd = true;
      description = await readSkillDescription(skillMd);
    } catch {
      hasSkillMd = false;
    }
    skills.push({ name: entry.name, description, hasSkillMd });
  }

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
