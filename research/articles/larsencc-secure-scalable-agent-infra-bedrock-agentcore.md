# How We Built Secure, Scalable Agent Infrastructure with AWS Bedrock AgentCore

**Author:** Larsen Cundric (@larsencc) — Browser Use (@browser_use)
**Source:** https://x.com/larsencc/status/2076744742776488351 → https://x.com/larsencc/article/2076744742776488351
**Published:** July 13, 2026
**Method:** Claude-in-Chrome (X native long-form article, login-walled)
**Engagement:** 12 replies · 22 reposts · 158 likes · 61K views

---

## Summary

A concrete production-architecture writeup of how Browser Use runs "millions of untrusted AI agents"
— each in a **zero-secret micro-VM** — behind a **control plane that holds all credentials and
proxies every outbound call**. The durable claim: the agent sandbox "should have nothing worth
stealing and nothing worth preserving." Credentials never enter the VM; the sandbox carries only a
control-plane URL and, per request, a **session token** delivered inside the invocation payload.
Every credentialed operation (LLM calls, S3 upload/download, callbacks) hops through the control
plane, which validates the session token, swaps it for the real upstream key, meters/caps usage, and
forwards — the response returns "minus the upstream key." Per-session isolated durable workspace
(`/mnt/workspace`) survives stop/resume; **the control plane is the cold-recovery source of truth and
the durable mount is a warm cache.** Elastic per-session micro-VMs (AWS Bedrock AgentCore) scale
zero→thousands tracking a spiky demand curve, billed per active session. Practitioner report —
shipped at scale; AWS-specific but the control-plane-as-credential-proxy pattern is
substrate-independent.

---

## Relevance to dot-agents

Evaluated 2026-07-15 (targeted, connects the `control-plane-multiplayer-lead` fold-back to its real
home). This is the **architectural reference** for the direction the owner named: the H8b cross-org
control plane belongs with **kg-as-sot + workstore backend + the daemon/bg-service auth-proxy** (which
"could grow into a full session proxy"). Three tight mappings:

- **[OVERLAP-SHARPEN → design reference] control plane = credential proxy.** The sandbox never holds a
  credential; it posts a **session token**, the control plane swaps it for the real upstream key,
  meters, and forwards. This is exactly the H12 secret-non-disclosure model `t7-oci-auth` is building
  at the library level (`resolveOCIAuthorizationHeader`: secret refs only, resolved secret lives only
  in a request header) — **elevated to a service**. The dot-agents daemon/bg-service auth-proxy is the
  natural home for this: a local control plane that holds registry/LLM/source credentials and hands
  workers session-scoped tokens, so a worker (or a delegated subagent, or a cross-org collaborator)
  "never sees a real credential." Sharpens t7 → t8's live-pull auth and the daemon design.
- **[OVERLAP-SHARPEN] control plane = cold-recovery SOT, durable mount = warm cache.** "The control
  plane is the cold-recovery source of truth; the durable mount is a cache that makes warm resumes
  cheap." This is the **kg-as-sot** thesis verbatim (KG/warm store = SOT, hot filesystem = cache) and
  the **session-handoff-journal** cold-recovery model. The per-session durable workspace that survives
  idle-stop / recycle and is wiped only on version bump = the workstore-backend's session lifecycle.
- **[UNVERIFIED-LEAD → parked, re-anchored] multiplayer / cross-operator control plane.** Session
  token + per-session isolation + a single credential-holding proxy is the substrate on which
  cross-operator shared context + scoped permissions (Variant H8b) become tractable — one control
  plane brokering many isolated sessions with per-session scoped auth is the multiplayer control plane.
  Re-anchored from `full-loop-orchestration-runtime` to the kg-as-sot / workstore / daemon-auth-proxy
  surface.

[WE-AHEAD / OUT-OF-SCOPE] the AWS-native substrate (AgentCore per-session micro-VMs, IAM/VPC/S3
scoping, elastic per-session billing) is Browser-Use's hosting problem, not dot-agents' — cited for
the **pattern** (proxy-holds-creds, token-per-session, SOT-plus-cache, per-session isolation), never
the substrate. Practitioner grade; scale numbers ("millions", "zero→thousands") [UNVERIFIED].

---

## Body

### The architecture
Untrusted agents run in **zero-secret micro-VMs**; a control plane holds all credentials; one image
runs everywhere. AWS Bedrock AgentCore provisions/schedules the per-session micro-VMs, each with a
durable `/mnt/workspace` scoped to the session.

### A sandbox that keeps credentials safe
The VM sits in a private VPC with an execution role that can only pull its image (ECR) and write logs
(CloudWatch) — it cannot reach S3, the DB, or any other AWS API on its own. Egress is allowlisted to
the control plane and public HTTPS. **No secrets are baked into the VM**: the runtime holds only a
control-plane URL; session token, session id, and per-run flags arrive inside the `/invocations`
payload per request, so nothing sensitive persists between runs. Each session's `/mnt/workspace` is
scoped to its session id — sessions cannot see each other.

### Your agent never sees a real credential
"Think of the control plane as a proxy service." The sandbox has no direct outside access; every
request hops through the control plane. It is a stateless FastAPI service; every request carries a
**session-token header**; the CP looks up the session by token, validates it is active, and executes
with real credentials.
- **LLM proxying** — the agent SDK calls its normal provider endpoint but the base URL points at the
  control plane (`/api/v4/llm/anthropic/v1`, `/api/v4/llm/openai/v1`, …). The CP validates the token,
  swaps it for the real upstream key, meters usage, forwards; response returns minus the key. Cost
  caps + billing happen on the way through.
- **Agent-safe file access** — the sandbox never holds AWS creds; it asks the CP for a **presigned
  URL** scoped to the session (upload/download), then uses it over plain HTTPS. Files generated during
  a run are promoted to S3 via the CP at completion, so a wiped VM never loses them.

### Session state survives stop and resume
Expensive-to-rebuild per-session state (Bcode's SQLite `bcode.db`) lives on the durable
`/mnt/workspace`, surviving idle stops (15-min timeout) and session recycles (8-hr max lifetime),
wiped only on a runtime version bump or 14-day idle. **Below that, the control plane is the
cold-recovery source of truth; the durable mount is a cache that makes warm resumes cheap.**

### Scales from zero to thousands
Spiky workload → a fixed fleet is the wrong shape (pay for idle peak, or drop spikes). AgentCore
treats each session as its own primitive: boot on demand, stop when idle, bill only for active
session time. The control plane scales independently (ECS Fargate, ALB, CPU autoscale); sandboxes
scale independently through AgentCore. Each layer scales on its own bottleneck.

### Running it on AgentCore
Sessions scoped to their own session id (`runtimeSessionId = session_id`) — AgentCore's session
concept and theirs are the same. Driven by two boto3 calls: `invoke_agent_runtime` (dispatch a task
into a session) and `stop_runtime_session` (kill an in-flight session). Session context arrives inside
the `/invocations` payload, not the VM environment; two runs on the same VM carry different context,
neither persists. Prod settings: 15-min idle timeout, 8-hr max lifetime.

### The takeaway
"Zero-secret micro-VMs and a control plane holding all credentials" held up; what changed is the
runtime layer is now native AWS too. "Your agent should have nothing worth stealing and nothing worth
preserving."

---

## Key Quotes

> "Think of the control plane as a proxy service. … Every request has to hop through the control
> plane. … It is the only way the agent can talk to anything outside its VM."

> "The sandbox never sees a real API key or talks to a provider directly, and cost caps and billing
> happen on the way through."

> "The control plane is the cold-recovery source of truth. The durable mount is a cache that makes
> warm resumes cheap."

> "Your agent should have nothing worth stealing and nothing worth preserving."

---

## Extraction Notes

Practitioner report (Browser Use, shipped at scale). AWS-Bedrock-AgentCore-specific in substrate, but
the load-bearing ideas — control-plane-as-credential-proxy, token-per-session, SOT-plus-warm-cache,
per-session isolation, elastic per-session billing — are substrate-independent. Scale figures
[UNVERIFIED]. Cited for the pattern, not the AWS mechanics.
