/**
 * `dot-agents refresh` — Report projects that need refreshing from ~/.agents/.
 *
 * The TypeScript port of `refresh` is scoped to: load config, resolve project
 * paths, and report which projects exist / are missing. Full link re-application
 * requires Go-side internals (links, platform) and is deferred to phase-5.
 *
 * Aligned with commands/refresh.go (read-only status portion).
 */

import { stat } from "node:fs/promises";
import { agentsHome, loadConfig, listProjects, getProjectPath } from "../core/config.js";

export interface RefreshOptions {
  /** Only process this one project (by name). */
  filter?: string;
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export type ProjectRefreshStatus = "ok" | "missing_path" | "not_found";

export interface ProjectRefreshEntry {
  name: string;
  path: string;
  status: ProjectRefreshStatus;
}

export interface RefreshResult {
  projects: ProjectRefreshEntry[];
  noProjects: boolean;
}

/**
 * Report refresh status for all (or one filtered) managed projects.
 */
export async function runRefresh(opts: RefreshOptions = {}): Promise<RefreshResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const cfg = await loadConfig(home);
  const names = listProjects(cfg);

  if (names.length === 0) {
    return { projects: [], noProjects: true };
  }

  const filtered = opts.filter ? names.filter((n) => n === opts.filter) : names;
  const entries: ProjectRefreshEntry[] = [];

  for (const name of filtered) {
    const path = getProjectPath(cfg, name) ?? "";
    let status: ProjectRefreshStatus = "ok";
    if (!path) {
      status = "not_found";
    } else {
      try {
        await stat(path);
      } catch {
        status = "missing_path";
      }
    }
    entries.push({ name, path, status });
  }

  return { projects: entries, noProjects: false };
}
