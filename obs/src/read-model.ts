const API_ROOT = "/api/v1/observability";
const DEFAULT_LIMIT = 50;
const MAX_LIMIT = 500;

const WINNING_CTE = `
WITH ranked AS (
  SELECT
    i.*,
    s.rubric_version,
    COALESCE(s.scored, 0) AS scored,
    s.score,
    COALESCE(s.band, 'unscored') AS band,
    ROW_NUMBER() OVER (
      PARTITION BY i.project_id, i.plan_id, i.task_id, i.iteration
      ORDER BY i.occurred_at DESC, i.ingested_at DESC, i.schema_hash DESC
    ) AS version_rank
  FROM iterations AS i
  LEFT JOIN scores AS s
    ON s.project_id = i.project_id
   AND s.plan_id = i.plan_id
   AND s.task_id = i.task_id
   AND s.iteration = i.iteration
   AND s.schema_hash = i.schema_hash
  WHERE i.project_id = ?
), winning AS (
  SELECT * FROM ranked WHERE version_rank = 1
)`;

const ITERATION_COLUMNS = `
  iteration, session_id, iteration_schema_version, date, wave, task_id,
  commit_sha, rubric_version, scored, score, band, files_changed, lines_added,
  lines_removed, retries, integrity_observation_count, transcript_turn_count,
  input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
  cache_hit_rate, verifiers_json, integrity_json, objective_json, occurred_at,
  ingested_at, schema_hash, plan_id`;

interface RunRow {
  session_id: string;
  harness: string;
  model: string;
  wave: string;
  rubric_version: string;
  iteration_count: number;
  scored: number;
  score: number | null;
  band: string;
  first_iteration: number | null;
  last_iteration: number | null;
  last_update: string | null;
  iter_log_dir: string;
  mean_cache_hit_rate: number | null;
}

interface IterationRow {
  iteration: number;
  session_id: string;
  iteration_schema_version: number;
  date: string;
  wave: string;
  task_id: string;
  commit_sha: string;
  rubric_version: string | null;
  scored: number;
  score: number | null;
  band: string;
  files_changed: number;
  lines_added: number;
  lines_removed: number;
  retries: number;
  integrity_observation_count: number;
  transcript_turn_count: number | null;
  input_tokens: number | null;
  output_tokens: number | null;
  cache_read_tokens: number | null;
  cache_creation_tokens: number | null;
  cache_hit_rate: number | null;
  verifiers_json: string;
  integrity_json: string | null;
  objective_json: string | null;
  occurred_at: string;
  ingested_at: string;
  schema_hash: string;
  plan_id: string;
}

interface BreakdownRow {
  signal: string;
  label: string;
  present: number;
  sub_score: number;
  detail: string;
  nominal_weight: number;
  effective_weight: number;
  contribution: number;
}

interface CountRow {
  count: number;
}

interface IterationHealthRow extends CountRow {
  last_update: string | null;
}

interface RubricRow {
  version: string;
  document_json: string;
}

function errorResponse(status: number, code: string, message: string): Response {
  return Response.json(
    { error: { code, message } },
    { status, headers: { "cache-control": "no-store" } },
  );
}

function contentEtag(resource: string, data: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(data));
  let hash = 0xcbf29ce484222325n;
  for (const byte of bytes) {
    hash ^= BigInt(byte);
    hash = BigInt.asUintN(64, hash * 0x100000001b3n);
  }
  return `${resource}:${hash.toString(16).padStart(16, "0")}`;
}

function ifNoneMatchSatisfied(header: string | null, etag: string): boolean {
  if (header === null || header.length === 0) {
    return false;
  }
  return header.split(",").some((candidate) => {
    let normalized = candidate.trim();
    if (normalized.startsWith("W/")) {
      normalized = normalized.slice(2);
    }
    normalized = normalized.replace(/^"|"$/g, "");
    return normalized === "*" || normalized === etag;
  });
}

function respond(
  request: Request,
  resource: string,
  data: unknown,
  count?: number,
  etagOverride?: string,
): Response {
  const etag = etagOverride ?? contentEtag(resource, data);
  const headers = new Headers({
    "cache-control": "no-store",
    etag: `"${etag}"`,
  });
  if (ifNoneMatchSatisfied(request.headers.get("if-none-match"), etag)) {
    return new Response(null, { status: 304, headers });
  }

  const meta: { etag: string; count?: number } = { etag };
  if (count !== undefined) {
    meta.count = count;
  }
  return Response.json({ data, meta }, { headers });
}

function parsePage(url: URL): { limit: number; offset: number } | Response {
  const parseInteger = (
    name: string,
    defaultValue: number,
    minimum: number,
    maximum: number,
    expectation: string,
  ): number | Response => {
    const raw = url.searchParams.get(name);
    if (raw === null || raw === "") {
      return defaultValue;
    }
    if (!/^\d+$/.test(raw)) {
      return errorResponse(400, "bad_request", `invalid "${name}": must be ${expectation}`);
    }
    const value = Number(raw);
    if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
      return errorResponse(400, "bad_request", `invalid "${name}": must be ${expectation}`);
    }
    return value;
  };

  const limit = parseInteger("limit", DEFAULT_LIMIT, 1, MAX_LIMIT, "an integer between 1 and 500");
  if (limit instanceof Response) {
    return limit;
  }
  const offset = parseInteger(
    "offset",
    0,
    0,
    Number.MAX_SAFE_INTEGER,
    "an integer >= 0",
  );
  if (offset instanceof Response) {
    return offset;
  }
  return { limit, offset };
}

function mapRun(row: RunRow): Record<string, unknown> {
  return {
    session_id: row.session_id,
    harness: row.harness,
    model: row.model,
    wave: row.wave,
    rubric_version: row.rubric_version,
    iteration_count: row.iteration_count,
    scored: row.scored === 1,
    score: row.score,
    band: row.band,
    first_iteration: row.first_iteration,
    last_iteration: row.last_iteration,
    last_update: row.last_update,
    iter_log_dir: row.iter_log_dir,
    mean_cache_hit_rate: row.mean_cache_hit_rate,
  };
}

function decodeJson(value: string | null, fallback: unknown): unknown {
  if (value === null) {
    return fallback;
  }
  return JSON.parse(value) as unknown;
}

function mapIteration(
  row: IterationRow,
  details?: {
    breakdown: BreakdownRow[];
  },
): Record<string, unknown> {
  const hasTokenUsage =
    row.input_tokens !== null ||
    row.output_tokens !== null ||
    row.cache_read_tokens !== null ||
    row.cache_creation_tokens !== null ||
    row.cache_hit_rate !== null;
  const dto: Record<string, unknown> = {
    iteration: row.iteration,
    session_id: row.session_id,
    schema_version: row.iteration_schema_version,
    date: row.date,
    wave: row.wave,
    task_id: row.task_id,
    commit: row.commit_sha,
    rubric_version: row.rubric_version ?? "",
    scored: row.scored === 1,
    score: row.score,
    band: row.band,
    files_changed: row.files_changed,
    lines_added: row.lines_added,
    lines_removed: row.lines_removed,
    retries: row.retries,
    integrity_observation_count: row.integrity_observation_count,
    transcript_turn_count: row.transcript_turn_count,
    token_usage: hasTokenUsage
      ? {
          input_tokens: row.input_tokens ?? 0,
          output_tokens: row.output_tokens ?? 0,
          cache_read_tokens: row.cache_read_tokens ?? 0,
          cache_creation_tokens: row.cache_creation_tokens ?? 0,
          cache_hit_rate: row.cache_hit_rate ?? 0,
        }
      : null,
  };

  if (details !== undefined) {
    dto.verifiers = decodeJson(row.verifiers_json, []);
    dto.breakdown = details.breakdown.map((item) => ({
      signal: item.signal,
      label: item.label,
      present: item.present === 1,
      sub_score: item.sub_score,
      detail: item.detail,
      nominal_weight: item.nominal_weight,
      effective_weight: item.effective_weight,
      contribution: item.contribution,
    }));
    dto.integrity = decodeJson(row.integrity_json, []);
    dto.objective = decodeJson(row.objective_json, null);
  }
  return dto;
}

function decodePathSegment(value: string): string | null {
  try {
    const decoded = decodeURIComponent(value);
    return decoded.length > 0 ? decoded : null;
  } catch {
    return null;
  }
}

async function listRuns(request: Request, env: Env, url: URL): Promise<Response> {
  const page = parsePage(url);
  if (page instanceof Response) {
    return page;
  }

  const sorts: Record<string, string> = {
    last_update: "last_update",
    score: "score",
    iteration_count: "iteration_count",
    session_id: "session_id",
  };
  const sort = url.searchParams.get("sort") ?? "last_update";
  const order = url.searchParams.get("order") ?? "desc";
  const band = url.searchParams.get("band") ?? "";
  const harness = url.searchParams.get("harness") ?? "";
  if (!Object.hasOwn(sorts, sort)) {
    return errorResponse(
      400,
      "bad_request",
      'invalid "sort": must be one of last_update|score|iteration_count|session_id',
    );
  }
  if (order !== "asc" && order !== "desc") {
    return errorResponse(400, "bad_request", 'invalid "order": must be one of asc|desc');
  }
  if (
    band !== "" &&
    band !== "excellent" &&
    band !== "good" &&
    band !== "fair" &&
    band !== "poor" &&
    band !== "unscored"
  ) {
    return errorResponse(
      400,
      "bad_request",
      'invalid "band": must be one of excellent|good|fair|poor|unscored',
    );
  }

  const clauses = ["project_id = ?"];
  const bindings: unknown[] = [env.OBS_PROJECT_ID];
  if (band !== "") {
    clauses.push("band = ?");
    bindings.push(band);
  }
  if (harness !== "") {
    clauses.push("harness = ?");
    bindings.push(harness);
  }
  bindings.push(page.limit, page.offset);
  const rows = await env.OBS_DB.prepare(
    `SELECT session_id, harness, model, wave, rubric_version, iteration_count,
            scored, score, band, first_iteration, last_iteration, last_update,
            iter_log_dir, mean_cache_hit_rate
       FROM sessions
      WHERE ${clauses.join(" AND ")}
      ORDER BY ${sorts[sort]} ${order.toUpperCase()}, session_id ${order.toUpperCase()}
      LIMIT ? OFFSET ?`,
  )
    .bind(...bindings)
    .all<RunRow>();
  const data = rows.results.map(mapRun);
  return respond(request, "runs", data, data.length);
}

async function getRun(request: Request, env: Env, rawSessionId: string): Promise<Response> {
  const sessionId = decodePathSegment(rawSessionId);
  if (sessionId === null) {
    return errorResponse(404, "not_found", `no run for session_id "${rawSessionId}"`);
  }
  const row = await env.OBS_DB.prepare(
    `SELECT session_id, harness, model, wave, rubric_version, iteration_count,
            scored, score, band, first_iteration, last_iteration, last_update,
            iter_log_dir, mean_cache_hit_rate
       FROM sessions WHERE project_id = ? AND session_id = ?`,
  )
    .bind(env.OBS_PROJECT_ID, sessionId)
    .first<RunRow>();
  if (row === null) {
    return errorResponse(404, "not_found", `no run for session_id "${sessionId}"`);
  }

  const iterations = await env.OBS_DB.prepare(
    `${WINNING_CTE}
     SELECT iteration, scored, score, band
       FROM winning
      WHERE session_id = ?
      ORDER BY iteration ASC, plan_id ASC, task_id ASC`,
  )
    .bind(env.OBS_PROJECT_ID, sessionId)
    .all<{ iteration: number; scored: number; score: number | null; band: string }>();
  const data = mapRun(row);
  data.per_iteration = iterations.results.map((item) => ({
    iteration: item.iteration,
    scored: item.scored === 1,
    score: item.score,
    band: item.band,
  }));
  return respond(request, "run", data);
}

async function listIterations(
  request: Request,
  env: Env,
  url: URL,
  rawSessionId: string,
): Promise<Response> {
  const page = parsePage(url);
  if (page instanceof Response) {
    return page;
  }
  const sessionId = decodePathSegment(rawSessionId);
  if (sessionId === null) {
    return errorResponse(404, "not_found", `no run for session_id "${rawSessionId}"`);
  }
  const exists = await env.OBS_DB.prepare(
    "SELECT 1 AS found FROM sessions WHERE project_id = ? AND session_id = ?",
  )
    .bind(env.OBS_PROJECT_ID, sessionId)
    .first<{ found: number }>();
  if (exists === null) {
    return errorResponse(404, "not_found", `no run for session_id "${sessionId}"`);
  }

  const rows = await env.OBS_DB.prepare(
    `${WINNING_CTE}
     SELECT ${ITERATION_COLUMNS}
       FROM winning
      WHERE session_id = ?
      ORDER BY iteration ASC, plan_id ASC, task_id ASC
      LIMIT ? OFFSET ?`,
  )
    .bind(env.OBS_PROJECT_ID, sessionId, page.limit, page.offset)
    .all<IterationRow>();
  const data = rows.results.map((row) => mapIteration(row));
  return respond(request, "iterations", data, data.length);
}

async function getIteration(
  request: Request,
  env: Env,
  url: URL,
  rawIteration: string,
): Promise<Response> {
  const iteration = Number(rawIteration);
  if (!Number.isSafeInteger(iteration) || iteration < 1) {
    return errorResponse(400, "bad_request", 'invalid "n": must be an integer >= 1');
  }

  const iterLogDir = url.searchParams.get("iter_log_dir") ?? "";
  let planId = "";
  if (iterLogDir !== "") {
    if (!iterLogDir.startsWith("remote:") || iterLogDir.length === "remote:".length) {
      return errorResponse(
        400,
        "bad_request",
        'invalid "iter_log_dir": not a configured iter-log root',
      );
    }
    planId = iterLogDir.slice("remote:".length);
  }

  const planClause = planId === "" ? "" : "AND plan_id = ?";
  const bindings = planId === "" ? [env.OBS_PROJECT_ID, iteration] : [env.OBS_PROJECT_ID, iteration, planId];
  const row = await env.OBS_DB.prepare(
    `${WINNING_CTE}
     SELECT ${ITERATION_COLUMNS}
       FROM winning
      WHERE iteration = ? ${planClause}
      ORDER BY occurred_at DESC, ingested_at DESC, schema_hash DESC
      LIMIT 1`,
  )
    .bind(...bindings)
    .first<IterationRow>();
  if (row === null) {
    return errorResponse(404, "not_found", `no iteration ${iteration} in the resolved root`);
  }

  const breakdown = await env.OBS_DB.prepare(
    `SELECT signal, label, present, sub_score, detail, nominal_weight,
            effective_weight, contribution
       FROM score_breakdown
      WHERE project_id = ? AND plan_id = ? AND task_id = ?
        AND iteration = ? AND schema_hash = ?
      ORDER BY ordinal ASC`,
  )
    .bind(env.OBS_PROJECT_ID, row.plan_id, row.task_id, row.iteration, row.schema_hash)
    .all<BreakdownRow>();
  return respond(request, "iteration", mapIteration(row, { breakdown: breakdown.results }));
}

async function getRubric(request: Request, env: Env): Promise<Response> {
  const row = await env.OBS_DB.prepare(
    "SELECT version, document_json FROM rubrics WHERE project_id = ? AND active = 1",
  )
    .bind(env.OBS_PROJECT_ID)
    .first<RubricRow>();
  if (row === null) {
    return errorResponse(500, "internal", "unexpected server error");
  }
  return respond(request, "rubric", JSON.parse(row.document_json) as unknown, undefined, row.version);
}

async function getHealth(request: Request, env: Env): Promise<Response> {
  try {
    const [runs, iterations, rubric] = await env.OBS_DB.batch([
      env.OBS_DB.prepare("SELECT COUNT(*) AS count FROM sessions WHERE project_id = ?").bind(
        env.OBS_PROJECT_ID,
      ),
      env.OBS_DB.prepare(
        `${WINNING_CTE}
         SELECT COUNT(*) AS count, MAX(occurred_at) AS last_update FROM winning`,
      ).bind(env.OBS_PROJECT_ID),
      env.OBS_DB.prepare(
        "SELECT version, document_json FROM rubrics WHERE project_id = ? AND active = 1",
      ).bind(env.OBS_PROJECT_ID),
    ]);
    const runCount = (runs.results[0] as CountRow | undefined)?.count ?? 0;
    const iterationHealth = iterations.results[0] as IterationHealthRow | undefined;
    const rubricVersion = (rubric.results[0] as RubricRow | undefined)?.version ?? "";

    let subscriberCount = 0;
    try {
      const id = env.PROJECT_DO.idFromName(env.OBS_PROJECT_ID);
      const response = await env.PROJECT_DO.get(id).fetch(
        "https://project.internal/subscriber-count",
      );
      if (response.ok) {
        const body: unknown = await response.json();
        if (
          typeof body === "object" &&
          body !== null &&
          "count" in body &&
          typeof body.count === "number"
        ) {
          subscriberCount = body.count;
        }
      }
    } catch {
      subscriberCount = 0;
    }

    return respond(request, "health", {
      status: "ok",
      run_count: runCount,
      iteration_count: iterationHealth?.count ?? 0,
      last_iter_log_mtime: iterationHealth?.last_update ?? null,
      subscriber_count: subscriberCount,
      rubric_version: rubricVersion,
      roots: [],
    });
  } catch {
    return respond(request, "health", {
      status: "ok",
      run_count: 0,
      iteration_count: 0,
      last_iter_log_mtime: null,
      subscriber_count: 0,
      rubric_version: "",
      roots: [],
    });
  }
}

export function isReadRoute(pathname: string): boolean {
  return (
    pathname === `${API_ROOT}/runs` ||
    pathname === `${API_ROOT}/rubric` ||
    pathname === `${API_ROOT}/health` ||
    /^\/api\/v1\/observability\/runs\/[^/]+$/.test(pathname) ||
    /^\/api\/v1\/observability\/runs\/[^/]+\/iterations$/.test(pathname) ||
    /^\/api\/v1\/observability\/iterations\/[^/]+$/.test(pathname)
  );
}

export async function handleReadRoute(request: Request, env: Env): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname === `${API_ROOT}/runs`) {
    return listRuns(request, env, url);
  }
  if (url.pathname === `${API_ROOT}/rubric`) {
    return getRubric(request, env);
  }
  if (url.pathname === `${API_ROOT}/health`) {
    return getHealth(request, env);
  }

  const runIterations = url.pathname.match(
    /^\/api\/v1\/observability\/runs\/([^/]+)\/iterations$/,
  );
  if (runIterations !== null) {
    return listIterations(request, env, url, runIterations[1]);
  }
  const run = url.pathname.match(/^\/api\/v1\/observability\/runs\/([^/]+)$/);
  if (run !== null) {
    return getRun(request, env, run[1]);
  }
  const iteration = url.pathname.match(/^\/api\/v1\/observability\/iterations\/([^/]+)$/);
  if (iteration !== null) {
    return getIteration(request, env, url, iteration[1]);
  }
  return errorResponse(404, "not_found", "route not found");
}
