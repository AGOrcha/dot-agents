import type { HashableIngestEvent, ScoreSidecar } from "./validation";

export interface D1CommitInput {
  project_id: string;
  client: {
    da_version: string;
    host_os: string;
    agent_runtime: string;
  };
  event: HashableIngestEvent;
}

export type D1CommitResult = "accepted" | "deduped" | "invalid_event";

const REBUILD_SESSIONS_SQL = `
WITH ranked AS (
  SELECT
    i.*,
    s.rubric_version,
    COALESCE(s.scored, 0) AS scored,
    s.score,
    COALESCE(s.band, 'unscored') AS score_band,
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
), source_ranked AS (
  SELECT
    winning.*,
    ROW_NUMBER() OVER (
      PARTITION BY project_id, session_id
      ORDER BY occurred_at DESC, ingested_at DESC, schema_hash DESC
    ) AS session_rank
  FROM winning
), latest_source AS (
  SELECT * FROM source_ranked WHERE session_rank = 1
), scored_ranked AS (
  SELECT
    winning.*,
    ROW_NUMBER() OVER (
      PARTITION BY project_id, session_id
      ORDER BY occurred_at DESC, ingested_at DESC, schema_hash DESC
    ) AS scored_rank
  FROM winning
  WHERE scored = 1
), latest_scored AS (
  SELECT * FROM scored_ranked WHERE scored_rank = 1
), aggregates AS (
  SELECT
    w.project_id,
    w.session_id,
    COUNT(*) AS iteration_count,
    MIN(w.iteration) AS first_iteration,
    MAX(w.iteration) AS last_iteration,
    MAX(w.occurred_at) AS last_update,
    MAX(w.ingested_at) AS updated_at,
    AVG(w.cache_hit_rate) AS mean_cache_hit_rate,
    ls.rubric_version,
    AVG(
      CASE
        WHEN w.scored = 1 AND w.rubric_version = ls.rubric_version THEN w.score
        ELSE NULL
      END
    ) AS score
  FROM winning AS w
  LEFT JOIN latest_scored AS ls
    ON ls.project_id = w.project_id AND ls.session_id = w.session_id
  GROUP BY w.project_id, w.session_id, ls.rubric_version
), projected AS (
  SELECT
    a.*,
    CASE
      WHEN a.score IS NULL THEN 'unscored'
      ELSE COALESCE(
        (
          SELECT json_extract(band.value, '$.name')
          FROM rubrics AS r, json_each(r.document_json, '$.bands') AS band
          WHERE r.project_id = a.project_id
            AND r.version = a.rubric_version
            AND CAST(json_extract(band.value, '$.min') AS REAL) <= a.score
          ORDER BY CAST(json_extract(band.value, '$.min') AS REAL) DESC
          LIMIT 1
        ),
        CASE
          WHEN a.score >= 0.85 THEN 'excellent'
          WHEN a.score >= 0.70 THEN 'good'
          WHEN a.score >= 0.50 THEN 'fair'
          ELSE 'poor'
        END
      )
    END AS band
  FROM aggregates AS a
)
INSERT INTO sessions (
  project_id, session_id, plan_id, task_id, harness, model, wave,
  rubric_version, iteration_count, scored, score, band, first_iteration,
  last_iteration, last_update, iter_log_dir, mean_cache_hit_rate,
  source_plan_id, source_task_id, source_iteration, source_schema_hash, updated_at
)
SELECT
  p.project_id,
  p.session_id,
  source.plan_id,
  source.task_id,
  source.harness,
  source.model,
  source.wave,
  COALESCE(p.rubric_version, ''),
  p.iteration_count,
  CASE WHEN p.score IS NULL THEN 0 ELSE 1 END,
  p.score,
  p.band,
  p.first_iteration,
  p.last_iteration,
  p.last_update,
  'remote:' || source.plan_id,
  p.mean_cache_hit_rate,
  source.plan_id,
  source.task_id,
  source.iteration,
  source.schema_hash,
  p.updated_at
FROM projected AS p
JOIN latest_source AS source
  ON source.project_id = p.project_id AND source.session_id = p.session_id
WHERE TRUE
ON CONFLICT(project_id, session_id) DO UPDATE SET
  plan_id = excluded.plan_id,
  task_id = excluded.task_id,
  harness = excluded.harness,
  model = excluded.model,
  wave = excluded.wave,
  rubric_version = excluded.rubric_version,
  iteration_count = excluded.iteration_count,
  scored = excluded.scored,
  score = excluded.score,
  band = excluded.band,
  first_iteration = excluded.first_iteration,
  last_iteration = excluded.last_iteration,
  last_update = excluded.last_update,
  iter_log_dir = excluded.iter_log_dir,
  mean_cache_hit_rate = excluded.mean_cache_hit_rate,
  source_plan_id = excluded.source_plan_id,
  source_task_id = excluded.source_task_id,
  source_iteration = excluded.source_iteration,
  source_schema_hash = excluded.source_schema_hash,
  updated_at = excluded.updated_at
WHERE
  sessions.plan_id IS NOT excluded.plan_id OR
  sessions.task_id IS NOT excluded.task_id OR
  sessions.harness IS NOT excluded.harness OR
  sessions.model IS NOT excluded.model OR
  sessions.wave IS NOT excluded.wave OR
  sessions.rubric_version IS NOT excluded.rubric_version OR
  sessions.iteration_count IS NOT excluded.iteration_count OR
  sessions.scored IS NOT excluded.scored OR
  sessions.score IS NOT excluded.score OR
  sessions.band IS NOT excluded.band OR
  sessions.first_iteration IS NOT excluded.first_iteration OR
  sessions.last_iteration IS NOT excluded.last_iteration OR
  sessions.last_update IS NOT excluded.last_update OR
  sessions.iter_log_dir IS NOT excluded.iter_log_dir OR
  sessions.mean_cache_hit_rate IS NOT excluded.mean_cache_hit_rate OR
  sessions.source_plan_id IS NOT excluded.source_plan_id OR
  sessions.source_task_id IS NOT excluded.source_task_id OR
  sessions.source_iteration IS NOT excluded.source_iteration OR
  sessions.source_schema_hash IS NOT excluded.source_schema_hash OR
  sessions.updated_at IS NOT excluded.updated_at`;

const DELETE_STALE_SESSIONS_SQL = `
DELETE FROM sessions
WHERE project_id = ?
  AND NOT EXISTS (
    SELECT 1
    FROM (
      SELECT
        i.session_id,
        ROW_NUMBER() OVER (
          PARTITION BY i.project_id, i.plan_id, i.task_id, i.iteration
          ORDER BY i.occurred_at DESC, i.ingested_at DESC, i.schema_hash DESC
        ) AS version_rank
      FROM iterations AS i
      WHERE i.project_id = ?
    ) AS winners
    WHERE winners.version_rank = 1 AND winners.session_id = sessions.session_id
  )`;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(value: Record<string, unknown>, key: string): string {
  return typeof value[key] === "string" ? value[key] : "";
}

function integerField(value: Record<string, unknown>, key: string): number {
  return typeof value[key] === "number" && Number.isInteger(value[key]) ? value[key] : 0;
}

function nullableIntegerField(value: Record<string, unknown>, key: string): number | null {
  return typeof value[key] === "number" && Number.isInteger(value[key]) ? value[key] : null;
}

function nullableNumberField(value: Record<string, unknown>, key: string): number | null {
  return typeof value[key] === "number" ? value[key] : null;
}

function verifierProjection(payload: Record<string, unknown>): Record<string, unknown>[] {
  if (!Array.isArray(payload.verifiers)) {
    return [];
  }
  return payload.verifiers.filter(isRecord).map((verifier) => ({
    type: stringField(verifier, "type"),
    status: stringField(verifier, "status") || "unknown",
    gate_passed: verifier.gate_passed === true,
    tests_added: integerField(verifier, "tests_added"),
    retries: integerField(verifier, "retries"),
  }));
}

function scoreStatements(
  db: D1Database,
  input: D1CommitInput,
  score: ScoreSidecar,
): D1PreparedStatement[] {
  const { event, project_id } = input;
  const key = [project_id, event.plan_id, event.task_id, event.iteration, event.schema_hash];
  const statements = [
    db
      .prepare(
        `INSERT INTO scores (
           project_id, plan_id, task_id, iteration, schema_hash, rubric_version,
           scored, score, band, linked_traces_to_outcomes
         )
         SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
         WHERE EXISTS (
           SELECT 1 FROM iterations
           WHERE project_id = ? AND plan_id = ? AND task_id = ?
             AND iteration = ? AND schema_hash = ?
         )
         ON CONFLICT(project_id, plan_id, task_id, iteration, schema_hash) DO NOTHING`,
      )
      .bind(
        ...key,
        score.rubric_version,
        score.scored ? 1 : 0,
        score.scored ? score.value : null,
        score.band,
        score.linked_traces_to_outcomes ? 1 : 0,
        ...key,
      ),
  ];

  for (const [ordinal, row] of score.breakdown.entries()) {
    statements.push(
      db
        .prepare(
          `INSERT INTO score_breakdown (
             project_id, plan_id, task_id, iteration, schema_hash, ordinal,
             signal, label, present, sub_score, detail, nominal_weight,
             effective_weight, contribution
           )
           SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
           WHERE EXISTS (
             SELECT 1 FROM scores
             WHERE project_id = ? AND plan_id = ? AND task_id = ?
               AND iteration = ? AND schema_hash = ?
           )
           ON CONFLICT(project_id, plan_id, task_id, iteration, schema_hash, ordinal)
           DO NOTHING`,
        )
        .bind(
          ...key,
          ordinal,
          row.signal,
          row.label,
          row.present ? 1 : 0,
          row.sub_score,
          row.detail ?? "",
          row.nominal_weight,
          row.effective_weight,
          row.contribution,
          ...key,
        ),
    );
  }
  return statements;
}

export async function commitEventToD1(
  db: D1Database,
  input: D1CommitInput,
): Promise<D1CommitResult> {
  const { event, project_id } = input;
  const payload = event.payload;
  const agent = isRecord(payload.agent) ? payload.agent : {};
  const impl = isRecord(payload.impl) ? payload.impl : {};
  const tokens = isRecord(payload.session_tokens) ? payload.session_tokens : {};
  const sessionId =
    stringField(agent, "session_id") || `legacy:${event.plan_id}:${event.task_id}`;
  const ingestedAt = new Date().toISOString();
  const key = [project_id, event.plan_id, event.task_id, event.iteration, event.schema_hash];

  const insertIteration = db
    .prepare(
      `INSERT INTO iterations (
         project_id, plan_id, task_id, iteration, schema_hash, event_kind,
         ingest_schema_version, iteration_schema_version, occurred_at, ingested_at,
         session_id, date, wave, commit_sha, harness, model, files_changed,
         lines_added, lines_removed, retries, integrity_observation_count,
         transcript_turn_count, input_tokens, output_tokens, cache_read_tokens,
         cache_creation_tokens, cache_hit_rate, verifiers_json, integrity_json,
         objective_json, payload_json, client_json
       )
       SELECT ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0,
              ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?
       WHERE ? <> 'score.recomputed'
          OR EXISTS (
            SELECT 1 FROM iterations
            WHERE project_id = ? AND plan_id = ? AND task_id = ? AND iteration = ?
          )
       ON CONFLICT(project_id, plan_id, task_id, iteration, schema_hash) DO NOTHING`,
    )
    .bind(
      ...key,
      event.kind,
      integerField(payload, "schema_version"),
      event.occurred_at,
      ingestedAt,
      sessionId,
      stringField(payload, "date"),
      stringField(payload, "wave"),
      stringField(payload, "commit"),
      stringField(agent, "harness"),
      stringField(agent, "model"),
      integerField(payload, "files_changed"),
      integerField(payload, "lines_added"),
      integerField(payload, "lines_removed"),
      integerField(impl, "retries"),
      nullableIntegerField(tokens, "message_count"),
      nullableIntegerField(tokens, "input_tokens"),
      nullableIntegerField(tokens, "output_tokens"),
      nullableIntegerField(tokens, "cache_read_tokens"),
      nullableIntegerField(tokens, "cache_creation_tokens"),
      nullableNumberField(tokens, "cache_hit_rate"),
      JSON.stringify(verifierProjection(payload)),
      JSON.stringify(payload),
      JSON.stringify(input.client),
      event.kind,
      project_id,
      event.plan_id,
      event.task_id,
      event.iteration,
    );

  const statements: D1PreparedStatement[] = [insertIteration];
  if (event.score_sidecar !== null) {
    statements.push(...scoreStatements(db, input, event.score_sidecar));
  }
  statements.push(
    db.prepare(REBUILD_SESSIONS_SQL).bind(project_id),
    db.prepare(DELETE_STALE_SESSIONS_SQL).bind(project_id, project_id),
    db
      .prepare(
        `SELECT
           EXISTS(
             SELECT 1 FROM iterations
             WHERE project_id = ? AND plan_id = ? AND task_id = ?
               AND iteration = ? AND schema_hash = ?
           ) AS exact_exists,
           EXISTS(
             SELECT 1 FROM iterations
             WHERE project_id = ? AND plan_id = ? AND task_id = ? AND iteration = ?
           ) AS logical_exists`,
      )
      .bind(...key, project_id, event.plan_id, event.task_id, event.iteration),
  );

  const results = await db.batch(statements);
  if (results[0].meta.changes === 1) {
    return "accepted";
  }
  const status = results.at(-1)?.results[0] as
    | { exact_exists: number; logical_exists: number }
    | undefined;
  if (status?.exact_exists === 1) {
    return "deduped";
  }
  return "invalid_event";
}
