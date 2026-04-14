/**
 * Read-only KG command stubs — health (filesystem) and query (Go-only stub).
 */

import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it, beforeEach, afterEach } from "vitest";

import { runKgHealth, runKgQuery } from "../src/commands/kg.js";

const STUB =
  "KG query requires Go CLI — not available in TS port";

async function makeTmp(): Promise<string> {
  return mkdtemp(join(tmpdir(), "dot-agents-kg-"));
}

describe("runKgHealth", () => {
  const prev = process.env.KG_HOME;

  beforeEach(() => {
    delete process.env.KG_HOME;
  });

  afterEach(() => {
    if (prev === undefined) delete process.env.KG_HOME;
    else process.env.KG_HOME = prev;
  });

  it("is healthy when KG home has notes/ directory (override)", async () => {
    const root = await makeTmp();
    await mkdir(join(root, "notes"), { recursive: true });

    const r = await runKgHealth({ kgHomeOverride: root });
    expect(r.healthy).toBe(true);
    expect(r.kgHome).toBe(root);
    expect(r.warnings).toEqual([]);
  });

  it("uses KG_HOME when override omitted", async () => {
    const root = await makeTmp();
    await mkdir(join(root, "notes"), { recursive: true });
    process.env.KG_HOME = root;

    const r = await runKgHealth();
    expect(r.healthy).toBe(true);
    expect(r.kgHome).toBe(root);
    expect(r.warnings).toEqual([]);
  });

  it("is unhealthy when KG_HOME is unset and no override", async () => {
    const r = await runKgHealth();
    expect(r.healthy).toBe(false);
    expect(r.kgHome).toBe("");
    expect(r.warnings.some((w) => w.includes("KG_HOME"))).toBe(true);
  });

  it("is unhealthy when home exists but notes/ is missing (empty dir case)", async () => {
    const root = await makeTmp();
    // directory exists, no notes/
    const r = await runKgHealth({ kgHomeOverride: root });
    expect(r.healthy).toBe(false);
    expect(r.kgHome).toBe(root);
    expect(r.warnings.some((w) => w.includes("notes"))).toBe(true);
  });

  it("is unhealthy when notes exists as a file", async () => {
    const root = await makeTmp();
    await writeFile(join(root, "notes"), "x", "utf8");

    const r = await runKgHealth({ kgHomeOverride: root });
    expect(r.healthy).toBe(false);
    expect(r.warnings.some((w) => w.toLowerCase().includes("directory"))).toBe(true);
  });

  it("is unhealthy when KG_HOME path does not exist", async () => {
    const r = await runKgHealth({ kgHomeOverride: join(tmpdir(), "nonexistent-kg-" + Date.now()) });
    expect(r.healthy).toBe(false);
    expect(r.warnings.some((w) => w.includes("does not exist"))).toBe(true);
  });
});

describe("runKgQuery", () => {
  it("returns the phase-4 stub message for any query", async () => {
    const r = await runKgQuery("callers_of kg.ts");
    expect(r.query).toBe("callers_of kg.ts");
    expect(r.result).toBe(STUB);
  });

  it("ignores kgHomeOverride (stub does not invoke Go)", async () => {
    const r = await runKgQuery("x", { kgHomeOverride: "/tmp/fake-kg" });
    expect(r.result).toBe(STUB);
  });
});
