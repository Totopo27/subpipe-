# Feature: backend-modules

Objective: make each vendor (Antigravity/Gemini today; Claude, GPT later) a self-contained backend module behind one contract, so the HTTP gateway stays vendor-neutral. Then resume Ticket 02 on top of it.
Branch: `feat/backend-modules` (base `34768b7`). Delivery: ask-on-risk. Test runner: `go test ./...`.

## Specs

- **S1** — User request (verbatim): "algo importante que quiero que trabajemos también, además de esto, es el hecho de que el apartado de antigravity/gemini sea un módulo, ya que he estado pensando que si esto sucede con este, también sucederá caso similar con claude, gpt, etc."
- **S2** — Scope decision (user answer): "Clientes oficiales propios" — each vendor is a backend that drives its own official local client (Antigravity `language_server.exe`, Claude Code CLI, Codex CLI); network egress is always done by the official binary. The contract must tolerate different protocols and lifecycles.
- **S3** — "si es necesario regresar a modificar cosas que ya habíamos hecho. Pues lo haremos": fix already-written artifacts that contradict reality — GLOSSARY, ADR 0001, README, spec describe the transport as stdin/stdout pipes, but the implemented and probed transport is ConnectRPC over local HTTP (`127.0.0.1:<httpPort>`, `x-codeium-csrf-token`).
- **S4** — Antigravity-specific code (discovery, spawn, CSRF, Cascade RPC) lives only under `internal/backend/antigravity`. Vendor-neutral code (contract, routing, HTTP ingress, OpenAI wire format, SSE) never imports a vendor package except at wiring time.
- **S5** — Assumption (proposed, not yet confirmed by the user): model routing by prefix, `<backend>/<model>` (e.g. `antigravity/gemini-3.8-pro`); `/v1/models` lists prefixed ids. Only the Antigravity backend is built now (no Claude/GPT stubs).
- **S6** — Resume Ticket 02 (`.scratch/subpipe/issues/02-tracer-bullet-completion.md`) on top of the backend contract; tickets 02–04 are re-scoped into neutral vs backend parts.

## Tasks

- [x] T1 — S3: correct transport description in GLOSSARY, ADR 0001, README, spec; add ADR 0002 (backend modules) and GLOSSARY term **Backend**. Route: inline. Commit `df6fa0f`.
- [x] T2 — S4: move `internal/supervisor` → `internal/backend/antigravity`; tests green. Route: inline. Commit `df6fa0f`.
- [x] T3 — S2, S4, S5: `internal/backend` contract (`Backend` interface) + `Registry` prefix routing with tests (RED: undefined Model/Request → GREEN). Route: inline. Commit `df6fa0f`.
- [x] T4 — S6: re-scope tickets 02–04 and spec module list to the backend layout. Route: inline. Commit `df6fa0f`.
- [ ] T5 — S6: Ticket 02 implementation. Route: inline (no writer reason: single dependent unit).

## Log

- **L1** (2026-11-10, verbatim): "Habíamos quedado aquí: - Creé el archivo documental en el repositorio: docs/references/google-gemini-repos.md detallando estas comparativas y puntos de contacto. - Guardé el análisis en la memoria permanente de Engram (observación #3158) y actualicé el session_summary (#3159) para que quede todo enlazado al retomar en el Ticket 02.  por lo que me gustaría retomar y si es necesario regresar a modificar cosas que ya habíamos hecho. Pues lo haremos, algo importante que quiero que trabajemos también, además de esto, es el hecho de que el apartado de antigravity/gemini sea un módulo, ya que he estado pensando que si esto sucede con este, también sucederá caso similar con claude, gpt, etc. Se entiende la idea?"
- **L2** (2026-11-10): user chose "Clientes oficiales propios (Recommended)" for the meaning of Claude/GPT.
- **L3**: baseline commit `34768b7` (repo had no commits); branch `feat/backend-modules` created. `go test ./...` green at baseline.
- **L4** Route evidence: all tasks inline — no parallel independent units, no context backstop. Risk forecast: refactor of internal package only (no external consumers yet) → none of items 1–5; item 6 mitigated by existing supervisor tests.
- **L5**: T1–T4 done in `df6fa0f`. `go vet` clean, `go test ./...` green (backend, backend/antigravity). RDD assess (base `34768b7`, committed-only): risk `medium`, `review_due: false` (`under_budget`) — no review, slice stays open. `Health` dropped from the contract until ticket 04 (YAGNI); ADR 0002 updated.
- **L6**: Next: T5 (Ticket 02). Pending user confirmation of S5 (prefixed model ids) before building ingress on it. Unary response retrieval in Cascade (poll trajectory vs. drain `StreamAgentStateUpdates`) is still unprobed and needs a live call through the user's account.
