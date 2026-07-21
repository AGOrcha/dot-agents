import { DurableObject } from "cloudflare:workers";

const INGEST_PATH = "/api/v1/observability/ingest";
const API_ROOT = "/api/v1/observability";
type IngestKind =
  | "iteration.checkpointed"
  | "iteration.scored"
  | "score.recomputed";

const SUPPORTED_KINDS: Record<IngestKind, true> = {
  "iteration.checkpointed": true,
  "iteration.scored": true,
  "score.recomputed": true,
};

type RejectionCode =
  | "unsupported_schema"
  | "unsupported_kind"
  | "invalid_event"
  | "foreign_project"
  | "storage_unavailable";

interface IngestClient {
  da_version: string;
  host_os: string;
  agent_runtime: string;
}

interface IngestEvent {
  kind: IngestKind;
  occurred_at: string;
  plan_id: string;
  task_id: string;
  iteration: number;
  schema_hash: string;
  payload: Record<string, unknown>;
  score_sidecar: Record<string, unknown> | null;
}

interface IngestEnvelope {
  schema_version: number;
  project_id: string;
  client: IngestClient;
  events: unknown[];
}

interface EventKey {
  project_id: string;
  plan_id: string;
  task_id: string;
  iteration: number;
  schema_hash: string;
}

interface RejectedItem {
  index: number;
  key: EventKey;
  code: RejectionCode;
  message: string;
  retryable: boolean;
}

interface IngestResponse {
  accepted: number;
  deduped: number;
  rejected: RejectedItem[];
}

interface ProjectDoRequest {
  project_id: string;
  client: IngestClient;
  event: IngestEvent;
}

interface ProjectDoResponse {
  status: "accepted" | "deduped";
}

export interface AccessJwksProvider {
  getJwks(): Promise<JsonWebKey[]>;
}

export interface AccessJwtVerifier {
  verify(assertion: string): Promise<{ subject: string }>;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isSupportedKind(value: string): value is IngestKind {
  return Object.hasOwn(SUPPORTED_KINDS, value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function json(value: unknown, status = 200): Response {
  return Response.json(value, {
    status,
    headers: { "cache-control": "no-store" },
  });
}

function errorResponse(status: number, code: string, message: string): Response {
  return json({ error: { code, message } }, status);
}

function parseEnvelope(value: unknown): IngestEnvelope | null {
  if (!isRecord(value)) {
    return null;
  }

  const { schema_version, project_id, client, events } = value;
  if (
    typeof schema_version !== "number" ||
    !Number.isInteger(schema_version) ||
    !isNonEmptyString(project_id) ||
    !isRecord(client) ||
    !isNonEmptyString(client.da_version) ||
    !isNonEmptyString(client.host_os) ||
    !isNonEmptyString(client.agent_runtime) ||
    !Array.isArray(events)
  ) {
    return null;
  }

  return {
    schema_version,
    project_id,
    client: {
      da_version: client.da_version,
      host_os: client.host_os,
      agent_runtime: client.agent_runtime,
    },
    events,
  };
}

function eventKey(projectId: string, value: unknown): EventKey {
  const event = isRecord(value) ? value : {};
  return {
    project_id: projectId,
    plan_id: typeof event.plan_id === "string" ? event.plan_id : "",
    task_id: typeof event.task_id === "string" ? event.task_id : "",
    iteration:
      typeof event.iteration === "number" && Number.isInteger(event.iteration)
        ? event.iteration
        : 0,
    schema_hash: typeof event.schema_hash === "string" ? event.schema_hash : "",
  };
}


function parseEvent(value: unknown): IngestEvent | null {
  if (!isRecord(value)) {
    return null;
  }

  const {
    kind,
    occurred_at,
    plan_id,
    task_id,
    iteration,
    schema_hash,
    payload,
    score_sidecar,
  } = value;

  if (
    typeof kind !== "string" ||
    !isSupportedKind(kind) ||
    !isNonEmptyString(occurred_at) ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(occurred_at) ||
    Number.isNaN(Date.parse(occurred_at)) ||
    !isNonEmptyString(plan_id) ||
    !isNonEmptyString(task_id) ||
    typeof iteration !== "number" ||
    !Number.isInteger(iteration) ||
    iteration < 1 ||
    typeof schema_hash !== "string" ||
    !/^[0-9a-f]{64}$/.test(schema_hash) ||
    !isRecord(payload)
  ) {
    return null;
  }

  let parsedScoreSidecar: Record<string, unknown> | null;
  if (kind === "iteration.checkpointed") {
    if (score_sidecar !== null) {
      return null;
    }
    parsedScoreSidecar = null;
  } else {
    if (!isRecord(score_sidecar)) {
      return null;
    }
    parsedScoreSidecar = score_sidecar;
  }

  return {
    kind,
    occurred_at,
    plan_id,
    task_id,
    iteration,
    schema_hash,
    payload,
    score_sidecar: parsedScoreSidecar,
  };
}

function parseProjectDoRequest(value: unknown): ProjectDoRequest | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.project_id) ||
    !isRecord(value.client) ||
    !isNonEmptyString(value.client.da_version) ||
    !isNonEmptyString(value.client.host_os) ||
    !isNonEmptyString(value.client.agent_runtime)
  ) {
    return null;
  }

  const event = parseEvent(value.event);
  if (event === null) {
    return null;
  }

  return {
    project_id: value.project_id,
    client: {
      da_version: value.client.da_version,
      host_os: value.client.host_os,
      agent_runtime: value.client.agent_runtime,
    },
    event,
  };
}

function rejection(
  index: number,
  key: EventKey,
  code: RejectionCode,
  message: string,
  retryable = false,
): RejectedItem {
  return { index, key, code, message, retryable };
}

async function dispatchToProjectDo(
  env: Env,
  envelope: IngestEnvelope,
  event: IngestEvent,
): Promise<"accepted" | "deduped"> {
  const id = env.PROJECT_DO.idFromName(envelope.project_id);
  const stub = env.PROJECT_DO.get(id);
  const response = await stub.fetch("https://project.internal/ingest", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      project_id: envelope.project_id,
      client: envelope.client,
      event,
    } satisfies ProjectDoRequest),
  });

  if (!response.ok) {
    throw new Error(`project DO returned ${response.status}`);
  }
  const result: unknown = await response.json();
  if (
    !isRecord(result) ||
    (result.status !== "accepted" && result.status !== "deduped")
  ) {
    throw new Error("project DO returned an invalid result");
  }
  return result.status;
}

async function handleIngest(request: Request, env: Env): Promise<Response> {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return errorResponse(400, "invalid_envelope", "request body must be valid JSON");
  }

  const envelope = parseEnvelope(body);
  if (envelope === null) {
    return errorResponse(400, "invalid_envelope", "request body does not match the ingest envelope");
  }

  const result: IngestResponse = { accepted: 0, deduped: 0, rejected: [] };
  for (const [index, rawEvent] of envelope.events.entries()) {
    const key = eventKey(envelope.project_id, rawEvent);

    if (envelope.schema_version !== 1) {
      result.rejected.push(
        rejection(index, key, "unsupported_schema", "ingest schema version is not supported"),
      );
      continue;
    }

    const kind = isRecord(rawEvent) ? rawEvent.kind : null;
    if (typeof kind === "string" && !isSupportedKind(kind)) {
      result.rejected.push(
        rejection(index, key, "unsupported_kind", "event kind is not supported"),
      );
      continue;
    }

    const event = parseEvent(rawEvent);
    if (event === null) {
      result.rejected.push(
        rejection(index, key, "invalid_event", "event does not match the v1 ingest shape"),
      );
      continue;
    }

    if (envelope.project_id !== env.OBS_PROJECT_ID) {
      result.rejected.push(
        rejection(
          index,
          key,
          "foreign_project",
          "event project does not match this deployment",
        ),
      );
      continue;
    }

    try {
      const status = await dispatchToProjectDo(env, envelope, event);
      result[status === "accepted" ? "accepted" : "deduped"] += 1;
    } catch {
      result.rejected.push(
        rejection(
          index,
          key,
          "storage_unavailable",
          "event storage is temporarily unavailable",
          true,
        ),
      );
    }
  }

  return json(result);
}

function isLoopback(hostname: string): boolean {
  return (
    hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "::1"
  );
}

export async function verifyAccess(
  request: Request,
  env: Env,
): Promise<{ subject: string } | null> {
  const hostname = new URL(request.url).hostname;
  if (
    env.OBS_AUTH_MODE === "fixture-jwt" &&
    env.ENVIRONMENT === "development" &&
    isLoopback(hostname)
  ) {
    // TODO(o6): verify the signed fixture JWT through StaticJwksProvider instead of this scaffold grant.
    return { subject: "fixture-cli" };
  }

  // TODO(o6): construct the production AccessJwtVerifier and verify Cf-Access-Jwt-Assertion.
  return null;
}

function isReadOrLiveRoute(pathname: string): boolean {
  if (
    pathname === `${API_ROOT}/runs` ||
    pathname === `${API_ROOT}/rubric` ||
    pathname === `${API_ROOT}/health` ||
    pathname === `${API_ROOT}/events`
  ) {
    return true;
  }

  return (
    /^\/api\/v1\/observability\/runs\/[^/]+$/.test(pathname) ||
    /^\/api\/v1\/observability\/runs\/[^/]+\/iterations$/.test(pathname) ||
    /^\/api\/v1\/observability\/iterations\/[1-9]\d*$/.test(pathname)
  );
}

async function dispatch(request: Request, env: Env): Promise<Response> {
  const url = new URL(request.url);
  if (request.method === "POST" && url.pathname === INGEST_PATH) {
    return handleIngest(request, env);
  }

  if (request.method === "GET" && isReadOrLiveRoute(url.pathname)) {
    // TODO(o5): replace this read/live route seam with D1 DTO queries and the project DO transport.
    return errorResponse(501, "not_implemented", "observability read model is not implemented");
  }

  return errorResponse(404, "not_found", "route not found");
}

export class ProjectDO extends DurableObject<Env> {
  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (request.method !== "POST" || url.pathname !== "/ingest") {
      return errorResponse(404, "not_found", "route not found");
    }

    let input: unknown;
    try {
      input = await request.json();
    } catch {
      return errorResponse(400, "invalid_event", "project DO request must be valid JSON");
    }
    const parsedInput = parseProjectDoRequest(input);
    if (parsedInput === null) {
      return errorResponse(400, "invalid_event", "project DO request is malformed");
    }

    const status = await this.commitD1Transaction(parsedInput);
    await this.broadcastAfterCommit(parsedInput.event);
    return json({ status } satisfies ProjectDoResponse);
  }

  private commitD1Transaction(
    _input: ProjectDoRequest,
  ): Promise<"accepted" | "deduped"> {
    // TODO(o5): perform the idempotency check and all D1 mutations in one transaction.
    void this.env.OBS_DB;
    return Promise.resolve("accepted");
  }

  private broadcastAfterCommit(_event: IngestEvent): Promise<void> {
    // TODO(o5): update the last-100-event buffer and broadcast only after D1 commits.
    return Promise.resolve();
  }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const assertion = await verifyAccess(request, env);
    if (assertion === null) {
      return errorResponse(403, "forbidden", "a verified Cloudflare Access assertion is required");
    }
    return dispatch(request, env);
  },
} satisfies ExportedHandler<Env>;
