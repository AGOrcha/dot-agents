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

export async function runDoctor(opts: DoctorOptions = {}): Promise<DoctorResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const checks: DoctorCheck[] = [];

  // Check ~/.agents/
  try {
    const s = await stat(home);
    if (s.isDirectory()) {
      checks.push({ name: "agents_home", status: "ok", message: "~/.agents/ exists" });
    } else {
      checks.push({ name: "agents_home", status: "error", message: "~/.agents/ exists but is not a directory" });
    }
  } catch {
    checks.push({
      name: "agents_home",
      status: "error",
      message: "~/.agents/ not found — run: dot-agents init",
    });
  }

  // Check config.json
  const cfgPath = join(home, "config.json");
  try {
    await stat(cfgPath);
    checks.push({ name: "config_json", status: "ok", message: "config.json exists" });
  } catch {
    checks.push({ name: "config_json", status: "warn", message: "config.json not found" });
  }

  // Check managed project paths
  const cfg = await loadConfig(home);
  const names = listProjects(cfg);
  const projects: ProjectDoctorEntry[] = [];

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

    if (!pathExists) {
      checks.push({
        name: `project:${name}`,
        status: "warn",
        message: `Project "${name}" path not found: ${path}`,
      });
    } else if (!agentsRcFound) {
      checks.push({
        name: `project:${name}`,
        status: "warn",
        message: `Project "${name}" has no .agentsrc.json`,
      });
    } else {
      if (opts.verbose) {
        checks.push({
          name: `project:${name}`,
          status: "ok",
          message: `Project "${name}" healthy`,
        });
      }
    }

    projects.push({ name, path, pathExists, agentsRcFound });
  }

  const ok = checks.every((c) => c.status !== "error");
  return { checks, projects, ok };
}
