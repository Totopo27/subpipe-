---
status: accepted
---

# 0001: Persistent Subprocess Go Bridge

We chose Go to build a local micro-gateway that bridges OpenAI-compatible HTTP requests directly to a persistent `language_server.exe` instance via standard OS pipes (`stdin`/`stdout`). 

Rather than emulating Google's network headers through reverse proxies (which trigger `403 TOS_VIOLATION` from changelist desync and ephemeral sockets), we delegate all upstream HTTPS transport to Google's official binary. Running it as a persistent daemon in Go gives us zero-runtime dependencies on Windows, sub-15MB RAM footprint, and keeps upstream TLS keep-alive sockets warm without tearing down connections between conversational turns.
