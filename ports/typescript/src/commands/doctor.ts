/**
 * `dot-agents doctor` — Audit the local dot-agents installation.
 *
 * Checks ~/.agents/ existence, config.json, and managed project links.
 * Aligned with the structural checks in commands/doctor.go.
 */

import { stat } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome, loadConfig, listProjects, getProjectPath } from "../core/config.js";

export interface DoctorOptions {
  verbose?: boolean;
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export type DoctorCheckStatus = "ok" | "warn" | "error";

export interface DoctorCheck {
  name: string;
  status: DoctorCheckStatus;
  message: string;
}

export interface ProjectDoctorEntry {
  name: string;
  path: string;
  pathExists: boolean;
  agentsRcFound: boolean;
}

export interface DoctorResult {
  checks: DoctorCheck[];
  projects: ProjectDoctorEntry[];
  ok: boolean;
}

async function checkAgentsHome(home: string): Promise<DoctorCheck> {
  try {
    const s = await stat(home);
    if (s.isDirectory()) {
      return { name: "agents_home", status: "ok", message: "~/.agents/ exists" };
    }
    return { name: "agents_home", status: "error", message: "~/.agents/ exists but is not a directory" };
  } catch {
    return {
      name: "agents_home",
      status: "error",
      message: "~/.agents/ not found — run: dot-agents init",
    };
  }
}

async function checkConfigJson(home: string): Promise<DoctorCheck> {
  const cfgPath = join(home, "config.json");
  try {
    await stat(cfgPath);
    return { name: "config_json", status: "ok", message: "config.json exists" };
  } catch {
    return { name: "config_json", status: "warn", message: "config.json not found" };
  }
}

async function checkProjectAgentsRc(
  name: string,
  path: string,
): Promise<{ pathExists: boolean; agentsRcFound: boolean }> {
  try {
    await stat(path);
    try {
      await stat(join(path, ".agentsrc.json"));
      return { pathExists: true, agentsRcFound: true };
    } catch {
      return { pathExists: true, agentsRcFound: false };
    }
  } catch {
    return { pathExists: false, agentsRcFound: false };
  }
}

function createProjectCheck(name: string, path: string, pathExists: boolean, agentsRcFound: boolean, verbose: boolean): DoctorCheck | null {
  if (pathExists && agentsRcFound && !verbose) return null;
  if (pathExists && agentsRcFound && verbose) {
    return { name: `project:${name}`, status: "ok", message: `Project "${name}" healthy` };
  }
  if (pathExists && !agentsRcFound) {
    return { name: `project:${name}`, status: "warn", message: `Project "${name}" has no .agentsrc.json` };
  }
  return { name: `project:${name}`, status: "warn", message: `Project "${name}" path not found: ${path}` };
}

export async function runDoctor(opts: DoctorOptions = {}): Promise<DoctorResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const checks: DoctorCheck[] = [];

  checks.push(await checkAgentsHome(home));
  checks.push(await checkConfigJson(home));

  const cfg = await loadConfig(home);
  const names = listProjects(cfg);
  const projects: ProjectDoctorEntry[] = [];

  for (const name of names) {
    const path = getProjectPath(cfg, name) ?? "";
    const { pathExists, agentsRcFound } = await checkProjectAgentsRc(name, path);
    const projectCheck = createProjectCheck(name, path, pathExists, agentsRcFound, opts.verbose ?? false);
    if (projectCheck) {
      checks.push(projectCheck);
    }
    projects.push({ name, path, pathExists, agentsRcFound });
  }

  const ok = checks.every((c) => c.status !== "error");
  return { checks, projects, ok };
}
