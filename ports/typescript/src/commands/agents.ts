/**
 * `dot-agents agents` — Manage agents in ~/.agents/agents/.
 *
 * Supports list and new subcommands. Aligned with commands/agents.go.
 */

import type { Dirent } from "node:fs";
import { readdir, stat, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";

export interface AgentsOptions {
  /** Custom agents home override (used in tests). */
  agentsHomeOverride?: string;
}

export interface AgentEntry {
  name: string;
  description?: string;
  hasAgentMd: boolean;
}

export interface AgentsListResult {
  scope: string;
  agents: AgentEntry[];
}

/** Read description from AGENT.md front-matter if present. */
async function readAgentDescription(agentMd: string): Promise<string | undefined> {
  try {
    const { readFile } = await import("node:fs/promises");
    const content = await readFile(agentMd, "utf8");
    for (const rawLine of content.split("\n")) {
      const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
      if (!line.startsWith("description:")) continue;
      return stripOuterQuotes(line.slice("description:".length).trim());
    }
  } catch {
    // ignore
  }
  return undefined;
}

function stripOuterQuotes(value: string): string {
  if (value.length < 2) return value;
  const first = value[0];
  const last = value[value.length - 1];
  if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
    return value.slice(1, -1);
  }
  return value;
}

/**
 * List agents in the given scope (default: global).
 */
export async function runAgentsList(scope = "global", opts: AgentsOptions = {}): Promise<AgentsListResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const agentsDir = join(home, "agents", scope);
  const agents: AgentEntry[] = [];

  let entries: Dirent[];
  try {
    entries = await readdir(agentsDir, { withFileTypes: true });
  } catch {
    return { scope, agents };
  }

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const agentPath = join(agentsDir, entry.name);
    const agentMd = join(agentPath, "AGENT.md");
    let hasAgentMd = false;
    let description: string | undefined;
    try {
      await stat(agentMd);
      hasAgentMd = true;
      description = await readAgentDescription(agentMd);
    } catch {
      hasAgentMd = false;
    }
    agents.push({ name: entry.name, description, hasAgentMd });
  }

  return { scope, agents };
}

/** Create a new agent directory scaffold. */
export async function runAgentsNew(
  agentName: string,
  scope = "global",
  opts: AgentsOptions = {},
): Promise<{ created: boolean; path: string; alreadyExists: boolean }> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const agentPath = join(home, "agents", scope, agentName);

  try {
    await stat(agentPath);
    return { created: false, path: agentPath, alreadyExists: true };
  } catch {
    // doesn't exist — create it
  }

  await mkdir(agentPath, { recursive: true });
  const agentMd = `---
name: "${agentName}"
description: ""
---

# ${agentName}

<!-- Describe what this agent does -->
`;
  await writeFile(join(agentPath, "AGENT.md"), agentMd, "utf8");
  return { created: true, path: agentPath, alreadyExists: false };
}
