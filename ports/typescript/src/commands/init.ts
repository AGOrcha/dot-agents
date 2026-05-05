/**
 * `dot-agents init` — Initialize the ~/.agents/ directory structure.
 *
 * Creates standard subdirectories. Safe to run multiple times (existing dirs preserved).
 * Aligned with commands/init.go.
 */

import { mkdir, stat } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";
import { CANONICAL_BUCKET_SPECS } from "../platforms/canonical-buckets.js";

export interface InitOptions {
  /** Print what would be done without making changes. */
  dryRun?: boolean;
  /** Force reinitialization even if ~/.agents/ already exists. */
  force?: boolean;
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export interface InitResult {
  alreadyExists: boolean;
  created: string[];
  skipped: string[];
}

/** Standard directories created by `dot-agents init`. */
export function standardDirs(home: string): string[] {
  const dirs = [
    home,
    join(home, "resources"),
    join(home, "rules", "global"),
    join(home, "settings", "global"),
    join(home, "mcp", "global"),
    join(home, "skills", "global", "agent-start"),
    join(home, "skills", "global", "agent-handoff"),
    join(home, "skills", "global", "self-review"),
    join(home, "agents", "global"),
    join(home, "hooks", "global"),
  ];
  // Match commands/init.go: one global scope dir per canonical store bucket (Stage 1 + Stage 2).
  for (const spec of CANONICAL_BUCKET_SPECS) {
    dirs.push(join(home, spec.name, "global"));
  }
  return dirs;
}

/** Check if home directory exists and is a directory. */
async function checkHomeExists(home: string): Promise<boolean> {
  try {
    const s = await stat(home);
    return s.isDirectory();
  } catch {
    return false;
  }
}

/** Handle the case where home already exists and force is not set. */
function handleExistingHome(home: string, result: InitResult): InitResult {
  for (const dir of standardDirs(home)) {
    result.skipped.push(dir);
  }
  return result;
}

/** Create a single directory, handling EEXIST gracefully. */
async function createDir(dir: string, result: InitResult): Promise<void> {
  try {
    await mkdir(dir, { recursive: true });
    result.created.push(dir);
  } catch (e) {
    const err = e as NodeJS.ErrnoException;
    if (err.code !== "EEXIST") throw e;
    result.skipped.push(dir);
  }
}

/** Create a single directory in dry-run mode (no-op). */
async function createDirDryRun(dir: string, result: InitResult): Promise<void> {
  result.created.push(dir);
}

/** Run the init command. Returns a result summary. */
export async function runInit(opts: InitOptions = {}): Promise<InitResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const result: InitResult = { alreadyExists: false, created: [], skipped: [] };

  const homeExists = await checkHomeExists(home);
  if (homeExists) {
    result.alreadyExists = true;
    if (!opts.force) {
      return handleExistingHome(home, result);
    }
  }

  const dirs = standardDirs(home);
  if (opts.dryRun) {
    return runInitDryRun(dirs, result);
  }
  return runInitExecute(dirs, result);
}

/** Execute init with actual directory creation. */
async function runInitExecute(dirs: string[], result: InitResult): Promise<InitResult> {
  for (const dir of dirs) {
    await createDir(dir, result);
  }
  return result;
}

/** Execute init in dry-run mode (no filesystem changes). */
async function runInitDryRun(dirs: string[], result: InitResult): Promise<InitResult> {
  for (const dir of dirs) {
    await createDirDryRun(dir, result);
  }
  return result;
}
