# Feature: models-and-resilience

Objective: implement Ticket 04 (Models Catalog and Supervisor Resilience), including `GET /v1/models`, `GET /healthz`, `Backend.Health` contract, dynamic ACP model catalogue discovery, watchdog automatic child respawn, and config profile isolation flags.
Branch: `feat/models-and-resilience` (base `59c2875`). Delivery: ask-on-risk. Test runner: `go test ./...`.

## Specs

- **S1** — `GET /v1/models`: `internal/server` exposes `GET /v1/models` returning `{ "object": "list", "data": [...] }` with prefixed model IDs from `backend.Registry.Models(ctx)`. Each model entry includes `id`, `object: "model"`, `created`, and `owned_by`.
- **S2** — Vendor-neutral health contract: `backend.Backend` gains `Health(ctx context.Context) (Health, error)`. `Health` struct reports `Alive bool`, `Initialized bool`, `Authenticated bool`, and optional `Details string`. `backend.Registry` aggregates health across backends.
- **S3** — `GET /healthz`: `internal/server` exposes `GET /healthz` returning `{ "status": "ok"|"degraded", "backends": map[string]Health }`. Responds HTTP 200 when healthy, HTTP 503 when degraded or disconnected.
- **S4** — Dynamic model extraction: `internal/backend/antigravity` dynamically discovers available models from the `model` config option in `session/new` responses, caching them. `Models(ctx)` returns discovered models, falling back to configured default models if not yet probed.
- **S5** — Watchdog & auto-respawn: `internal/backend/antigravity` monitors child process lifecycle (`c.done`). If the child exits unexpectedly while the backend is active, a background watchdog respawns and re-initializes the child process so it stays warm and ready for the next request.
- **S6** — Profile isolation: `cmd/subpipe` adds `-config-dir` (and supports `GEMINI_DIR` / `AGY_CONFIG_DIR` environment variables). `antigravity.Config` gains `ConfigDir string`, passing isolated profile environment variables to the child process.
- **S7** — Verification: unit and integration tests covering `/v1/models`, `/healthz`, dynamic model extraction, watchdog auto-respawn, and config profile isolation; `go test -race ./...` passes cleanly.

## Tasks

- [x] T1 — S1, S2: `backend.Backend` health contract (`Health` struct and method), `Registry.Health`, `GET /v1/models` in `internal/server` with unit tests. Route: inline. Commit `67a1521`.
- [x] T2 — S3: `GET /healthz` in `internal/server` with unit tests. Route: inline. Commit `67a1521`.
- [x] T3 — S4: dynamic model extraction from `session/new` config options in `internal/backend/antigravity` with tests. Route: inline. Commit `67a1521`.
- [x] T4 — S5: watchdog supervisor and auto-respawn on child death in `internal/backend/antigravity` with tests. Route: inline. Commit `67a1521`.
- [x] T5 — S6: profile isolation `-config-dir` flag in `cmd/subpipe` and `Config.ConfigDir` in `internal/backend/antigravity` with tests. Route: inline. Commit `67a1521`.
- [x] T6 — S7: verification across all packages with race detector (`go test -race ./...`) and ticket 04 update. Route: inline. Commit `67a1521`.

## Log

- **L1** (2026-11-13): User authorized Ticket 04 (Models Catalog and Supervisor Resilience) following consolidation of Ticket 03 into `main` (`59c2875`).
- **L2**: Branch `feat/models-and-resilience` created from `59c2875`. Baseline tests clean.
- **L3** Route evidence: inline execution for T1–T6 (sequential implementation in existing packages, no parallel disjoint writers). Risk forecast: item 3 (Backend interface addition) and item 4 (concurrency in watchdog goroutine).
- **L4** (2026-11-13): Implementation with observed RED -> GREEN:
  - T1: RED (`undefined: Health`, `reg.Health undefined`, 404 on `/v1/models`) -> GREEN in `internal/backend` and `internal/server`.
  - T2: RED (404 on `/healthz`) -> GREEN in `internal/server` (`TestHealthzEndpointHealthy`, `TestHealthzEndpointDegradedReturns503`).
  - T3: RED (`b.Health undefined`, `discovered models`) -> GREEN in `internal/backend/antigravity` (`TestModelsDiscoveredDynamicallyFromSessionNew`).
  - T4: RED (`b.cfg.Watchdog undefined`) -> GREEN in `internal/backend/antigravity` (`TestWatchdogRespawnsDeadSubprocess`).
  - T5: RED (`ConfigDir` undefined, `resolveConfigDir` undefined) -> GREEN in `cmd/subpipe` and `internal/backend/antigravity` (`TestConfigDirSetsProfileEnvironment`, `TestBackendConfigConfigDir`, `TestResolveConfigDirFallsBackToEnv`).
  - T6: Verification clean: `go test -count=1 ./...` and `go test -race -count=2 ./...` pass cleanly. Ticket 04 acceptance criteria marked completed.
- **L5**: Risk: item 3 (Backend interface gained Health) and item 4 (concurrency: watchdog auto-respawn goroutine).
