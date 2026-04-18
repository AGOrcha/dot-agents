# External agent sources — design fork (v1.5 / v2)

**Status:** design artifact (not an implementation commitment in [`loop-agent-pipeline`](../../plans/loop-agent-pipeline/PLAN.yaml))

**Forked from:** [`../loop-agent-pipeline/decisions.1.md`](../loop-agent-pipeline/decisions.1.md) — decisions **D6** and **D6.a** (locked TOC). This file is the standalone home for external-source exploration; the main pipeline plan does not track registry or source-package implementation work.

**Locked direction (summary):** Option **B** in v1.5 (transport + auth + content split, shared auth across git/http/local) → Option **C** in v2 (registry-first, signing, rich discovery). OCI Distribution as the wire protocol; BYO registry in v1.5.

---

## Table of contents

1. [Current state audit](#1-current-state-audit)
2. [Consumer personas & regulatory constraints](#2-consumer-personas--regulatory-constraints)
3. [Transport + content + auth architecture (Option B, v1.5)](#3-transport--content--auth-architecture-option-b-v15)
4. [Auth provider model](#4-auth-provider-model)
5. [Registry content model](#5-registry-content-model)
6. [Registry wire protocol (OCI Distribution)](#6-registry-wire-protocol-oci-distribution)
7. [FIPS posture](#7-fips-posture)
8. [Audit logging (CMMC AU-2 / AU-3)](#8-audit-logging-cmmc-au-2--au-3)
9. [Trust & attestation (v2 material, flagged in v1.5)](#9-trust--attestation-v2-material-flagged-in-v15)
10. [Caching & offline](#10-caching--offline)
11. [Rollout phasing](#11-rollout-phasing)
12. [Migration](#12-migration)
13. [Open questions](#13-open-questions)

---

## 1. Current state audit

### 1.1 Existing sources (local, git) — fields, auth story, gaps

- Today’s consumption paths (local paths, git remotes) must be documented against actual `.agentsrc` / package-resolution behavior in the product — this fork assumes a future `{ transport, auth, content }` shape without prescribing implementation tickets here.
- **Gap:** enterprise needs (registry behind IdP, VPN, artifact types) are not fully served by git-only or ad hoc local layouts.

### 1.2 How consumers select from a source today vs. what enterprise customers need

- **Today:** developers point at local trees or git URLs; selection is informal compared to versioned OCI refs.
- **Enterprise:** needs allowlisted registries, policy-friendly auth (OAuth2/OIDC, mTLS), digest pinning, and audit trails — see personas in [§2](#2-consumer-personas--regulatory-constraints).

---

## 2. Consumer personas & regulatory constraints

| Persona | Constraints | Design pressure |
|--------|-------------|-----------------|
| **Insurance / gov contractor** | CMMC L2, FIPS, Okta IdP, self-hosted registry, VPN | MIP-state FIPS posture, BYO registry, structured audit events |
| **General public** | Publish + consume, minimal ops | Zero-infra defaults, simple auth fallbacks |

**Constraint → requirement:** map each constraint to transport choice, auth provider, cache/offline behavior, and audit sink (detailed in later sections).

---

## 3. Transport + content + auth architecture (Option B, v1.5)

- **Transports:** `http`, `git`, `local` (exact CLI surface TBD at integration time — not in the pipeline implementation plan).
- **Shared auth block:** one auth configuration applies across transports where applicable.
- **Content layout:** `tree` | `tarball` | `registry` — registry uses OCI artifact types (see [§5](#5-registry-content-model)).

---

## 4. Auth provider model

| Provider | Role |
|----------|------|
| `oauth2-auth-code-pkce` | Browser callback; primary for Okta / OIDC IdPs |
| `mtls` | Client cert + CA for PKI-first orgs |
| `bearer` | Static token from env / file (CI, simple cases) |
| `credential-helper` | External binary on stdout (git-credential style) |
| **Device code** | Headless / CI when browser callback is not viable |

**Cross-cutting:** token storage (`keychain` | `file` | `env`), refresh, rotation, revocation.

---

## 5. Registry content model

- **Separate OCI artifact types** (not a single bundled image): agent, skill, verifier, bundle manifest.
- **Media types** gate client-side schema validation — wrong type in a slot fails before execution.
- **Namespace:** repo-path form such as `<org>/verifier/<name>`; type prefix in the path.
- **Bundle manifest:** custom `application/vnd.dotagents.bundle.v1+json` **pointer document** listing member refs — **not** OCI Image Index.
- **Version semantics:** tags on the wire; client resolves SemVer ranges; digest pin as `pinned:sha256:...` for reproducibility.

Illustrative `.agentsrc` shapes (future integration):

```json
"packages": [
  "verifier/playwright-api@^1.2",
  "agent/impl-agent@1.4",
  "skill/review-pr@pinned:sha256:..."
]
```

```json
"verifier_profiles": {
  "unit": "verifier/unit@^1.0",
  "api": "agents/verifiers/api-sre"
}
```

**Publish CLI (conceptual):** `dot-agents publish agent|verifier|skill|bundle` per artifact type.

---

## 6. Registry wire protocol (OCI Distribution)

- **Why OCI:** enterprise allowlists (Harbor, Artifactory, ECR, GHCR), mature token auth, cosign/sigstore alignment for later phases.
- **v1.5:** **BYO** — customer points the tool at any OCI-compatible registry they operate.
- **v2:** optional thin-wrapper server / richer discovery — only if demand warrants.
- **Public default registry:** open question — see [Q2](#q2-public-default-registry).

---

## 7. FIPS posture

- **Single binary, no build variant** for v1.5: Go 1.24.3+ with `GOFIPS140=inprocess`; runtime opt-in `GODEBUG=fips140=on`.
- **MIP-state** module acceptable per NIST IG D.G for many regulated deployments; documented risk acknowledgment.
- **BoringCrypto / strict-validated** variant stays **persona-gated** — ship only if compliance requires CMVP-validated (not MIP). See [Q1](#q1-mip-vs-strict-validated-fips).

---

## 8. Audit logging (CMMC AU-2 / AU-3)

- **Taxonomy (illustrative):** `auth.login`, `auth.token_refresh`, `auth.logout`, `registry.fetch_manifest`, `registry.fetch_blob`, `registry.publish`, `signature.verify`, `cache.hit`, `cache.miss`.
- **Schema:** `{ timestamp, actor, principal, action, target, outcome, trace_id }` (structured JSON).
- **Sinks:** stderr default; overrides: file, syslog, JSONL, HTTP endpoint.
- **Retention:** customer-managed; dot-agents does not centralize storage.
- Align with existing structured-logging conventions in the product when integrated.

---

## 9. Trust & attestation (v2 material, flagged in v1.5)

- **v1.5:** HTTPS + registry auth provide baseline integrity for transport.
- **Roadmap:** Cosign/sigstore signatures, in-toto attestations — not blocking v1.5 transport/auth/content split.

---

## 10. Caching & offline

- Content-addressed cache layout (e.g. `~/.agents/cache/<sha256>/`).
- **Lockfile:** `.agentsrc.lock` for digest pinning (conceptual).
- **Offline:** prefer local cache hits; deterministic failure when content missing and network unavailable.

---

## 11. Rollout phasing

| Phase | Scope |
|-------|--------|
| **v1.5 (Option B)** | Transport + auth + content; OCI wire; BYO registry; audit events; no separate FIPS build variant; no signing |
| **v2 (Option C)** | Registry-first UX; optional thin server; signing; richer discovery |

---

## 12. Migration

- **Git sources:** preserve backward-compatible shorthand where possible; document retrofit for registry-style refs.
- **`.agentsrc.json`:** expect a schema version bump when integrating.
- **Authors:** document how existing agent/skill authors publish to OCI refs when integration lands (outside this plan).

---

## 13. Open questions

### Q1 (MIP vs strict-validated FIPS)

Ask the insurance persona’s CMMC assessor: is **MIP-state** FIPS 140-3 acceptable per NIST IG D.G, or is **fully validated CMVP** required? Answer gates whether a BoringCrypto variant ever ships. Until confirmed: single binary + `GOFIPS140=inprocess`.

### Q2 (public default registry)

Where does the public default live? Candidates: GHCR under a `dot-agents` org; owned infra; or **no default** (explicit config only). Decision deferred to this design track, not to the loop-agent-pipeline implementation tasks.

---

## Completeness note (D6.a)

This document implements the **locked D6.a table of contents** from [`decisions.1.md`](../loop-agent-pipeline/decisions.1.md): sections **1–13** above correspond to that TOC. Further detail may accumulate here without expanding the pipeline plan’s implementation scope.
