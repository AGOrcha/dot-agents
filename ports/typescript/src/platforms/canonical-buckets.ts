/**
 * Canonical ~/.agents store buckets — mirrors internal/platform/buckets.go
 * (CanonicalStoreBucketSpecs, stage-1 + stage-2 ordering).
 *
 * Used by `status` for parity with Go `dot-agents status` and by `init` to
 * create `~/.agents/<bucket>/global` for each bucket (see commands/init.go).
 */

export const PLUGIN_MANIFEST_NAME = "PLUGIN.yaml" as const;

export interface CanonicalBucketSpec {
  readonly name: string;
  readonly stage: 1 | 2;
  /** When true, count only subdirs of each scope that contain `markerFile`. */
  readonly countDirs: boolean;
  /** Required when countDirs is true (e.g. SKILL.md, AGENT.md). */
  readonly markerFile: string;
}

/** Stage 1 + Stage 2, same order as platform.CanonicalStoreBucketSpecs(). */
export const CANONICAL_BUCKET_SPECS: readonly CanonicalBucketSpec[] = [
  { name: "rules", stage: 1, countDirs: false, markerFile: "" },
  { name: "settings", stage: 1, countDirs: false, markerFile: "" },
  { name: "mcp", stage: 1, countDirs: false, markerFile: "" },
  { name: "skills", stage: 1, countDirs: true, markerFile: "SKILL.md" },
  { name: "agents", stage: 1, countDirs: true, markerFile: "AGENT.md" },
  { name: "hooks", stage: 1, countDirs: true, markerFile: "HOOK.yaml" },
  { name: "commands", stage: 2, countDirs: false, markerFile: "" },
  { name: "output-styles", stage: 2, countDirs: false, markerFile: "" },
  { name: "ignore", stage: 2, countDirs: false, markerFile: "" },
  { name: "modes", stage: 2, countDirs: false, markerFile: "" },
  { name: "plugins", stage: 2, countDirs: true, markerFile: PLUGIN_MANIFEST_NAME },
  { name: "themes", stage: 2, countDirs: false, markerFile: "" },
  { name: "prompts", stage: 2, countDirs: false, markerFile: "" },
] as const;
