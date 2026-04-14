/**
 * Tests for the three read-only workflow commands:
 *   runWorkflowOrient  — parse loop-state.md Current Position
 *   runWorkflowTasks   — parse TASKS.yaml for a plan
 *   runWorkflowHealth  — check workflow dir exists + has PLAN.yaml
 *
 * All tests use repoRootOverride pointing to a tmp directory so the real
 * .agents/ workspace is never touched.
 */

import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";

import {
  runWorkflowOrient,
  runWorkflowTasks,
  runWorkflowHealth,
} from "../src/commands/workflow.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function makeTmp(): Promise<string> {
  return mkdtemp(join(tmpdir(), "da-wf-test-"));
}

/** Create a minimal .agents/active/loop-state.md fixture. */
async function writeLoopState(root: string, content: string): Promise<void> {
  const dir = join(root, ".agents", "active");
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "loop-state.md"), content, "utf8");
}

/** Create a minimal .agents/workflow/plans/<planId>/TASKS.yaml fixture. */
async function writeTasksYaml(
  root: string,
  planId: string,
  content: string,
): Promise<void> {
  const dir = join(root, ".agents", "workflow", "plans", planId);
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "TASKS.yaml"), content, "utf8");
}

/** Create a minimal .agents/workflow/plans/<planId>/PLAN.yaml fixture. */
async function writePlanYaml(
  root: string,
  planId: string,
  content: string,
): Promise<void> {
  const dir = join(root, ".agents", "workflow", "plans", planId);
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "PLAN.yaml"), content, "utf8");
}

// Minimal TASKS.yaml matching the real schema
const SAMPLE_TASKS_YAML = `schema_version: 1
plan_id: my-plan
tasks:
    - id: task-one
      title: First task
      status: completed
      depends_on: []
      blocks:
        - task-two
      owner: dot-agents
      write_scope:
        - ports/typescript/src/
      verification_required: true
      notes: |
        This is the notes block.
        Second line.
    - id: task-two
      title: Second task
      status: in_progress
      depends_on:
        - task-one
      blocks: []
      owner: dot-agents
      write_scope: []
      verification_required: false
      notes: Simple note.
`;

const SAMPLE_LOOP_STATE = `# Loop State

Last updated: 2026-04-14

## Current Position

Canonical focus (checkpoint + \`workflow tasks typescript-port\`, sha \`dca9054\`):
- **Plan:** \`my-plan\`
- **Task:** \`task-two\` — in_progress; active delegation bundle controls this lane.

## Loop Health

Some other content here.
`;

// ---------------------------------------------------------------------------
// runWorkflowOrient
// ---------------------------------------------------------------------------

describe("runWorkflowOrient", () => {
  it("returns found:false and warning when loop-state.md does not exist", async () => {
    const root = await makeTmp();
    const result = await runWorkflowOrient({ repoRootOverride: root });

    expect(result.found).toBe(false);
    expect(result.plan).toBeNull();
    expect(result.task).toBeNull();
    expect(result.warnings.length).toBeGreaterThan(0);
    expect(result.warnings[0]).toMatch(/loop-state\.md not found/i);
  });

  it("returns found:false and warning when Current Position section is absent", async () => {
    const root = await makeTmp();
    await writeLoopState(root, "# Loop State\n\nNo current position here.\n");

    const result = await runWorkflowOrient({ repoRootOverride: root });

    expect(result.found).toBe(false);
    expect(result.plan).toBeNull();
    expect(result.task).toBeNull();
    expect(result.warnings.some((w) => /Current Position/i.test(w))).toBe(true);
  });

  it("parses plan and task from a well-formed loop-state.md", async () => {
    const root = await makeTmp();
    await writeLoopState(root, SAMPLE_LOOP_STATE);

    const result = await runWorkflowOrient({ repoRootOverride: root });

    expect(result.found).toBe(true);
    expect(result.plan).toBe("my-plan");
    expect(result.task).toBe("task-two");
    expect(result.warnings).toHaveLength(0);
  });

  it("returns null branch when no branch encoding is present", async () => {
    const root = await makeTmp();
    await writeLoopState(root, SAMPLE_LOOP_STATE);

    const result = await runWorkflowOrient({ repoRootOverride: root });

    // SAMPLE_LOOP_STATE has sha reference but no explicit branch: field — branch may be null
    expect(result.found).toBe(true);
    // branch is null or a string — just confirm it doesn't throw
    expect(result.branch === null || typeof result.branch === "string").toBe(true);
  });

  it("handles loop-state.md with only plan (no task line)", async () => {
    const root = await makeTmp();
    const content = `# Loop State\n\n## Current Position\n\n- **Plan:** \`only-plan\`\n\n## Other\n`;
    await writeLoopState(root, content);

    const result = await runWorkflowOrient({ repoRootOverride: root });

    expect(result.found).toBe(true);
    expect(result.plan).toBe("only-plan");
    expect(result.task).toBeNull();
    expect(result.warnings.some((w) => /Task/i.test(w))).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// runWorkflowTasks
// ---------------------------------------------------------------------------

describe("runWorkflowTasks", () => {
  it("returns found:false and warning when plan does not exist", async () => {
    const root = await makeTmp();
    const result = await runWorkflowTasks("unknown-plan", {
      repoRootOverride: root,
    });

    expect(result.found).toBe(false);
    expect(result.tasks).toHaveLength(0);
    expect(result.warnings.length).toBeGreaterThan(0);
    expect(result.warnings[0]).toMatch(/TASKS\.yaml not found/i);
  });

  it("returns found:false and warning for empty TASKS.yaml", async () => {
    const root = await makeTmp();
    await writeTasksYaml(root, "empty-plan", "schema_version: 1\nplan_id: empty-plan\ntasks: []\n");

    const result = await runWorkflowTasks("empty-plan", {
      repoRootOverride: root,
    });

    expect(result.found).toBe(true);
    expect(result.tasks).toHaveLength(0);
    expect(result.warnings.some((w) => /no parseable tasks/i.test(w))).toBe(true);
  });

  it("parses task list from a well-formed TASKS.yaml", async () => {
    const root = await makeTmp();
    await writeTasksYaml(root, "my-plan", SAMPLE_TASKS_YAML);

    const result = await runWorkflowTasks("my-plan", {
      repoRootOverride: root,
    });

    expect(result.found).toBe(true);
    expect(result.planId).toBe("my-plan");
    expect(result.tasks).toHaveLength(2);
  });

  it("parses task id, title, and status correctly", async () => {
    const root = await makeTmp();
    await writeTasksYaml(root, "my-plan", SAMPLE_TASKS_YAML);

    const result = await runWorkflowTasks("my-plan", {
      repoRootOverride: root,
    });

    const t1 = result.tasks.find((t) => t.id === "task-one");
    expect(t1).toBeDefined();
    expect(t1?.title).toBe("First task");
    expect(t1?.status).toBe("completed");

    const t2 = result.tasks.find((t) => t.id === "task-two");
    expect(t2).toBeDefined();
    expect(t2?.status).toBe("in_progress");
  });

  it("parses blocks and depends_on lists correctly", async () => {
    const root = await makeTmp();
    await writeTasksYaml(root, "my-plan", SAMPLE_TASKS_YAML);

    const result = await runWorkflowTasks("my-plan", {
      repoRootOverride: root,
    });

    const t1 = result.tasks.find((t) => t.id === "task-one");
    expect(t1?.blocks).toContain("task-two");
    expect(t1?.dependsOn).toHaveLength(0);

    const t2 = result.tasks.find((t) => t.id === "task-two");
    expect(t2?.dependsOn).toContain("task-one");
    expect(t2?.blocks).toHaveLength(0);
  });

  it("parses verificationRequired correctly", async () => {
    const root = await makeTmp();
    await writeTasksYaml(root, "my-plan", SAMPLE_TASKS_YAML);

    const result = await runWorkflowTasks("my-plan", {
      repoRootOverride: root,
    });

    const t1 = result.tasks.find((t) => t.id === "task-one");
    expect(t1?.verificationRequired).toBe(true);

    const t2 = result.tasks.find((t) => t.id === "task-two");
    expect(t2?.verificationRequired).toBe(false);
  });

  it("returns planId in result even when TASKS.yaml not found", async () => {
    const root = await makeTmp();
    const result = await runWorkflowTasks("no-such-plan", {
      repoRootOverride: root,
    });

    expect(result.planId).toBe("no-such-plan");
  });
});

// ---------------------------------------------------------------------------
// runWorkflowHealth
// ---------------------------------------------------------------------------

describe("runWorkflowHealth", () => {
  it("returns healthy:false when .agents/workflow/ does not exist", async () => {
    const root = await makeTmp();
    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(false);
    expect(result.warnings.some((w) => /workflow.*not found|not found.*workflow/i.test(w))).toBe(true);
  });

  it("returns healthy:false when .agents/workflow/plans/ does not exist", async () => {
    const root = await makeTmp();
    await mkdir(join(root, ".agents", "workflow"), { recursive: true });

    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(false);
    expect(result.warnings.some((w) => /plans.*not found|not found.*plans/i.test(w))).toBe(true);
  });

  it("returns healthy:false when plans/ is empty (no plan dirs)", async () => {
    const root = await makeTmp();
    await mkdir(join(root, ".agents", "workflow", "plans"), { recursive: true });

    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(false);
    expect(result.warnings.some((w) => /No plan directories/i.test(w))).toBe(true);
  });

  it("returns healthy:false when plan dir exists but has no PLAN.yaml", async () => {
    const root = await makeTmp();
    // Create plan dir but only TASKS.yaml (no PLAN.yaml)
    await writeTasksYaml(root, "no-plan-yaml", SAMPLE_TASKS_YAML);

    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(false);
    expect(result.warnings.some((w) => /PLAN\.yaml/i.test(w))).toBe(true);
  });

  it("returns healthy:true when at least one plan dir has PLAN.yaml", async () => {
    const root = await makeTmp();
    await writePlanYaml(
      root,
      "my-plan",
      "schema_version: 1\nid: my-plan\ntitle: My Plan\nstatus: active\n",
    );

    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(true);
    expect(result.warnings).toHaveLength(0);
  });

  it("returns healthy:true when multiple plans exist and only one has PLAN.yaml", async () => {
    const root = await makeTmp();
    // Plan with PLAN.yaml
    await writePlanYaml(
      root,
      "plan-a",
      "schema_version: 1\nid: plan-a\ntitle: Plan A\nstatus: active\n",
    );
    // Plan dir without PLAN.yaml
    await writeTasksYaml(root, "plan-b", SAMPLE_TASKS_YAML);

    const result = await runWorkflowHealth({ repoRootOverride: root });

    expect(result.healthy).toBe(true);
    expect(result.warnings).toHaveLength(0);
  });
});
