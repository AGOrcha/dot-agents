# Security lens — dot-agents repo overlay

Repo-local committed layer. In this repo the base (`reviewers/reviewer.base.md`) and lens
(`reviewers/security.md`) layers resolve unresolved, so this overlay is **self-sufficient**: it
carries the full lens contract for reviewing the daemon's auth-proxy + secure-handling design.

## What this lens judges

Read the diff (`da workflow tasks` for the bundle + acceptance) and judge the daemon's **auth-proxy +
secure handling**: secret management, authorization boundaries, input validation, least-privilege,
and freedom from injection or leak. One lens only; read-only; stay in scope.

## Attack surface (daemon: HBHF comms + auth-proxy + secure handling)

- **Secret management:** credentials/tokens the proxy holds live in memory only as long as needed and
  are never logged, never written to `.agentsrc.lock`, generated outputs, or journal/iter-log
  artifacts (the config-layer secret-leak class), and never appear in a subprocess argv (visible in
  `ps`).
- **AuthZ boundaries:** every proxied request is authenticated AND authorized before it reaches an
  upstream; there is no unauthenticated path that reaches the KG store or spawns work; the local
  socket has restrictive perms (not world-accessible).
- **Input validation:** HBHF framing is length-bounded and validated (no unbounded read, no
  frame-size DoS, no integer-overflow on the length field); malformed frames are rejected, not
  partially acted on. Untrusted fields are never interpolated into a shell / SQL / exec.
- **Injection / exec hardening:** subprocess spawns (CRG worker, harness binaries) resolve
  **absolute or `execabs`-checked** binaries, never a relative/poisonable PATH lookup (SonarCloud
  go:S4036); caller-influenced input never becomes a binary name or an unescaped arg.
- **Config-layer trust:** an inherited org/team layer (or a `sources` entry it declares) cannot
  inject a malicious upstream, credential, or protected-field override that the proxy would honor.
- **Least-privilege:** the daemon runs with the minimum FS/network scope; no ambient broad filesystem
  write outside the managed set.

## APPROVE when

Secrets are confined and never egress to logs/lock/outputs/argv; every route is authN+authZ gated;
inputs are bounded and validated before use; spawns are absolute/`execabs`; the socket/process is
least-privilege; there is no untrusted-config trust path.

## REJECT when (name the specific risk + location)

A secret reachable in a log line, lock, output, or argv; an unauthenticated or over-privileged path
to the store or to exec; an unbounded/unvalidated frame read; a caller-influenced PATH exec
(go:S4036) or a shell/SQL interpolation of untrusted input; a config-injected upstream/credential the
proxy honors.

Verdict line `(lens: security)`. `fail` on any BLOCKER/HIGH.
