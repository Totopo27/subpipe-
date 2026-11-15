# Future client-tool integration

Subpipe currently supports text chat, not client-tool execution. Requests containing
`tools` or `tool_choice` are rejected explicitly rather than silently losing their
intent. This document preserves design references for a future integration; it
does not approve an architecture or promise tool support.

## Current boundary

- HTTP ingress and the vendor-neutral Backend contract transport text messages.
  Backend responses do not represent tool calls or tool results.
- The Antigravity Backend registers sessions with an empty `mcpServers` array.
  Its ACP event handling forwards assistant text, not a client-visible tool-call
  lifecycle, and denies incoming permission requests.
- Agent-native tools and tools supplied by an HTTP client are different surfaces.
  Denying permission requests is not proof that every agent operation is blocked.

## Reference worth retaining

The compact catalogue in
[pi-antigravity-bridge at a pinned revision](https://github.com/EstebanForge/pi-extensions/blob/fc95fd3358a15143570829c1c38ebaa88683953f/packages/pi-antigravity-bridge/src/bridge-catalog.ts)
exposes client tools to an agent through MCP. Its
[catalogue tests](https://github.com/EstebanForge/pi-extensions/blob/fc95fd3358a15143570829c1c38ebaa88683953f/packages/pi-antigravity-bridge/tests/bridge-catalog.test.ts)
and [MCP registration](https://github.com/EstebanForge/pi-extensions/blob/fc95fd3358a15143570829c1c38ebaa88683953f/packages/pi-antigravity-bridge/src/mcp-registration.ts)
are references, not code to copy into the current text-only path.

| Principle | Future acceptance evidence |
| --- | --- |
| Compact discovery | Start with a small catalogue and fetch full schemas on demand; measure context savings with a realistically large tool set. |
| Argument fidelity | Round-trip nested objects, arrays, booleans, numbers, nulls, Unicode and escaped strings through the actual transport; compare values, not just successful parsing. |
| Live authorization | A tool disabled after discovery must not execute through a stale catalogue entry. Recheck authorization at invocation time. |
| Bounded discovery | Bound schema requests and test oversized or non-serializable declarations; upstream's batch limit is a reference, not a selected Subpipe limit. |

Upstream catalogue tests alone do not establish end-to-end ACP argument fidelity
or prove compatibility with Subpipe's official subprocess.

## Decisions and prerequisites deferred

Start with a real client and its required execution flow. MCP-hosted tools and
OpenAI client-executed tools are not interchangeable contracts:

- **MCP-hosted execution:** register a real MCP server with the official
  subprocess and define who executes tools, how access is authenticated, and how
  its lifecycle is isolated.
- **OpenAI client-executed execution:** return `tool_calls`, accept subsequent
  `tool` results with call identifiers, and resume the conversation without losing
  its association with those calls.

Either approach needs explicit vendor-neutral request/response types (following
[ADR 0002](adr/0002-vendor-backend-modules.md)), validation, applicable ACP event
handling, permission policy, and tests before a compact catalogue is useful.
Do not weaken the current permission denial as an incidental part of discovery.

The exact applicable ACP v1 `mcpServers` registration schema and the installed
`agy` binary's advertised MCP transports have not been verified in this work.
Verify both before choosing an MCP design; do not infer support from newer ACP
documentation or a draft extension. No live vendor probe was performed.

## Not part of this change

No MCP server, proxy-tool API, tool-call streaming, argument transformation,
catalogue cache or new permission policy is introduced. The current change only
makes unsupported `tools` and `tool_choice` explicit and documents the text-chat
boundary. Broader unsupported-field validation remains separate work.

Neither compact discovery nor explicit request rejection is a demonstrated fix
for HTTP 429 responses, account suspensions or vendor usage-policy restrictions.
