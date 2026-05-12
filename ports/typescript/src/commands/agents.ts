/**
 * `da agents` — Manage agents in ~/.agents/agents/.
 *
 * Supports list and new subcommands. Aligned with commands/agents.go.
 */

import { mkdir, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { agentsHome } from "../core/config.js";
import { listBucket, type BucketEntry } from "../lib/projectsync.js";

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

/**
 * List agents in the given scope (default: global).
 */
export async function runAgentsList(scope = "global", opts: AgentsOptions = {}): Promise<AgentsListResult> {
  const home = opts.agentsHomeOverride ?? agentsHome();
  const entries = await listBucket(home, scope, { bucket: "agents", manifestName: "AGENT.md" });
  const agents: AgentEntry[] = entries.map((e: BucketEntry) => ({
    name: e.name,
    description: e.description,
    hasAgentMd: e.hasManifest,
  }));
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
