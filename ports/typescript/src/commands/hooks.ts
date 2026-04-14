/**
 * `dot-agents hooks` — Inspect hook definitions in ~/.agents/.
 *
 * Supports `list` subcommand: reads canonical HOOK.yaml bundles first,
 * then falls back to legacy claude-code.json settings.
 * Aligned with commands/hooks.go listHooks / listCanonicalHooks.
 */

import { readdir, readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";

export interface HooksOptions {
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export interface HookBundle {
  name: string;
  path: string;
}

export interface LegacyHookEvent {
  event: string;
  count: number;
}

export type HookSourceKind = "canonical" | "legacy" | "none";

export interface HooksListResult {
  scope: string;
  kind: HookSourceKind;
  bundles?: HookBundle[];
  legacyEvents?: LegacyHookEvent[];
}

/**
 * List canonical HOOK.yaml bundles under <home>/hooks/<scope>/.
 * Returns undefined if the directory doesn't exist or has no bundles.
 */
async function listCanonicalBundles(hooksDir: string): Promise<HookBundle[] | undefined> {
  let entries: Awaited<ReturnType<typeof readdir>>;
  try {
    entries = await readdir(hooksDir, { withFileTypes: true });
  } catch {
    return undefined;
  }

  const bundles: HookBundle[] = [];
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const bundlePath = join(hooksDir, entry.name);
    const hookYaml = join(bundlePath, "HOOK.yaml");
    try {
      await stat(hookYaml);
      bundles.push({ name: entry.name, path: bundlePath });
    } catch {
      // not a canonical bundle
    }
  }

  return bundles.length > 0 ? bundles : undefined;
}

/**
 * List legacy hook events from claude-code.json for the given scope.
 */
async function listLegacyEvents(
  home: string,
  scope: string,
): Promise<LegacyHookEvent[] | undefined> {
  const settingsPath = join(home, "settings", scope, "claude-code.json");
  let data: string;
  try {
    data = await readFile(settingsPath, "utf8");
  } catch {
    return undefined;
  }

  let settings: Record<string, unknown>;
  try {
    settings = JSON.parse(data) as Record<string, unknown>;
  } catch {
    return undefined;
  }

  const hooksVal = settings["hooks"];
  if (hooksVal === null || typeof hooksVal !== "object" || Array.isArray(hooksVal)) {
    return undefined;
  }

  const hooksMap = hooksVal as Record<string, unknown>;
  const events: LegacyHookEvent[] = [];
  for (const [event, val] of Object.entries(hooksMap)) {
    if (Array.isArray(val) && val.length > 0) {
      events.push({ event, count: val.length });
    }
  }
  events.sort((a, b) => a.event.localeCompare(b.event));
  return events.length > 0 ? events : undefined;
}

/**
 * List hooks for the given scope (default: global).
 * Prefers canonical HOOK.yaml bundles; falls back to legacy claude-code.json.
 */
export async function runHooksList(scope = "global", opts: HooksOptions = {}): Promise<HooksListResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const hooksDir = join(home, "hooks", scope);

  const bundles = await listCanonicalBundles(hooksDir);
  if (bundles !== undefined) {
    return { scope, kind: "canonical", bundles };
  }

  const legacyEvents = await listLegacyEvents(home, scope);
  if (legacyEvents !== undefined) {
    return { scope, kind: "legacy", legacyEvents };
  }

  return { scope, kind: "none" };
}
