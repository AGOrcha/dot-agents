/**
 * `dot-agents status` — Report project health and canonical store summary.
 *
 * Reads config.json, checks each project path's existence, and summarizes the
 * canonical store directories. Aligned with the read-only portion of
 * commands/status.go (git, project list, canonical dir counts).
 */

import { stat, readdir } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome, loadConfig, listProjects, getProjectPath } from "../core/config.js";

export interface StatusOptions {
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export type ProjectStatus = "ok" | "missing_path" | "no_agentsrc";

export interface StatusProject {
  name: string;
  path: string;
  pathExists: boolean;
  agentsRcFound: boolean;
}

export interface CanonicalStoreEntry {
  bucket: string;
  scopes: number;
  items: number;
}

export interface StatusResult {
  agentsHome: string;
  agentsHomeExists: boolean;
  configExists: boolean;
  projects: StatusProject[];
  canonicalStore: CanonicalStoreEntry[];
}

const STORE_BUCKETS = ["agents", "rules", "skills", "hooks", "mcp", "settings", "resources"] as const;

/** Count direct subdirectories of a directory (returns 0 if dir doesn't exist). */
async function countSubdirs(dir: string): Promise<number> {
  try {
    const entries = await readdir(dir, { withFileTypes: true });
    return entries.filter((e) => e.isDirectory()).length;
  } catch {
    return 0;
  }
}

/** Count items (any entry) inside all scope subdirs of a bucket directory. */
async function countBucketItems(bucketDir: string): Promise<{ scopes: number; items: number }> {
  let scopes = 0;
  let items = 0;
  let scopeDirs: string[];
  try {
    const entries = await readdir(bucketDir, { withFileTypes: true });
    scopeDirs = entries.filter((e) => e.isDirectory()).map((e) => e.name);
    scopes = scopeDirs.length;
  } catch {
    return { scopes: 0, items: 0 };
  }
  for (const scope of scopeDirs) {
    items += await countSubdirs(join(bucketDir, scope));
  }
  return { scopes, items };
}

export async function runStatus(opts: StatusOptions = {}): Promise<StatusResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();

  // Check ~/.agents/ directory exists
  let agentsHomeExists = false;
  try {
    const s = await stat(home);
    agentsHomeExists = s.isDirectory();
  } catch {
    agentsHomeExists = false;
  }

  // Check config.json
  let configExists = false;
  try {
    await stat(join(home, "config.json"));
    configExists = true;
  } catch {
    configExists = false;
  }

  // Load config and check projects
  const cfg = await loadConfig(home);
  const names = listProjects(cfg);
  const projects: StatusProject[] = [];

  for (const name of names) {
    const path = getProjectPath(cfg, name) ?? "";
    let pathExists = false;
    let agentsRcFound = false;
    try {
      await stat(path);
      pathExists = true;
      try {
        await stat(join(path, ".agentsrc.json"));
        agentsRcFound = true;
      } catch {
        agentsRcFound = false;
      }
    } catch {
      pathExists = false;
    }
    projects.push({ name, path, pathExists, agentsRcFound });
  }

  // Canonical store summary
  const canonicalStore: CanonicalStoreEntry[] = [];
  for (const bucket of STORE_BUCKETS) {
    const bucketDir = join(home, bucket);
    const { scopes, items } = await countBucketItems(bucketDir);
    canonicalStore.push({ bucket, scopes, items });
  }

  return { agentsHome: home, agentsHomeExists, configExists, projects, canonicalStore };
}
