---
status: accepted
---

# 0002: Vendor Backend Modules

Each vendor is a **Backend**: a self-contained module under `internal/backend/<name>` that drives that vendor's official local client (Antigravity `language_server.exe` today; Claude Code CLI and Codex CLI are expected later). The gateway core — HTTP ingress, OpenAI wire format, SSE egress, routing — is vendor-neutral and only sees the `backend.Backend` interface.

## Why

The Antigravity work showed that everything vendor-specific — discovery, spawn flags, local auth, the RPC protocol, the model catalogue — changes independently of the OpenAI-facing gateway. Claude and GPT have the same shape: an official client with its own protocol and lifecycle that must remain the only process talking to the vendor's network. Hard-wiring Cascade calls into the HTTP handlers would force a rewrite per vendor.

## Decision

- Contract: a backend reports its `Name`, lists its `Models`, and answers `Complete` (unary). Streaming (ticket 03) and health (ticket 04) are added to the contract when first needed.
- Requests and responses cross the boundary as vendor-neutral types (`backend.Request`, `backend.Response`); each backend translates to its own protocol internally.
- Routing: model ids are `<backend>/<model>` (e.g. `antigravity/gemini-3.8-pro`). The `Registry` resolves the prefix to a backend and passes the bare model id on. Unknown prefixes are a client error.
- Session affinity stays inside each backend: a backend never spreads one conversation across accounts or processes.
- Only backends that exist are built. No placeholder Claude/GPT packages.

## Consequences

- Adding a vendor is a new package plus one registration line in `main`; no change to ingress or SSE code.
- Clients must use prefixed model ids. This is a deliberate trade for one subpipe instance serving several vendors without ambiguity.
