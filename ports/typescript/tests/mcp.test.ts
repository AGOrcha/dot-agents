import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { mkdtemp } from "node:fs/promises";
import { describe, expect, it } from "vitest";
import { detectMCPServers, readMCPScope } from "../src/index.js";

async function makeTmp(): Promise<string> {
  return mkdtemp(join(tmpdir(), "mcp-ts-"));
}

describe("readMCPScope", () => {
  it("reads `servers` key and returns sorted names (parity with TestGenerateAgentsRCMCPNamedServers)", async () => {
    const tmp = await makeTmp();
    const scopeDir = join(tmp, "mcp", "myproject");
    await mkdir(scopeDir, { recursive: true });
    await writeFile(
      join(scopeDir, "mcp.json"),
      JSON.stringify({ servers: { "server-b": {}, "server-a": {} } }),
    );

    const result = await readMCPScope(tmp, "myproject");
    expect(result.all).toBe(false);
    expect(result.names).toEqual(["server-a", "server-b"]);
  });

  it("reads `mcpServers` key as fallback (parity with TestGenerateAgentsRCMCPReadsDocumentedMCPServersShape)", async () => {
    const tmp = await makeTmp();
    const scopeDir = join(tmp, "mcp", "myproject");
    await mkdir(scopeDir, { recursive: true });
    await writeFile(
      join(scopeDir, "mcp.json"),
      JSON.stringify({ mcpServers: { "code-review-graph": {}, sonarqube: {} } }),
    );

    const result = await readMCPScope(tmp, "myproject");
    expect(result.names).toEqual(["code-review-graph", "sonarqube"]);
  });

  it("reads .mcp.json filename (parity with TestGenerateAgentsRCMCPReadsDotMCPJSON)", async () => {
    const tmp = await makeTmp();
    const scopeDir = join(tmp, "mcp", "myproject");
    await mkdir(scopeDir, { recursive: true });
    await writeFile(
      join(scopeDir, ".mcp.json"),
      JSON.stringify({ mcpServers: { "repo-srv": {} } }),
    );

    const result = await readMCPScope(tmp, "myproject");
    expect(result.names).toEqual(["repo-srv"]);
  });

  it("returns empty when no MCP file exists", async () => {
    const tmp = await makeTmp();
    const result = await readMCPScope(tmp, "missing-scope");
    expect(result.all).toBe(false);
    expect(result.names).toEqual([]);
  });
});

describe("detectMCPServers", () => {
  it("uses project scope before global (parity with TestGenerateAgentsRCMCPNamedServers)", async () => {
    const tmp = await makeTmp();
    const projectDir = join(tmp, "mcp", "myproject");
    const globalDir = join(tmp, "mcp", "global");
    await mkdir(projectDir, { recursive: true });
    await mkdir(globalDir, { recursive: true });
    await writeFile(
      join(projectDir, "mcp.json"),
      JSON.stringify({ servers: { "server-a": {}, "server-b": {} } }),
    );
    await writeFile(
      join(globalDir, "mcp.json"),
      JSON.stringify({ servers: { "global-srv": {} } }),
    );

    const result = await detectMCPServers(tmp, "myproject");
    expect(result.names).toEqual(["server-a", "server-b"]);
  });

  it("falls back to global when no project scope (parity with TestGenerateAgentsRCMCPFallsBackToGlobal)", async () => {
    const tmp = await makeTmp();
    const globalDir = join(tmp, "mcp", "global");
    await mkdir(globalDir, { recursive: true });
    await writeFile(
      join(globalDir, "mcp.json"),
      JSON.stringify({ servers: { "global-srv": {} } }),
    );

    const result = await detectMCPServers(tmp, "myproject");
    expect(result.names).toEqual(["global-srv"]);
  });

  it("falls back to global using mcpServers shape (parity with TestGenerateAgentsRCMCPFallsBackToGlobalDocumentedMCPServersShape)", async () => {
    const tmp = await makeTmp();
    const globalDir = join(tmp, "mcp", "global");
    await mkdir(globalDir, { recursive: true });
    await writeFile(
      join(globalDir, "mcp.json"),
      JSON.stringify({ mcpServers: { "global-srv": {} } }),
    );

    const result = await detectMCPServers(tmp, "myproject");
    expect(result.names).toEqual(["global-srv"]);
  });

  it("returns empty when no MCP config exists anywhere", async () => {
    const tmp = await makeTmp();
    const result = await detectMCPServers(tmp, "noscope");
    expect(result.all).toBe(false);
    expect(result.names).toEqual([]);
  });
});
