/**
 * Hook event detection aligned with internal/config/agentsrc.go detectHookEvents.
 *
 * Two-pass detection:
 *   1. Canonical bundles: if any <agentsHome>/hooks/<scope>/<name>/HOOK.yaml exists
 *      across project or global scope → return {all: true} (hooks are managed).
 *   2. Legacy settings: read <agentsHome>/settings/<scope>/claude-code.json and
 *      return named events from non-empty hook arrays (project scope before global).
 */

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { readdir, stat } from "node:fs/promises";
import type { StringsOrBool } from "./agentsrc.js";

/**
 * Check whether any canonical hook bundle (directory containing HOOK.yaml) exists
 * under <agentsHome>/hooks/<scope>/.
 */
async function hasCanonicalHookBundles(agentsHome: string, scope: string): Promise<boolean> {
  const hooksDir = join(agentsHome, "hooks", scope);
  let entries: Awaited<ReturnType<typeof readdir>>;
  try {
    entries = await readdir(hooksDir);
  } catch {
    return false;
  }
  for (const name of entries) {
    const hookYaml = join(hooksDir, name, "HOOK.yaml");
    try {
      await stat(hookYaml);
      return true;
    } catch {
      // not a bundle dir — continue
    }
  }
  return false;
}

/**
 * Read named hook events from a legacy claude-code.json settings file.
 * Returns only event names whose array is non-empty (mirrors Go detectHookEvents filter).
 */
async function readLegacyHookEvents(
  agentsHome: string,
  scope: string,
): Promise<string[] | null> {
  const settingsPath = join(agentsHome, "settings", scope, "claude-code.json");
  let data: string;
  try {
    data = await readFile(settingsPath, "utf8");
  } catch {
    return null;
  }

  let settings: Record<string, unknown>;
  try {
    settings = JSON.parse(data) as Record<string, unknown>;
  } catch {
    return null;
  }

  const hooksVal = settings["hooks"];
  if (hooksVal === null || typeof hooksVal !== "object" || Array.isArray(hooksVal)) {
    return null;
  }

  const hooksMap = hooksVal as Record<string, unknown>;
  const events: string[] = [];
  for (const [event, val] of Object.entries(hooksMap)) {
    if (Array.isArray(val) && val.length > 0) {
      events.push(event);
    }
  }

  if (events.length === 0) return null;
  return events.sort();
}

/**
 * Detect hook configuration for a project (parity with Go detectHookEvents).
 *
 * Returns {all: true} if any canonical HOOK.yaml bundle exists in project or global scope.
 * Returns {all: false, names: [...events]} if only a legacy claude-code.json is found.
 * Returns {all: false, names: []} if no hook configuration is found.
 */
export async function detectHookEvents(
  agentsHome: string,
  projectName: string,
): Promise<StringsOrBool> {
  // Pass 1: canonical bundles
  for (const scope of [projectName, "global"]) {
    if (await hasCanonicalHookBundles(agentsHome, scope)) {
      return { all: true, names: [] };
    }
  }

  // Pass 2: legacy settings fallback
  for (const scope of [projectName, "global"]) {
    const events = await readLegacyHookEvents(agentsHome, scope);
    if (events !== null) {
      return { all: false, names: events };
    }
  }

  return { all: false, names: [] };
}
