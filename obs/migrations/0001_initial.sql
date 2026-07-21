PRAGMA foreign_keys = ON;

CREATE TABLE iterations (
  project_id                  TEXT    NOT NULL,
  plan_id                     TEXT    NOT NULL,
  task_id                     TEXT    NOT NULL,
  iteration                   INTEGER NOT NULL CHECK (iteration >= 1),
  schema_hash                 TEXT    NOT NULL
    CHECK (length(schema_hash) = 64 AND schema_hash NOT GLOB '*[^0-9a-f]*'),
  event_kind                  TEXT    NOT NULL
    CHECK (event_kind IN ('iteration.checkpointed', 'iteration.scored', 'score.recomputed')),
  ingest_schema_version       INTEGER NOT NULL CHECK (ingest_schema_version = 1),
  iteration_schema_version    INTEGER NOT NULL CHECK (iteration_schema_version >= 1),
  occurred_at                 TEXT    NOT NULL,
  ingested_at                 TEXT    NOT NULL,
  session_id                  TEXT    NOT NULL,
  date                        TEXT    NOT NULL,
  wave                        TEXT    NOT NULL,
  commit_sha                  TEXT    NOT NULL DEFAULT '',
  harness                     TEXT    NOT NULL DEFAULT '',
  model                       TEXT    NOT NULL DEFAULT '',
  files_changed               INTEGER NOT NULL DEFAULT 0 CHECK (files_changed >= 0),
  lines_added                 INTEGER NOT NULL DEFAULT 0 CHECK (lines_added >= 0),
  lines_removed               INTEGER NOT NULL DEFAULT 0 CHECK (lines_removed >= 0),
  retries                     INTEGER NOT NULL DEFAULT 0 CHECK (retries >= 0),
  integrity_observation_count INTEGER NOT NULL DEFAULT 0 CHECK (integrity_observation_count >= 0),
  transcript_turn_count       INTEGER CHECK (transcript_turn_count IS NULL OR transcript_turn_count >= 0),
  input_tokens                INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
  output_tokens               INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
  cache_read_tokens           INTEGER CHECK (cache_read_tokens IS NULL OR cache_read_tokens >= 0),
  cache_creation_tokens       INTEGER CHECK (cache_creation_tokens IS NULL OR cache_creation_tokens >= 0),
  cache_hit_rate              REAL    CHECK (cache_hit_rate IS NULL OR cache_hit_rate BETWEEN 0.0 AND 1.0),
  verifiers_json              TEXT    NOT NULL DEFAULT '[]'
    CHECK (json_valid(verifiers_json) AND json_type(verifiers_json) = 'array'),
  integrity_json              TEXT
    CHECK (integrity_json IS NULL OR (json_valid(integrity_json) AND json_type(integrity_json) = 'array')),
  objective_json              TEXT
    CHECK (objective_json IS NULL OR (json_valid(objective_json) AND json_type(objective_json) = 'object')),
  payload_json                TEXT    NOT NULL
    CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
  client_json                 TEXT    NOT NULL
    CHECK (json_valid(client_json) AND json_type(client_json) = 'object'),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash)
) WITHOUT ROWID;

CREATE INDEX iterations_by_project_session
  ON iterations (project_id, session_id, occurred_at DESC, iteration DESC);
CREATE INDEX iterations_by_project_logical_iteration
  ON iterations (project_id, plan_id, task_id, iteration, occurred_at DESC, ingested_at DESC);

CREATE TABLE scores (
  project_id       TEXT    NOT NULL,
  plan_id          TEXT    NOT NULL,
  task_id          TEXT    NOT NULL,
  iteration        INTEGER NOT NULL,
  schema_hash      TEXT    NOT NULL,
  rubric_version   TEXT    NOT NULL,
  scored           INTEGER NOT NULL CHECK (scored IN (0, 1)),
  score            REAL    CHECK (score IS NULL OR score BETWEEN 0.0 AND 1.0),
  band             TEXT    NOT NULL
    CHECK (band IN ('excellent', 'good', 'fair', 'poor', 'unscored')),
  linked_traces_to_outcomes INTEGER NOT NULL DEFAULT 0
    CHECK (linked_traces_to_outcomes IN (0, 1)),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash),
  FOREIGN KEY (project_id, plan_id, task_id, iteration, schema_hash)
    REFERENCES iterations (project_id, plan_id, task_id, iteration, schema_hash)
    ON DELETE CASCADE,
  CHECK ((scored = 0 AND score IS NULL AND band = 'unscored') OR
         (scored = 1 AND score IS NOT NULL AND band <> 'unscored'))
) WITHOUT ROWID;

CREATE INDEX scores_by_project_rubric
  ON scores (project_id, rubric_version, scored, score);

CREATE TABLE score_breakdown (
  project_id       TEXT    NOT NULL,
  plan_id          TEXT    NOT NULL,
  task_id          TEXT    NOT NULL,
  iteration        INTEGER NOT NULL,
  schema_hash      TEXT    NOT NULL,
  ordinal          INTEGER NOT NULL CHECK (ordinal >= 0),
  signal           TEXT    NOT NULL,
  label            TEXT    NOT NULL,
  present          INTEGER NOT NULL CHECK (present IN (0, 1)),
  sub_score        REAL    NOT NULL CHECK (sub_score BETWEEN 0.0 AND 1.0),
  detail           TEXT    NOT NULL DEFAULT '',
  nominal_weight   REAL    NOT NULL CHECK (nominal_weight BETWEEN 0.0 AND 1.0),
  effective_weight REAL    NOT NULL CHECK (effective_weight BETWEEN 0.0 AND 1.0),
  contribution     REAL    NOT NULL CHECK (contribution BETWEEN 0.0 AND 1.0),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash, ordinal),
  UNIQUE (project_id, plan_id, task_id, iteration, schema_hash, signal),
  FOREIGN KEY (project_id, plan_id, task_id, iteration, schema_hash)
    REFERENCES scores (project_id, plan_id, task_id, iteration, schema_hash)
    ON DELETE CASCADE
) WITHOUT ROWID;

CREATE TABLE sessions (
  project_id          TEXT    NOT NULL,
  session_id          TEXT    NOT NULL,
  plan_id             TEXT    NOT NULL,
  task_id             TEXT    NOT NULL,
  harness             TEXT    NOT NULL DEFAULT '',
  model               TEXT    NOT NULL DEFAULT '',
  wave                TEXT    NOT NULL DEFAULT '',
  rubric_version      TEXT    NOT NULL DEFAULT '',
  iteration_count     INTEGER NOT NULL CHECK (iteration_count >= 0),
  scored              INTEGER NOT NULL CHECK (scored IN (0, 1)),
  score               REAL    CHECK (score IS NULL OR score BETWEEN 0.0 AND 1.0),
  band                TEXT    NOT NULL
    CHECK (band IN ('excellent', 'good', 'fair', 'poor', 'unscored')),
  first_iteration     INTEGER CHECK (first_iteration IS NULL OR first_iteration >= 1),
  last_iteration      INTEGER CHECK (last_iteration IS NULL OR last_iteration >= 1),
  last_update         TEXT,
  iter_log_dir        TEXT    NOT NULL,
  mean_cache_hit_rate REAL    CHECK (mean_cache_hit_rate IS NULL OR mean_cache_hit_rate BETWEEN 0.0 AND 1.0),
  source_plan_id      TEXT    NOT NULL,
  source_task_id      TEXT    NOT NULL,
  source_iteration    INTEGER NOT NULL,
  source_schema_hash  TEXT    NOT NULL,
  updated_at          TEXT    NOT NULL,
  PRIMARY KEY (project_id, session_id),
  FOREIGN KEY (project_id, source_plan_id, source_task_id, source_iteration, source_schema_hash)
    REFERENCES iterations (project_id, plan_id, task_id, iteration, schema_hash),
  CHECK ((scored = 0 AND score IS NULL AND band = 'unscored') OR
         (scored = 1 AND score IS NOT NULL AND band <> 'unscored'))
) WITHOUT ROWID;

CREATE INDEX sessions_by_project_last_update
  ON sessions (project_id, last_update DESC, session_id);
CREATE INDEX sessions_by_project_score
  ON sessions (project_id, score DESC, session_id);
CREATE INDEX sessions_by_project_band
  ON sessions (project_id, band, last_update DESC, session_id);
CREATE INDEX sessions_by_project_harness
  ON sessions (project_id, harness, last_update DESC, session_id);

CREATE TABLE rubrics (
  project_id   TEXT    NOT NULL,
  version      TEXT    NOT NULL,
  active       INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
  document_json TEXT   NOT NULL
    CHECK (json_valid(document_json) AND json_type(document_json) = 'object'),
  created_at   TEXT    NOT NULL,
  PRIMARY KEY (project_id, version)
) WITHOUT ROWID;

CREATE UNIQUE INDEX rubrics_one_active_per_project
  ON rubrics (project_id) WHERE active = 1;
