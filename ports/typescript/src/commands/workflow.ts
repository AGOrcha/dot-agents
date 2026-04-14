/**
 * `dot-agents workflow` — Read-only workflow surface for the TS port.
 *
 * Three commands implemented as pure file reads (no Go CLI invocation):
 *   - runWorkflowOrient  — parse loop-state.md Current Position section
 *   - runWorkflowTasks   — parse TASKS.yaml for a given plan ID
 *   - runWorkflowHealth  — check workflow/ dir exists and contains PLAN.yaml
 *
 * These are the bounded read-only surfaces decided in phase-4.  KG writes and
 * orchestration commands remain Go-only per the phase-4 boundary decision.
 */

import { readFile, stat, readdir } from "node:fs/promises";
import { join } from "node:path";

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

/** Resolve the repo-local .agents/ workflow directory. */
function workflowDir(repoRootOverride?: string): string {
  const root = repoRootOverride ?? process.cwd();
  return join(root, ".agents", "workflow");
}

/** Resolve the active loop-state path (repo-local .agents/active/loop-state.md). */
function loopStatePath(repoRootOverride?: string): string {
  const root = repoRootOverride ?? process.cwd();
  return join(root, ".agents", "active", "loop-state.md");
}

// ---------------------------------------------------------------------------
// runWorkflowOrient
// ---------------------------------------------------------------------------

export interface WorkflowOrientOptions {
  /** Override repo root for .agents/ resolution (used in tests). */
  repoRootOverride?: string;
  /** Unused in this command; kept for API symmetry. */
  agentsHomeOverride?: string;
}

export interface WorkflowOrientResult {
  /** Current git branch extracted from the Current Position section, or null. */
  branch: string | null;
  /** Active plan ID extracted from the Current Position section, or null. */
  plan: string | null;
  /** Active task ID extracted from the Current Position section, or null. */
  task: string | null;
  /** True when loop-state.md was found and the section was present. */
  found: boolean;
  /** Warnings about missing or unparseable content. */
  warnings: string[];
}

/**
 * Read loop-state.md and extract the Current Position: branch, plan, task.
 *
 * Expected format in the ## Current Position section:
 *   - **Plan:** `<plan-id>`
 *   - **Task:** `<task-id>` — ...
 *
 * Branch is taken from the git section header in the same section (optional).
 */
export async function runWorkflowOrient(
  opts: WorkflowOrientOptions = {},
): Promise<WorkflowOrientResult> {
  const warnings: string[] = [];
  const filePath = loopStatePath(opts.repoRootOverride);

  let content: string;
  try {
    content = await readFile(filePath, "utf8");
  } catch {
    warnings.push(`loop-state.md not found at: ${filePath}`);
    return { branch: null, plan: null, task: null, found: false, warnings };
  }

  // Extract the ## Current Position section (up to the next ## heading)
  const sectionMatch = content.match(
    /^## Current Position\s*\n([\s\S]*?)(?=^## |\z)/m,
  );
  if (!sectionMatch) {
    warnings.push("## Current Position section not found in loop-state.md");
    return { branch: null, plan: null, task: null, found: false, warnings };
  }

  const section = sectionMatch[1];

  // Extract plan: - **Plan:** `<plan-id>`
  const planMatch = section.match(/\*\*Plan:\*\*\s*`([^`]+)`/);
  const plan = planMatch ? planMatch[1] : null;
  if (!plan) {
    warnings.push("Could not parse Plan from ## Current Position");
  }

  // Extract task: - **Task:** `<task-id>` or **Task:** `<task-id>` — ...
  const taskMatch = section.match(/\*\*Task:\*\*\s*`([^`]+)`/);
  const task = taskMatch ? taskMatch[1] : null;
  if (!task) {
    warnings.push("Could not parse Task from ## Current Position");
  }

  // Branch: loop-state.md doesn't always encode it in the Current Position section.
  // Attempt a best-effort parse from the checkpoint reference line if present.
  const branchMatch = section.match(/sha\s+`[a-f0-9]+`/) ?? section.match(/branch[:\s]+`([^`]+)`/i);
  const branch = branchMatch ? (branchMatch[1] ?? null) : null;

  return { branch, plan, task, found: true, warnings };
}

// ---------------------------------------------------------------------------
// runWorkflowTasks
// ---------------------------------------------------------------------------

export interface WorkflowTasksOptions {
  /** Override repo root for .agents/ resolution (used in tests). */
  repoRootOverride?: string;
  /** Unused in this command; kept for API symmetry. */
  agentsHomeOverride?: string;
}

export interface WorkflowTask {
  id: string;
  title: string;
  status: string;
  dependsOn: string[];
  blocks: string[];
  owner: string;
  writeScope: string[];
  verificationRequired: boolean;
  notes: string;
}

export interface WorkflowTasksResult {
  planId: string;
  tasks: WorkflowTask[];
  /** True when the TASKS.yaml was found and parsed. */
  found: boolean;
  warnings: string[];
}

/**
 * Read TASKS.yaml for a given plan ID and return the structured task list.
 *
 * Path: .agents/workflow/plans/<planId>/TASKS.yaml
 * Parsed with a minimal hand-rolled YAML reader (avoids runtime deps).
 */
export async function runWorkflowTasks(
  planId: string,
  opts: WorkflowTasksOptions = {},
): Promise<WorkflowTasksResult> {
  const warnings: string[] = [];
  const tasksPath = join(
    workflowDir(opts.repoRootOverride),
    "plans",
    planId,
    "TASKS.yaml",
  );

  let raw: string;
  try {
    raw = await readFile(tasksPath, "utf8");
  } catch {
    warnings.push(`TASKS.yaml not found for plan "${planId}" at: ${tasksPath}`);
    return { planId, tasks: [], found: false, warnings };
  }

  // Parse TASKS.yaml with a hand-rolled block splitter — avoids pulling in a
  // YAML parser dependency while keeping the implementation correct for the
  // known TASKS.yaml format.
  const tasks = parseTasksYaml(raw, warnings);
  return { planId, tasks, found: true, warnings };
}

/**
 * Minimal TASKS.yaml parser.
 *
 * Handles the block-scalar `notes: |` and list items under `tasks:`.
 * Not a general YAML parser — scoped to the known schema only.
 */
function parseTasksYaml(raw: string, warnings: string[]): WorkflowTask[] {
  // Split on task entries: each task begins with `    - id:` (4-space indent list item).
  // The first line of the file contains `tasks:` — we skip header lines before the first `- id:`.
  const taskBlocks = raw.split(/\n(?=    - id:)/);

  const tasks: WorkflowTask[] = [];
  for (const block of taskBlocks) {
    // Skip header block (schema_version, plan_id, tasks:)
    if (!block.includes("id:") || block.trimStart().startsWith("schema_version") || block.trimStart().startsWith("plan_id") || block.trimStart().startsWith("tasks:")) {
      continue;
    }

    const id = extractScalar(block, "id") ?? "";
    if (!id) continue;

    const task: WorkflowTask = {
      id,
      title: extractScalar(block, "title") ?? "",
      status: extractScalar(block, "status") ?? "pending",
      dependsOn: extractStringList(block, "depends_on"),
      blocks: extractStringList(block, "blocks"),
      owner: extractScalar(block, "owner") ?? "",
      writeScope: extractStringList(block, "write_scope"),
      verificationRequired: extractScalar(block, "verification_required") === "true",
      notes: extractBlockScalar(block, "notes"),
    };
    tasks.push(task);
  }

  if (tasks.length === 0) {
    warnings.push("TASKS.yaml contained no parseable tasks");
  }

  return tasks;
}

/** Extract a simple scalar value: `  key: value` or `  - key: value` (list-item first line). */
function extractScalar(block: string, key: string): string | null {
  // Match `  key: value` and also `  - key: value` (YAML list item marker before the first key)
  const re = new RegExp(`^\\s+(?:- )?${key}:\\s+(.+)$`, "m");
  const m = block.match(re);
  if (!m) return null;
  return m[1].trim().replace(/^["']|["']$/g, "");
}

/** Extract an inline or block list:
 *  depends_on: []           → []
 *  depends_on:
 *    - foo
 *    - bar
 */
function extractStringList(block: string, key: string): string[] {
  // Inline empty: `key: []`
  const inlineEmpty = new RegExp(`^\\s+${key}:\\s*\\[\\]`, "m");
  if (inlineEmpty.test(block)) return [];

  // Inline with values: `key: [a, b]`
  const inlineRe = new RegExp(`^\\s+${key}:\\s*\\[([^\\]]+)\\]`, "m");
  const inlineM = block.match(inlineRe);
  if (inlineM) {
    return inlineM[1].split(",").map((s) => s.trim().replace(/^["']|["']$/g, "")).filter(Boolean);
  }

  // Block list: find the key then collect `        - item` lines
  const blockRe = new RegExp(`^(\\s+)${key}:\\s*\\n((?:\\s+- .+\\n?)*)`, "m");
  const blockM = block.match(blockRe);
  if (blockM) {
    return blockM[2]
      .split("\n")
      .map((l) => l.replace(/^\s+- /, "").trim().replace(/^["']|["']$/g, ""))
      .filter(Boolean);
  }

  return [];
}

/** Extract a block scalar value (notes: | or notes: |-). */
function extractBlockScalar(block: string, key: string): string {
  const blockRe = new RegExp(`^(\\s+)${key}:\\s*\\|[-]?\\n((?:\\1  [^\\n]*\\n?)*)`, "m");
  const m = block.match(blockRe);
  if (!m) return extractScalar(block, key) ?? "";
  // Strip the common leading indent
  const indent = m[1].length + 2;
  return m[2]
    .split("\n")
    .map((l) => l.slice(indent))
    .join("\n")
    .trim();
}

// ---------------------------------------------------------------------------
// runWorkflowHealth
// ---------------------------------------------------------------------------

export interface WorkflowHealthOptions {
  /** Override repo root for .agents/ resolution (used in tests). */
  repoRootOverride?: string;
  /** Unused in this command; kept for API symmetry. */
  agentsHomeOverride?: string;
}

export interface WorkflowHealthResult {
  healthy: boolean;
  warnings: string[];
}

/**
 * Check workflow directory health:
 *   1. .agents/workflow/ exists
 *   2. .agents/workflow/plans/ exists
 *   3. At least one plan directory contains a PLAN.yaml
 */
export async function runWorkflowHealth(
  opts: WorkflowHealthOptions = {},
): Promise<WorkflowHealthResult> {
  const warnings: string[] = [];
  const wfDir = workflowDir(opts.repoRootOverride);

  // Check .agents/workflow/ exists
  try {
    const s = await stat(wfDir);
    if (!s.isDirectory()) {
      warnings.push(".agents/workflow exists but is not a directory");
      return { healthy: false, warnings };
    }
  } catch {
    warnings.push(".agents/workflow/ directory not found");
    return { healthy: false, warnings };
  }

  // Check .agents/workflow/plans/ exists
  const plansDir = join(wfDir, "plans");
  try {
    const s = await stat(plansDir);
    if (!s.isDirectory()) {
      warnings.push(".agents/workflow/plans exists but is not a directory");
      return { healthy: false, warnings };
    }
  } catch {
    warnings.push(".agents/workflow/plans/ directory not found");
    return { healthy: false, warnings };
  }

  // Check at least one plan dir contains PLAN.yaml
  let planEntries: string[];
  try {
    const entries = await readdir(plansDir, { withFileTypes: true });
    planEntries = entries.filter((e) => e.isDirectory()).map((e) => e.name);
  } catch {
    warnings.push("Could not read .agents/workflow/plans/ directory");
    return { healthy: false, warnings };
  }

  if (planEntries.length === 0) {
    warnings.push("No plan directories found under .agents/workflow/plans/");
    return { healthy: false, warnings };
  }

  let foundPlanYaml = false;
  for (const planDir of planEntries) {
    const planYamlPath = join(plansDir, planDir, "PLAN.yaml");
    try {
      await stat(planYamlPath);
      foundPlanYaml = true;
      break;
    } catch {
      // try next
    }
  }

  if (!foundPlanYaml) {
    warnings.push("No PLAN.yaml found in any plan directory under .agents/workflow/plans/");
    return { healthy: false, warnings };
  }

  return { healthy: true, warnings };
}
