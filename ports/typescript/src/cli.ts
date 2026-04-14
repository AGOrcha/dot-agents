#!/usr/bin/env node
/**
 * dot-agents TypeScript CLI — Stage 1 MVP entry point.
 *
 * Wires the 8 Stage 1 commands (init, add, refresh, status, doctor, skills, agents, hooks)
 * into a minimal command dispatcher. No external CLI framework dependency — uses
 * plain process.argv parsing to stay lightweight on restricted machines.
 *
 * Phase 4 boundary (workflow / KG / orchestration) is documented in CLI --help and
 * docs/TYPESCRIPT_PORT_BOUNDARY.md — TS does not implement kg/* or workflow writes.
 *
 * Usage:
 *   dot-agents-ts <command> [subcommand] [args...] [flags]
 */

import { boundaryHelpLines } from "./boundary.js";
import { runInit } from "./commands/init.js";
import { runAdd } from "./commands/add.js";
import { runRefresh } from "./commands/refresh.js";
import { runStatus } from "./commands/status.js";
import { runDoctor } from "./commands/doctor.js";
import { runSkillsList, runSkillsNew } from "./commands/skills.js";
import { runAgentsList, runAgentsNew } from "./commands/agents.js";
import { runHooksList } from "./commands/hooks.js";

// ---- Minimal output helpers ----

function printLine(msg: string): void {
  process.stdout.write(msg + "\n");
}

function printError(msg: string): void {
  process.stderr.write("error: " + msg + "\n");
}

function flagSet(args: string[], flag: string): boolean {
  return args.includes(flag);
}

function flagValue(args: string[], flag: string): string | undefined {
  const idx = args.indexOf(flag);
  if (idx !== -1 && idx + 1 < args.length) return args[idx + 1];
  return undefined;
}

function positionals(args: string[]): string[] {
  return args.filter((a) => !a.startsWith("-"));
}

// ---- Command handlers ----

async function cmdInit(args: string[]): Promise<number> {
  const dryRun = flagSet(args, "--dry-run");
  const force = flagSet(args, "--force");
  const result = await runInit({ dryRun, force });
  if (result.alreadyExists && !force) {
    printLine("~/.agents/ already exists. Use --force to reinitialize.");
  } else if (dryRun) {
    printLine("DRY RUN — would create:");
    for (const d of result.created) printLine("  " + d);
  } else {
    printLine("Initialized ~/.agents/ directory structure.");
    for (const d of result.created) printLine("  created: " + d);
  }
  return 0;
}

async function cmdAdd(args: string[]): Promise<number> {
  const pos = positionals(args);
  if (pos.length === 0) {
    printError("add requires a project path argument");
    printLine("Usage: dot-agents add <path> [--name <name>] [--force]");
    return 1;
  }
  const projectPath = pos[0];
  const name = flagValue(args, "--name");
  const force = flagSet(args, "--force");
  const result = await runAdd(projectPath, { name, force });
  switch (result.status) {
    case "added":
      printLine(`Added project "${result.name}" at ${result.path}`);
      break;
    case "updated":
      printLine(`Updated project "${result.name}" at ${result.path}`);
      break;
    case "already_registered":
      printLine(`Project "${result.name}" already registered at ${result.path}. Use --force to update.`);
      break;
    case "path_not_found":
      printError(`Path not found: ${result.path}`);
      return 1;
  }
  return 0;
}

async function cmdRefresh(args: string[]): Promise<number> {
  const pos = positionals(args);
  const filter = pos[0];
  const result = await runRefresh({ filter });
  if (result.noProjects) {
    printLine("No managed projects. Add one with: dot-agents add <path>");
    return 0;
  }
  for (const p of result.projects) {
    const tag = p.status === "ok" ? "[ok]" : `[${p.status}]`;
    printLine(`${tag} ${p.name} — ${p.path}`);
  }
  return 0;
}

async function cmdStatus(args: string[]): Promise<number> {
  const result = await runStatus();
  printLine(`agents_home: ${result.agentsHome} [${result.agentsHomeExists ? "ok" : "missing"}]`);
  printLine(`config.json: [${result.configExists ? "ok" : "missing"}]`);
  printLine("");
  printLine("Projects:");
  if (result.projects.length === 0) {
    printLine("  (none)");
  } else {
    for (const p of result.projects) {
      const exists = p.pathExists ? "ok" : "missing";
      const rc = p.agentsRcFound ? " (agentsrc)" : "";
      printLine(`  ${p.name}: ${p.path} [${exists}]${rc}`);
    }
  }
  printLine("");
  printLine("Canonical store:");
  for (const entry of result.canonicalStore) {
    printLine(`  ${entry.bucket}: ${entry.scopes} scope(s), ${entry.items} item(s)`);
  }
  return 0;
}

async function cmdDoctor(args: string[]): Promise<number> {
  const verbose = flagSet(args, "--verbose") || flagSet(args, "-v");
  const result = await runDoctor({ verbose });
  for (const check of result.checks) {
    printLine(`[${check.status}] ${check.message}`);
  }
  if (result.ok) {
    printLine("\nAll checks passed.");
    return 0;
  } else {
    printLine("\nSome checks failed. See above for details.");
    return 1;
  }
}

async function cmdSkills(args: string[]): Promise<number> {
  const [sub, ...rest] = args;
  const pos = positionals(rest);

  if (!sub || sub === "list") {
    const scope = pos[0] ?? "global";
    const result = await runSkillsList(scope);
    printLine(`Skills (${result.scope}):`);
    if (result.skills.length === 0) {
      printLine("  (none)");
    } else {
      for (const s of result.skills) {
        const desc = s.description ? `  — ${s.description}` : "";
        printLine(`  ${s.name}${desc}`);
      }
    }
    return 0;
  }

  if (sub === "new") {
    if (pos.length === 0) {
      printError("skills new requires a skill name");
      return 1;
    }
    const [skillName, scope = "global"] = pos;
    const result = await runSkillsNew(skillName, scope);
    if (result.alreadyExists) {
      printLine(`Skill "${skillName}" already exists at ${result.path}`);
    } else {
      printLine(`Created skill "${skillName}" at ${result.path}`);
    }
    return 0;
  }

  printError(`Unknown skills subcommand: ${sub}`);
  printLine("Usage: dot-agents skills [list|new] [args]");
  return 1;
}

async function cmdAgents(args: string[]): Promise<number> {
  const [sub, ...rest] = args;
  const pos = positionals(rest);

  if (!sub || sub === "list") {
    const scope = pos[0] ?? "global";
    const result = await runAgentsList(scope);
    printLine(`Agents (${result.scope}):`);
    if (result.agents.length === 0) {
      printLine("  (none)");
    } else {
      for (const a of result.agents) {
        const desc = a.description ? `  — ${a.description}` : "";
        printLine(`  ${a.name}${desc}`);
      }
    }
    return 0;
  }

  if (sub === "new") {
    if (pos.length === 0) {
      printError("agents new requires an agent name");
      return 1;
    }
    const [agentName, scope = "global"] = pos;
    const result = await runAgentsNew(agentName, scope);
    if (result.alreadyExists) {
      printLine(`Agent "${agentName}" already exists at ${result.path}`);
    } else {
      printLine(`Created agent "${agentName}" at ${result.path}`);
    }
    return 0;
  }

  printError(`Unknown agents subcommand: ${sub}`);
  printLine("Usage: dot-agents agents [list|new] [args]");
  return 1;
}

async function cmdHooks(args: string[]): Promise<number> {
  const [sub, ...rest] = args;
  const pos = positionals(rest);

  if (!sub || sub === "list") {
    const scope = pos[0] ?? "global";
    const result = await runHooksList(scope);
    printLine(`Hooks (${result.scope}):`);
    switch (result.kind) {
      case "canonical":
        for (const b of result.bundles ?? []) {
          printLine(`  [bundle] ${b.name}`);
        }
        break;
      case "legacy":
        for (const e of result.legacyEvents ?? []) {
          printLine(`  [${e.event}] ${e.count} hook(s)`);
        }
        break;
      case "none":
        printLine("  (none configured)");
        break;
    }
    return 0;
  }

  printError(`Unknown hooks subcommand: ${sub}`);
  printLine("Usage: dot-agents hooks [list] [scope]");
  return 1;
}

function printHelp(): void {
  printLine("dot-agents TypeScript CLI — Stage 1 MVP");
  printLine("");
  printLine("Usage: dot-agents-ts <command> [subcommand] [args...] [flags]");
  printLine("");
  printLine("Commands:");
  printLine("  init                  Initialize ~/.agents/ directory structure");
  printLine("  add <path>            Register a project directory");
  printLine("  refresh [project]     Report refresh status for managed projects");
  printLine("  status                Show project health and canonical store summary");
  printLine("  doctor                Audit local dot-agents installation");
  printLine("  skills list [scope]   List skills in ~/.agents/skills/");
  printLine("  skills new <name>     Create a new skill scaffold");
  printLine("  agents list [scope]   List agents in ~/.agents/agents/");
  printLine("  agents new <name>     Create a new agent scaffold");
  printLine("  hooks list [scope]    Inspect hook definitions");
  printLine("");
  printLine("Global flags:");
  printLine("  --dry-run             Print what would be done without changes (init)");
  printLine("  --force               Overwrite existing entries");
  printLine("  --verbose, -v         Show more detail (doctor)");
  for (const line of boundaryHelpLines()) {
    printLine(line);
  }
}

// ---- Main dispatcher ----

async function main(): Promise<void> {
  const [, , command, ...rest] = process.argv;

  if (!command || command === "--help" || command === "-h" || command === "help") {
    printHelp();
    process.exit(0);
  }

  let exitCode = 0;
  try {
    switch (command) {
      case "init":
        exitCode = await cmdInit(rest);
        break;
      case "add":
        exitCode = await cmdAdd(rest);
        break;
      case "refresh":
        exitCode = await cmdRefresh(rest);
        break;
      case "status":
        exitCode = await cmdStatus(rest);
        break;
      case "doctor":
        exitCode = await cmdDoctor(rest);
        break;
      case "skills":
        exitCode = await cmdSkills(rest);
        break;
      case "agents":
        exitCode = await cmdAgents(rest);
        break;
      case "hooks":
        exitCode = await cmdHooks(rest);
        break;
      default:
        printError(`Unknown command: ${command}`);
        printHelp();
        exitCode = 1;
    }
  } catch (e) {
    printError(String(e));
    exitCode = 2;
  }

  process.exit(exitCode);
}

main();
