#!/usr/bin/env node
/**
 * Boundary sync check.
 *
 * Diffs the Go cobra command tree (parsed statically from commands/**.go and
 * cmd/dot-agents/main.go) against:
 *   1. The TS commander tree in ports/typescript/src/cli.ts (top-level only)
 *   2. The boundary spec in docs/typescript-port-boundary.json
 *
 * Failure modes:
 *   - Go gained a top-level command that is not classified in
 *     stage1_commands / phase4_optouts / phase5_deferred.
 *   - A Stage 1 command exists in Go but is not mirrored in the TS CLI.
 *   - A Stage 1 command's local flag/subcommand surface drifted from
 *     stage1_flag_lock without a corresponding entry in stage2_deferred_subitems.
 *
 * Exit codes:
 *   0 — clean
 *   1 — one or more boundary violations (printed with actionable diffs)
 *   2 — internal/parse error (malformed Go source, missing spec, etc.)
 *
 * NOTE: this is a static parser; it intentionally avoids invoking the Go
 * binary or adding a `__dump-tree` Go subcommand. It is a "first-pass" check
 * — false negatives are tolerable, but every failure message should be
 * actionable enough that a contributor knows exactly which file to edit.
 */

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// --- Locate repo root from this script's location ----------------------------

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
// scripts/check-boundary.ts → repo root is three levels up
const REPO_ROOT = resolve(SCRIPT_DIR, "..", "..", "..");

// --- Boundary spec types -----------------------------------------------------

export interface BoundarySpec {
  version: number;
  stage1_commands: string[];
  stage1_flag_lock: Record<string, string[]>;
  phase4_optouts: string[];
  phase5_deferred: string[];
  stage2_deferred_subitems: string[];
}

// --- Parser results ----------------------------------------------------------

export interface GoCommandSurface {
  /** Top-level command name (e.g. "init", "skills"). */
  name: string;
  /** Constructor function name from commands/root.go (e.g. "NewInitCmd"). */
  constructor: string;
  /** Local flags declared via cmd.Flags().XxxVar(...). Excludes inherited
   *  persistent root flags. */
  localFlags: string[];
  /** Direct subcommand names (e.g. ["list", "new"] for "skills"). */
  subcommands: string[];
}

export interface TSCommandTree {
  /** Top-level command names dispatched in cli.ts. */
  topLevel: string[];
}

export interface CheckFailure {
  category:
    | "go-unclassified-top-level"
    | "stage1-not-mirrored"
    | "stage1-locked-item-missing"
    | "stage1-undocumented-addition";
  message: string;
}

export interface CheckResult {
  failures: CheckFailure[];
  goCommands: GoCommandSurface[];
  tsTopLevel: string[];
  spec: BoundarySpec;
}

// --- Persistent root flags (defined in commands/root.go) ---------------------
//
// These are inherited by every subcommand. They are intentionally excluded
// from the per-command "local flags" surface so the diff focuses on flags
// that are unique to each command.
const PERSISTENT_ROOT_FLAGS = new Set([
  "--dry-run",
  "--force",
  "--verbose",
  "--yes",
  "--json",
  "--help",
]);

// --- Spec loader -------------------------------------------------------------

export function loadSpec(repoRoot: string = REPO_ROOT): BoundarySpec {
  const path = join(repoRoot, "docs", "typescript-port-boundary.json");
  if (!existsSync(path)) {
    throw new Error(
      `Boundary spec not found at ${path}. Run from a checkout that contains docs/typescript-port-boundary.json.`,
    );
  }
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (err) {
    throw new Error(`Failed to read boundary spec at ${path}: ${err}`);
  }
  if (raw.trim().length === 0) {
    throw new Error(
      `Boundary spec at ${path} is empty. Restore the file or run \`git checkout docs/typescript-port-boundary.json\`.`,
    );
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `Boundary spec at ${path} is not valid JSON: ${err}. Fix the JSON syntax before re-running the boundary check.`,
    );
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error(`Boundary spec at ${path} did not parse to an object.`);
  }
  const spec = parsed as Partial<BoundarySpec>;
  for (const key of [
    "stage1_commands",
    "phase4_optouts",
    "phase5_deferred",
    "stage2_deferred_subitems",
  ] as const) {
    if (!Array.isArray(spec[key])) {
      throw new TypeError(
        `Boundary spec at ${path} is missing required array field "${key}".`,
      );
    }
  }
  if (!spec.stage1_flag_lock || typeof spec.stage1_flag_lock !== "object") {
    throw new Error(
      `Boundary spec at ${path} is missing required object field "stage1_flag_lock".`,
    );
  }
  return spec as BoundarySpec;
}

// --- Go parser ---------------------------------------------------------------
//
// The Go parser is intentionally minimal. It extracts:
//   1. Top-level constructor calls from commands/root.go
//      (root.AddCommand(NewXxxCmd()))
//   2. The Use: literal for each constructor's primary cobra.Command
//   3. Local flags (cmd.Flags().<Type>Var{P,}(..., "name", ...))
//   4. Direct subcommand Use: literals
//
// Anti-patterns to be aware of:
//   - Wrappers in commands/<cmd>.go that delegate to commands/<cmd>/cmd.go
//     (e.g. NewAgentsCmd, NewHooksCmd). The parser follows one level of
//     redirection by also scanning commands/<cmd>/cmd.go.
//   - cobra.Command literals declared in helper functions other than the
//     primary constructor. These are still picked up as long as the file
//     is in the search set.

function readRoot(repoRoot: string): string {
  const path = join(repoRoot, "commands", "root.go");
  if (!existsSync(path)) {
    throw new Error(
      `Could not locate commands/root.go at ${path}. Boundary check expects to run from the dot-agents repo root.`,
    );
  }
  return readFileSync(path, "utf8");
}

/** Extracts the ordered list of `NewXxxCmd` constructors registered on root. */
export function parseRootConstructors(rootSource: string): string[] {
  const re = /root\.AddCommand\(\s*(New\w+Cmd)\s*\(\s*\)\s*\)/g;
  const out: string[] = [];
  let match: RegExpExecArray | null;
  while ((match = re.exec(rootSource)) !== null) {
    out.push(match[1]);
  }
  if (out.length === 0) {
    throw new Error(
      "Could not find any `root.AddCommand(NewXxxCmd())` lines in commands/root.go. " +
        "The cobra wiring may have been refactored — update scripts/check-boundary.ts " +
        "to match.",
    );
  }
  return out;
}

/** Searches commands/ recursively for files containing the constructor. */
function locateConstructor(
  repoRoot: string,
  ctor: string,
): { filePath: string; packageDir: string } {
  const commandsDir = join(repoRoot, "commands");
  const candidates: string[] = [];
  walkGoFiles(commandsDir, candidates);
  // Prefer non-test files.
  const nonTest = candidates.filter((f) => !f.endsWith("_test.go"));
  // Match `func <ctor>(` — argument list may be empty or take a Deps struct.
  const re = new RegExp(String.raw`^func\s+${ctor}\s*\(`, "m");
  for (const path of nonTest) {
    const src = readFileSync(path, "utf8");
    if (re.test(src)) {
      return { filePath: path, packageDir: dirname(path) };
    }
  }
  throw new Error(
    `Could not find the body of constructor ${ctor} under commands/. ` +
      `Looked in ${nonTest.length} .go files. If the constructor was renamed, ` +
      `update scripts/check-boundary.ts.`,
  );
}

function walkGoFiles(dir: string, out: string[]): void {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    let st;
    try {
      st = statSync(full);
    } catch {
      continue;
    }
    if (st.isDirectory()) {
      // Skip vendored dependencies and static asset dirs.
      if (entry === "static" || entry === "testdata" || entry.startsWith(".")) {
        continue;
      }
      walkGoFiles(full, out);
    } else if (entry.endsWith(".go")) {
      out.push(full);
    }
  }
}

/**
 * Resolves a Go import alias to a directory. Given:
 *   wf "github.com/NikashPrakash/dot-agents/commands/workflow"
 * and alias "wf", returns the on-disk path of commands/workflow/.
 *
 * Falls back to `packageDir/<alias>` if no explicit alias line is found
 * (covers the common case where the import alias matches the directory name).
 */
function resolvePackageDir(
  packageDir: string,
  ctorSrc: string,
  alias: string,
): string | undefined {
  // Aliased import: `wf "github.com/.../workflow"`
  const aliasRe = new RegExp(
    String.raw`\b${alias}\s+"([^"]+)"`,
  );
  const aliasMatch = aliasRe.exec(ctorSrc);
  if (aliasMatch) {
    const importPath = aliasMatch[1];
    // The repo prefix is everything before `/commands/`. We cannot infer the
    // repo root from imports alone; instead, find the segment after
    // `/commands/` and join with the parent of packageDir.
    const idx = importPath.lastIndexOf("/commands/");
    if (idx !== -1) {
      const sub = importPath.slice(idx + "/commands/".length);
      return join(packageDir, sub);
    }
  }
  // Bare import: `"github.com/.../commands/agents"` whose last segment
  // matches the alias name.
  const bareRe = new RegExp(String.raw`"([^"]*\/commands\/${alias}(?:\/[^"]+)?)"`);
  const bareMatch = bareRe.exec(ctorSrc);
  if (bareMatch) {
    const importPath = bareMatch[1];
    const idx = importPath.lastIndexOf("/commands/");
    if (idx !== -1) {
      return join(packageDir, importPath.slice(idx + "/commands/".length));
    }
  }
  // Last-resort: assume alias === subdir name.
  return join(packageDir, alias);
}

/**
 * Reads all .go files in a package directory and returns their concatenated
 * source. Used because cobra.Command literals for a single command may be
 * spread across cmd.go / list.go / new.go / etc. in the package.
 *
 * In the top-level `commands/` package, multiple distinct top-level commands
 * are defined in sibling files (commands/init.go, commands/add.go, …). When
 * `restrictToFile` is set we read only that file plus any explicit "extras"
 * — sufficient for those single-file top-levels — and avoid mixing surfaces
 * across siblings. When `restrictToFile` is unset we read every .go file in
 * the directory (right behavior for sub-packages like commands/agents/).
 */
function readPackageSource(
  packageDir: string,
  restrictToFile?: string,
): string {
  if (restrictToFile) {
    return readFileSync(restrictToFile, "utf8");
  }
  const entries = readdirSync(packageDir).filter(
    (e) => e.endsWith(".go") && !e.endsWith("_test.go"),
  );
  return entries
    .map((e) => readFileSync(join(packageDir, e), "utf8"))
    .join("\n\n");
}

/**
 * Extracts the bare command name from a `Use:` literal.
 * Handles arguments after the name: `Use: "add <path>"` → "add".
 */
function bareName(useLiteral: string): string {
  return useLiteral.split(/\s+/)[0];
}

/**
 * Walks a Go source string and returns every `Use: "..."` literal in order.
 */
function extractUseLiterals(src: string): string[] {
  const re = /Use:\s*"([^"]+)"/g;
  const out: string[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    out.push(m[1]);
  }
  return out;
}

/**
 * Extracts every flag name declared via cmd.Flags().XxxVar(...., "name", ...).
 * Captures both Var and VarP variants.
 */
function extractLocalFlags(src: string): string[] {
  const out = new Set<string>();
  let pos = 0;
  while (pos < src.length) {
    const flagsAt = src.indexOf("Flags().", pos);
    if (flagsAt === -1) break;
    const open = src.indexOf("(", flagsAt + "Flags().".length);
    if (open === -1) break;
    const close = findCallEnd(src, open);
    if (close === -1) {
      pos = open + 1;
      continue;
    }

    // Skip non-flag-looking captures (defensive; should always be a flag name).
    const name = firstStringLiteral(src.slice(open + 1, close));
    if (name && /^[a-zA-Z][a-zA-Z0-9-]*$/.test(name)) {
      out.add("--" + name);
    }
    pos = close + 1;
  }
  return [...out].sort((a, b) => a.localeCompare(b));
}

function findCallEnd(src: string, open: number): number {
  let depth = 0;
  let quote: string | null = null;
  for (let i = open; i < src.length; i++) {
    const ch = src[i];
    if (quote) {
      if (ch === "\\" && i+1 < src.length) {
        i++;
        continue;
      }
      if (ch === quote) quote = null;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      quote = ch;
      continue;
    }
    if (ch === "(") depth++;
    if (ch === ")") {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function firstStringLiteral(src: string): string | null {
  let quote: string | null = null;
  let start = -1;
  for (let i = 0; i < src.length; i++) {
    const ch = src[i];
    if (!quote) {
      if (ch === '"' || ch === "'") {
        quote = ch;
        start = i + 1;
      }
      continue;
    }
    if (ch === "\\" && i + 1 < src.length) {
      i++;
      continue;
    }
    if (ch === quote) {
      return src.slice(start, i);
    }
  }
  return null;
}

/**
 * Detects and redirects a wrapper constructor to its delegate package.
 * Returns packageDir unchanged if the constructor is not a wrapper.
 */
function resolveDelegatePackage(
  packageDir: string,
  ctorSrc: string,
  filePath: string,
): { packageDir: string; restrictToFile: string | undefined } {
  // Check if this is a wrapper file (no cobra.Command literal in body).
  if (/&cobra\.Command\s*\{/.test(ctorSrc)) {
    return { packageDir, restrictToFile: filePath };
  }
  // Look for delegate call: e.g. `agents.NewAgentsCmd(...)` or `wf.NewCmd(...)`.
  const delegate = /\b(\w+)\.New\w*\s*\(/.exec(ctorSrc);
  if (!delegate) {
    return { packageDir, restrictToFile: filePath };
  }
  const alias = delegate[1];
  const subDir = resolvePackageDir(packageDir, ctorSrc, alias);
  if (!subDir || !existsSync(subDir) || !statSync(subDir).isDirectory()) {
    return { packageDir, restrictToFile: filePath };
  }
  return { packageDir: subDir, restrictToFile: undefined };
}

/**
 * For a Stage 1 top-level command, returns its surface:
 *   - The first `Use:` literal in the constructor file is the top-level name.
 *   - Subsequent `Use:` literals in the package are subcommands.
 *   - Local flags come from any `cmd.Flags().XxxVar(...)` in the package.
 */
export function parseGoCommandSurface(
  repoRoot: string,
  ctor: string,
): GoCommandSurface {
  const { filePath, packageDir } = locateConstructor(repoRoot, ctor);
  const ctorSrc = readFileSync(filePath, "utf8");
  const { packageDir: packageDirToScan, restrictToFile } = resolveDelegatePackage(
    packageDir,
    ctorSrc,
    filePath,
  );

  const src = readPackageSource(packageDirToScan, restrictToFile);
  const uses = extractUseLiterals(src);
  if (uses.length === 0) {
    throw new Error(
      `No Use: literals found for constructor ${ctor} in ${packageDirToScan}. ` +
        `The cobra wiring may have moved — inspect manually and update the parser.`,
    );
  }
  // Top-level Use: in same file ideally; if scanning a sub-package, the
  // first cobra.Command literal in that package is the top-level one.
  const topUse = uses[0];
  const topName = bareName(topUse);
  const subcommands = uses
    .slice(1)
    .map(bareName)
    // Filter out empty / sentinel uses (defensive).
    .filter((s) => s.length > 0 && /^[a-zA-Z][a-zA-Z0-9_-]*$/.test(s));

  const flags = extractLocalFlags(src).filter(
    (f) => !PERSISTENT_ROOT_FLAGS.has(f),
  );

  return {
    name: topName,
    constructor: ctor,
    localFlags: flags,
    subcommands,
  };
}

/** Public entry: returns the full Go command surface map. */
export function parseGoCommandTree(
  repoRoot: string = REPO_ROOT,
): GoCommandSurface[] {
  const rootSrc = readRoot(repoRoot);
  // The fallback parser also reads cmd/dot-agents/main.go for completeness.
  // In the current layout, main.go only calls NewRootCommand() and contains
  // no AddCommand wires of its own — but if that ever changes, surface a
  // helpful error rather than silently missing a top-level command.
  const mainPath = join(repoRoot, "cmd", "dot-agents", "main.go");
  if (existsSync(mainPath)) {
    const mainSrc = readFileSync(mainPath, "utf8");
    if (/AddCommand\(/.test(mainSrc)) {
      throw new Error(
        `cmd/dot-agents/main.go now wires AddCommand calls directly. The boundary ` +
          `parser only inspects commands/root.go — extend scripts/check-boundary.ts ` +
          `to also parse main.go before re-running.`,
      );
    }
  }
  const ctors = parseRootConstructors(rootSrc);
  return ctors.map((c) => parseGoCommandSurface(repoRoot, c));
}

// --- TS parser ---------------------------------------------------------------

export function parseTSCommandTree(repoRoot: string = REPO_ROOT): TSCommandTree {
  const path = join(repoRoot, "ports", "typescript", "src", "cli.ts");
  if (!existsSync(path)) {
    throw new Error(
      `Could not locate ports/typescript/src/cli.ts at ${path}. ` +
        `The TS port may have been moved or removed.`,
    );
  }
  const src = readFileSync(path, "utf8");
  // The dispatcher uses `case "<name>":` arms inside the main switch.
  // Capture each one and ignore obvious sentinels like "default".
  const re = /case\s+"([a-zA-Z][a-zA-Z0-9_-]*)"\s*:/g;
  const seen = new Set<string>();
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    const name = m[1];
    // Skip help arms that the parser shouldn't treat as commands.
    if (name === "help" || name === "default") continue;
    seen.add(name);
  }
  if (seen.size === 0) {
    throw new Error(
      `parseTSCommandTree: no command case-arms found in ${path}. ` +
        `The dispatcher may have switched to a different shape — update the parser.`,
    );
  }
  return { topLevel: [...seen].sort((a, b) => a.localeCompare(b)) };
}

// --- Diff core ---------------------------------------------------------------

function escapeRegex(s: string): string {
  return s.replaceAll(/[.*+?^${}()|[\]\\]/g, String.raw`\$&`);
}

function classifyTopLevel(
  goCmd: GoCommandSurface,
  spec: BoundarySpec,
): "stage1" | "phase4" | "phase5" | "unclassified" {
  if (spec.stage1_commands.includes(goCmd.name)) return "stage1";
  if (spec.phase4_optouts.includes(goCmd.name)) return "phase4";
  // phase5_deferred entries can be either bare command names OR feature-level
  // qualifiers like "workflow:read-only-cli-subset". Match either form.
  if (
    spec.phase5_deferred.some(
      (d) => d === goCmd.name || d.startsWith(goCmd.name + ":"),
    )
  ) {
    return "phase5";
  }
  return "unclassified";
}

/** Builds the per-command surface set for diff: locked items must be in
 *  this set; unlocked-additions are flagged unless documented. */
function commandSurfaceItems(go: GoCommandSurface): string[] {
  return [...go.subcommands, ...go.localFlags];
}

function diffStage1Surface(
  go: GoCommandSurface,
  spec: BoundarySpec,
): CheckFailure[] {
  const failures: CheckFailure[] = [];
  const lockedItems = spec.stage1_flag_lock[go.name] ?? [];
  const surface = new Set(commandSurfaceItems(go));

  // Persistent root flags satisfy a "locked" entry as well — the lock can
  // declaratively pin inherited flags (e.g. init has --dry-run, --force).
  const surfacePlusPersistent = new Set([...surface, ...PERSISTENT_ROOT_FLAGS]);

  // 1. Locked items missing from Go surface → removal failure.
  for (const item of lockedItems) {
    if (!surfacePlusPersistent.has(item)) {
      failures.push({
        category: "stage1-locked-item-missing",
        message:
          `Stage 1 command "${go.name}" lost locked surface item "${item}". ` +
          `Either restore it in commands/${go.constructor.replaceAll(/^New|Cmd$/g, "").toLowerCase()}* ` +
          `or update docs/typescript-port-boundary.json (remove from stage1_flag_lock["${go.name}"] ` +
          `and decide if it is now phase5_deferred or stage2_deferred_subitems).`,
      });
    }
  }

  // 2. Unlocked Go items not in stage2_deferred_subitems → addition failure.
  //
  // The deferred list uses a flexible notation:
  //   "<cmd>:<item>"            — single subcommand or flag of the top-level
  //   "<cmd>:<sub><flag>"       — flag scoped to a subcommand, e.g.
  //                               "agents:remove--purge". The parser cannot
  //                               cheaply attribute flags to subcommands, so we
  //                               match on token containment instead.
  const lockedSet = new Set(lockedItems);
  const deferredCmdEntries = spec.stage2_deferred_subitems.filter((d) =>
    d.startsWith(`${go.name}:`),
  );
  for (const item of surface) {
    if (lockedSet.has(item)) continue;
    const exact = `${go.name}:${item}`;
    const isDeferred =
      deferredCmdEntries.includes(exact) ||
      // Loose match for flag tokens that appear inside a compound deferred
      // entry like "agents:remove--purge" → matches "--purge".
      (item.startsWith("--") &&
        deferredCmdEntries.some((d) => d.includes(item))) ||
      // Loose match for subcommand tokens nested inside a compound entry
      // (e.g. spec writes "agents:remove--purge"; parser sees "remove").
      (!item.startsWith("--") &&
        deferredCmdEntries.some((d) =>
          new RegExp(
            String.raw`(^|:|--|\\b)${escapeRegex(item)}(--|$|\\b)`,
          ).test(d),
        ));
    if (isDeferred) continue;
    failures.push({
      category: "stage1-undocumented-addition",
      message:
        `Stage 1 command "${go.name}" gained surface item "${item}" not in ` +
        `stage1_flag_lock["${go.name}"] and not in stage2_deferred_subitems. ` +
        `Classify it: add to stage1_flag_lock["${go.name}"] (mirrored in TS) ` +
        `or stage2_deferred_subitems as "${exact}" (deferred).`,
    });
  }

  return failures;
}

export function runCheck(
  repoRoot: string = REPO_ROOT,
): CheckResult {
  const spec = loadSpec(repoRoot);
  const goCommands = parseGoCommandTree(repoRoot);
  const ts = parseTSCommandTree(repoRoot);
  const tsTopLevel = ts.topLevel;
  const tsSet = new Set(tsTopLevel);

  const failures: CheckFailure[] = [];

  // Step 4: classify every top-level Go command.
  for (const go of goCommands) {
    const klass = classifyTopLevel(go, spec);
    switch (klass) {
      case "unclassified":
        failures.push({
          category: "go-unclassified-top-level",
          message:
            `Go gained a top-level command "${go.name}" not classified in the ` +
            `boundary spec. Add it to docs/typescript-port-boundary.json — pick ` +
            `one of stage1_commands (must mirror in TS), phase4_optouts ` +
            `(intentionally Go-only), or phase5_deferred (deferred to a future ` +
            `Stage 2 wave).`,
        });
        break;
      case "stage1":
        if (!tsSet.has(go.name)) {
          failures.push({
            category: "stage1-not-mirrored",
            message:
              `Stage 1 command "${go.name}" is required by the boundary spec ` +
              `but is not mirrored in ports/typescript/src/cli.ts. Either add ` +
              `a TS dispatch arm for "${go.name}" or move the command from ` +
              `stage1_commands → phase4_optouts/phase5_deferred (with a rationale).`,
          });
        }
        // Always run the flag-level diff for Stage 1 commands.
        failures.push(...diffStage1Surface(go, spec));
        break;
      case "phase4":
      case "phase5":
        // Allowed; no further check.
        break;
    }
  }

  return { failures, goCommands, tsTopLevel, spec };
}

// --- CLI entry ---------------------------------------------------------------

function printResult(result: CheckResult): void {
  if (result.failures.length === 0) {
    process.stdout.write(
      `boundary-sync: OK — ${result.goCommands.length} Go top-level commands ` +
        `classified; ${result.tsTopLevel.length} TS top-level commands present.\n`,
    );
    return;
  }
  process.stderr.write(
    `boundary-sync: FAIL — ${result.failures.length} violation(s):\n\n`,
  );
  for (const [i, f] of result.failures.entries()) {
    process.stderr.write(`  ${i + 1}. [${f.category}] ${f.message}\n\n`);
  }
}

function isMain(): boolean {
  // Detect direct invocation when run as a script.
  // Works for both `node dist/scripts/check-boundary.js` and tsx.
  const argvScript = process.argv[1] ?? "";
  const thisFile = fileURLToPath(import.meta.url);
  return resolve(argvScript) === resolve(thisFile);
}

if (isMain()) {
  try {
    const result = runCheck();
    printResult(result);
    process.exit(result.failures.length === 0 ? 0 : 1);
  } catch (err) {
    process.stderr.write(`boundary-sync: internal error — ${err}\n`);
    process.exit(2);
  }
}
