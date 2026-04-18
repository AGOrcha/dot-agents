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
import { CANONICAL_BUCKET_SPECS } from "../platforms/canonical-buckets.js";

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

/**
 * Match commands/status.go summarizeCanonicalScope + summarizeCanonicalBucket
 * (marker dirs vs loose files per bucket).
 */
async function summarizeCanonicalScope(
  scopePath: string,
  countDirs: boolean,
  markerFile: string,
): Promise<number> {
  let entries;
  try {
    entries = await readdir(scopePath, { withFileTypes: true });
  } catch {
    return 0;
  }
  if (countDirs) {
    let count = 0;
    for (const e of entries) {
      if (!e.isDirectory()) continue;
      const dirPath = join(scopePath, e.name);
      try {
        await stat(join(dirPath, markerFile));
        count++;
      } catch {
        /* no marker */
      }
    }
    return count;
  }
  return entries.filter((e) => !e.isDirectory()).length;
}

async function summarizeCanonicalBucket(
  root: string,
  countDirs: boolean,
  markerFile: string,
): Promise<{ scopes: number; items: number }> {
  let scopeEntries;
  try {
    scopeEntries = await readdir(root, { withFileTypes: true });
  } catch {
    return { scopes: 0, items: 0 };
  }
  let scopeCount = 0;
  let itemCount = 0;
  for (const e of scopeEntries) {
    if (!e.isDirectory()) continue;
    const scopePath = join(root, e.name);
    const n = await summarizeCanonicalScope(scopePath, countDirs, markerFile);
    if (n > 0) {
      scopeCount++;
      itemCount += n;
    }
  }
  return { scopes: scopeCount, items: itemCount };
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

  // Canonical store summary (parity with platform.CanonicalStoreBucketSpecs + summarizeCanonicalBucket)
  const canonicalStore: CanonicalStoreEntry[] = [];
  for (const spec of CANONICAL_BUCKET_SPECS) {
    const bucketDir = join(home, spec.name);
    const { scopes, items } = await summarizeCanonicalBucket(
      bucketDir,
      spec.countDirs,
      spec.markerFile,
    );
    canonicalStore.push({ bucket: spec.name, scopes, items });
  }

  return { agentsHome: home, agentsHomeExists, configExists, projects, canonicalStore };
}
