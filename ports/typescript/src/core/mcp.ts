/**
 * MCP server detection aligned with internal/config/agentsrc.go detectMCPServers / readMCPScope.
 *
 * Scans ~/.agents/mcp/<scope>/ for claude.json, mcp.json, or .mcp.json and
 * extracts server names from either the `servers` or `mcpServers` top-level key.
 * Project scope is tried before global; the first non-empty result wins.
 */

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { StringsOrBool } from "./agentsrc.js";

/** File names tried in order within each scope directory (mirrors Go readMCPScope). */
const MCP_FILE_CANDIDATES = ["claude.json", "mcp.json", ".mcp.json"] as const;

/**
 * Read one scope directory under <agentsHome>/mcp/<scope>/ and return named servers.
 * Tries claude.json, mcp.json, .mcp.json in order; stops at the first readable file.
 * Returns empty StringsOrBool if no file is found or no servers are listed.
 */
export async function readMCPScope(agentsHome: string, scope: string): Promise<StringsOrBool> {
  for (const fname of MCP_FILE_CANDIDATES) {
    const mcpPath = join(agentsHome, "mcp", scope, fname);
    let data: string;
    try {
      data = await readFile(mcpPath, "utf8");
    } catch {
      continue;
    }

    let mcpConfig: Record<string, unknown>;
    try {
      mcpConfig = JSON.parse(data) as Record<string, unknown>;
    } catch {
      continue;
    }

    // Try `servers` first (canonical), fall back to `mcpServers` (documented shape)
    let serversVal = mcpConfig["servers"];
    if (serversVal === null || typeof serversVal !== "object" || Array.isArray(serversVal)) {
      serversVal = mcpConfig["mcpServers"];
    }
    if (serversVal === null || typeof serversVal !== "object" || Array.isArray(serversVal)) {
      // File found but no usable servers key — stop trying other file names for this scope
      break;
    }

    const names = Object.keys(serversVal as Record<string, unknown>).sort();
    if (names.length > 0) {
      return { all: false, names };
    }
    // Found a file but zero server entries — stop trying other file names
    break;
  }
  return { all: false, names: [] };
}

/**
 * Detect MCP servers for a project by scanning project scope then global scope.
 * Returns the first non-empty StringsOrBool found, or an empty one if neither scope
 * has a readable MCP config (parity with Go detectMCPServers).
 */
export async function detectMCPServers(
  agentsHome: string,
  projectName: string,
): Promise<StringsOrBool> {
  for (const scope of [projectName, "global"]) {
    const result = await readMCPScope(agentsHome, scope);
    if (result.names.length > 0 || result.all) {
      return result;
    }
  }
  return { all: false, names: [] };
}
