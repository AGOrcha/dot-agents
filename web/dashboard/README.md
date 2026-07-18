# R2 Observability Dashboard Frontend

React and Vite frontend for the read-only workflow observability dashboard. The
HTTP and SSE backend is the Go service in `cmd/da-dashboard`; see the
[operator guide](../../docs/OBSERVABILITY_DASHBOARD.md) and
[API reference](../../docs/DASHBOARD_API.md).

## Prerequisites

The repository is a pnpm workspace (`pnpm-lock.yaml` and
`pnpm-workspace.yaml`). Install dependencies from the repository root:

```bash
pnpm install
```

The commands below use the workspace package name. Running the corresponding
`pnpm <script>` from `web/dashboard` is equivalent.

## Development loop

Start Vite and the Go dashboard server in separate terminals from the repository
root:

```bash
# Terminal 1: frontend with hot module reload
pnpm --filter r2-observability-dashboard dev -- --host 127.0.0.1
```

```bash
# Terminal 2: API, SSE, iter-log watcher, and reverse proxy to Vite
go run ./cmd/da-dashboard \
  --iter-log-dir .agents/active/iteration-log \
  --dev-asset-proxy http://127.0.0.1:5173
```

Open <http://127.0.0.1:7300>. Do not use port 5173 as the application URL: the
Vite config has no `/api` proxy. The Go server owns `/api` and proxies all other
requests to Vite when `--dev-asset-proxy` is set.

Repeat `--iter-log-dir` to view more than one root. The first root is the default
for an iteration detail request that omits `iter_log_dir`.

## Build

```bash
pnpm --filter r2-observability-dashboard build
```

Vite writes `web/dashboard/dist`. Exercise that exact build through the Go
server with:

```bash
go run ./cmd/da-dashboard \
  --iter-log-dir .agents/active/iteration-log \
  --static-dir web/dashboard/dist
```

For a Vite-only preview of the compiled frontend, run:

```bash
pnpm --filter r2-observability-dashboard preview
```

The Vite-only preview still has no Go API backend, so use the Go server command
above for an end-to-end check.

## Type checking and tests

```bash
# TypeScript
pnpm --filter r2-observability-dashboard typecheck

# Vitest once
pnpm --filter r2-observability-dashboard test

# Vitest in watch mode
pnpm --filter r2-observability-dashboard test:watch

# Vitest with V8 lcov output in web/dashboard/coverage
pnpm --filter r2-observability-dashboard test:coverage

# Install Chromium once, then run Playwright
pnpm --filter r2-observability-dashboard test:e2e:install
pnpm --filter r2-observability-dashboard test:e2e
```

Vitest uses jsdom and excludes `e2e`; Playwright owns the end-to-end specs.
These settings are in `vite.config.ts` and `playwright.config.ts`.

## Generate API types

The generated TypeScript API declarations live at `src/api/types.gen.ts`. After
changing any dashboard JSON Schema, regenerate them with:

```bash
pnpm --filter r2-observability-dashboard types
```

`scripts/gen-types.mjs` runs `json-schema-to-typescript` over:

- `schemas/dashboard-run.schema.json`
- `schemas/dashboard-iteration.schema.json`
- `schemas/dashboard-rubric.schema.json`
- `schemas/dashboard-event.schema.json`

The script resolves schemas from the repository root and overwrites
`src/api/types.gen.ts`; do not edit that generated file by hand. Run `typecheck`
and the frontend tests after regeneration.
