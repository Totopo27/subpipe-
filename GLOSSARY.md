# subpipe

Local micro-gateway translating OpenAI-compatible HTTP chat completion requests into calls to the official, locally installed client of each AI vendor, so upstream network traffic always comes from the vendor's own binary and never from a synthetic proxy.

## Language

**Micro-gateway**:
A lightweight local HTTP server exposing standard `/v1/chat/completions` and `/v1/models`, routing each request to a Backend.
_Avoid_: Reverse proxy, forward proxy, API interceptor

**Backend**:
A self-contained module (`internal/backend/<name>`) that drives one vendor's official local client and translates vendor-neutral requests to that client's protocol. Selected by the model id prefix `<backend>/<model>`.
_Avoid_: Provider adapter, plugin, driver

**Official Subprocess**:
The genuine, locally installed vendor executable (for the `antigravity` Backend, Google's `language_server.exe`), either attached to or spawned and supervised by subpipe.
_Avoid_: Fake client, emulated binary, mock engine

**Local RPC**:
The loopback channel a Backend uses to talk to its Official Subprocess. For `antigravity`: ConnectRPC with JSON bodies on `http://127.0.0.1:<httpPort>`, authenticated by `x-codeium-csrf-token`.
_Avoid_: OS pipe (not the transport), upstream API

**Session Affinity**:
A strict one-to-one binding between a workspace task and a persistent subprocess profile, preventing multi-account round-robin leaks.
_Avoid_: Load balancing, account rotation, session hopping

**Header Leak**:
Synthetic or outdated HTTP headers (such as missing Piper changelists or explicit agent names) sent to upstream servers.
_Avoid_: Bug, connection error

**Tracer-Bullet Slice**:
A thin, end-to-end vertical implementation slice traversing HTTP ingress, routing, Backend translation, Local RPC, and response egress.
_Avoid_: Horizontal layer, frontend-only task, backend-only stub
