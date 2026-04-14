/**
 * `dot-agents add` — Register a project directory in config.json.
 *
 * Resolves the absolute path, registers it under the given (or derived) name,
 * and writes back config.json. Aligned with commands/add.go.
 */

import { stat } from "node:fs/promises";
import { resolve, basename } from "node:path";
import {
  agentsHome,
  loadConfig,
  saveConfig,
  addProject,
  getProjectPath,
} from "../core/config.js";

export interface AddOptions {
  /** Explicit project name (defaults to directory basename). */
  name?: string;
  /** Overwrite if project already registered. */
  force?: boolean;
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export type AddStatus = "added" | "already_registered" | "path_not_found" | "updated";

export interface AddResult {
  status: AddStatus;
  name: string;
  path: string;
}

/**
 * Register a project directory.
 *
 * @param projectPath Absolute or relative path to the project directory.
 * @param opts Options.
 */
export async function runAdd(projectPath: string, opts: AddOptions = {}): Promise<AddResult> {
  const absPath = resolve(projectPath);
  const home = opts.agentsHomeOverride ?? agentsHome();

  // Verify path exists
  try {
    await stat(absPath);
  } catch {
    return { status: "path_not_found", name: "", path: absPath };
  }

  const name = opts.name ?? basename(absPath);
  const cfg = await loadConfig(home);

  const existing = getProjectPath(cfg, name);
  if (existing !== undefined && !opts.force) {
    return { status: "already_registered", name, path: existing };
  }

  const status: AddStatus = existing !== undefined ? "updated" : "added";
  addProject(cfg, name, absPath);
  await saveConfig(cfg, home);
  return { status, name, path: absPath };
}
