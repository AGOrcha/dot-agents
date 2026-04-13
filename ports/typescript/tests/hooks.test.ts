import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { mkdtemp } from "node:fs/promises";
import { describe, expect, it } from "vitest";
import { detectHookEvents } from "../src/index.js";

async function makeTmp(): Promise<string> {
  return mkdtemp(join(tmpdir(), "hooks-ts-"));
}

describe("detectHookEvents", () => {
  it("returns all=true when canonical HOOK.yaml bundle exists (parity with TestGenerateAgentsRCHooksCanonicalBundlesEnableAll)", async () => {
    const tmp = await makeTmp();
    const bundleDir = join(tmp, "hooks", "global", "session-orient");
    await mkdir(bundleDir, { recursive: true });
    await writeFile(
      join(bundleDir, "HOOK.yaml"),
      "name: session-orient\nwhen: session_start\nrun:\n  command: ./orient.sh\n",
    );

    const result = await detectHookEvents(tmp, "myproject");
    expect(result.all).toBe(true);
    expect(result.names).toEqual([]);
  });

  it("returns named events from legacy claude-code.json, excluding empty arrays (parity with TestGenerateAgentsRCHooksNamedEvents)", async () => {
    const tmp = await makeTmp();
    const settingsDir = join(tmp, "settings", "myproject");
    await mkdir(settingsDir, { recursive: true });
    await writeFile(
      join(settingsDir, "claude-code.json"),
      JSON.stringify({
        hooks: {
          PreToolUse: [{ command: "echo pre" }],
          PostToolUse: [{ command: "echo post" }],
          Notification: [],
        },
      }),
    );

    const result = await detectHookEvents(tmp, "myproject");
    expect(result.all).toBe(false);
    expect(result.names).toEqual(["PostToolUse", "PreToolUse"]);
  });

  it("falls back to global claude-code.json (parity with TestGenerateAgentsRCHooksLegacySettingsFallBackToGlobal)", async () => {
    const tmp = await makeTmp();
    const globalSettingsDir = join(tmp, "settings", "global");
    await mkdir(globalSettingsDir, { recursive: true });
    await writeFile(
      join(globalSettingsDir, "claude-code.json"),
      JSON.stringify({
        hooks: {
          PreToolUse: [{ command: "echo pre" }],
          PostToolUse: [],
          Stop: [{ command: "echo stop" }],
        },
      }),
    );

    const result = await detectHookEvents(tmp, "myproject");
    expect(result.all).toBe(false);
    expect(result.names).toEqual(["PreToolUse", "Stop"]);
  });

  it("returns empty when no hook configuration exists (parity with TestGenerateAgentsRCHooksNoSettings)", async () => {
    const tmp = await makeTmp();
    const result = await detectHookEvents(tmp, "noscope");
    expect(result.all).toBe(false);
    expect(result.names).toEqual([]);
  });

  it("canonical bundle takes precedence over legacy settings", async () => {
    const tmp = await makeTmp();
    // Both exist — canonical should win
    const bundleDir = join(tmp, "hooks", "global", "my-hook");
    await mkdir(bundleDir, { recursive: true });
    await writeFile(join(bundleDir, "HOOK.yaml"), "name: my-hook\n");

    const settingsDir = join(tmp, "settings", "myproject");
    await mkdir(settingsDir, { recursive: true });
    await writeFile(
      join(settingsDir, "claude-code.json"),
      JSON.stringify({ hooks: { PreToolUse: [{ command: "echo x" }] } }),
    );

    const result = await detectHookEvents(tmp, "myproject");
    expect(result.all).toBe(true);
  });
});
