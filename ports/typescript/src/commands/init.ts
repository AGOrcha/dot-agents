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

/** Run the init command. Returns a result summary. */
export async function runInit(opts: InitOptions = {}): Promise<InitResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const result: InitResult = { alreadyExists: false, created: [], skipped: [] };

  let homeExists = false;
  try {
    const s = await stat(home);
    homeExists = s.isDirectory();
  } catch {
    homeExists = false;
  }

  if (homeExists) {
    result.alreadyExists = true;
    if (!opts.force) {
      // Dry-run still reports dirs as skipped
      for (const dir of standardDirs(home)) {
        result.skipped.push(dir);
      }
      return result;
    }
  }

  const dirs = standardDirs(home);
  for (const dir of dirs) {
    if (opts.dryRun) {
      result.created.push(dir);
    } else {
      try {
        await mkdir(dir, { recursive: true });
        result.created.push(dir);
      } catch (e) {
        // If directory already exists that's fine; anything else is unexpected
        const err = e as NodeJS.ErrnoException;
        if (err.code !== "EEXIST") throw e;
        result.skipped.push(dir);
      }
    }
  }
  return result;
}
