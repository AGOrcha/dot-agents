/**
 * Shared bucket-list helpers used by `commands/agents.ts` and
 * `commands/skills.ts`. Mirrors the Go-port `internal/projectsync`
 * package's BucketSpec/ListBucket/ReadFrontmatterDescription pair.
 *
 * Two parallel resource buckets ("agents" and "skills") share the
 * same directory layout under `~/.agents/<bucket>/<scope>/<name>/`
 * with a manifest file inside. This file collapses the previously
 * duplicated readDescription + listBucket bodies into one
 * implementation parameterized by BucketSpec.
 */

import type { Dirent } from "node:fs";
import { readdir, stat } from "node:fs/promises";
import { join } from "node:path";

/** Describes a per-resource-type bucket under ~/.agents/<bucket>/. */
export interface BucketSpec {
  /** Directory under ~/.agents/ ("agents" | "skills"). */
  bucket: string;
  /** Marker filename ("AGENT.md" | "SKILL.md"). */
  manifestName: string;
}

/** One entry returned from listBucket. */
export interface BucketEntry {
  name: string;
  description?: string;
  hasManifest: boolean;
}

/** Strip a single matched pair of outer quotes (single or double). */
export function stripOuterQuotes(value: string): string {
  if (value.length < 2) return value;
  const first = value[0];
  const last = value.at(-1);
  if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
    return value.slice(1, -1);
  }
  return value;
}

/**
 * Parse the YAML frontmatter of a markdown file and return the
 * `description:` field value, or undefined if not present / file
 * unreadable. CRLF-tolerant.
 */
export async function readFrontmatterDescription(manifestPath: string): Promise<string | undefined> {
  try {
    const { readFile } = await import("node:fs/promises");
    const content = await readFile(manifestPath, "utf8");
    for (const rawLine of content.split("\n")) {
      const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
      if (!line.startsWith("description:")) continue;
      return stripOuterQuotes(line.slice("description:".length).trim());
    }
  } catch {
    // ignore — readFile failures, malformed frontmatter, etc.
  }
  return undefined;
}

/**
 * List entries under `<home>/<spec.bucket>/<scope>/`, returning each
 * directory's name + manifest presence + parsed description. Mirrors
 * the Go projectsync.ListBucket helper.
 */
export async function listBucket(home: string, scope: string, spec: BucketSpec): Promise<BucketEntry[]> {
  const dir = join(home, spec.bucket, scope);
  const out: BucketEntry[] = [];

  let entries: Dirent[];
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return out;
  }

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const manifestPath = join(dir, entry.name, spec.manifestName);
    let hasManifest = false;
    let description: string | undefined;
    try {
      await stat(manifestPath);
      hasManifest = true;
      description = await readFrontmatterDescription(manifestPath);
    } catch {
      hasManifest = false;
    }
    out.push({ name: entry.name, description, hasManifest });
  }

  return out;
}
