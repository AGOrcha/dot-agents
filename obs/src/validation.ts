import Ajv2020 from "ajv/dist/2020.js";
import workflowIterLogSchema from "../../schemas/workflow-iter-log.schema.json" with { type: "json" };

const SCORE_BANDS: Record<string, true> = {
  excellent: true,
  good: true,
  fair: true,
  poor: true,
  unscored: true,
};
const SCORE_SIGNALS: Record<string, true> = {
  landed: true,
  verifier: true,
  tests: true,
  human_label: true,
  correction_pressure: true,
  scope: true,
  hook_outcomes: true,
  token_efficiency: true,
};
const SCORE_SIDECAR_KEYS: Record<string, true> = {
  iteration: true,
  rubric_version: true,
  scored: true,
  value: true,
  band: true,
  breakdown: true,
  linked_traces_to_outcomes: true,
};
const SCORE_BREAKDOWN_KEYS: Record<string, true> = {
  signal: true,
  label: true,
  present: true,
  sub_score: true,
  detail: true,
  nominal_weight: true,
  effective_weight: true,
  contribution: true,
};

const ajv = new Ajv2020({ allErrors: true, strict: true });
const validateWorkflowIterLog = ajv.compile(workflowIterLogSchema);

export interface ScoreBreakdown {
  signal: string;
  label: string;
  present: boolean;
  sub_score: number;
  detail?: string;
  nominal_weight: number;
  effective_weight: number;
  contribution: number;
}

export interface ScoreSidecar {
  iteration: number;
  rubric_version: string;
  scored: boolean;
  value: number;
  band: string;
  breakdown: ScoreBreakdown[];
  linked_traces_to_outcomes?: boolean;
}

export interface HashableIngestEvent {
  kind: "iteration.checkpointed" | "iteration.scored" | "score.recomputed";
  occurred_at: string;
  plan_id: string;
  task_id: string;
  iteration: number;
  schema_hash: string;
  payload: Record<string, unknown>;
  score_sidecar: ScoreSidecar | null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isUnitNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1;
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: Readonly<Record<string, true>>): boolean {
  return Object.keys(value).every((key) => Object.hasOwn(allowed, key));
}

export function parseScoreSidecar(value: unknown, iteration: number): ScoreSidecar | null {
  if (!isRecord(value)) {
    return null;
  }

  if (
    !hasOnlyKeys(value, SCORE_SIDECAR_KEYS) ||
    value.iteration !== iteration ||
    typeof value.rubric_version !== "string" ||
    value.rubric_version.length === 0 ||
    typeof value.scored !== "boolean" ||
    !isUnitNumber(value.value) ||
    typeof value.band !== "string" ||
    !Object.hasOwn(SCORE_BANDS, value.band) ||
    !Array.isArray(value.breakdown) ||
    (value.linked_traces_to_outcomes !== undefined &&
      typeof value.linked_traces_to_outcomes !== "boolean") ||
    (value.scored && value.band === "unscored") ||
    (!value.scored && (value.value !== 0 || value.band !== "unscored"))
  ) {
    return null;
  }

  const breakdown: ScoreBreakdown[] = [];
  const signals = new Set<string>();
  const breakdownKeys = SCORE_BREAKDOWN_KEYS;
  for (const rawRow of value.breakdown) {
    if (
      !isRecord(rawRow) ||
      !hasOnlyKeys(rawRow, breakdownKeys) ||
      typeof rawRow.signal !== "string" ||
      !Object.hasOwn(SCORE_SIGNALS, rawRow.signal) ||
      signals.has(rawRow.signal) ||
      typeof rawRow.label !== "string" ||
      typeof rawRow.present !== "boolean" ||
      !isUnitNumber(rawRow.sub_score) ||
      (rawRow.detail !== undefined && typeof rawRow.detail !== "string") ||
      !isUnitNumber(rawRow.nominal_weight) ||
      !isUnitNumber(rawRow.effective_weight) ||
      !isUnitNumber(rawRow.contribution)
    ) {
      return null;
    }
    signals.add(rawRow.signal);
    const row: ScoreBreakdown = {
      signal: rawRow.signal,
      label: rawRow.label,
      present: rawRow.present,
      sub_score: rawRow.sub_score,
      nominal_weight: rawRow.nominal_weight,
      effective_weight: rawRow.effective_weight,
      contribution: rawRow.contribution,
    };
    if (rawRow.detail !== undefined) {
      row.detail = rawRow.detail;
    }
    breakdown.push(row);
  }

  const score: ScoreSidecar = {
    iteration: value.iteration,
    rubric_version: value.rubric_version,
    scored: value.scored,
    value: value.value,
    band: value.band,
    breakdown,
  };
  if (value.linked_traces_to_outcomes !== undefined) {
    score.linked_traces_to_outcomes = value.linked_traces_to_outcomes;
  }
  return score;
}

function assertValidUnicode(value: string): void {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.codePointAt(index) ?? 0;
    if (code > 0xffff) {
      // A valid surrogate pair: codePointAt combined it, so skip the low half.
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      throw new TypeError("canonical JSON contains an unpaired surrogate");
    } else if (code >= 0xd800 && code <= 0xdbff && index + 1 < value.length) {
      throw new TypeError("canonical JSON contains an unpaired surrogate");
    }
  }
}

function canonicalize(value: unknown): string {
  if (value === null) {
    return "null";
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new TypeError("canonical JSON contains a non-finite number");
    }
    return JSON.stringify(value);
  }
  if (typeof value === "string") {
    assertValidUnicode(value);
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map((item) => canonicalize(item)).join(",")}]`;
  }
  if (isRecord(value)) {
    return `{${Object.keys(value)
      // explicit code-unit comparator (NOT localeCompare): canonical JSON must
      // stay byte-stable for signing/hashing, so keys sort by UTF-16 code unit
      .sort((a, b) => {
        if (a < b) return -1;
        if (a > b) return 1;
        return 0;
      })
      .map((key) => {
        assertValidUnicode(key);
        return `${JSON.stringify(key)}:${canonicalize(value[key])}`;
      })
      .join(",")}}`;
  }
  throw new TypeError("canonical JSON contains a non-JSON value");
}

async function sha256Hex(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export async function validateEventContent(event: HashableIngestEvent): Promise<boolean> {
  if (
    !validateWorkflowIterLog(event.payload) ||
    event.payload.wave !== event.plan_id ||
    event.payload.task_id !== event.task_id ||
    event.payload.iteration !== event.iteration
  ) {
    return false;
  }

  const canonical = canonicalize({
    kind: event.kind,
    occurred_at: event.occurred_at,
    plan_id: event.plan_id,
    task_id: event.task_id,
    iteration: event.iteration,
    payload: event.payload,
    score_sidecar: event.score_sidecar,
  });
  return (await sha256Hex(canonical)) === event.schema_hash;
}
