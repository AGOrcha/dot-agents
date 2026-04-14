/**
 * Tests for the 8 Stage 1 MVP commands.
 *
 * Each test creates a tmp directory and passes it as agentsHomeOverride so the
 * real ~/.agents/ is never touched.
 *
 * Commands under test: init, add, refresh, status, doctor, skills, agents, hooks.
 */

import { mkdtemp, mkdir, writeFile, readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";

import { runInit, standardDirs } from "../src/commands/init.js";
import { runAdd } from "../src/commands/add.js";
import { runRefresh } from "../src/commands/refresh.js";
import { runStatus } from "../src/commands/status.js";
import { runDoctor } from "../src/commands/doctor.js";
import { runSkillsList, runSkillsNew } from "../src/commands/skills.js";
import { runAgentsList, runAgentsNew } from "../src/commands/agents.js";
import { runHooksList } from "../src/commands/hooks.js";
import { loadConfig } from "../src/core/config.js";

async function makeTmp(): Promise<string> {
  return mkdtemp(join(tmpdir(), "dot-agents-ts-"));
}

// ----------------------------------------------------------------
// init
// ----------------------------------------------------------------
describe("runInit", () => {
  it("creates standard directories on fresh home", async () => {
    const home = await makeTmp();
    // Use a subdirectory that doesn't exist yet
    const agentsDir = join(home, ".agents");
    const result = await runInit({ agentsHomeOverride: agentsDir });

    expect(result.alreadyExists).toBe(false);
    expect(result.created.length).toBeGreaterThan(0);
    // Verify the home dir itself was created
    const s = await stat(agentsDir);
    expect(s.isDirectory()).toBe(true);
  });

  it("returns alreadyExists when home exists and --force not set", async () => {
    const home = await makeTmp();
    const agentsDir = join(home, ".agents");
    await mkdir(agentsDir, { recursive: true });

    const result = await runInit({ agentsHomeOverride: agentsDir });
    expect(result.alreadyExists).toBe(true);
    expect(result.created).toHaveLength(0);
  });

  it("reinitializes when --force is set", async () => {
    const home = await makeTmp();
    const agentsDir = join(home, ".agents");
    await mkdir(agentsDir, { recursive: true });

    const result = await runInit({ agentsHomeOverride: agentsDir, force: true });
    expect(result.alreadyExists).toBe(true);
    // All standard dirs should have been processed
    expect(result.created.length + result.skipped.length).toBe(standardDirs(agentsDir).length);
  });

  it("dry-run does not create dirs", async () => {
    const home = await makeTmp();
    const agentsDir = join(home, ".agents-dryrun");
    const result = await runInit({ agentsHomeOverride: agentsDir, dryRun: true });

    expect(result.created.length).toBeGreaterThan(0);
    // Dir should NOT have been created
    await expect(stat(agentsDir)).rejects.toThrow();
  });
});

// ----------------------------------------------------------------
// add
// ----------------------------------------------------------------
describe("runAdd", () => {
  it("adds a project and writes config.json", async () => {
    const home = await makeTmp();
    const project = await makeTmp(); // project path must exist

    const result = await runAdd(project, { name: "my-project", agentsHomeOverride: home });
    expect(result.status).toBe("added");
    expect(result.name).toBe("my-project");
    expect(result.path).toBe(project);

    const cfg = await loadConfig(home);
    expect(cfg.projects["my-project"]).toBeDefined();
    expect(cfg.projects["my-project"].path).toBe(project);
  });

  it("returns path_not_found for non-existent paths", async () => {
    const home = await makeTmp();
    const result = await runAdd("/tmp/__does_not_exist_999__", { agentsHomeOverride: home });
    expect(result.status).toBe("path_not_found");
  });

  it("returns already_registered without --force", async () => {
    const home = await makeTmp();
    const project = await makeTmp();

    await runAdd(project, { name: "dup", agentsHomeOverride: home });
    const result = await runAdd(project, { name: "dup", agentsHomeOverride: home });
    expect(result.status).toBe("already_registered");
  });

  it("updates with --force", async () => {
    const home = await makeTmp();
    const project = await makeTmp();

    await runAdd(project, { name: "overwrite", agentsHomeOverride: home });
    const result = await runAdd(project, { name: "overwrite", force: true, agentsHomeOverride: home });
    expect(result.status).toBe("updated");
  });

  it("derives name from basename when no name given", async () => {
    const home = await makeTmp();
    const project = await makeTmp(); // e.g. /tmp/dot-agents-ts-XYZ

    const result = await runAdd(project, { agentsHomeOverride: home });
    expect(result.status).toBe("added");
    // Name should be derived from the directory's basename
    expect(result.name).toBeTruthy();
    expect(result.name.length).toBeGreaterThan(0);
  });
});

// ----------------------------------------------------------------
// refresh
// ----------------------------------------------------------------
describe("runRefresh", () => {
  it("returns noProjects when config has no projects", async () => {
    const home = await makeTmp();
    const result = await runRefresh({ agentsHomeOverride: home });
    expect(result.noProjects).toBe(true);
    expect(result.projects).toHaveLength(0);
  });

  it("reports ok for existing project paths", async () => {
    const home = await makeTmp();
    const project = await makeTmp();
    await runAdd(project, { name: "live", agentsHomeOverride: home });

    const result = await runRefresh({ agentsHomeOverride: home });
    expect(result.noProjects).toBe(false);
    const entry = result.projects.find((p) => p.name === "live");
    expect(entry).toBeDefined();
    expect(entry?.status).toBe("ok");
  });

  it("reports missing_path for stale project paths", async () => {
    const home = await makeTmp();
    // Directly write a config with a bogus path
    await mkdir(home, { recursive: true });
    await writeFile(
      join(home, "config.json"),
      JSON.stringify({
        version: 1,
        projects: { ghost: { path: "/tmp/__ghost_project_9999__", added: new Date().toISOString() } },
      }) + "\n",
      "utf8",
    );

    const result = await runRefresh({ agentsHomeOverride: home });
    const entry = result.projects.find((p) => p.name === "ghost");
    expect(entry?.status).toBe("missing_path");
  });

  it("filters to a single project by name", async () => {
    const home = await makeTmp();
    const projectA = await makeTmp();
    const projectB = await makeTmp();
    await runAdd(projectA, { name: "alpha", agentsHomeOverride: home });
    await runAdd(projectB, { name: "beta", agentsHomeOverride: home });

    const result = await runRefresh({ filter: "alpha", agentsHomeOverride: home });
    expect(result.projects).toHaveLength(1);
    expect(result.projects[0].name).toBe("alpha");
  });
});

// ----------------------------------------------------------------
// status
// ----------------------------------------------------------------
describe("runStatus", () => {
  it("reports agentsHomeExists false when home missing", async () => {
    const home = join(await makeTmp(), ".agents-nonexistent");
    const result = await runStatus({ agentsHomeOverride: home });
    expect(result.agentsHomeExists).toBe(false);
    expect(result.configExists).toBe(false);
    expect(result.projects).toHaveLength(0);
  });

  it("reports agentsHomeExists true and lists projects", async () => {
    const home = await makeTmp();
    const project = await makeTmp();
    await runInit({ agentsHomeOverride: home });
    await runAdd(project, { name: "statustest", agentsHomeOverride: home });

    const result = await runStatus({ agentsHomeOverride: home });
    expect(result.agentsHomeExists).toBe(true);
    expect(result.configExists).toBe(true);
    const p = result.projects.find((x) => x.name === "statustest");
    expect(p).toBeDefined();
    expect(p?.pathExists).toBe(true);
    expect(p?.agentsRcFound).toBe(false); // no .agentsrc.json in temp dir
  });

  it("detects agentsRcFound when .agentsrc.json exists", async () => {
    const home = await makeTmp();
    const project = await makeTmp();
    await writeFile(
      join(project, ".agentsrc.json"),
      JSON.stringify({ version: 1, hooks: false, mcp: false, settings: false, sources: [{ type: "local" }] }) + "\n",
      "utf8",
    );
    await runAdd(project, { name: "rctest", agentsHomeOverride: home });

    const result = await runStatus({ agentsHomeOverride: home });
    const p = result.projects.find((x) => x.name === "rctest");
    expect(p?.agentsRcFound).toBe(true);
  });

  it("returns canonical store buckets", async () => {
    const home = await makeTmp();
    await runInit({ agentsHomeOverride: home });

    const result = await runStatus({ agentsHomeOverride: home });
    const buckets = result.canonicalStore.map((e) => e.bucket);
    expect(buckets).toContain("skills");
    expect(buckets).toContain("agents");
    expect(buckets).toContain("hooks");
    expect(buckets).toContain("rules");
  });
});

// ----------------------------------------------------------------
// doctor
// ----------------------------------------------------------------
describe("runDoctor", () => {
  it("reports error when ~/.agents/ missing", async () => {
    const home = join(await makeTmp(), ".agents-nonexistent");
    const result = await runDoctor({ agentsHomeOverride: home });
    const homeCheck = result.checks.find((c) => c.name === "agents_home");
    expect(homeCheck?.status).toBe("error");
    expect(result.ok).toBe(false);
  });

  it("reports ok when ~/.agents/ exists with config.json", async () => {
    const home = await makeTmp();
    await runInit({ agentsHomeOverride: home });

    const result = await runDoctor({ agentsHomeOverride: home });
    const homeCheck = result.checks.find((c) => c.name === "agents_home");
    expect(homeCheck?.status).toBe("ok");
  });

  it("warns about stale project paths", async () => {
    const home = await makeTmp();
    await mkdir(home, { recursive: true });
    await writeFile(
      join(home, "config.json"),
      JSON.stringify({
        version: 1,
        projects: { stale: { path: "/tmp/__stale_project__", added: new Date().toISOString() } },
      }) + "\n",
      "utf8",
    );

    const result = await runDoctor({ agentsHomeOverride: home });
    const staleCheck = result.checks.find((c) => c.name === "project:stale");
    expect(staleCheck?.status).toBe("warn");
  });

  it("verbose mode emits ok checks for healthy projects", async () => {
    const home = await makeTmp();
    const project = await makeTmp();
    await writeFile(
      join(project, ".agentsrc.json"),
      JSON.stringify({ version: 1, hooks: false, mcp: false, settings: false, sources: [{ type: "local" }] }) + "\n",
      "utf8",
    );
    await runAdd(project, { name: "vtest", agentsHomeOverride: home });

    const verbose = await runDoctor({ verbose: true, agentsHomeOverride: home });
    const check = verbose.checks.find((c) => c.name === "project:vtest");
    expect(check?.status).toBe("ok");
  });
});

// ----------------------------------------------------------------
// skills
// ----------------------------------------------------------------
describe("runSkillsList / runSkillsNew", () => {
  it("returns empty list when no skills dir", async () => {
    const home = await makeTmp();
    const result = await runSkillsList("global", { agentsHomeOverride: home });
    expect(result.skills).toHaveLength(0);
    expect(result.scope).toBe("global");
  });

  it("creates a skill and lists it", async () => {
    const home = await makeTmp();
    const created = await runSkillsNew("my-skill", "global", { agentsHomeOverride: home });
    expect(created.created).toBe(true);
    expect(created.alreadyExists).toBe(false);

    const list = await runSkillsList("global", { agentsHomeOverride: home });
    expect(list.skills.map((s) => s.name)).toContain("my-skill");
  });

  it("returns alreadyExists when skill exists", async () => {
    const home = await makeTmp();
    await runSkillsNew("dup-skill", "global", { agentsHomeOverride: home });
    const second = await runSkillsNew("dup-skill", "global", { agentsHomeOverride: home });
    expect(second.alreadyExists).toBe(true);
    expect(second.created).toBe(false);
  });

  it("lists skill with description from frontmatter", async () => {
    const home = await makeTmp();
    const skillDir = join(home, "skills", "global", "with-desc");
    await mkdir(skillDir, { recursive: true });
    await writeFile(
      join(skillDir, "SKILL.md"),
      '---\nname: "with-desc"\ndescription: "A test skill"\n---\n\n# with-desc\n',
      "utf8",
    );

    const list = await runSkillsList("global", { agentsHomeOverride: home });
    const entry = list.skills.find((s) => s.name === "with-desc");
    expect(entry?.description).toBe("A test skill");
    expect(entry?.hasSkillMd).toBe(true);
  });
});

// ----------------------------------------------------------------
// agents
// ----------------------------------------------------------------
describe("runAgentsList / runAgentsNew", () => {
  it("returns empty list when no agents dir", async () => {
    const home = await makeTmp();
    const result = await runAgentsList("global", { agentsHomeOverride: home });
    expect(result.agents).toHaveLength(0);
    expect(result.scope).toBe("global");
  });

  it("creates an agent and lists it", async () => {
    const home = await makeTmp();
    const created = await runAgentsNew("my-agent", "global", { agentsHomeOverride: home });
    expect(created.created).toBe(true);

    const list = await runAgentsList("global", { agentsHomeOverride: home });
    expect(list.agents.map((a) => a.name)).toContain("my-agent");
  });

  it("returns alreadyExists when agent exists", async () => {
    const home = await makeTmp();
    await runAgentsNew("dup-agent", "global", { agentsHomeOverride: home });
    const second = await runAgentsNew("dup-agent", "global", { agentsHomeOverride: home });
    expect(second.alreadyExists).toBe(true);
  });

  it("lists agent with description from frontmatter", async () => {
    const home = await makeTmp();
    const agentDir = join(home, "agents", "global", "desc-agent");
    await mkdir(agentDir, { recursive: true });
    await writeFile(
      join(agentDir, "AGENT.md"),
      '---\nname: "desc-agent"\ndescription: "Does things"\n---\n\n# desc-agent\n',
      "utf8",
    );

    const list = await runAgentsList("global", { agentsHomeOverride: home });
    const entry = list.agents.find((a) => a.name === "desc-agent");
    expect(entry?.description).toBe("Does things");
    expect(entry?.hasAgentMd).toBe(true);
  });
});

// ----------------------------------------------------------------
// hooks
// ----------------------------------------------------------------
describe("runHooksList", () => {
  it("returns kind:none when no hooks configured", async () => {
    const home = await makeTmp();
    const result = await runHooksList("global", { agentsHomeOverride: home });
    expect(result.kind).toBe("none");
    expect(result.scope).toBe("global");
  });

  it("detects canonical HOOK.yaml bundles", async () => {
    const home = await makeTmp();
    const bundleDir = join(home, "hooks", "global", "my-hook");
    await mkdir(bundleDir, { recursive: true });
    await writeFile(join(bundleDir, "HOOK.yaml"), "name: my-hook\n", "utf8");

    const result = await runHooksList("global", { agentsHomeOverride: home });
    expect(result.kind).toBe("canonical");
    expect(result.bundles?.map((b) => b.name)).toContain("my-hook");
  });

  it("falls back to legacy claude-code.json when no bundles", async () => {
    const home = await makeTmp();
    const settingsDir = join(home, "settings", "global");
    await mkdir(settingsDir, { recursive: true });
    await writeFile(
      join(settingsDir, "claude-code.json"),
      JSON.stringify({
        hooks: {
          PreToolUse: [{ type: "command", command: "echo hi" }],
          PostToolUse: [{ type: "command", command: "echo bye" }],
        },
      }) + "\n",
      "utf8",
    );

    const result = await runHooksList("global", { agentsHomeOverride: home });
    expect(result.kind).toBe("legacy");
    const events = (result.legacyEvents ?? []).map((e) => e.event).sort();
    expect(events).toContain("PreToolUse");
    expect(events).toContain("PostToolUse");
  });

  it("prefers canonical bundles over legacy settings", async () => {
    const home = await makeTmp();

    // Create both
    const bundleDir = join(home, "hooks", "global", "canonical-hook");
    await mkdir(bundleDir, { recursive: true });
    await writeFile(join(bundleDir, "HOOK.yaml"), "name: canonical-hook\n", "utf8");

    const settingsDir = join(home, "settings", "global");
    await mkdir(settingsDir, { recursive: true });
    await writeFile(
      join(settingsDir, "claude-code.json"),
      JSON.stringify({ hooks: { PreToolUse: [{ type: "command", command: "echo" }] } }) + "\n",
      "utf8",
    );

    const result = await runHooksList("global", { agentsHomeOverride: home });
    expect(result.kind).toBe("canonical");
  });
});
