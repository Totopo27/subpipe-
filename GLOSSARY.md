# subpipe

Local micro-gateway translating OpenAI-compatible HTTP chat completion requests into the official Google Antigravity language server process via operating system standard pipes, eliminating WAF header discrepancy bans.

## Language

**Micro-gateway**:
A lightweight local HTTP server exposing standard `/v1/chat/completions` and translating requests to standard I/O pipes.
_Avoid_: Reverse proxy, forward proxy, API interceptor

**Official Subprocess**:
The genuine, locally installed Google `language_server.exe` executable launched by the operating system.
_Avoid_: Fake client, emulated binary, mock engine

**OS Pipe**:
Operating system standard input (`stdin`) and standard output (`stdout`) inter-process communication channels.
_Avoid_: Socket connection, internal HTTP, webhook

**Session Affinity**:
A strict one-to-one binding between a workspace task and a persistent subprocess profile, preventing multi-account round-robin leaks.
_Avoid_: Load balancing, account rotation, session hopping

**Header Leak**:
Synthetic or outdated HTTP headers (such as missing Piper changelists or explicit agent names) sent to upstream servers.
_Avoid_: Bug, connection error

**Tracer-Bullet Slice**:
A thin, end-to-end vertical implementation slice traversing HTTP ingress, payload translation, OS pipe transport, and SSE streaming egress.
_Avoid_: Horizontal layer, frontend-only task, backend-only stub
