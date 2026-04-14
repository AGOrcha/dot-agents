/**
 * Config and paths utilities aligned with internal/config/config.go and paths.go.
 *
 * Provides AgentsHome resolution (AGENTS_HOME env override, Windows APPDATA fallback),
 * and a Config type + loader matching the ~/.agents/config.json structure.
 */

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import { platform } from "node:process";

/**
 * Returns the path to the ~/.agents directory.
 * Respects AGENTS_HOME env override. On Windows, falls back to %APPDATA%\.agents.
 */
export function agentsHome(): string {
  const override = process.env["AGENTS_HOME"];
  if (override) return override;

  if (platform === "win32") {
    const appData = process.env["APPDATA"];
    if (appData) return join(appData, ".agents");
  }

  return join(homedir(), ".agents");
}

/** Returns the current user's home directory. */
export function userHome(): string {
  return homedir();
}

/** Expand a path with ~ to the full absolute path. */
export function expandPath(p: string): string {
  if (p === "~") return homedir();
  if (p.startsWith("~/") || p.startsWith("~\\")) {
    return join(homedir(), p.slice(2));
  }
  return p;
}

/** Converts an absolute path to a ~ prefixed display path. */
export function displayPath(p: string): string {
  const home = homedir();
  if (p.startsWith(home)) {
    return "~" + p.slice(home.length);
  }
  return p;
}

// -------------------------
// Config types and loader
// -------------------------

export interface ConfigProject {
  path: string;
  added: string; // ISO date string
}

export interface ConfigAgent {
  enabled: boolean;
  version?: string;
}

export interface ConfigDefaults {
  agent?: string;
}

export interface ConfigFeatures {
  tasks?: boolean;
  history?: boolean;
  sync?: boolean;
}

export interface Config {
  version: number;
  defaults?: ConfigDefaults;
  projects: Record<string, ConfigProject>;
  agents?: Record<string, ConfigAgent>;
  features?: ConfigFeatures;
}

/** Load ~/.agents/config.json. Returns a default Config if the file does not exist. */
export async function loadConfig(home?: string): Promise<Config> {
  const configPath = join(home ?? agentsHome(), "config.json");
  let data: string;
  try {
    data = await readFile(configPath, "utf8");
  } catch {
    return { version: 1, projects: {}, agents: {} };
  }
  const cfg = JSON.parse(data) as Config;
  if (!cfg.projects) cfg.projects = {};
  if (!cfg.agents) cfg.agents = {};
  return cfg;
}

/** Save config to ~/.agents/config.json. */
export async function saveConfig(cfg: Config, home?: string): Promise<void> {
  const configPath = join(home ?? agentsHome(), "config.json");
  await mkdir(dirname(configPath), { recursive: true });
  await writeFile(configPath, JSON.stringify(cfg, null, "  ") + "\n", "utf8");
}

/** List all registered project names in sorted order. */
export function listProjects(cfg: Config): string[] {
  return Object.keys(cfg.projects).sort();
}

/** Add a project entry to the config. */
export function addProject(cfg: Config, name: string, path: string): void {
  cfg.projects[name] = { path, added: new Date().toISOString() };
}

/** Remove a project from the config. */
export function removeProject(cfg: Config, name: string): void {
  delete cfg.projects[name];
}

/** Get the path for a registered project, or undefined. */
export function getProjectPath(cfg: Config, name: string): string | undefined {
  return cfg.projects[name]?.path;
}
