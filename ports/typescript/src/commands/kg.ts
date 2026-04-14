/**
 * Read-only KG helpers for the TypeScript port (phase-4 boundary).
 *
 * KG query / graph operations require the Go CLI and are intentionally stubbed here.
 * No subprocesses — filesystem + env checks only.
 */

import { stat } from "node:fs/promises";
import { join } from "node:path";

export interface KgHealthOptions {
  /** Effective KG home (tests); when unset, uses `process.env.KG_HOME`. */
  kgHomeOverride?: string;
}

export interface KgHealthResult {
  healthy: boolean;
  /** Resolved KG root directory, or empty string when none is configured. */
  kgHome: string;
  warnings: string[];
}

export interface KgQueryOptions {
  kgHomeOverride?: string;
}

export interface KgQueryResult {
  query: string;
  result: string;
}

const KG_QUERY_STUB =
  "KG query requires Go CLI — not available in TS port";

function effectiveKgHome(opts: KgHealthOptions): string {
  const fromOverride = opts.kgHomeOverride?.trim();
  if (fromOverride) return fromOverride;
  return (process.env.KG_HOME ?? "").trim();
}

/**
 * Reports whether a knowledge-graph home looks usable: non-empty KG home and a `notes/` directory.
 * Uses `kgHomeOverride` when set; otherwise requires `KG_HOME`.
 */
export async function runKgHealth(opts: KgHealthOptions = {}): Promise<KgHealthResult> {
  const kgHome = effectiveKgHome(opts);
  const warnings: string[] = [];

  if (!kgHome) {
    warnings.push("KG_HOME is not set (set KG_HOME or pass kgHomeOverride)");
    return { healthy: false, kgHome: "", warnings };
  }

  try {
    const homeStat = await stat(kgHome);
    if (!homeStat.isDirectory()) {
      warnings.push(`KG_HOME is not a directory: ${kgHome}`);
      return { healthy: false, kgHome, warnings };
    }
  } catch {
    warnings.push(`KG_HOME path does not exist: ${kgHome}`);
    return { healthy: false, kgHome, warnings };
  }

  const notesDir = join(kgHome, "notes");
  try {
    const notesStat = await stat(notesDir);
    if (!notesStat.isDirectory()) {
      warnings.push("notes exists but is not a directory");
      return { healthy: false, kgHome, warnings };
    }
  } catch {
    warnings.push("missing notes/ subdirectory under KG_HOME");
    return { healthy: false, kgHome, warnings };
  }

  return { healthy: true, kgHome, warnings };
}

/**
 * Intentional stub: full KG query is Go-only per phase-4 boundary decision.
 */
export async function runKgQuery(query: string, opts: KgQueryOptions = {}): Promise<KgQueryResult> {
  void opts.kgHomeOverride;
  return {
    query,
    result: KG_QUERY_STUB,
  };
}
