/**
 * Minimal .agentsrc.json handling aligned with internal/config/agentsrc.go:
 * known keys map into typed fields; other top-level keys are kept in extraFields
 * and merged back on serialize so they are not dropped (see TestAgentsRCUnknownFieldsRoundtrip).
 */

import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

export const AGENTS_RC_FILE = ".agentsrc.json";

/** Keys owned by AgentsRC in Go (agentsRCKnown). */
export const AGENTS_RC_KNOWN_KEYS = new Set([
  "$schema",
  "version",
  "project",
  "skills",
  "rules",
  "agents",
  "hooks",
  "mcp",
  "settings",
  "sources",
]);

export interface Source {
  type: string;
  path?: string;
  url?: string;
  ref?: string;
}

/** Mirrors config.StringsOrBool semantics (bool or string array in JSON). */
export interface StringsOrBool {
  all: boolean;
  names: string[];
}

export interface AgentsRc {
  schema?: string;
  version: number;
  project?: string;
  skills?: string[];
  rules?: string[];
  agents?: string[];
  hooks: StringsOrBool;
  mcp: StringsOrBool;
  settings: boolean;
  sources: Source[];
  /** Top-level JSON keys not in AGENTS_RC_KNOWN_KEYS — preserved on save. */
  extraFields: Record<string, unknown>;
}

function parseStringsOrBool(raw: unknown, field: string): StringsOrBool {
  if (typeof raw === "boolean") {
    return raw ? { all: true, names: [] } : { all: false, names: [] };
  }
  if (Array.isArray(raw) && raw.every((x) => typeof x === "string")) {
    return { all: false, names: raw as string[] };
  }
  throw new Error(`${field} must be a boolean or string array`);
}

function stringsOrBoolToJson(s: StringsOrBool): boolean | string[] {
  if (s.names.length > 0) {
    return s.names;
  }
  return s.all;
}

function readSources(raw: unknown): Source[] {
  if (raw === undefined) {
    return [];
  }
  if (!Array.isArray(raw)) {
    throw new Error("sources must be an array");
  }
  const out: Source[] = [];
  for (const item of raw) {
    if (item === null || typeof item !== "object" || Array.isArray(item)) {
      throw new Error("each source must be an object");
    }
    const o = item as Record<string, unknown>;
    const type = o.type;
    if (typeof type !== "string") {
      throw new Error('source must have string "type"');
    }
    const s: Source = { type };
    if (typeof o.path === "string") {
      s.path = o.path;
    }
    if (typeof o.url === "string") {
      s.url = o.url;
    }
    if (typeof o.ref === "string") {
      s.ref = o.ref;
    }
    out.push(s);
  }
  return out;
}

function readStringArray(raw: unknown, field: string): string[] | undefined {
  if (raw === undefined) {
    return undefined;
  }
  if (!Array.isArray(raw) || !raw.every((x) => typeof x === "string")) {
    throw new Error(`${field} must be an array of strings`);
  }
  return raw as string[];
}

/** Parse JSON text into AgentsRc, splitting unknown top-level keys into extraFields. */
export function parseAgentsRcJson(text: string): AgentsRc {
  let root: unknown;
  try {
    root = JSON.parse(text) as unknown;
  } catch (e) {
    throw new Error(`parsing ${AGENTS_RC_FILE}: ${e}`);
  }
  if (root === null || typeof root !== "object" || Array.isArray(root)) {
    throw new Error(`${AGENTS_RC_FILE} must contain a JSON object`);
  }
  const all = root as Record<string, unknown>;

  const extraFields: Record<string, unknown> = {};
  for (const key of Object.keys(all)) {
    if (!AGENTS_RC_KNOWN_KEYS.has(key)) {
      extraFields[key] = all[key];
    }
  }

  const version = all.version;
  if (typeof version !== "number" || !Number.isInteger(version)) {
    throw new Error("version must be an integer");
  }

  const project = all.project;
  if (project !== undefined && typeof project !== "string") {
    throw new Error("project must be a string");
  }

  const schema = all.$schema;
  if (schema !== undefined && typeof schema !== "string") {
    throw new Error("$schema must be a string");
  }

  let sources = readSources(all.sources);
  if (sources.length === 0) {
    sources = [{ type: "local" }];
  }

  const hooks =
    all.hooks === undefined ? { all: false, names: [] } : parseStringsOrBool(all.hooks, "hooks");
  const mcp = all.mcp === undefined ? { all: false, names: [] } : parseStringsOrBool(all.mcp, "mcp");

  const rc: AgentsRc = {
    schema,
    version,
    project,
    skills: readStringArray(all.skills, "skills"),
    rules: readStringArray(all.rules, "rules"),
    agents: readStringArray(all.agents, "agents"),
    hooks,
    mcp,
    settings: Boolean(all.settings),
    sources,
    extraFields,
  };

  return rc;
}

type CorePlain = {
  $schema?: string;
  version: number;
  project?: string;
  skills?: string[];
  rules?: string[];
  agents?: string[];
  hooks: boolean | string[];
  mcp: boolean | string[];
  settings: boolean;
  sources: Source[];
};

function toCorePlain(rc: AgentsRc): CorePlain {
  const core: CorePlain = {
    version: rc.version,
    hooks: stringsOrBoolToJson(rc.hooks),
    mcp: stringsOrBoolToJson(rc.mcp),
    settings: rc.settings,
    sources: rc.sources,
  };
  if (rc.schema !== undefined) {
    core.$schema = rc.schema;
  }
  if (rc.project !== undefined) {
    core.project = rc.project;
  }
  if (rc.skills !== undefined && rc.skills.length > 0) {
    core.skills = rc.skills;
  }
  if (rc.rules !== undefined && rc.rules.length > 0) {
    core.rules = rc.rules;
  }
  if (rc.agents !== undefined && rc.agents.length > 0) {
    core.agents = rc.agents;
  }
  return core;
}

/**
 * Serialize AgentsRc to formatted JSON (2-space indent, trailing newline), matching Go Save/MarshalIndent.
 * Unknown keys are merged like AgentsRC.MarshalJSON: only add an extra key if the core object does not already define it.
 */
export function serializeAgentsRc(rc: AgentsRc): string {
  const core = toCorePlain(rc);
  const data = JSON.stringify(core);
  const m = JSON.parse(data) as Record<string, unknown>;
  for (const [k, v] of Object.entries(rc.extraFields)) {
    if (!(k in m)) {
      m[k] = v;
    }
  }
  return `${JSON.stringify(m, null, 2)}\n`;
}

export async function loadAgentsRc(projectPath: string): Promise<AgentsRc> {
  const path = join(projectPath, AGENTS_RC_FILE);
  const text = await readFile(path, "utf8");
  return parseAgentsRcJson(text);
}

export async function saveAgentsRc(projectPath: string, rc: AgentsRc): Promise<void> {
  const path = join(projectPath, AGENTS_RC_FILE);
  await writeFile(path, serializeAgentsRc(rc), "utf8");
}
