---
status: accepted (revised 2026-11-10, twice)
---

# 0001: Persistent Subprocess Go Bridge

We chose Go to build a local micro-gateway that bridges OpenAI-compatible HTTP requests to a persistent, official Google Antigravity process.

Rather than emulating Google's network headers through reverse proxies (which trigger `403 TOS_VIOLATION` from changelist desync and ephemeral sockets), we delegate all upstream HTTPS transport to Google's official binary. Running it as a persistent child in Go gives us zero-runtime dependencies on Windows, a small RAM footprint, and keeps upstream TLS keep-alive sockets warm between conversational turns.

The decision itself — delegate egress to the official binary, run it persistently — is unchanged by both revisions below. Since ADR 0002 this binary is one backend among several.

## Revision 2026-11-10 (2): `agy acp` replaces `language_server.exe`

The Antigravity backend now drives `agy acp`, the officially documented Agent Client Protocol server of the Antigravity CLI (https://antigravity.google/docs/ide/extensions/acp), instead of the internal ConnectRPC API of `language_server.exe`.

- **Transport:** ACP v1 — newline-delimited JSON-RPC 2.0 over the child's `stdin`/`stdout`; `stderr` is logging only. subpipe spawns one persistent `agy acp` child and owns its lifetime (closing `stdin`, then killing after a grace period). There is no attach mode, discovery file, port or CSRF token.
- **Flow per completion:** `initialize` (protocol version 1, no `fs`/`terminal` client capabilities) once per child; `session/new {cwd, mcpServers: []}`; `session/set_config_option` for the model; `session/prompt`; text from `agent_message_chunk` updates is concatenated until the prompt resolves with a `stopReason`. Permission requests are rejected. A cancelled request sends `session/cancel`.
- **Auth:** `agy acp` reuses the credentials cached by an interactive `agy` login (or `agy acp --login`). subpipe never handles credentials; an `Authentication required` (-32000) error is reported, not worked around.

Why:

- ACP is a published, versioned contract; the ConnectRPC API (`StartCascade`, `SendUserCascadeMessage`, `StreamAgentStateUpdates`) is internal, unprobed for responses, and can change with any release.
- It is the path the community bridges use (pi-antigravity-bridge), see `docs/references/vendor-official-clients.md` sections 1.1 and 7.
- It removes the IDE-coupled pieces we could not verify: spawning `language_server.exe` without the IDE host bridge, hub-file discovery, and CSRF handling.

Accepted risk: the Antigravity FAQ says wrapping `agy` in third-party applications is not supported on consumer plans. The user accepted this risk explicitly (feature log `odd/tasks/backend-modules.md`, L10), the same way the pi bridges do.

Consequence: the `language_server.exe` supervisor (attach via `hub_*.json`, spawn, CSRF ping) is removed; it remains in git history (`df6fa0f`).

## Revision 2026-11-10 (1): transport, superseded

The original decision named standard OS pipes (`stdin`/`stdout`) as the transport. Probing showed `language_server.exe` is driven through its local RPC server instead: ConnectRPC with JSON bodies on `http://127.0.0.1:<httpPort>`, authenticated by the `x-codeium-csrf-token` header, with port and token from `%USERPROFILE%\.gemini\antigravity\daemon\hub_<port>.json` (attach) or chosen by subpipe (spawn). Revision (2) replaces this with `agy acp`, which brings the transport back to `stdin`/`stdout`.
