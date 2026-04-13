/**
 * Codex agent TOML rendering aligned with internal/platform/codex.go renderCodexAgentToml.
 *
 * Reads an AGENT.md file, parses the YAML frontmatter for name/description/model,
 * strips the frontmatter to get the body, and emits a .toml file using
 * `developer_instructions` (not `is_background`) for the body content.
 */

import { readFile } from "node:fs/promises";

/**
 * Parse simple key: value pairs from a `---` delimited frontmatter block.
 * Strips surrounding quotes from values (mirrors Go readFrontmatter).
 * Returns an empty map if there is no frontmatter.
 */
export function parseFrontmatter(content: string): Record<string, string> {
  const normalized = content.replace(/\r\n/g, "\n");
  if (!normalized.startsWith("---\n")) {
    return {};
  }
  const rest = normalized.slice("---\n".length);
  const endIdx = rest.indexOf("\n---\n");
  const block = endIdx === -1 ? rest : rest.slice(0, endIdx);

  const result: Record<string, string> = {};
  for (const line of block.split("\n")) {
    const colonIdx = line.indexOf(":");
    if (colonIdx === -1) continue;
    const key = line.slice(0, colonIdx).trim();
    const rawVal = line.slice(colonIdx + 1).trim();
    result[key] = rawVal.replace(/^['"]|['"]$/g, "");
  }
  return result;
}

/**
 * Extract the body of an AGENT.md: everything after the closing `---` of the
 * frontmatter block, with leading blank lines trimmed.
 * If there is no frontmatter the entire content is returned (mirrors Go readAgentBody).
 */
export function extractAgentBody(content: string): string {
  const normalized = content.replace(/\r\n/g, "\n");
  if (!normalized.startsWith("---\n")) {
    return normalized;
  }
  const rest = normalized.slice("---\n".length);
  const endIdx = rest.indexOf("\n---\n");
  if (endIdx === -1) {
    return normalized;
  }
  return rest.slice(endIdx + "\n---\n".length).replace(/^\n+/, "");
}

/**
 * Wrap a string in a TOML multiline literal (`"""`), escaping backslashes and
 * embedded triple-quotes (mirrors Go tomlMultilineString).
 */
export function tomlMultilineString(value: string): string {
  const escaped = value.replace(/\\/g, "\\\\").replace(/"""/g, '\\"\\"\\"');
  return `"""\n${escaped}\n"""`;
}

/**
 * Render an AGENT.md file to Codex-compatible TOML content.
 *
 * Output fields:
 *   name         = "<frontmatter name or dir-derived fallback>"
 *   description  = "<frontmatter description>"
 *   model        = "<frontmatter model>"   (omitted if not set)
 *   developer_instructions = """..."""     (omitted if body is blank)
 *
 * `is_background` is intentionally NOT emitted — Codex uses `developer_instructions`
 * for agent prompt content (parity with Go renderCodexAgentToml contract, proven by
 * TestRenderCodexAgentTomlUsesFrontmatterAndBody).
 */
export async function renderCodexAgentToml(agentMDPath: string): Promise<string> {
  const raw = await readFile(agentMDPath, "utf8");
  return renderCodexAgentTomlFromContent(raw, agentMDPath);
}

/**
 * Pure (non-IO) version of renderCodexAgentToml — accepts file content directly.
 * Exported for testing without disk access.
 */
export function renderCodexAgentTomlFromContent(content: string, agentMDPath: string): string {
  const meta = parseFrontmatter(content);
  const body = extractAgentBody(content);

  let name = (meta["name"] ?? "").trim();
  if (name === "") {
    // Derive from the parent directory name, stripping extension
    const parts = agentMDPath.replace(/\\/g, "/").split("/");
    const dirName = parts.at(-2) ?? parts.at(-1) ?? "unknown";
    name = dirName.replace(/\.[^.]+$/, "");
  }

  const description = (meta["description"] ?? "").trim();
  const model = (meta["model"] ?? "").trim();

  const lines: string[] = [];
  lines.push(`name = ${JSON.stringify(name)}`);
  lines.push(`description = ${JSON.stringify(description)}`);
  if (model !== "") {
    lines.push(`model = ${JSON.stringify(model)}`);
  }
  if (body.trim() !== "") {
    lines.push(`developer_instructions = ${tomlMultilineString(body)}`);
  }
  return lines.join("\n") + "\n";
}
