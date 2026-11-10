---
status: accepted (revised 2026-11-10)
---

# 0001: Persistent Subprocess Go Bridge

We chose Go to build a local micro-gateway that bridges OpenAI-compatible HTTP requests to a persistent, official `language_server.exe` instance.

Rather than emulating Google's network headers through reverse proxies (which trigger `403 TOS_VIOLATION` from changelist desync and ephemeral sockets), we delegate all upstream HTTPS transport to Google's official binary. Running it as a persistent daemon in Go gives us zero-runtime dependencies on Windows, a small RAM footprint, and keeps upstream TLS keep-alive sockets warm between conversational turns.

## Revision 2026-11-10: transport

The original decision named standard OS pipes (`stdin`/`stdout`) as the transport. Probing the binary showed it is driven through its local RPC server instead: ConnectRPC with JSON bodies on `http://127.0.0.1:<httpPort>`, authenticated by the `x-codeium-csrf-token` header. Port and token come from `%USERPROFILE%\.gemini\antigravity\daemon\hub_<port>.json` (attach) or are chosen by subpipe and passed as flags (spawn). `stdin` is used only for lifetime: `--exit_on_stdin_close` makes the child exit when subpipe closes it.

The decision itself — delegate egress to the official binary, run it persistently — is unchanged. Since ADR 0002 this binary is one backend among several.
