# Feature: streaming-sse

Objective: add full streaming SSE support (`stream: true`) to `POST /v1/chat/completions`, consuming ACP `session/update` chunks and relaying them as `chat.completion.chunk` events ending with `data: [DONE]`.
Branch: `feat/streaming-sse` (base `1897025`). Delivery: ask-on-risk. Test runner: `go test ./...`.

## Specs

- **S1** — User authorization: resume subpipe and implement Ticket 03 (Streaming SSE Engine) on `feat/streaming-sse`.
- **S2** — Vendor-neutral contract: `backend.Backend` gains `Stream(ctx context.Context, req Request, onChunk func(StreamChunk) error) error`, where `StreamChunk` has `Content string` and `FinishReason string`. `Registry.Resolve` routes stream requests to the resolved backend.
- **S3** — Antigravity ACP streaming: `internal/backend/antigravity` reads `session/update` (`agent_message_chunk`) notifications from `agy acp`, streams text chunks progressively to `onChunk` as they arrive without waiting for `stopReason`, and delivers the final `FinishReason` mapped from `stopReason`.
- **S4** — HTTP SSE wire format: `internal/server` handles `stream: true` on `POST /v1/chat/completions`. Emits headers `Content-Type: text/event-stream`, `Cache-Control: no-cache`, progressive `data: {"id":"chatcmpl-...","object":"chat.completion.chunk",...}\n\n` events with flushes, and terminates with `data: [DONE]\n\n`.
- **S5** — Cancellation and resilience: if the client disconnects or context is cancelled, subpipe immediately stops writing and issues ACP `session/cancel` without hanging the gateway or leaking goroutines.
- **S6** — Verification: all existing tests pass; new tests cover `Stream` on `backend`, fake ACP agent chunk streaming, client cancellation, and HTTP SSE streaming responses with `data: [DONE]`.

## Tasks

- [x] T1 — S2: `backend.Backend` streaming contract (`StreamChunk`, `Stream` method) + `Registry` support and unit tests. Route: inline. Commit `81b5e55`.
- [x] T2 — S3, S5: `internal/backend/antigravity` streaming implementation over ACP with cancellation; fake agent streaming tests. Route: inline. Commit `81b5e55`.
- [x] T3 — S4, S5: `internal/server` `stream: true` SSE handler, chunk encoding and `data: [DONE]` terminator with tests. Route: inline. Commit `81b5e55`.
- [x] T4 — S6: End-to-end integration check, race detector sweep (`go test -race ./...`), and ticket 03 update. Route: inline. Commit `81b5e55`.

## Log

- **L1** (2026-11-13): User authorized Ticket 03 (Streaming SSE Engine) after consolidating `feat/backend-modules` into `main` at `1897025`.
- **L2**: Branch `feat/streaming-sse` created from `1897025`. All tests clean at baseline.
- **L3** Route evidence: inline execution for T1–T4 (sequential dependency chain, single developer workflow, no disjoint parallel work surfaces). Risk forecast: item 3 (Backend interface addition) and item 4 (concurrency in ACP reader goroutine and channel streaming).
- **L4** (2026-11-13): Implementation with observed RED -> GREEN:
  - T1: RED (`undefined: StreamChunk` in `backend_test.go`) -> GREEN in `internal/backend`.
  - T2: RED (`b.Stream undefined` in `antigravity_test.go`) -> GREEN in `internal/backend/antigravity` (`TestStreamYieldsChunksAndTerminalFinishReason`, `TestStreamCancelSendsSessionCancel`, `TestStreamClientErrorStopsAndCancels`). Decoupled ACP reader loop from stream consumers with buffered channel and `done` signal to prevent reader deadlock.
  - T3: RED (`stream: true is not supported yet` 400 in `server_test.go`) -> GREEN in `internal/server` (`TestChatCompletionStreamSuccess`, `TestChatCompletionStreamBackendErrorReturns502`). Emits standard OpenAI SSE chunks with `text/event-stream`, flushes, and closes with `data: [DONE]`. Returns write errors from SSE flusher so aborted client connections trigger immediate `session/cancel`.
  - T4: Verification clean across whole repo: `go test -count=1 ./...` and `go test -race -count=2 ./...` pass. Ticket 03 updated.
- **L5**: Risk: item 4 (concurrency in ACP streaming channel, prompt goroutine, and cancellation signals).
- **L6** (2026-11-13): Native review of slice `1897025..8bee272` (lineage `review-2910e2cfb91d279c`, lenses: resilience, readability, reliability). Outcome: **approved** with zero blockers and 5 informational advisories (R4-STREAM-AUTH-502, R3-stream-auth-error-mapping, R3-stream-initial-write-error-ignored, R2-NAMING-ENSURE-HEADERS, R2-DUPLICATED-SSE-CHUNK-ENVELOPE). Authority acknowledged and burned. Reviewed boundary is now `8bee272`.
